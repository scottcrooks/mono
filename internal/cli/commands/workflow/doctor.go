package workflow

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/output"
	"github.com/scottcrooks/mono/internal/cli/validation"
)

type doctorCommand struct{}

func init() {
	registerCommand("doctor", &doctorCommand{})
}

// Run performs environment health checks and fixes
func (c *doctorCommand) Run(_ []string) error {
	p := output.DefaultPrinter()
	p.Section("Checking development environment...")
	p.Blank()

	hasErrors := false

	// Check Go
	goInstalled := false
	p.Summary("Go (1.25.7+):")
	if err := checkCommand("go", "version"); err != nil {
		p.StepErr("doctor", "Go is NOT installed")
		p.Summary("  Install from: https://go.dev/dl/")
		hasErrors = true
	} else {
		goInstalled = true
		cmd := exec.Command("go", "version")
		if output, err := cmd.Output(); err == nil {
			p.Summary(strings.TrimSpace(string(output)))
			p.StepOK("doctor", "Go is installed")
		}
	}
	p.Blank()

	// Check go fix support (required by verification workflow)
	if goInstalled {
		p.Summary("go fix support:")
		if err := checkGoFixSupport(); err != nil {
			p.StepErr("doctor", "go fix is NOT available")
			p.Summary(fmt.Sprintf("  %v", err))
			printGoToolchainDiagnostics()
			p.Summary("  Reinstall/upgrade Go from: https://go.dev/dl/")
			hasErrors = true
		} else {
			p.Summary("available")
			p.StepOK("doctor", "go fix is available")
		}
		p.Blank()
	}

	// Check Node.js
	p.Summary("Node.js (22.x+):")
	if err := checkCommand("node", "--version"); err != nil {
		p.StepErr("doctor", "Node.js is NOT installed")
		p.Summary("  Install from: https://nodejs.org/ or use nvm")
		hasErrors = true
	} else {
		cmd := exec.Command("node", "--version")
		if output, err := cmd.Output(); err == nil {
			p.Summary(strings.TrimSpace(string(output)))
			p.StepOK("doctor", "Node.js is installed")
		}
	}
	p.Blank()

	// Check pnpm (auto-install if missing)
	p.Summary("pnpm (10.x+):")
	if err := checkCommand("pnpm", "--version"); err != nil {
		p.StepErr("doctor", "pnpm is NOT installed")
		p.StepStart("doctor", "Installing pnpm...")
		installCmd := exec.Command("npm", "install", "-g", "pnpm")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			p.StepErr("doctor", "Failed to install pnpm")
			hasErrors = true
		} else {
			p.StepOK("doctor", "pnpm installed successfully")
		}
	} else {
		cmd := exec.Command("pnpm", "--version")
		if output, err := cmd.Output(); err == nil {
			p.Summary(strings.TrimSpace(string(output)))
			p.StepOK("doctor", "pnpm is installed")
		}
	}
	p.Blank()

	if hasErrors {
		return fmt.Errorf("missing critical dependencies")
	}

	// Check kubectl (optional - only needed for mono infra)
	p.Summary("kubectl (optional):")
	if err := checkCommand("kubectl", "version", "--client"); err != nil {
		p.Summary("not installed")
	} else {
		cmd := exec.Command("kubectl", "version", "--client")
		if output, err := cmd.Output(); err == nil {
			// Extract just the version line
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 {
				p.Summary(strings.TrimSpace(lines[0]))
			}
		}
	}
	p.Blank()

	// Install Go tools from go.mod
	p.StepStart("doctor", "Checking Go tools from go.mod...")
	if !fileExists("go.mod") {
		p.StepWarn("doctor", "go.mod not found; skipping Go tool installation")
	} else {
		installed, err := installGoTools()
		if err != nil {
			p.StepErr("doctor", fmt.Sprintf("Failed to install Go tools: %v", err))
			return err
		}
		if installed == 0 {
			p.StepOK("doctor", "Go tools already installed")
		} else {
			p.StepOK("doctor", fmt.Sprintf("Installed %d missing Go tool(s)", installed))
		}
	}
	p.Blank()

	// Install pnpm dependencies
	p.StepStart("doctor", "Installing pnpm dependencies...")
	if !fileExists("package.json") {
		p.StepWarn("doctor", "package.json not found; skipping pnpm install")
	} else {
		pnpmCmd := exec.Command("pnpm", "install", "--frozen-lockfile")
		pnpmCmd.Stdout = os.Stdout
		pnpmCmd.Stderr = os.Stderr
		if err := pnpmCmd.Run(); err != nil {
			p.StepErr("doctor", fmt.Sprintf("Failed to install pnpm dependencies: %v", err))
			return err
		}
		p.StepOK("doctor", "pnpm dependencies installed")
	}
	p.Blank()

	// Verify mono tool
	p.StepStart("doctor", "Verifying mono tool...")
	if !fileExists("services.yaml") {
		p.StepWarn("doctor", "services.yaml not found; skipping service verification")
	} else {
		if err := listServices(); err != nil {
			return err
		}
		p.Blank()
		if err := validateManifestForDoctor("services.yaml", os.Stdout); err != nil {
			return err
		}
	}
	p.Blank()

	// Install repo-managed git hooks
	p.StepStart("doctor", "Installing git hooks...")
	if err := installGitHooks(); err != nil {
		p.StepErr("doctor", fmt.Sprintf("Failed to install git hooks: %v", err))
		return err
	}
	p.StepOK("doctor", "Git hooks configured")
	p.Blank()

	p.StepOK("doctor", "All checks passed! Environment is ready.")
	return nil
}

// checkCommand checks if a command is available and runs successfully
func checkCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// installGoTools reads go.mod and installs only missing tools from the tool directive.
func installGoTools() (int, error) {
	p := output.DefaultPrinter()
	tools, err := parseGoModTools()
	if err != nil {
		return 0, fmt.Errorf("failed to parse go.mod: %w", err)
	}
	if len(tools) == 0 {
		p.Summary("  No Go tools declared in go.mod")
		return 0, nil
	}

	missing := missingGoTools(tools)
	if len(missing) == 0 {
		p.Summary("  All declared Go tools are already available")
		return 0, nil
	}

	installed := 0
	for _, tool := range missing {
		p.Summary(fmt.Sprintf("  Installing missing tool %s...", tool))
		//nolint:gosec // G204: tool paths are from go.mod, trusted input
		cmd := exec.Command("go", "install", tool)
		cmd.Stdout = nil // Suppress output unless there's an error
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Don't fail on individual tool errors, just report and continue
			p.StepWarn("doctor", fmt.Sprintf("failed to install %s", tool))
			continue
		}
		installed++
	}

	return installed, nil
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
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) == 0 {
				continue
			}
			tools = append(tools, fields[0])
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return tools, nil
}

func missingGoTools(tools []string) []string {
	var missing []string
	for _, tool := range tools {
		if !isGoToolAvailable(tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

func isGoToolAvailable(tool string) bool {
	name := toolBinaryName(tool)
	cmd := exec.Command("go", "tool", name, "-h")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}

	combined := string(output)
	return !strings.Contains(combined, fmt.Sprintf(`no such tool "%s"`, name))
}

func toolBinaryName(toolPath string) string {
	trimmed := strings.TrimSpace(toolPath)
	if trimmed == "" {
		return ""
	}

	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
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
	p := output.DefaultPrinter()

	cmd := exec.Command("go", "env", "GOROOT", "GOTOOLDIR", "GOVERSION")
	output, err := cmd.Output()
	if err != nil {
		p.Summary("  Unable to read Go toolchain diagnostics")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) >= 3 {
		p.Summary(fmt.Sprintf("  Go version: %s", lines[2]))
		p.Summary(fmt.Sprintf("  GOROOT: %s", lines[0]))
		p.Summary(fmt.Sprintf("  GOTOOLDIR: %s", lines[1]))
	}
}

func installGitHooks() error {
	p := output.DefaultPrinter()

	// Skip outside git worktrees.
	if err := checkCommand("git", "rev-parse", "--is-inside-work-tree"); err != nil {
		p.StepWarn("doctor", "Not in a git repository; skipping hook setup")
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

func validateManifestForDoctor(path string, out io.Writer) error {
	mode := output.DetectMode(out)
	fmt.Fprintln(out, output.ApplyStyle(mode, output.StyleInfo, "🧭 Validating manifest policy..."))
	report, err := validation.ValidateServicesManifest(path)
	if err != nil {
		return err
	}

	summaryStyle := output.StyleSuccess
	if report.HasErrors() {
		summaryStyle = output.StyleError
	} else if report.WarningCount() > 0 {
		summaryStyle = output.StyleWarn
	}
	fmt.Fprintf(out, "  %s\n", output.ApplyStyle(mode, summaryStyle,
		fmt.Sprintf("Summary: %d error(s), %d warning(s)", report.ErrorCount(), report.WarningCount())))
	if len(report.Diagnostics) == 0 {
		fmt.Fprintln(out, "  ✓ Manifest policy checks passed")
		return nil
	}

	if report.HasErrors() {
		fmt.Fprintf(out, "  %s\n", output.ApplyStyle(mode, output.StyleError, "Errors:"))
	}
	if report.WarningCount() > 0 {
		fmt.Fprintf(out, "  %s\n", output.ApplyStyle(mode, output.StyleWarn, "Warnings:"))
	}

	for _, diag := range report.Diagnostics {
		level := strings.ToUpper(string(diag.Severity))
		if level == "" {
			level = "ERROR"
		}
		labelStyle := output.StyleError
		if strings.EqualFold(level, "WARNING") {
			labelStyle = output.StyleWarn
		}
		location := diag.Path
		if diag.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", diag.Path, diag.Line, diag.Column)
		}

		label := output.ApplyStyle(mode, labelStyle, "["+level+"]")
		fmt.Fprintf(out, "    - %s [%s] %s\n", label, diag.Code, location)
		fmt.Fprintf(out, "      %s\n", diag.Message)
		if strings.TrimSpace(diag.Service) != "" {
			fmt.Fprintf(out, "      service: %s\n", diag.Service)
		}
	}
	if !report.HasErrors() {
		fmt.Fprintln(out, "  ✓ Manifest policy checks passed with warnings")
		return nil
	}

	return core.NewExitCodeError(core.ExitCodeValidation, fmt.Sprintf("manifest validation failed with %d error(s)", report.ErrorCount()))
}
