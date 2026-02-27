// Package main implements the mono monorepo orchestration tool.
package runtimecmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type devCommand struct{}

func init() {
	registerCommand("dev", &devCommand{})
}

// Run starts all services with dev commands concurrently
func (c *devCommand) Run(args []string) error {
	p := output.DefaultPrinter()

	// Load config
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine which services to run
	var servicesToRun []Service
	if len(args) > 2 {
		// Specific services requested
		requestedServices := args[2:]
		for _, name := range requestedServices {
			svc := findService(config, name)
			if svc == nil {
				return fmt.Errorf("unknown service '%s'", name)
			}
			servicesToRun = append(servicesToRun, *svc)
		}
	} else {
		// Run all services that have dev commands
		for _, svc := range config.Services {
			if hasDevCommand(svc) {
				servicesToRun = append(servicesToRun, svc)
			}
		}
	}

	if len(servicesToRun) == 0 {
		p.Summary("No services with 'dev' command found")
		return nil
	}

	// Auto-start any infrastructure dependencies that aren't already running
	if err := ensureInfraDeps(config, servicesToRun); err != nil {
		return fmt.Errorf("failed to start infrastructure dependencies: %w", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// WaitGroup to track all service goroutines
	var wg sync.WaitGroup

	// Error channel to collect errors
	errChan := make(chan error, len(servicesToRun))

	// Start each service in its own goroutine
	for _, svc := range servicesToRun {
		wg.Add(1)
		go func(service Service) {
			defer wg.Done()
			if err := runServiceDev(ctx, service); err != nil {
				errChan <- fmt.Errorf("[%s] %w", service.Name, err)
			}
		}(svc)
	}

	// Wait for shutdown signal in separate goroutine
	go func() {
		<-sigChan
		p.Blank()
		p.StepWarn("dev", "Shutting down all services...")
		cancel()
	}()

	// Wait for all services to complete
	wg.Wait()

	// Check for errors
	close(errChan)
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		p.Blank()
		p.StepErr("dev", "Errors occurred:")
		for _, err := range errors {
			fmt.Fprintf(os.Stderr, "  %v\n", err)
		}
		return fmt.Errorf("%d service(s) failed", len(errors))
	}

	return nil
}

// ensureInfraDeps checks all depends entries for the given services and
// auto-starts any local infrastructure resources that are not already running.
// It does NOT tear them down on exit — infrastructure persists between dev sessions.
func ensureInfraDeps(config *Config, services []Service) error {
	p := output.DefaultPrinter()

	if config.Local == nil {
		return nil
	}

	// Collect unique dependency names across all services being run
	seen := make(map[string]bool)
	var deps []string
	for _, svc := range services {
		for _, dep := range svc.Depends {
			if !seen[dep] {
				seen[dep] = true
				deps = append(deps, dep)
			}
		}
	}

	if len(deps) == 0 {
		return nil
	}

	state := loadState()

	// Check each dependency; start it if not already running
	for _, depName := range deps {
		var resource *InfraResource
		for i := range config.Local.Resources {
			if config.Local.Resources[i].Name == depName {
				resource = &config.Local.Resources[i]
				break
			}
		}
		if resource == nil {
			return fmt.Errorf("service depends on unknown infrastructure resource %q", depName)
		}

		if isInfraResourceRunning(config.Local, *resource, state) {
			p.StepWarn("infra", depName+" already running")
			continue
		}

		p.StepStart("infra", "Starting dependency: "+depName)
		// Reuse infraUp by passing the resource name; build synthetic args slice
		syntheticArgs := []string{"mono", "infra", "up", depName}
		if err := infraUp(syntheticArgs); err != nil {
			return fmt.Errorf("failed to start %s: %w", depName, err)
		}
	}

	return nil
}

// runServiceDev runs a single service's dev command
func runServiceDev(ctx context.Context, svc Service) error {
	p := output.DefaultPrinter()
	cmdString, exists := svc.Commands["dev"]
	if !exists {
		cmdString = strings.TrimSpace(svc.Dev)
		exists = cmdString != ""
	}
	if !exists {
		return fmt.Errorf("no 'dev' command defined for service %s", svc.Name)
	}

	// Resolve absolute path to service directory
	absPath, err := filepath.Abs(svc.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Parse the command string and build a command without a shell interpreter.
	// commandFromParts uses an explicit allowlist so only known binaries execute.
	parts := strings.Fields(cmdString)
	cmd, err := commandFromParts(ctx, parts)
	if err != nil {
		return fmt.Errorf("[%s] %w", svc.Name, err)
	}
	cmd.Dir = absPath

	// Create prefix writer for stdout
	stdoutWriter := output.NewPrefixWriter(fmt.Sprintf("[%s]", svc.Name), os.Stdout)

	// Create prefix writer for stderr
	stderrWriter := output.NewPrefixWriter(fmt.Sprintf("[%s]", svc.Name), os.Stderr)

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	// Start the process
	p.StepStart(svc.Name, "Starting...")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	// Wait for process to complete or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled, attempt graceful shutdown
		p.StepWarn(svc.Name, "Stopping...")

		// Send interrupt signal
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			// If interrupt fails, kill the process
			_ = cmd.Process.Kill()
		}

		// Wait for process to exit with timeout
		select {
		case <-done:
			p.StepOK(svc.Name, "Stopped gracefully")
		case <-time.After(5 * time.Second):
			// Force kill after timeout
			_ = cmd.Process.Kill()
			p.StepWarn(svc.Name, "Stopped (forced)")
		}
		return nil

	case err := <-done:
		// Process exited on its own
		if err != nil {
			// Check if it was killed by signal
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("exited with error: %w", err)
		}
		p.StepOK(svc.Name, "Exited")
		return nil
	}
}

func hasDevCommand(svc Service) bool {
	if _, ok := svc.Commands["dev"]; ok {
		return true
	}
	return strings.TrimSpace(svc.Dev) != ""
}
