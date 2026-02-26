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
	if !strings.Contains(output, "[service=svc (apps/missing)]") {
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
