package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the services.yaml structure
type Config struct {
	Services []Service `yaml:"services"`
}

// Service represents a single service configuration
type Service struct {
	Name        string            `yaml:"name"`
	Path        string            `yaml:"path"`
	Description string            `yaml:"description"`
	Commands    map[string]string `yaml:"commands"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Special case: list command
	if command == "list" {
		listServices()
		os.Exit(0)
	}

	// Special case: dev command
	if command == "dev" {
		if err := runDev(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Special case: doctor command
	if command == "doctor" {
		if err := runDoctor(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Special case: infra command
	if command == "infra" {
		if err := runInfra(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Load config
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

	// Execute command in service directory
	cmd := exec.Command("sh", "-c", cmdString)
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
	fmt.Println("  infra <subcommand>    Manage local infrastructure (see: mono infra)")
	fmt.Println("  <command>             Run command across services")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mono test             Run tests for all services")
	fmt.Println("  mono test pythia      Run tests for pythia only")
	fmt.Println("  mono lint polaris     Lint polaris and pythia")
	fmt.Println("  mono run pythia       Start the backend")
	fmt.Println("  mono dev              Start all services with hot reload")
	fmt.Println("  mono dev pythia       Start pythia with hot reload only")
	fmt.Println("  mono infra up         Deploy local infrastructure")
	fmt.Println("  mono infra status     Check infrastructure status")
	fmt.Println("  mono list             Show all services")
}
