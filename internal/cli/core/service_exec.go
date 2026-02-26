package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunServiceCommand runs a registry-defined command (test, lint, build, etc.)
// across one or more services.
func RunServiceCommand(command string, args []string) error {
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	return RunServiceCommandWithConfig(config, command, args)
}

func RunServiceCommandWithConfig(config *Config, command string, args []string) error {
	var servicesToRun []Service
	if len(args) > 2 {
		requestedServices := args[2:]
		for _, name := range requestedServices {
			svc := FindService(config, name)
			if svc == nil {
				return fmt.Errorf("unknown service %q", name)
			}
			servicesToRun = append(servicesToRun, *svc)
		}
	} else {
		servicesToRun = config.Services
	}

	for _, svc := range servicesToRun {
		if err := RunCommand(svc, command); err != nil {
			return err
		}
	}

	return nil
}

// RunCommand executes a command for a specific service.
func RunCommand(svc Service, command string) error {
	cmdString, exists := svc.Commands[command]
	if !exists && command == "dev" && strings.TrimSpace(svc.Dev) != "" {
		cmdString = svc.Dev
		exists = true
	}
	if !exists {
		fmt.Printf("⊘ [%s] skipping (no '%s' command defined)\n", svc.Name, command)
		return nil
	}

	fmt.Printf("==> [%s] %s\n", svc.Name, command)

	absPath, err := filepath.Abs(svc.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path for %s: %v\n", svc.Name, err)
		return err
	}

	parts := strings.Fields(cmdString)
	cmd, err := CommandFromParts(context.Background(), parts)
	if err != nil {
		return fmt.Errorf("[%s] %w", svc.Name, err)
	}

	cmd.Dir = absPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n✗ [%s] %s failed\n", svc.Name, command)
		return err
	}

	fmt.Printf("✓ [%s] %s completed\n\n", svc.Name, command)
	return nil
}

// CommandFromParts builds an exec.Cmd from a parsed command slice without using a shell.
func CommandFromParts(ctx context.Context, parts []string) (*exec.Cmd, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	args := parts[1:]
	switch parts[0] {
	case "go":
		return exec.CommandContext(ctx, "go", args...), nil //nolint:gosec
	case "pnpm":
		return exec.CommandContext(ctx, "pnpm", args...), nil //nolint:gosec
	case "npx":
		return exec.CommandContext(ctx, "npx", args...), nil //nolint:gosec
	case "node":
		return exec.CommandContext(ctx, "node", args...), nil //nolint:gosec
	case "npm":
		return exec.CommandContext(ctx, "npm", args...), nil //nolint:gosec
	default:
		return nil, fmt.Errorf("binary %q is not in the command allowlist; add it to commandFromParts if needed", parts[0])
	}
}
