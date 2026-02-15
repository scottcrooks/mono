package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runDoctor performs environment health checks and fixes
func runDoctor() error {
	fmt.Println("🔍 Checking development environment...")
	fmt.Println()

	hasErrors := false

	// Check Go
	fmt.Print("Go (1.25.7+): ")
	if err := checkCommand("go", "version"); err != nil {
		fmt.Println("  ✗ Go is NOT installed")
		fmt.Println("  → Install from: https://go.dev/dl/")
		hasErrors = true
	} else {
		cmd := exec.Command("go", "version")
		if output, err := cmd.Output(); err == nil {
			fmt.Println(strings.TrimSpace(string(output)))
			fmt.Println("  ✓ Go is installed")
		}
	}
	fmt.Println()

	// Check Node.js
	fmt.Print("Node.js (22.x+): ")
	if err := checkCommand("node", "--version"); err != nil {
		fmt.Println("  ✗ Node.js is NOT installed")
		fmt.Println("  → Install from: https://nodejs.org/ or use nvm")
		hasErrors = true
	} else {
		cmd := exec.Command("node", "--version")
		if output, err := cmd.Output(); err == nil {
			fmt.Println(strings.TrimSpace(string(output)))
			fmt.Println("  ✓ Node.js is installed")
		}
	}
	fmt.Println()

	// Check pnpm (auto-install if missing)
	fmt.Print("pnpm (10.x+): ")
	if err := checkCommand("pnpm", "--version"); err != nil {
		fmt.Println("  ✗ pnpm is NOT installed")
		fmt.Println("  → Installing pnpm...")
		installCmd := exec.Command("npm", "install", "-g", "pnpm")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			fmt.Println("  ✗ Failed to install pnpm")
			hasErrors = true
		} else {
			fmt.Println("  ✓ pnpm installed successfully")
		}
	} else {
		cmd := exec.Command("pnpm", "--version")
		if output, err := cmd.Output(); err == nil {
			fmt.Println(strings.TrimSpace(string(output)))
			fmt.Println("  ✓ pnpm is installed")
		}
	}
	fmt.Println()

	if hasErrors {
		return fmt.Errorf("missing critical dependencies")
	}

	// Install Go tools from go.mod
	fmt.Println("📦 Installing Go tools from go.mod...")
	if err := installGoTools(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Failed to install Go tools: %v\n", err)
		return err
	}
	fmt.Println("  ✓ Go tools installed")
	fmt.Println()

	// Install pnpm dependencies
	fmt.Println("📦 Installing pnpm dependencies...")
	pnpmCmd := exec.Command("pnpm", "install", "--frozen-lockfile")
	pnpmCmd.Stdout = os.Stdout
	pnpmCmd.Stderr = os.Stderr
	if err := pnpmCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Failed to install pnpm dependencies: %v\n", err)
		return err
	}
	fmt.Println("  ✓ pnpm dependencies installed")
	fmt.Println()

	// Verify mono tool
	fmt.Println("🔧 Verifying mono tool...")
	listServices()
	fmt.Println()

	fmt.Println("✅ All checks passed! Environment is ready.")
	return nil
}

// checkCommand checks if a command is available and runs successfully
func checkCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// installGoTools reads go.mod and installs all tools from the tool directive
func installGoTools() error {
	tools, err := parseGoModTools()
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	for _, tool := range tools {
		fmt.Printf("  → Installing %s...\n", tool)
		//nolint:gosec // G204: tool paths are from go.mod, trusted input
		cmd := exec.Command("go", "install", tool+"@latest")
		cmd.Stdout = nil // Suppress output unless there's an error
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Don't fail on individual tool errors, just report and continue
			fmt.Fprintf(os.Stderr, "    ⚠ Warning: failed to install %s\n", tool)
		}
	}

	return nil
}

// parseGoModTools extracts tool directives from go.mod
func parseGoModTools() ([]string, error) {
	file, err := os.Open("go.mod")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close go.mod: %v\n", err)
		}
	}()

	var tools []string
	scanner := bufio.NewScanner(file)
	inToolBlock := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Start of tool block
		if strings.HasPrefix(trimmed, "tool (") {
			inToolBlock = true
			continue
		}

		// End of tool block
		if inToolBlock && trimmed == ")" {
			break
		}

		// Inside tool block - extract tool path
		if inToolBlock && trimmed != "" {
			tools = append(tools, trimmed)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return tools, nil
}
