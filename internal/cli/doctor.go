package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type doctorCommand struct{}

func init() {
	registerCommand("doctor", &doctorCommand{})
}

// Run performs environment health checks and fixes
func (c *doctorCommand) Run(_ []string) error {
	fmt.Println("🔍 Checking development environment...")
	fmt.Println()

	hasErrors := false

	// Check Go
	goInstalled := false
	fmt.Print("Go (1.25.7+): ")
	if err := checkCommand("go", "version"); err != nil {
		fmt.Println("  ✗ Go is NOT installed")
		fmt.Println("  → Install from: https://go.dev/dl/")
		hasErrors = true
	} else {
		goInstalled = true
		cmd := exec.Command("go", "version")
		if output, err := cmd.Output(); err == nil {
			fmt.Println(strings.TrimSpace(string(output)))
			fmt.Println("  ✓ Go is installed")
		}
	}
	fmt.Println()

	// Check go fix support (required by verification workflow)
	if goInstalled {
		fmt.Print("go fix support: ")
		if err := checkGoFixSupport(); err != nil {
			fmt.Println("  ✗ go fix is NOT available")
			fmt.Printf("  → %v\n", err)
			printGoToolchainDiagnostics()
			fmt.Println("  → Reinstall/upgrade Go from: https://go.dev/dl/")
			hasErrors = true
		} else {
			fmt.Println("available")
			fmt.Println("  ✓ go fix is available")
		}
		fmt.Println()
	}

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

	// Check kubectl (optional - only needed for mono infra)
	fmt.Print("kubectl (optional): ")
	if err := checkCommand("kubectl", "version", "--client"); err != nil {
		fmt.Println("not installed")
	} else {
		cmd := exec.Command("kubectl", "version", "--client")
		if output, err := cmd.Output(); err == nil {
			// Extract just the version line
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 {
				fmt.Println(strings.TrimSpace(lines[0]))
			}
		}
	}
	fmt.Println()

	// Install Go tools from go.mod
	fmt.Println("📦 Installing Go tools from go.mod...")
	if !fileExists("go.mod") {
		fmt.Println("  ⚠ Warning: go.mod not found; skipping Go tool installation")
	} else {
		if err := installGoTools(); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Failed to install Go tools: %v\n", err)
			return err
		}
		fmt.Println("  ✓ Go tools installed")
	}
	fmt.Println()

	// Install pnpm dependencies
	fmt.Println("📦 Installing pnpm dependencies...")
	if !fileExists("package.json") {
		fmt.Println("  ⚠ Warning: package.json not found; skipping pnpm install")
	} else {
		pnpmCmd := exec.Command("pnpm", "install", "--frozen-lockfile")
		pnpmCmd.Stdout = os.Stdout
		pnpmCmd.Stderr = os.Stderr
		if err := pnpmCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Failed to install pnpm dependencies: %v\n", err)
			return err
		}
		fmt.Println("  ✓ pnpm dependencies installed")
	}
	fmt.Println()

	// Verify mono tool
	fmt.Println("🔧 Verifying mono tool...")
	if !fileExists("services.yaml") {
		fmt.Println("  ⚠ Warning: services.yaml not found; skipping service verification")
	} else {
		if err := listServices(); err != nil {
			return err
		}
	}
	fmt.Println()

	// Install repo-managed git hooks
	fmt.Println("🪝 Installing git hooks...")
	if err := installGitHooks(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Failed to install git hooks: %v\n", err)
		return err
	}
	fmt.Println("  ✓ Git hooks configured")
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

// checkGoFixSupport verifies go fix can execute its underlying toolchain command.
func checkGoFixSupport() error {
	helpCmd := exec.Command("go", "fix", "-h")
	helpOutput, helpErr := helpCmd.CombinedOutput()
	if helpErr == nil {
		return nil
	}
	helpCombined := strings.TrimSpace(string(helpOutput))
	if strings.Contains(helpCombined, `unknown command "fix"`) {
		return fmt.Errorf("go fix command is unavailable (output: %s)", helpCombined)
	}

	cmd := exec.Command("go", "tool", "fix", "-h")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	combined := strings.TrimSpace(string(output))
	if strings.Contains(combined, `no such tool "fix"`) {
		return fmt.Errorf("Go toolchain is missing cmd/fix (output: %s)", combined)
	}

	// Non-zero from -h is acceptable as long as the tool exists.
	return nil
}

func printGoToolchainDiagnostics() {
	cmd := exec.Command("go", "env", "GOROOT", "GOTOOLDIR", "GOVERSION")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("  → Unable to read Go toolchain diagnostics")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) >= 3 {
		fmt.Printf("  → Go version: %s\n", lines[2])
		fmt.Printf("  → GOROOT: %s\n", lines[0])
		fmt.Printf("  → GOTOOLDIR: %s\n", lines[1])
	}
}

func installGitHooks() error {
	// Skip outside git worktrees.
	if err := checkCommand("git", "rev-parse", "--is-inside-work-tree"); err != nil {
		fmt.Println("  ! Not in a git repository; skipping hook setup")
		return nil
	}

	hooksPath := ".githooks"
	preCommitHook := filepath.Join(hooksPath, "pre-commit")

	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		return fmt.Errorf("create hooks directory: %w", err)
	}

	// Configure repository-local hooks path.
	gitConfigCmd := exec.Command("git", "config", "core.hooksPath", hooksPath)
	gitConfigCmd.Stdout = nil
	gitConfigCmd.Stderr = os.Stderr
	if err := gitConfigCmd.Run(); err != nil {
		return fmt.Errorf("set core.hooksPath: %w", err)
	}

	if _, err := os.Stat(preCommitHook); err == nil {
		if err := os.Chmod(preCommitHook, 0o755); err != nil {
			return fmt.Errorf("chmod pre-commit hook: %w", err)
		}
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
