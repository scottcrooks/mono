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

	servicesToRun, err := selectServicesForDev(config, args[2:])
	if err != nil {
		return err
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

// ensureInfraDeps checks all effective dev dependency entries for the given services and
// auto-starts any local infrastructure resources that are not already running.
// It does NOT tear them down on exit — infrastructure persists between dev sessions.
func ensureInfraDeps(config *Config, services []Service) error {
	p := output.DefaultPrinter()

	if config.Local == nil {
		return nil
	}

	deps, err := collectInfraDeps(config, services)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		return nil
	}

	// Fail fast before checking resource status to avoid hanging on kubectl calls
	// when the active Kubernetes context is unreachable (e.g., Rancher not running).
	if err := checkKubectl(); err != nil {
		return err
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
			p.StepOK("infra", depName+" already running")
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

// collectInfraDeps returns unique infra resource dependencies for the given services.
// Dependencies are the merged union of depends and devDepends, and may reference
// either other services or local infra resources.
func collectInfraDeps(config *Config, services []Service) ([]string, error) {
	serviceNames := make(map[string]struct{}, len(config.Services))
	for _, svc := range config.Services {
		serviceNames[svc.Name] = struct{}{}
	}

	infraNames := make(map[string]struct{}, len(config.Local.Resources))
	for _, res := range config.Local.Resources {
		infraNames[res.Name] = struct{}{}
	}

	seenInfraDeps := make(map[string]struct{})
	var infraDeps []string
	for _, svc := range services {
		for _, dep := range effectiveDevDependencies(svc) {
			if _, ok := infraNames[dep]; ok {
				if _, exists := seenInfraDeps[dep]; !exists {
					seenInfraDeps[dep] = struct{}{}
					infraDeps = append(infraDeps, dep)
				}
				continue
			}
			if _, ok := serviceNames[dep]; ok {
				continue
			}
			return nil, fmt.Errorf("service %q depends on unknown dependency %q (must reference a service or local resource)", svc.Name, dep)
		}
	}

	return infraDeps, nil
}

// runServiceDev runs a single service's dev command
func runServiceDev(ctx context.Context, svc Service) error {
	p := output.DefaultPrinter()
	cmdString, exists := resolveDevCommand(svc)
	if !exists {
		return fmt.Errorf("no 'dev' command defined for service %s (no default for archetype %q)", svc.Name, svc.Archetype)
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

	// Create per-service colored prefix writers for easier multi-service log scanning.
	mode := p.Mode()
	stdoutWriter := output.NewServicePrefixWriter(svc.Name, os.Stdout, mode)

	// Create prefix writer for stderr
	stderrWriter := output.NewServicePrefixWriter(svc.Name, os.Stderr, mode)

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
	_, ok := resolveDevCommand(svc)
	return ok
}

func selectServicesForDev(config *Config, requested []string) ([]Service, error) {
	if len(requested) == 0 {
		var all []Service
		for _, svc := range config.Services {
			if hasDevCommand(svc) {
				all = append(all, svc)
			}
		}
		return all, nil
	}

	byName := make(map[string]Service, len(config.Services))
	for _, svc := range config.Services {
		byName[svc.Name] = svc
	}

	explicitlyRequested := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		explicitlyRequested[name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(requested))
	orderedNames := make([]string, 0, len(requested))
	queue := append([]string(nil), requested...)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if _, ok := seen[name]; ok {
			continue
		}

		svc, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown service '%s'", name)
		}

		seen[name] = struct{}{}
		orderedNames = append(orderedNames, name)

		for _, dep := range effectiveDevDependencies(svc) {
			if _, ok := byName[dep]; ok {
				queue = append(queue, dep)
			}
		}
	}

	servicesToRun := make([]Service, 0, len(orderedNames))
	for _, name := range orderedNames {
		svc := byName[name]
		if hasDevCommand(svc) {
			servicesToRun = append(servicesToRun, svc)
			continue
		}
		if _, explicit := explicitlyRequested[name]; explicit {
			return nil, fmt.Errorf("no 'dev' command defined for service %s (no default for archetype %q)", svc.Name, svc.Archetype)
		}
	}

	return servicesToRun, nil
}

func resolveDevCommand(svc Service) (string, bool) {
	if cmd, ok := svc.Commands["dev"]; ok && strings.TrimSpace(cmd) != "" {
		return cmd, true
	}
	if cmd := strings.TrimSpace(svc.Dev); cmd != "" {
		return cmd, true
	}
	return defaultDevCommandForService(svc)
}

func defaultDevCommandForService(svc Service) (string, bool) {
	// Only infer defaults for runnable services. Libraries/packages must opt in.
	if strings.TrimSpace(svc.Kind) != "" && svc.Kind != "service" {
		return "", false
	}

	switch svc.Archetype {
	case "go":
		return "go run github.com/air-verse/air@latest -c .air.toml", true
	case "react":
		return "pnpm dev", true
	default:
		return "", false
	}
}

func effectiveDevDependencies(svc Service) []string {
	seen := make(map[string]struct{}, len(svc.Depends)+len(svc.DevDepends))
	deps := make([]string, 0, len(svc.Depends)+len(svc.DevDepends))

	for _, dep := range svc.Depends {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := seen[dep]; ok {
			continue
		}
		seen[dep] = struct{}{}
		deps = append(deps, dep)
	}
	for _, dep := range svc.DevDepends {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := seen[dep]; ok {
			continue
		}
		seen[dep] = struct{}{}
		deps = append(deps, dep)
	}

	return deps
}
