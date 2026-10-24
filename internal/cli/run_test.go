package cli

import (
	"bytes"
	"errors"
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

func TestTelemetryDispatchOutsideProject(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", "")
	stdout := captureStdout(t, func() {
		if code := Run([]string{"mono", "telemetry", "env", "--service-name=worker"}); code != 0 {
			t.Fatalf("offline env failed: %d", code)
		}
	})
	if !strings.HasPrefix(stdout, "export OTEL_EXPORTER_OTLP_ENDPOINT=") || !strings.Contains(stdout, "export OTEL_SERVICE_NAME='worker'\n") {
		t.Fatalf("offline env output includes noise: %q", stdout)
	}
	for _, args := range [][]string{
		{"mono", "telemetry", "--help"},
		{"mono", "telemetry", "doctor", "--help"},
	} {
		stdout := captureStdout(t, func() {
			if code := Run(args); code != 0 {
				t.Fatalf("help failed: %v: %d", args, code)
			}
		})
		if !strings.Contains(stdout, "mono telemetry") {
			t.Fatalf("unexpected help: %q", stdout)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"mono", "telemetry", "bogus"}, "unknown subcommand"},
		{[]string{"mono", "telemetry", "up", "--storage-size=0Gi"}, "positive integer"},
		{[]string{"mono", "telemetry", "doctor", "--check"}, "telemetry tools"},
		{[]string{"mono", "telemetry", "up"}, "telemetry tools"},
		{[]string{"mono", "telemetry", "status"}, "telemetry tools"},
		{[]string{"mono", "telemetry", "connect"}, "telemetry tools"},
		{[]string{"mono", "telemetry", "down"}, "telemetry tools"},
	} {
		stderr := captureStderr(t, func() {
			if code := Run(tc.args); code != 1 {
				t.Fatalf("expected failure for %v, got %d", tc.args, code)
			}
		})
		if !strings.Contains(stderr, tc.want) || strings.Contains(stderr, "services.yaml") {
			t.Fatalf("unexpected error for %v: %q", tc.args, stderr)
		}
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
		"telemetry",
		"worktree",
	}
	got := registry.Names()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected registered commands:\n got: %v\nwant: %v", got, want)
	}
}

func TestRunReturnsTypedExitCodeFromCommand(t *testing.T) {
	prevRegistry, prevRegistered := registry, commandsRegistered
	registry = core.NewRegistry()
	commandsRegistered = true
	registry.Register("validate", stubCommand{err: core.NewExitCodeError(core.ExitCodeValidation, "validation failed")})
	t.Cleanup(func() {
		registry = prevRegistry
		commandsRegistered = prevRegistered
	})

	stderr := captureStderr(t, func() {
		code := Run([]string{"mono", "validate"})
		if code != core.ExitCodeValidation {
			t.Fatalf("expected exit code %d, got %d", core.ExitCodeValidation, code)
		}
	})
	if !strings.Contains(stderr, "validation failed") {
		t.Fatalf("expected validation failure output, got %q", stderr)
	}
	if strings.Contains(stderr, "Error:") {
		t.Fatalf("typed errors should not be prefixed with Error:, got %q", stderr)
	}
}

func TestRunReturnsGenericCodeForNormalError(t *testing.T) {
	prevRegistry, prevRegistered := registry, commandsRegistered
	registry = core.NewRegistry()
	commandsRegistered = true
	registry.Register("explode", stubCommand{err: errors.New("boom")})
	t.Cleanup(func() {
		registry = prevRegistry
		commandsRegistered = prevRegistered
	})

	stderr := captureStderr(t, func() {
		code := Run([]string{"mono", "explode"})
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
	})
	if !strings.Contains(stderr, "Error: boom") {
		t.Fatalf("expected generic error output, got %q", stderr)
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

type stubCommand struct {
	err error
}

func (s stubCommand) Run(_ []string) error {
	return s.err
}
