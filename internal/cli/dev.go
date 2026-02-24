// Package main implements the mono monorepo orchestration tool.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type devCommand struct{}

func init() {
	registerCommand("dev", &devCommand{})
}

// Run starts all services with dev commands concurrently
func (c *devCommand) Run(args []string) error {
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
			if _, hasDevCommand := svc.Commands["dev"]; hasDevCommand {
				servicesToRun = append(servicesToRun, svc)
			}
		}
	}

	if len(servicesToRun) == 0 {
		fmt.Println("No services with 'dev' command found")
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
		fmt.Println("\n⊙ Shutting down all services...")
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
		fmt.Println("\nErrors occurred:")
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
			fmt.Printf("==> [infra] %s already running\n", depName)
			continue
		}

		fmt.Printf("==> [infra] Starting dependency: %s\n", depName)
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
	cmdString, exists := svc.Commands["dev"]
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
	stdoutWriter := &PrefixWriter{
		prefix: fmt.Sprintf("[%s]", svc.Name),
		writer: os.Stdout,
	}

	// Create prefix writer for stderr
	stderrWriter := &PrefixWriter{
		prefix: fmt.Sprintf("[%s]", svc.Name),
		writer: os.Stderr,
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	// Start the process
	fmt.Printf("[%s] Starting...\n", svc.Name)
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
		fmt.Printf("[%s] Stopping...\n", svc.Name)

		// Send interrupt signal
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			// If interrupt fails, kill the process
			_ = cmd.Process.Kill()
		}

		// Wait for process to exit with timeout
		select {
		case <-done:
			fmt.Printf("[%s] Stopped gracefully\n", svc.Name)
		case <-time.After(5 * time.Second):
			// Force kill after timeout
			_ = cmd.Process.Kill()
			fmt.Printf("[%s] Stopped (forced)\n", svc.Name)
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
		fmt.Printf("[%s] Exited\n", svc.Name)
		return nil
	}
}

// PrefixWriter wraps an io.Writer and prefixes each line with a service name
type PrefixWriter struct {
	prefix string
	writer io.Writer
	mu     sync.Mutex
	buffer []byte
}

// Write implements io.Writer
func (pw *PrefixWriter) Write(p []byte) (n int, err error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	// Append to buffer
	pw.buffer = append(pw.buffer, p...)

	// Process complete lines
	for {
		idx := -1
		for i, b := range pw.buffer {
			if b == '\n' {
				idx = i
				break
			}
		}

		if idx == -1 {
			// No complete line yet
			break
		}

		// Extract line (including newline)
		line := pw.buffer[:idx+1]
		pw.buffer = pw.buffer[idx+1:]

		// Write prefixed line
		prefixedLine := fmt.Sprintf("%s %s", pw.prefix, string(line))
		if _, err := pw.writer.Write([]byte(prefixedLine)); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

// Flush writes any remaining buffered data
func (pw *PrefixWriter) Flush() error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if len(pw.buffer) > 0 {
		prefixedLine := fmt.Sprintf("%s %s\n", pw.prefix, string(pw.buffer))
		if _, err := pw.writer.Write([]byte(prefixedLine)); err != nil {
			return err
		}
		pw.buffer = nil
	}

	return nil
}
