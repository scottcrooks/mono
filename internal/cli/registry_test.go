package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTaskVerbIgnoresLegacyCommandMap(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "apps", "svc", "README.md"), "placeholder\n")
	mustWrite(t, filepath.Join(repo, "services.yaml"), `services:
  - name: svc
    path: apps/svc
    description: test service
    kind: service
    archetype: unknown
    commands:
      build: go definitely-not-a-command
`)

	stdout := captureStdout(t, func() {
		code := Run([]string{"mono", "build", "svc"})
		if code != 0 {
			t.Fatalf("expected orchestrated task path to return success with skip, got %d", code)
		}
	})

	if !strings.Contains(stdout, "skipped") {
		t.Fatalf("expected skip output when archetype does not support task, got %q", stdout)
	}
}

func TestRunNonTaskVerbUsesLegacyCommandMap(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "apps", "svc", "go.mod"), "module svc\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "apps", "svc", "main.go"), "package main\nfunc main(){}\n")
	mustWrite(t, filepath.Join(repo, "services.yaml"), `services:
  - name: svc
    path: apps/svc
    description: test service
    kind: service
    archetype: go
    commands:
      custom: go version
`)

	code := Run([]string{"mono", "custom", "svc"})
	if code != 0 {
		t.Fatalf("expected custom non-task command to use legacy command map and succeed, got %d", code)
	}
}
