package cli

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/version"
)

func TestRunHelpFlag(t *testing.T) {
	code := Run([]string{"mono", "--help"})
	if code != 0 {
		t.Fatalf("expected exit code 0 for --help, got %d", code)
	}
}

func TestRunVersionFlag(t *testing.T) {
	prevVersion, prevCommit, prevDate := version.Version, version.Commit, version.Date
	version.Version = "v1.2.3"
	version.Commit = "abc123"
	version.Date = "2026-02-24T00:00:00Z"
	t.Cleanup(func() {
		version.Version = prevVersion
		version.Commit = prevCommit
		version.Date = prevDate
	})

	stdout := captureStdout(t, func() {
		code := Run([]string{"mono", "--version"})
		if code != 0 {
			t.Fatalf("expected exit code 0 for --version, got %d", code)
		}
	})

	if !strings.Contains(stdout, "Version: v1.2.3") {
		t.Fatalf("expected version in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Commit: abc123") {
		t.Fatalf("expected commit in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Date: 2026-02-24T00:00:00Z") {
		t.Fatalf("expected date in output, got %q", stdout)
	}
}

func TestRunMissingCommand(t *testing.T) {
	code := Run([]string{"mono"})
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing command, got %d", code)
	}
}

func TestRunIntegrationFlagRejectedForNonTestTask(t *testing.T) {
	stderr := captureStderr(t, func() {
		code := Run([]string{"mono", "build", "--integration"})
		if code != 1 {
			t.Fatalf("expected exit code 1 for invalid --integration usage, got %d", code)
		}
	})
	if !strings.Contains(stderr, "--integration is only supported with \"test\"") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestRegisterCommandsExpectedSet(t *testing.T) {
	prevRegistry, prevRegistered := registry, commandsRegistered
	registry = core.NewRegistry()
	commandsRegistered = false
	t.Cleanup(func() {
		registry = prevRegistry
		commandsRegistered = prevRegistered
	})

	registerCommands()

	want := []string{
		"affected",
		"check",
		"dev",
		"doctor",
		"hosts",
		"infra",
		"list",
		"metadata",
		"migrate",
		"status",
		"worktree",
	}
	got := registry.Names()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected registered commands:\n got: %v\nwant: %v", got, want)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	return buf.String()
}
