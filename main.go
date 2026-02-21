package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Command is the strategy interface for mono commands.
type Command interface {
	Run(args []string) error
}

var commands = map[string]Command{}

func registerCommand(name string, cmd Command) {
	commands[name] = cmd
}

// listCommand handles the "list" subcommand.
type listCommand struct{}

func init() {
	registerCommand("list", &listCommand{})
}

func (c *listCommand) Run(_ []string) error {
	listServices()
	return nil
}

// Config represents the services.yaml structure
type Config struct {
	Services []Service  `yaml:"services"`
	Local    *InfraSpec `yaml:"local"`
}

// Service represents a single service configuration
type Service struct {
	Name        string            `yaml:"name"`
	Path        string            `yaml:"path"`
	Description string            `yaml:"description"`
	Depends     []string          `yaml:"depends"`
	Commands    map[string]string `yaml:"commands"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	if cmd, ok := commands[command]; ok {
		if err := cmd.Run(os.Args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Fallback: generic service-registry command
	runServiceCommand(command)
}

// runServiceCommand runs a registry-defined command (test, lint, build, etc.)
// across one or more services.
func runServiceCommand(command string) {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Determine which services to run
	var servicesToRun []Service
	if len(os.Args) > 2 {
		// Specific services requested
		requestedServices := os.Args[2:]
		for _, name := range requestedServices {
			svc := findService(config, name)
			if svc == nil {
				fmt.Fprintf(os.Stderr, "Error: unknown service '%s'\n", name)
				os.Exit(1)
			}
			servicesToRun = append(servicesToRun, *svc)
		}
	} else {
		// Run all services
		servicesToRun = config.Services
	}

	// Execute command for each service
	for _, svc := range servicesToRun {
		if err := runCommand(svc, command); err != nil {
			// Fail-fast: exit immediately on first failure
			os.Exit(1)
		}
	}
}

// loadConfig reads and parses services.yaml from the repo root
func loadConfig() (*Config, error) {
	// Assume we're running from repo root (invoked via Makefile)
	configPath := "services.yaml"

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	return &config, nil
}

// findService searches for a service by name
func findService(config *Config, name string) *Service {
	for _, svc := range config.Services {
		if svc.Name == name {
			return &svc
		}
	}
	return nil
}

// runCommand executes a command for a specific service
func runCommand(svc Service, command string) error {
	cmdString, exists := svc.Commands[command]
	if !exists {
		fmt.Printf("⊘ [%s] skipping (no '%s' command defined)\n", svc.Name, command)
		return nil
	}

	fmt.Printf("==> [%s] %s\n", svc.Name, command)

	// Resolve absolute path to service directory
	absPath, err := filepath.Abs(svc.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path for %s: %v\n", svc.Name, err)
		return err
	}

	// Parse the command string and build a command without a shell interpreter.
	// commandFromParts uses an explicit allowlist so only known binaries execute.
	parts := strings.Fields(cmdString)
	cmd, err := commandFromParts(context.Background(), parts)
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

// commandFromParts builds an exec.Cmd from a parsed command slice without using a shell
// interpreter. Only binaries in the allowlist are accepted, which prevents arbitrary
// binary execution from config files and satisfies gosec G204.
// Add new entries here when services.yaml requires a new binary.
func commandFromParts(ctx context.Context, parts []string) (*exec.Cmd, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	args := parts[1:]
	switch parts[0] {
	case "go":
		return exec.CommandContext(ctx, "go", args...), nil //nolint:gosec // G204: binary is a string literal; args are from services.yaml (committed config), not shell-interpreted
	case "pnpm":
		return exec.CommandContext(ctx, "pnpm", args...), nil //nolint:gosec // G204: same as above
	case "npx":
		return exec.CommandContext(ctx, "npx", args...), nil //nolint:gosec // G204: same as above
	case "node":
		return exec.CommandContext(ctx, "node", args...), nil //nolint:gosec // G204: same as above
	case "npm":
		return exec.CommandContext(ctx, "npm", args...), nil //nolint:gosec // G204: same as above
	default:
		return nil, fmt.Errorf("binary %q is not in the command allowlist; add it to commandFromParts if needed", parts[0])
	}
}

// listServices prints all services and their available commands
func listServices() {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Available services:")
	fmt.Println()

	for _, svc := range config.Services {
		fmt.Printf("  %s - %s\n", svc.Name, svc.Description)
		fmt.Printf("    Path: %s\n", svc.Path)

		if len(svc.Depends) > 0 {
			fmt.Printf("    Depends: %s\n", strings.Join(svc.Depends, ", "))
		}

		fmt.Printf("    Commands: ")

		var cmds []string
		for cmdName := range svc.Commands {
			cmds = append(cmds, cmdName)
		}

		if len(cmds) > 0 {
			for i, cmd := range cmds {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(cmd)
			}
		}
		fmt.Println()
		fmt.Println()
	}

	if config.Local != nil && len(config.Local.Resources) > 0 {
		fmt.Println("Local infrastructure resources:")
		fmt.Println()
		for _, res := range config.Local.Resources {
			fmt.Printf("  %s - %s\n", res.Name, res.Description)
			if res.PortForward != nil {
				fmt.Printf("    Port-forward: localhost:%d -> %s:%d\n", res.PortForward.LocalPort, res.PortForward.Target, res.PortForward.TargetPort)
			}
			fmt.Println()
		}
	}
}

// printUsage prints usage information
func printUsage() {
	fmt.Println("mono - Monorepo orchestration tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono <command> [service...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list                  List all services and their commands")
	fmt.Println("  dev [service...]      Run services with hot reload (concurrent)")
	fmt.Println("  doctor                Check and fix development environment")
	fmt.Println("  hosts <subcommand>    Manage local hosts entries (see: mono hosts)")
	fmt.Println("  infra <subcommand>    Manage local infrastructure (see: mono infra)")
	fmt.Println("  migrate <service> <subcommand>  Manage database migrations (see: mono migrate)")
	fmt.Println("  metadata              Print spec metadata (date/git/repo/timestamp)")
	fmt.Println("  worktree <subcommand> Manage git worktrees (see: mono worktree)")
	fmt.Println("  <command>             Run command across services")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mono test             Run tests for all services")
	fmt.Println("  mono test pythia      Run tests for pythia only")
	fmt.Println("  mono lint polaris     Lint polaris and pythia")
	fmt.Println("  mono run pythia       Start the backend")
	fmt.Println("  mono dev              Start all services with hot reload")
	fmt.Println("  mono dev pythia       Start pythia with hot reload only")
	fmt.Println("  mono hosts sync       Add/update *.argus.local entries in /etc/hosts")
	fmt.Println("  mono hosts remove     Remove managed argus hosts entries from /etc/hosts")
	fmt.Println("  mono infra up         Deploy local infrastructure")
	fmt.Println("  mono infra status     Check infrastructure status")
	fmt.Println("  mono migrate pythia up       Apply all pending migrations")
	fmt.Println("  mono migrate pythia status   Show migration version")
	fmt.Println("  mono migrate pythia create add_foo  Create migration files")
	fmt.Println("  mono metadata         Print spec metadata for docs/handoffs")
	fmt.Println("  mono worktree create feature/foo   Create a branch worktree")
	fmt.Println("  mono worktree list                 List worktrees")
	fmt.Println("  mono list             Show all services")
}
