package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/output"
	"github.com/scottcrooks/mono/internal/cli/tasks"
	"github.com/scottcrooks/mono/internal/cli/validation"
)

type doctorCommand struct{}

var runServicePNPMInstall = installPNPMDependencies

type doctorFailure struct {
	message  string
	exitCode int
}

func init() {
	registerCommand("doctor", &doctorCommand{})
}

// Run performs environment health checks and fixes
func (c *doctorCommand) Run(_ []string) error {
	p := output.DefaultPrinter()
	p.Section("Checking development environment...")
	p.Blank()

	shouldValidateManifest := fileExists("services.yaml")
	failures := make([]doctorFailure, 0)
	recordFailure := func(message string, err error) {
		exitCode := core.ExitCodeGeneric
		if codeErr, ok := core.AsExitCodeError(err); ok {
			exitCode = codeErr.ExitCode()
		}
		failures = append(failures, doctorFailure{
			message:  message,
			exitCode: exitCode,
		})
	}

	// Check Go
	goInstalled := false
	p.Summary("Go (1.25.7+):")
	if err := checkCommand("go", "version"); err != nil {
		p.StepErr("doctor", "Go is NOT installed")
		p.Summary("  Install from: https://go.dev/dl/")
		recordFailure("Go is not installed", err)
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
			recordFailure("go fix is not available", err)
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
		recordFailure("Node.js is not installed", err)
	} else {
		cmd := exec.Command("node", "--version")
		if output, err := cmd.Output(); err == nil {
			p.Summary(strings.TrimSpace(string(output)))
			p.StepOK("doctor", "Node.js is installed")
		}
	}
	p.Blank()

	// Check pnpm (auto-install if missing)
	pnpmInstalled := false
	p.Summary("pnpm (10.x+):")
	if err := checkCommand("pnpm", "--version"); err != nil {
		p.StepErr("doctor", "pnpm is NOT installed")
		p.StepStart("doctor", "Installing pnpm...")
		installCmd := exec.Command("npm", "install", "-g", "pnpm")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			p.StepErr("doctor", "Failed to install pnpm")
			recordFailure("pnpm installation failed", err)
		} else {
			pnpmInstalled = true
			p.StepOK("doctor", "pnpm installed successfully")
		}
	} else {
		pnpmInstalled = true
		cmd := exec.Command("pnpm", "--version")
		if output, err := cmd.Output(); err == nil {
			p.Summary(strings.TrimSpace(string(output)))
			p.StepOK("doctor", "pnpm is installed")
		}
	}
	p.Blank()

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
	} else if !goInstalled {
		p.StepWarn("doctor", "Go is unavailable; skipping Go tool installation")
	} else {
		installed, err := installGoTools()
		if err != nil {
			p.StepErr("doctor", fmt.Sprintf("Failed to install Go tools: %v", err))
			recordFailure(fmt.Sprintf("Go tool installation failed: %v", err), err)
		}
		if err == nil && installed == 0 {
			p.StepOK("doctor", "Go tools already installed")
		} else if err == nil {
			p.StepOK("doctor", fmt.Sprintf("Installed %d missing Go tool(s)", installed))
		}
	}
	p.Blank()

	// Install pnpm dependencies
	p.StepStart("doctor", "Installing pnpm dependencies...")
	if !fileExists("package.json") {
		p.StepWarn("doctor", "package.json not found; skipping pnpm install")
	} else if !pnpmInstalled {
		p.StepWarn("doctor", "pnpm is unavailable; skipping pnpm install")
	} else {
		pnpmCmd := exec.Command("pnpm", "install", "--frozen-lockfile")
		pnpmCmd.Stdout = os.Stdout
		pnpmCmd.Stderr = os.Stderr
		if err := pnpmCmd.Run(); err != nil {
			p.StepErr("doctor", fmt.Sprintf("Failed to install pnpm dependencies: %v", err))
			recordFailure(fmt.Sprintf("pnpm dependency installation failed: %v", err), err)
		} else {
			p.StepOK("doctor", "pnpm dependencies installed")
		}
	}
	p.Blank()

	// Verify mono tool
	p.StepStart("doctor", "Verifying mono tool...")
	if !shouldValidateManifest {
		p.StepWarn("doctor", "services.yaml not found; skipping service verification")
	} else {
		cfg, err := core.LoadConfig()
		if err != nil {
			p.StepErr("doctor", fmt.Sprintf("Failed to load services config: %v", err))
			recordFailure(fmt.Sprintf("services config load failed: %v", err), err)
		} else {
			if err := listServices(); err != nil {
				p.StepErr("doctor", fmt.Sprintf("Failed to list services: %v", err))
				recordFailure(fmt.Sprintf("service verification failed: %v", err), err)
			} else {
				p.Blank()
				p.StepStart("doctor", "Ensuring service task defaults...")
				updated, err := ensureServiceTaskDefaults(cfg)
				if err != nil {
					p.StepErr("doctor", fmt.Sprintf("Failed to ensure service task defaults: %v", err))
					recordFailure(fmt.Sprintf("service task defaults failed: %v", err), err)
				} else if len(updated) == 0 {
					p.StepOK("doctor", "Service task defaults are configured")
				} else {
					sort.Strings(updated)
					for _, svc := range updated {
						p.Summary(fmt.Sprintf("  Updated React scripts for %s", svc))
					}
					p.StepOK("doctor", fmt.Sprintf("Configured service task defaults for %d service(s)", len(updated)))
				}
				p.Blank()
				p.StepStart("doctor", "Installing service dependencies...")
				installed, err := installServiceDependencies(cfg)
				if err != nil {
					p.StepErr("doctor", fmt.Sprintf("Failed to install service dependencies: %v", err))
					recordFailure(fmt.Sprintf("service dependency installation failed: %v", err), err)
				} else if len(installed) == 0 {
					p.StepOK("doctor", "Service dependencies are installed")
				} else {
					sort.Strings(installed)
					for _, svc := range installed {
						p.Summary(fmt.Sprintf("  Installed pnpm dependencies for %s", svc))
					}
					p.StepOK("doctor", fmt.Sprintf("Installed service dependencies for %d service(s)", len(installed)))
				}
				p.Blank()
			}
		}
	}
	p.Blank()

	p.StepStart("doctor", "Installing git hooks...")
	if err := installGitHooks(); err != nil {
		p.StepErr("doctor", fmt.Sprintf("Failed to install git hooks: %v", err))
		recordFailure(fmt.Sprintf("git hook setup failed: %v", err), err)
	} else {
		p.StepOK("doctor", "Git hooks configured")
	}
	p.Blank()

	if shouldValidateManifest {
		if err := validateManifestForDoctor("services.yaml", os.Stdout); err != nil {
			recordFailure(err.Error(), err)
		}
		p.Blank()
	}

	if len(failures) > 0 {
		p.StepErr("doctor", fmt.Sprintf("Completed with %d failure(s)", len(failures)))
		p.Summary("  Failures:")
		exitCode := core.ExitCodeValidation
		for _, failure := range failures {
			p.Summary("  - " + failure.message)
			if failure.exitCode == core.ExitCodeGeneric {
				exitCode = core.ExitCodeGeneric
			}
		}
		return core.NewExitCodeError(exitCode, fmt.Sprintf("doctor completed with %d failure(s)", len(failures)))
	}

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
	p.Summary(fmt.Sprintf("  Configured git core.hooksPath=%s", hooksPath))

	if commitTemplatePath := findCommitTemplatePath(); commitTemplatePath != "" {
		gitConfigCmd = exec.Command("git", "config", "commit.template", commitTemplatePath)
		gitConfigCmd.Stdout = nil
		gitConfigCmd.Stderr = os.Stderr
		if err := gitConfigCmd.Run(); err != nil {
			return fmt.Errorf("set commit.template: %w", err)
		}
		p.Summary(fmt.Sprintf("  Configured git commit.template=%s", commitTemplatePath))
	}

	if _, err := os.Stat(preCommitHook); err == nil {
		if err := os.Chmod(preCommitHook, 0o755); err != nil {
			return fmt.Errorf("chmod pre-commit hook: %w", err)
		}
	}

	return nil
}

func findCommitTemplatePath() string {
	candidates := []string{
		".gitmessage",
		filepath.Join(".github", ".gitmessage"),
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ensureServiceTaskDefaults(cfg *core.Config) ([]string, error) {
	updated := make([]string, 0)
	for _, svc := range cfg.Services {
		if svc.Archetype != "react" || strings.TrimSpace(svc.Path) == "" {
			continue
		}

		changed, err := ensureReactServiceDefaults(svc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", svc.Name, err)
		}
		if changed {
			updated = append(updated, svc.Name)
		}
	}
	return updated, nil
}

func ensureReactServiceDefaults(svc core.Service) (bool, error) {
	pkgPath := filepath.Join(svc.Path, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", pkgPath, err)
	}

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, fmt.Errorf("parse %s: %w", pkgPath, err)
	}

	scripts := jsonObjectMap(pkg["scripts"])
	if scripts == nil {
		scripts = map[string]any{}
	}
	devDependencies := jsonObjectMap(pkg["devDependencies"])
	if devDependencies == nil {
		devDependencies = map[string]any{}
	}

	changed := false
	for name, cmd := range defaultReactScripts() {
		if strings.TrimSpace(stringValue(scripts[name])) != "" {
			continue
		}
		scripts[name] = cmd
		changed = true
	}
	for name, version := range defaultReactDevDependencies() {
		if strings.TrimSpace(stringValue(devDependencies[name])) != "" {
			continue
		}
		devDependencies[name] = version
		changed = true
	}
	created, err := ensureReactConfigFiles(svc)
	if err != nil {
		return false, err
	}
	if created {
		changed = true
	}
	if !changed {
		return false, nil
	}

	pkg["scripts"] = scripts
	pkg["devDependencies"] = devDependencies
	formatted, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal %s: %w", pkgPath, err)
	}
	formatted = append(formatted, '\n')
	if err := os.WriteFile(pkgPath, formatted, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", pkgPath, err)
	}
	return true, nil
}

func defaultReactScripts() map[string]string {
	return map[string]string{
		"audit":     "pnpm audit --prod",
		"lint":      "eslint .",
		"test":      "vitest run --passWithNoTests",
		"typecheck": "tsc -b",
	}
}

func defaultReactDevDependencies() map[string]string {
	return map[string]string{
		"@eslint/js":                  "^9.18.0",
		"eslint":                      "^9.18.0",
		"eslint-plugin-react-hooks":   "^5.1.0",
		"eslint-plugin-react-refresh": "^0.4.18",
		"globals":                     "^15.14.0",
		"jsdom":                       "^25.0.0",
		"typescript-eslint":           "^8.20.0",
		"vitest":                      "^3.0.0",
	}
}

func ensureReactConfigFiles(svc core.Service) (bool, error) {
	files := map[string]string{
		filepath.Join(svc.Path, "eslint.config.js"): reactEslintConfigTemplate,
		filepath.Join(svc.Path, "vitest.config.ts"): reactVitestConfigTemplate,
	}
	createdAny := false
	for path, content := range files {
		created, err := ensureFileWithContents(path, content)
		if err != nil {
			return false, err
		}
		if created {
			createdAny = true
		}
	}
	return createdAny, nil
}

func ensureFileWithContents(path, content string) (bool, error) {
	if fileExists(path) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func installServiceDependencies(cfg *core.Config) ([]string, error) {
	serviceNames := make([]string, 0, len(cfg.Services))
	for _, svc := range cfg.Services {
		serviceNames = append(serviceNames, svc.Name)
	}

	targets, err := tasks.DependencyInstallTargetsForServices(cfg, serviceNames)
	if err != nil {
		return nil, err
	}

	installed := make(map[string]struct{})
	for _, target := range targets {
		if target.Archetype == "react" {
			if err := runServicePNPMInstall(target.Dir, false); err != nil {
				return nil, err
			}
		} else if err := runDependencyInstallTarget(target); err != nil {
			return nil, err
		}
		for _, svc := range target.Services {
			installed[svc] = struct{}{}
		}
	}

	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func installPNPMDependencies(dir string, frozen bool) error {
	args := []string{"install"}
	if frozen {
		args = append(args, "--frozen-lockfile")
	}
	cmd := exec.Command("pnpm", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pnpm %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return nil
}

func runDependencyInstallTarget(target tasks.DependencyInstallTarget) error {
	parts := strings.Fields(target.Command)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = target.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s in %s: %w", target.Command, target.Dir, err)
	}
	return nil
}

func jsonObjectMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return obj
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

const reactEslintConfigTemplate = `import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "coverage"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": [
        "warn",
        { allowConstantExport: true },
      ],
    },
  },
);
`

const reactVitestConfigTemplate = `import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    passWithNoTests: true,
    css: true,
    exclude: ["**/node_modules/**", "**/tests/integration/**"],
  },
});
`

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
