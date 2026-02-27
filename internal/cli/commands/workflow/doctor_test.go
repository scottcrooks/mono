package workflow

import (
	"bytes"
	"os"
	"path/filepath"
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
