package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scottcrooks/mono/internal/cli/core"
)

func TestValidateManifestForDoctorSuccess(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "svc"))
	mustWrite(t, filepath.Join(repo, "apps", "svc", "go.mod"), "module svc\n")
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, `services:
  - name: svc
    path: apps/svc
    kind: service
    archetype: go
    owner: team
    deploy:
      containerPort: 8080
      probes:
        readiness: {path: /ready, port: 8080}
        liveness: {path: /health, port: 8080}
      resources:
        requests: {cpu: 100m}
        limits: {cpu: 200m}
`)

	var out bytes.Buffer
	if err := validateManifestForDoctor(manifestPath, &out); err != nil {
		t.Fatalf("validateManifestForDoctor returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Summary: 0 error(s), 0 warning(s)") {
		t.Fatalf("expected summary output, got %q", out.String())
	}
}

func TestValidateManifestForDoctorValidationFailure(t *testing.T) {
	repo := t.TempDir()
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, `services:
  - name: svc
    path: apps/missing
    kind: service
    archetype: go
`)

	var out bytes.Buffer
	err := validateManifestForDoctor(manifestPath, &out)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	codeErr, ok := core.AsExitCodeError(err)
	if !ok {
		t.Fatalf("expected exit code error, got %T", err)
	}
	if codeErr.ExitCode() != core.ExitCodeValidation {
		t.Fatalf("unexpected exit code: %d", codeErr.ExitCode())
	}
	output := out.String()
	if !strings.Contains(output, "Summary: ") {
		t.Fatalf("expected summary output, got %q", output)
	}
	if !strings.Contains(output, "[ERROR]") {
		t.Fatalf("expected severity output, got %q", output)
	}
	if !strings.Contains(output, "service: svc (apps/missing)") {
		t.Fatalf("expected service context in output, got %q", output)
	}
}

func TestValidateManifestForDoctorWarningOnly(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "pkg"))
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, `services:
  - name: pkg
    path: apps/pkg
    kind: package
    archetype: go
    owner: team
`)

	var out bytes.Buffer
	if err := validateManifestForDoctor(manifestPath, &out); err != nil {
		t.Fatalf("expected warning-only validation to pass, got %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Summary: 0 error(s), 1 warning(s)") {
		t.Fatalf("expected warning summary, got %q", output)
	}
	if !strings.Contains(output, "[WARNING]") {
		t.Fatalf("expected warning detail output, got %q", output)
	}
	if !strings.Contains(output, "Warnings:") {
		t.Fatalf("expected warning section heading, got %q", output)
	}
}

func TestParseGoModToolsWithToolBlock(t *testing.T) {
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "go.mod"), `module example.com/test

go 1.25

tool (
	// comment
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	golang.org/x/vuln/cmd/govulncheck // inline comment
)
`)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	got, err := parseGoModTools()
	if err != nil {
		t.Fatalf("parseGoModTools returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d (%v)", len(got), got)
	}
	if got[0] != "github.com/golangci/golangci-lint/v2/cmd/golangci-lint" {
		t.Fatalf("unexpected first tool: %q", got[0])
	}
	if got[1] != "golang.org/x/vuln/cmd/govulncheck" {
		t.Fatalf("unexpected second tool: %q", got[1])
	}
}

func TestToolBinaryName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", want: "golangci-lint"},
		{in: "golang.org/x/vuln/cmd/govulncheck", want: "govulncheck"},
		{in: " stringer ", want: "stringer"},
		{in: "", want: ""},
	}

	for _, tt := range tests {
		if got := toolBinaryName(tt.in); got != tt.want {
			t.Fatalf("toolBinaryName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEnsureReactServiceDefaultsAddsMissingScripts(t *testing.T) {
	repo := t.TempDir()
	svcPath := filepath.Join(repo, "apps", "web")
	mustMkdirAll(t, svcPath)
	mustWrite(t, filepath.Join(svcPath, "package.json"), `{
  "name": "web",
  "private": true,
  "scripts": {
    "build": "tsc -b && vite build"
  }
}
`)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	changed, err := ensureReactServiceDefaults(core.Service{
		Name:      "web",
		Path:      "apps/web",
		Archetype: "react",
	})
	if err != nil {
		t.Fatalf("ensureReactServiceDefaults returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected package.json to be updated")
	}

	pkg := readPackageJSON(t, filepath.Join(svcPath, "package.json"))
	scripts := pkg["scripts"].(map[string]any)
	assertScript(t, scripts, "build", "tsc -b && vite build")
	assertScript(t, scripts, "lint", "eslint .")
	assertScript(t, scripts, "typecheck", "tsc -b")
	assertScript(t, scripts, "test", "vitest run --passWithNoTests")
	assertScript(t, scripts, "audit", "pnpm audit --prod")
	assertScript(t, pkg["devDependencies"].(map[string]any), "eslint", "^9.18.0")
	assertScript(t, pkg["devDependencies"].(map[string]any), "vitest", "^3.0.0")
	assertFileContains(t, filepath.Join(svcPath, "eslint.config.js"), `import js from "@eslint/js";`)
	assertFileContains(t, filepath.Join(svcPath, "vitest.config.ts"), `passWithNoTests: true`)
}

func TestEnsureReactServiceDefaultsPreservesExistingScripts(t *testing.T) {
	repo := t.TempDir()
	svcPath := filepath.Join(repo, "apps", "web")
	mustMkdirAll(t, svcPath)
	mustWrite(t, filepath.Join(svcPath, "package.json"), `{
  "name": "web",
  "private": true,
  "scripts": {
    "lint": "biome check .",
    "test": "custom-test"
  }
}
`)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	changed, err := ensureReactServiceDefaults(core.Service{
		Name:      "web",
		Path:      "apps/web",
		Archetype: "react",
	})
	if err != nil {
		t.Fatalf("ensureReactServiceDefaults returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected package.json to be updated")
	}

	pkg := readPackageJSON(t, filepath.Join(svcPath, "package.json"))
	scripts := pkg["scripts"].(map[string]any)
	assertScript(t, scripts, "lint", "biome check .")
	assertScript(t, scripts, "test", "custom-test")
	assertScript(t, scripts, "typecheck", "tsc -b")
}

func TestEnsureReactServiceDefaultsNoopWhenAlreadyConfigured(t *testing.T) {
	repo := t.TempDir()
	svcPath := filepath.Join(repo, "apps", "web")
	mustMkdirAll(t, svcPath)
	original := `{
  "name": "web",
  "private": true,
  "scripts": {
    "lint": "eslint .",
    "typecheck": "tsc -b",
    "test": "vitest run --passWithNoTests",
    "audit": "pnpm audit --prod"
  },
  "devDependencies": {
    "@eslint/js": "^9.18.0",
    "eslint": "^9.18.0",
    "eslint-plugin-react-hooks": "^5.1.0",
    "eslint-plugin-react-refresh": "^0.4.18",
    "globals": "^15.14.0",
    "jsdom": "^25.0.0",
    "typescript-eslint": "^8.20.0",
    "vitest": "^3.0.0"
  }
}
`
	pkgPath := filepath.Join(svcPath, "package.json")
	mustWrite(t, pkgPath, original)
	mustWrite(t, filepath.Join(svcPath, "eslint.config.js"), reactEslintConfigTemplate)
	mustWrite(t, filepath.Join(svcPath, "vitest.config.ts"), reactVitestConfigTemplate)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	changed, err := ensureReactServiceDefaults(core.Service{
		Name:      "web",
		Path:      "apps/web",
		Archetype: "react",
	})
	if err != nil {
		t.Fatalf("ensureReactServiceDefaults returned error: %v", err)
	}
	if changed {
		t.Fatal("expected package.json to remain unchanged")
	}
	got, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if string(got) != original {
		t.Fatalf("package.json changed unexpectedly: %q", string(got))
	}
}

func TestInstallServiceDependenciesInstallsSupportedArchetypeDependencies(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "web"))
	mustWrite(t, filepath.Join(repo, "apps", "web", "package.json"), `{"name":"web"}`)
	mustMkdirAll(t, filepath.Join(repo, "apps", "api"))
	mustWrite(t, filepath.Join(repo, "apps", "api", "go.mod"), "module api\n")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	calls := make([]string, 0)
	orig := runServicePNPMInstall
	runServicePNPMInstall = func(dir string, frozen bool) error {
		calls = append(calls, dir)
		if frozen {
			t.Fatal("expected non-frozen install for service dependencies")
		}
		return nil
	}
	t.Cleanup(func() {
		runServicePNPMInstall = orig
	})

	installed, err := installServiceDependencies(&core.Config{
		Services: []core.Service{
			{Name: "web", Path: "apps/web", Archetype: "react"},
			{Name: "api", Path: "apps/api", Archetype: "go"},
			{Name: "missing", Path: "apps/missing", Archetype: "react"},
		},
	})
	if err != nil {
		t.Fatalf("installServiceDependencies returned error: %v", err)
	}
	if !reflect.DeepEqual(installed, []string{"api", "web"}) {
		t.Fatalf("unexpected installed services: %v", installed)
	}
	if len(calls) != 1 || calls[0] != "apps/web" {
		t.Fatalf("unexpected pnpm install calls: %v", calls)
	}
}

func TestInstallGitHooksConfiguresHooksPathAndCommitTemplateWhenPresent(t *testing.T) {
	repo := initGitRepo(t)
	mustWrite(t, filepath.Join(repo, ".gitmessage"), "subject\n\nbody\n")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	if err := installGitHooks(); err != nil {
		t.Fatalf("installGitHooks returned error: %v", err)
	}

	if got := gitLocalConfig(t, repo, "core.hooksPath"); got != ".githooks" {
		t.Fatalf("core.hooksPath = %q, want %q", got, ".githooks")
	}
	if got := gitLocalConfig(t, repo, "commit.template"); got != ".gitmessage" {
		t.Fatalf("commit.template = %q, want %q", got, ".gitmessage")
	}
	if _, err := os.Stat(filepath.Join(repo, ".githooks")); err != nil {
		t.Fatalf("expected .githooks directory to exist: %v", err)
	}
}

func TestInstallGitHooksConfiguresCommitTemplateFromGitHubDir(t *testing.T) {
	repo := initGitRepo(t)
	mustMkdirAll(t, filepath.Join(repo, ".github"))
	mustWrite(t, filepath.Join(repo, ".github", ".gitmessage"), "subject\n\nbody\n")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	if err := installGitHooks(); err != nil {
		t.Fatalf("installGitHooks returned error: %v", err)
	}

	if got := gitLocalConfig(t, repo, "commit.template"); got != ".github/.gitmessage" {
		t.Fatalf("commit.template = %q, want %q", got, ".github/.gitmessage")
	}
}

func TestInstallGitHooksSkipsCommitTemplateWhenMissing(t *testing.T) {
	repo := initGitRepo(t)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(prev); chdirErr != nil {
			t.Fatalf("restore working dir: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	if err := installGitHooks(); err != nil {
		t.Fatalf("installGitHooks returned error: %v", err)
	}

	if got := gitLocalConfig(t, repo, "core.hooksPath"); got != ".githooks" {
		t.Fatalf("core.hooksPath = %q, want %q", got, ".githooks")
	}
	if got := gitLocalConfig(t, repo, "commit.template"); got != "" {
		t.Fatalf("commit.template = %q, want empty", got)
	}
}

func readPackageJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return pkg
}

func assertScript(t *testing.T, scripts map[string]any, name, want string) {
	t.Helper()
	got, ok := scripts[name].(string)
	if !ok {
		t.Fatalf("script %q missing or not a string", name)
	}
	if got != want {
		t.Fatalf("script %q = %q, want %q", name, got, want)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %s to contain %q, got %q", path, want, string(data))
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return repo
}

func gitLocalConfig(t *testing.T, repo, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--local", "--get", key)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
