package runtimecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestClassifyDevServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		services  []Service
		wantTUI   string
		wantOther int
		wantError []string
	}{
		{name: "no TUI", services: []Service{{Name: "api", Archetype: "go"}}, wantOther: 1},
		{name: "ts ink", services: []Service{{Name: "ui", Archetype: "ts-ink"}}, wantTUI: "ui"},
		{name: "go TUI", services: []Service{{Name: "ui", Archetype: "go-tui"}}, wantTUI: "ui"},
		{name: "mixed TUI pair", services: []Service{{Name: "ink", Archetype: "ts-ink"}, {Name: "go-ui", Archetype: "go-tui"}}, wantError: []string{"ink", "go-ui"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selection, err := classifyDevServices(tc.services)
			if len(tc.wantError) > 0 {
				if err == nil {
					t.Fatal("expected multiple-TUI error")
				}
				for _, name := range tc.wantError {
					if !strings.Contains(err.Error(), name) {
						t.Fatalf("error %q does not name %q", err, name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyDevServices returned error: %v", err)
			}
			if len(selection.background) != tc.wantOther {
				t.Fatalf("unexpected background services: %+v", selection.background)
			}
			if tc.wantTUI == "" && selection.tui != nil {
				t.Fatalf("unexpected TUI service: %+v", selection.tui)
			}
			if tc.wantTUI != "" && (selection.tui == nil || selection.tui.Name != tc.wantTUI) {
				t.Fatalf("unexpected TUI service: %+v", selection.tui)
			}
		})
	}
}

func TestSelectServicesForDevDeduplicatesTUIReferences(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "app", Kind: "service", Archetype: "react", Depends: []string{"terminal", "terminal"}, DevDepends: []string{"terminal"}},
		{Name: "terminal", Kind: "service", Archetype: "go-tui"},
	}}
	selected, err := selectServicesForDev(cfg, []string{"app", "app"})
	if err != nil {
		t.Fatalf("selectServicesForDev returned error: %v", err)
	}
	if len(selected) != 2 || selected[0].Name != "app" || selected[1].Name != "terminal" {
		t.Fatalf("unexpected deduplicated selection: %+v", selected)
	}
	classified, err := classifyDevServices(selected)
	if err != nil || classified.tui == nil || classified.tui.Name != "terminal" {
		t.Fatalf("unexpected classified selection: %+v, %v", classified, err)
	}
	explicit, err := selectServicesForDev(cfg, []string{"terminal"})
	if err != nil || len(explicit) != 1 || explicit[0].Name != "terminal" {
		t.Fatalf("unexpected explicit TUI selection: %+v, %v", explicit, err)
	}
}

func TestDefaultDevSelectionRejectsMultipleTUIs(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "ink", Kind: "service", Archetype: "ts-ink"},
		{Name: "terminal", Kind: "service", Archetype: "go-tui"},
		{Name: "api", Kind: "service", Archetype: "go"},
	}}
	selected, err := selectServicesForDev(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := classifyDevServices(selected); err == nil || !strings.Contains(err.Error(), "ink, terminal") {
		t.Fatalf("expected default selection to reject both TUIs by name, got %v", err)
	}
}

func TestPreflightTUIRejectsRedirectedStreamsIndependentOfColor(t *testing.T) {
	if !processGroupsSupported() {
		t.Skip("TUI takeover is unsupported on this platform")
	}
	stdin := openTestStream(t, "stdin")
	stdout := openTestStream(t, "stdout")
	stderr := openTestStream(t, "stderr")

	for _, colors := range []struct{ force, no string }{{"1", ""}, {"", "1"}, {"1", "1"}, {"", ""}} {
		t.Setenv("FORCE_COLOR", colors.force)
		t.Setenv("NO_COLOR", colors.no)
		if err := preflightTUI(stdin, stdout, stderr); err == nil || !strings.Contains(err.Error(), "terminal stdin") {
			t.Fatalf("expected redirected stdin rejection for FORCE_COLOR=%q NO_COLOR=%q, got %v", colors.force, colors.no, err)
		}
	}
}

func TestPreflightDevSelectionKeepsStreamingUsableWithRedirectedOutput(t *testing.T) {
	files := []*os.File{openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")}
	if err := preflightDevSelection(devSelection{background: []Service{{Name: "api"}}}, files[0], files[1], files[2]); err != nil {
		t.Fatalf("ordinary dev should allow redirected streams: %v", err)
	}
}

func TestRunTUIDevUsesDirectStreamsLogsBackgroundAndRestoresAfterReap(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"apps/backend", "apps/terminal"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stdin := openTestStream(t, "stdin")
	stdout := openTestStream(t, "stdout")
	stderr := openTestStream(t, "stderr")
	markers := filepath.Join(root, "markers")
	_ = os.Mkdir(markers, 0o700)
	backgroundReady := filepath.Join(markers, "background-ready")
	writeBackground := filepath.Join(markers, "write-background")
	backgroundWritten := filepath.Join(markers, "background-written")
	tuiActive := filepath.Join(markers, "tui-active")
	tuiExit := filepath.Join(markers, "tui-exit")
	tuiDone := filepath.Join(markers, "tui-done")
	commands := map[string]*exec.Cmd{}
	var commandsMu sync.Mutex
	restored := false
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		mode := parts[0]
		cmd := tuiHelperCommand(mode, map[string]string{
			"READY":   backgroundReady,
			"RELEASE": writeBackground,
			"WRITTEN": backgroundWritten,
			"ACTIVE":  tuiActive,
			"EXIT":    tuiExit,
			"DONE":    tuiDone,
		})
		commandsMu.Lock()
		commands[mode] = cmd
		commandsMu.Unlock()
		return cmd, nil
	})
	rt.snapshot = func(*os.File) (func() error, error) {
		return func() error {
			if _, err := os.Stat(tuiDone); err != nil {
				return fmt.Errorf("TUI was not reaped before terminal restoration: %w", err)
			}
			restored = true
			return nil
		}, nil
	}
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(
			[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
			Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
		)
	}()
	waitForPath(t, backgroundReady)
	waitForPath(t, tuiActive)
	commandsMu.Lock()
	tuiCommand := commands["tui"]
	backgroundCommand := commands["background"]
	commandsMu.Unlock()
	if tuiCommand.Stdin != stdin || tuiCommand.Stdout != stdout || tuiCommand.Stderr != stderr {
		t.Fatal("TUI command did not receive the exact parent stream files")
	}
	if backgroundCommand.Stdin != nil {
		t.Fatal("background command unexpectedly received terminal stdin")
	}

	writeMarker(t, writeBackground)
	waitForPath(t, backgroundWritten)
	transcript := readTestFile(t, stdout.Name())
	if strings.Contains(transcript, "out-fragment") || strings.Contains(transcript, "service backend") {
		t.Fatalf("background/session output reached the TUI terminal: %q", transcript)
	}
	writeMarker(t, tuiExit)
	if err := <-result; err != nil {
		t.Fatalf("runTUIDevWithRuntime returned error: %v", err)
	}
	if !restored {
		t.Fatal("terminal was not restored")
	}

	logs, err := filepath.Glob(filepath.Join(root, ".cache", "dev", "session-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected one retained session log, got %v, err %v", logs, err)
	}
	info, err := os.Stat(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected session log permissions: %o", info.Mode().Perm())
	}
	contents := readTestFile(t, logs[0])
	for _, expected := range []string{"[mono] session started", "[mono] starting background service backend", "[mono] starting TUI service terminal", "[backend] out-fragment\n", "[backend] err-fragment\n", "[mono] service backend exited"} {
		if !strings.Contains(contents, expected) {
			t.Errorf("session log missing %q: %q", expected, contents)
		}
	}
	if strings.Contains(contents, "\x1b[") {
		t.Fatalf("session log contains styled output: %q", contents)
	}
	transcript = readTestFile(t, stdout.Name())
	if !strings.Contains(transcript, "TUI ACTIVE") || !strings.Contains(transcript, "TUI DONE") {
		t.Fatalf("TUI output missing from terminal transcript: %q", transcript)
	}
	if strings.Count(transcript, "TUI session log:") != 2 {
		t.Fatalf("expected the retained log path before and after takeover: %q", transcript)
	}
}

func TestRunTUIDevRejectsLogSetupFailureBeforeStartingChildren(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	if err := os.WriteFile(filepath.Join(root, ".cache"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(root, "started")
	rt := testTUIRuntime(t, root, openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr"), func(context.Context, []string) (*exec.Cmd, error) {
		return tuiHelperCommand("background", map[string]string{"READY": started, "RELEASE": filepath.Join(root, "never-release")}), nil
	})
	err := runTUIDevWithRuntime([]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "bg"}}, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt)
	if err == nil || !strings.Contains(err.Error(), "create dev log directory") {
		t.Fatalf("expected log setup error, got %v", err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Fatal("service process started despite log setup failure")
	}
}

func TestRunTUIDevCancelsBeforeStartupWhenLogWriteFails(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	started := filepath.Join(root, "started")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(context.Context, []string) (*exec.Cmd, error) {
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": started}), nil
	})
	rt.createLog = func(string) (io.WriteCloser, string, error) {
		return failingWriteCloser{}, filepath.Join(root, "injected-session.log"), nil
	}
	err := runTUIDevWithRuntime(nil, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt)
	if err == nil || !strings.Contains(err.Error(), "write TUI session log") {
		t.Fatalf("expected runtime log write failure, got %v", err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Fatal("service process started despite initial log write failure")
	}
}

func TestCreateDevSessionLogUsesUniquePrivateFiles(t *testing.T) {
	root := t.TempDir()
	first, firstPath, err := createDevSessionLog(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, secondPath, err := createDevSessionLog(root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if firstPath == secondPath {
		t.Fatalf("session log paths are not unique: %s", firstPath)
	}
	for _, path := range []string{firstPath, secondPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected permissions for %s: %o", path, info.Mode().Perm())
		}
	}
}

func TestRunTUIDevCleansUpPartialStartupAndDoesNotStartTUI(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"apps/first", "apps/second", "apps/terminal"} {
		_ = os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	tuiStarted := filepath.Join(root, "tui-started")
	var mu sync.Mutex
	started := []string{}
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		mu.Lock()
		started = append(started, parts[0])
		mu.Unlock()
		if parts[0] == "fail-start" {
			return exec.Command(filepath.Join(root, "missing-executable")), nil
		}
		if parts[0] == "tui" {
			return tuiHelperCommand("tui", map[string]string{"ACTIVE": tuiStarted}), nil
		}
		return tuiHelperCommand("background", map[string]string{"RELEASE": filepath.Join(root, "never-release")}), nil
	})
	err := runTUIDevWithRuntime([]Service{
		{Name: "first", Path: "apps/first", Archetype: "go", Dev: "start"},
		{Name: "second", Path: "apps/second", Archetype: "go", Dev: "fail-start"},
	}, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt)
	if err == nil || !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("expected partial startup error, got %v", err)
	}
	want := []string{"start", "fail-start", "tui"}
	if !reflect.DeepEqual(started, want) {
		t.Fatalf("unexpected startup order: got %v want %v", started, want)
	}
	if _, err := os.Stat(tuiStarted); err == nil {
		t.Fatal("TUI process started after a background startup failure")
	}
}

func TestRunTUIDevRestoresTerminalAfterTUIFailure(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	done := filepath.Join(root, "done")
	restored := false
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, _ []string) (*exec.Cmd, error) {
		return tuiHelperCommand("fail", map[string]string{"DONE": done}), nil
	})
	rt.snapshot = func(*os.File) (func() error, error) {
		return func() error {
			if _, err := os.Stat(done); err != nil {
				return err
			}
			restored = true
			return errors.New("restore injected")
		}, nil
	}
	err := runTUIDevWithRuntime(nil, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "fail"}, rt)
	if err == nil || !strings.Contains(err.Error(), "exited with error") || !strings.Contains(err.Error(), "restore injected") {
		t.Fatalf("expected TUI and restore failures, got %v", err)
	}
	if !restored {
		t.Fatal("terminal was not restored after TUI failure")
	}
}

func TestRunTUIDevSignalStopsQuietlyAndRestoresTerminal(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, done := filepath.Join(root, "active"), filepath.Join(root, "done")
	signals := make(chan os.Signal, 1)
	restored := false
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, _ []string) (*exec.Cmd, error) {
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "DONE": done}), nil
	})
	rt.signals = signals
	rt.snapshot = func(*os.File) (func() error, error) {
		return func() error {
			if _, err := os.Stat(done); err != nil {
				return err
			}
			restored = true
			return nil
		}, nil
	}
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(nil, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt)
	}()
	waitForPath(t, active)
	signals <- syscall.SIGTERM
	if err := <-result; err != nil {
		t.Fatalf("expected quiet signal cleanup, got %v", err)
	}
	if !restored {
		t.Fatal("terminal was not restored after signal cleanup")
	}
}

func TestRunTUIDevTreatsForegroundInterruptAsExpectedExit(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, done := filepath.Join(root, "active"), filepath.Join(root, "done")
	pidPath := filepath.Join(root, "pid")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, _ []string) (*exec.Cmd, error) {
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "DONE": done, "SIGNAL_EXIT": "130", "PID": pidPath}), nil
	})
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(nil, Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt)
	}()
	waitForPath(t, active)
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("foreground interrupt was treated as a service failure: %v", err)
	}
	waitForPath(t, done)
}

func TestRunTUIDevBackgroundFailureStopsTUI(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, done := filepath.Join(root, "active"), filepath.Join(root, "done")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			return tuiHelperCommand("bg-fail", map[string]string{"ACTIVE": active}), nil
		}
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "DONE": done}), nil
	})
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(
			[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
			Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
		)
	}()
	if err := <-result; err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("expected background failure in result, got %v", err)
	}
	waitForPath(t, active)
	waitForPath(t, done)
}

func TestRunTUIDevTUIStartupFailureStopsBackground(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	ready := filepath.Join(root, "ready")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			return tuiHelperCommand("background", map[string]string{"READY": ready, "RELEASE": filepath.Join(root, "never-release")}), nil
		}
		return exec.Command(filepath.Join(root, "missing-tui")), nil
	})
	err := runTUIDevWithRuntime(
		[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
		Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
	)
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected TUI startup error, got %v", err)
	}
}

func TestRunTUIDevBackgroundZeroExitDoesNotEndTUISession(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, backgroundDone, tuiExit := filepath.Join(root, "active"), filepath.Join(root, "background-done"), filepath.Join(root, "tui-exit")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			return tuiHelperCommand("bg-zero", map[string]string{"DONE": backgroundDone}), nil
		}
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "EXIT": tuiExit}), nil
	})
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(
			[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
			Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
		)
	}()
	waitForPath(t, active)
	waitForPath(t, backgroundDone)
	select {
	case err := <-result:
		t.Fatalf("background zero exit ended the TUI session: %v", err)
	default:
	}
	writeMarker(t, tuiExit)
	if err := <-result; err != nil {
		t.Fatalf("runTUIDevWithRuntime returned error: %v", err)
	}
}

func TestTSInkUsesTUITakeoverPath(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	active, exit := filepath.Join(root, "active"), filepath.Join(root, "exit")
	rt := testTUIRuntime(t, root, openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr"), func(_ context.Context, _ []string) (*exec.Cmd, error) {
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "EXIT": exit}), nil
	})
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(nil, Service{Name: "ink", Path: "apps/terminal", Archetype: "ts-ink", Dev: "tui"}, rt)
	}()
	waitForPath(t, active)
	writeMarker(t, exit)
	if err := <-result; err != nil {
		t.Fatalf("ts-ink takeover returned error: %v", err)
	}
}

func TestRunTUIDevRuntimeLogWriteFailureCancelsServices(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	release, active, done := filepath.Join(root, "release"), filepath.Join(root, "active"), filepath.Join(root, "done")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			return tuiHelperCommand("bg-log-fail", map[string]string{"RELEASE": release}), nil
		}
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "DONE": done}), nil
	})
	rt.createLog = func(string) (io.WriteCloser, string, error) {
		return &failAfterWriter{remaining: 3}, filepath.Join(root, "injected-session.log"), nil
	}
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(
			[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
			Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
		)
	}()
	waitForPath(t, active)
	writeMarker(t, release)
	if err := <-result; err == nil || !strings.Contains(err.Error(), "write TUI session log") {
		t.Fatalf("expected runtime log write error, got %v", err)
	}
	waitForPath(t, done)
}

func TestRunTUIDevKillsBackgroundDescendantsAfterGracePeriod(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "apps/terminal"), 0o755)
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, exit, descendant := filepath.Join(root, "active"), filepath.Join(root, "exit"), filepath.Join(root, "descendant-pid")
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			return tuiHelperCommand("background-tree", map[string]string{"DESCENDANT": descendant}), nil
		}
		return tuiHelperCommand("tui", map[string]string{"ACTIVE": active, "EXIT": exit}), nil
	})
	started := time.Now()
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(
			[]Service{{Name: "backend", Path: "apps/backend", Archetype: "go", Dev: "background"}},
			Service{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui", Dev: "tui"}, rt,
		)
	}()
	waitForPath(t, descendant)
	waitForPath(t, active)
	writeMarker(t, exit)
	if err := <-result; err != nil {
		t.Fatalf("runTUIDevWithRuntime returned error: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 4*time.Second || elapsed > 8*time.Second {
		t.Fatalf("expected forced process-group cleanup near the five-second grace, took %s", elapsed)
	}
}

func TestSessionLogReportsWriteFailure(t *testing.T) {
	t.Parallel()

	logSink := newSessionLog(failingWriter{})
	if _, err := logSink.Write([]byte("fail")); err == nil {
		t.Fatal("expected log write error")
	}
	select {
	case err := <-logSink.Failures():
		if err == nil || !errors.Is(logSink.Err(), err) {
			t.Fatalf("unexpected reported log error: %v", err)
		}
	default:
		t.Fatal("expected log failure notification")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }

type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }
func (failingWriteCloser) Close() error              { return nil }

type failAfterWriter struct{ remaining int }

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.remaining == 0 {
		return 0, errors.New("injected delayed write failure")
	}
	w.remaining--
	return len(p), nil
}

func (*failAfterWriter) Close() error { return nil }

func testTUIRuntime(t *testing.T, root string, stdin, stdout, stderr *os.File, factory func(context.Context, []string) (*exec.Cmd, error)) devTUIRuntime {
	t.Helper()
	if !processGroupsSupported() {
		t.Skip("TUI takeover is unsupported on this platform")
	}
	return devTUIRuntime{
		stdin: stdin, stdout: stdout, stderr: stderr, root: root,
		isTerminal:     func(*os.File) bool { return true },
		snapshot:       func(*os.File) (func() error, error) { return func() error { return nil }, nil },
		commandFactory: factory,
	}
}

func tuiHelperCommand(mode string, markers map[string]string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDevTUIHelperProcess$")
	cmd.Env = append(os.Environ(), "MONO_TUI_HELPER=1", "MONO_TUI_MODE="+mode)
	for name, path := range markers {
		cmd.Env = append(cmd.Env, "MONO_TUI_"+name+"="+path)
	}
	return cmd
}

func TestDevTUIHelperProcess(t *testing.T) {
	if os.Getenv("MONO_TUI_HELPER") != "1" {
		return
	}
	if report := os.Getenv("MONO_TUI_REPORT"); report != "" {
		values := map[string]string{}
		for _, entry := range os.Environ() {
			key, value, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, "MONO_DEV_FIXTURE_") {
				values[key] = value
			}
		}
		values["cwd"], _ = os.Getwd()
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(report, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mode := os.Getenv("MONO_TUI_MODE")
	if mode == "fail" {
		writeMarker(t, os.Getenv("MONO_TUI_DONE"))
		os.Exit(17)
	}
	if mode == "background-once" {
		_, _ = fmt.Fprint(os.Stdout, "out-line\nout-fragment")
		_, _ = fmt.Fprint(os.Stderr, "err-line\nerr-fragment")
		os.Exit(0)
	}
	if mode == "background" {
		writeMarker(t, os.Getenv("MONO_TUI_READY"))
		wait := make(chan os.Signal, 1)
		signal.Notify(wait, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(wait)
		for {
			select {
			case <-wait:
				os.Exit(0)
			default:
			}
			if _, err := os.Stat(os.Getenv("MONO_TUI_RELEASE")); err == nil {
				if os.Getenv("MONO_TUI_LOG_LINES") == "1" {
					_, _ = fmt.Fprint(os.Stdout, "out-line\n")
					_, _ = fmt.Fprint(os.Stderr, "err-line\n")
				}
				_, _ = fmt.Fprint(os.Stdout, "out-fragment")
				_, _ = fmt.Fprint(os.Stderr, "err-fragment")
				_ = os.Remove(os.Getenv("MONO_TUI_RELEASE"))
				writeMarker(t, os.Getenv("MONO_TUI_WRITTEN"))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if mode == "background-tree" {
		child := exec.Command(os.Args[0], "-test.run=^TestDevTUIHelperProcess$")
		child.Env = append(os.Environ(), "MONO_TUI_HELPER=1", "MONO_TUI_MODE=ignore", "MONO_TUI_DESCENDANT="+os.Getenv("MONO_TUI_DESCENDANT"))
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		waitForPath(t, os.Getenv("MONO_TUI_DESCENDANT"))
		wait := make(chan os.Signal, 1)
		signal.Notify(wait, os.Interrupt)
		<-wait
		os.Exit(0)
	}
	if mode == "ignore" {
		if err := os.WriteFile(os.Getenv("MONO_TUI_DESCENDANT"), []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "bg-fail" {
		waitForPath(t, os.Getenv("MONO_TUI_ACTIVE"))
		os.Exit(11)
	}
	if mode == "bg-zero" {
		writeMarker(t, os.Getenv("MONO_TUI_DONE"))
		os.Exit(0)
	}
	if mode == "bg-log-fail" {
		wait := make(chan os.Signal, 1)
		signal.Notify(wait, os.Interrupt)
		defer signal.Stop(wait)
		for {
			select {
			case <-wait:
				os.Exit(0)
			default:
			}
			if _, err := os.Stat(os.Getenv("MONO_TUI_RELEASE")); err == nil {
				_, _ = fmt.Fprintln(os.Stdout, "trigger-log-failure")
				for {
					time.Sleep(time.Hour)
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if mode == "tui" {
		_, _ = fmt.Fprintln(os.Stdout, "TUI ACTIVE")
		wait := make(chan os.Signal, 1)
		signal.Notify(wait, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(wait)
		if path := os.Getenv("MONO_TUI_PID"); path != "" {
			if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				t.Fatal(err)
			}
		}
		writeMarker(t, os.Getenv("MONO_TUI_ACTIVE"))
		for {
			select {
			case <-wait:
				writeMarker(t, os.Getenv("MONO_TUI_DONE"))
				if os.Getenv("MONO_TUI_SIGNAL_EXIT") != "" {
					os.Exit(130)
				}
				os.Exit(0)
			default:
			}
			if _, err := os.Stat(os.Getenv("MONO_TUI_EXIT")); err == nil {
				_, _ = fmt.Fprintln(os.Stdout, "TUI DONE")
				writeMarker(t, os.Getenv("MONO_TUI_DONE"))
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if mode == "fail-start" {
		os.Exit(0)
	}
	os.Exit(0)
}

func openTestStream(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), name+"-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func writeMarker(t *testing.T, path string) {
	t.Helper()
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := os.WriteFile(path, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunTUIDevEnvironment(t *testing.T) {
	root, services := devEnvironmentFixture(t)
	before := os.Environ()
	services[0].Dev = "background"
	services[1].Dev = "tui"
	services[1].Archetype = "go-tui"
	exit := filepath.Join(root, "exit")
	done := filepath.Join(root, "done")
	reports := map[string]string{"background": filepath.Join(root, "background.json"), "tui": filepath.Join(root, "tui.json")}
	rt := testTUIRuntime(t, root, openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr"), func(_ context.Context, parts []string) (*exec.Cmd, error) {
		return tuiHelperCommand(parts[0], map[string]string{"REPORT": reports[parts[0]], "EXIT": exit, "DONE": done}), nil
	})
	result := make(chan error, 1)
	go func() { result <- runTUIDevWithRuntime(services[:1], services[1], rt) }()
	for _, path := range reports {
		waitForPath(t, path)
	}
	writeMarker(t, exit)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	for i, mode := range []string{"background", "tui"} {
		assertDevEnvironmentReport(t, reports[mode], filepath.Join(root, services[i].Path), services[i].Name)
	}
	if !reflect.DeepEqual(before, os.Environ()) {
		t.Fatal("parent environment changed")
	}
}

func TestRunTUIDevDotenvFailurePreventsAllStarts(t *testing.T) {
	for _, badIndex := range []int{0, 1} {
		t.Run(fmt.Sprint(badIndex), func(t *testing.T) {
			root := t.TempDir()
			services := []Service{{Name: "backend", Path: "backend", Dev: "background"}, {Name: "terminal", Path: "terminal", Dev: "tui", Archetype: "go-tui"}}
			for _, svc := range services {
				writeDevEnv(t, filepath.Join(root, svc.Path), "VALID=yes")
			}
			path := filepath.Join(root, services[badIndex].Path, ".env")
			writeDevEnv(t, filepath.Dir(path), "BAD!KEY=SECRET_SENTINEL")
			var commands []*exec.Cmd
			rt := testTUIRuntime(t, root, openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr"), func(_ context.Context, parts []string) (*exec.Cmd, error) {
				cmd := tuiHelperCommand(parts[0], nil)
				commands = append(commands, cmd)
				return cmd, nil
			})
			restored := false
			rt.snapshot = func(*os.File) (func() error, error) { return func() error { restored = true; return nil }, nil }
			err := runTUIDevWithRuntime(services[:1], services[1], rt)
			if err == nil {
				t.Fatal("expected startup failure")
			}
			for _, part := range []string{"[" + services[badIndex].Name + "]", path, "invalid dotenv"} {
				if !strings.Contains(err.Error(), part) {
					t.Fatalf("missing %q in %v", part, err)
				}
			}
			if strings.Contains(err.Error(), "SECRET_SENTINEL") {
				t.Fatal("secret leaked")
			}
			for _, cmd := range commands {
				if cmd.Process != nil {
					t.Fatal("child started despite preparation failure")
				}
			}
			if !restored {
				t.Fatal("terminal not restored")
			}
		})
	}
}

func TestRunTUIDevLokiCapturesBackgroundAndRestoresTerminal(t *testing.T) {
	if !processGroupsSupported() {
		t.Skip("TUI takeover is unsupported")
	}
	receiver := &lokiReceiver{values: make(map[string][]string)}
	server := httptest.NewServer(http.HandlerFunc(receiver.serve))
	defer server.Close()
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", "terminal"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	active, exit, done := filepath.Join(root, "active"), filepath.Join(root, "exit"), filepath.Join(root, "done")
	signals := make(chan os.Signal, 1)
	cmds := make(map[string]*exec.Cmd)
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		name := parts[0]
		markers := map[string]string{"ACTIVE": active, "EXIT": exit, "DONE": done}
		mode := "tui"
		if name != "terminal" {
			mode = "background"
			markers = map[string]string{"READY": filepath.Join(root, name+"-ready"), "RELEASE": filepath.Join(root, name+"-release"), "WRITTEN": filepath.Join(root, name+"-written"), "LOG_LINES": "1"}
			if name == "beta" {
				mode = "background-once"
			}
		}
		cmd := tuiHelperCommand(mode, markers)
		cmds[name] = cmd
		return cmd, nil
	})
	rt.lokiEndpoint = server.URL
	rt.signals = signals
	rt.createLog = func(string) (io.WriteCloser, string, error) {
		t.Error("created session log in telemetry mode")
		return nil, "", errors.New("unexpected log creation")
	}
	restored := false
	rt.snapshot = func(*os.File) (func() error, error) {
		return func() error {
			if _, err := os.Stat(done); err != nil {
				return err
			}
			restored = true
			return nil
		}, nil
	}
	background := []Service{{Name: "alpha", Path: "alpha", Dev: "alpha"}, {Name: "beta", Path: "beta", Dev: "beta"}}
	result := make(chan error, 1)
	go func() {
		result <- runTUIDevWithRuntime(background, Service{Name: "terminal", Path: "terminal", Archetype: "go-tui", Dev: "terminal"}, rt)
	}()
	waitForPath(t, active)
	for _, name := range []string{"alpha"} {
		waitForPath(t, filepath.Join(root, name+"-ready"))
		writeMarker(t, filepath.Join(root, name+"-release"))
		waitForPath(t, filepath.Join(root, name+"-written"))
	}
	// A trailing fragment is pushed only after Wait, so observe it before
	// shutdown to exercise both normal exit and cleanup flushing.
	deadline := time.Now().Add(5 * time.Second)
	for {
		receiver.mu.Lock()
		count := len(receiver.values["beta"])
		receiver.mu.Unlock()
		if count == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background service did not exit and flush")
		}
		time.Sleep(10 * time.Millisecond)
	}
	signals <- syscall.SIGTERM
	if err := <-result; err != nil {
		t.Fatalf("session shutdown: %v", err)
	}
	if !restored {
		t.Fatal("terminal was not restored")
	}
	if cmds["terminal"].Stdin != stdin || cmds["terminal"].Stdout != stdout || cmds["terminal"].Stderr != stderr {
		t.Fatal("foreground streams not passed directly")
	}
	for _, name := range []string{"alpha", "beta", "terminal"} {
		if cmds[name].ProcessState == nil {
			t.Errorf("%s not reaped", name)
		}
	}
	receiver.assert(t, map[string][]string{"alpha": {"out-line", "err-line", "out-fragment", "err-fragment"}, "beta": {"out-line", "err-line", "out-fragment", "err-fragment"}})
	transcript := readTestFile(t, stdout.Name()) + readTestFile(t, stderr.Name())
	if !strings.Contains(transcript, "TUI ACTIVE") || strings.Contains(transcript, "TUI session log") || strings.Contains(transcript, "out-fragment") || strings.Contains(transcript, "err-fragment") {
		t.Fatalf("unexpected terminal output: %q", transcript)
	}
	if _, err := os.Stat(filepath.Join(root, ".cache", "dev")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session log directory created: %v", err)
	}
}

func TestRunTUIDevLokiStartupFailureCleansBackground(t *testing.T) {
	if !processGroupsSupported() {
		t.Skip("TUI takeover is unsupported")
	}
	receiver := &lokiReceiver{values: make(map[string][]string)}
	server := httptest.NewServer(http.HandlerFunc(receiver.serve))
	defer server.Close()
	root := t.TempDir()
	for _, name := range []string{"background", "terminal"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	stdin, stdout, stderr := openTestStream(t, "stdin"), openTestStream(t, "stdout"), openTestStream(t, "stderr")
	var background *exec.Cmd
	rt := testTUIRuntime(t, root, stdin, stdout, stderr, func(_ context.Context, parts []string) (*exec.Cmd, error) {
		if parts[0] == "background" {
			background = tuiHelperCommand("background", map[string]string{"READY": filepath.Join(root, "ready"), "RELEASE": filepath.Join(root, "never")})
			return background, nil
		}
		return exec.Command(filepath.Join(root, "missing")), nil
	})
	rt.lokiEndpoint = server.URL
	rt.createLog = func(string) (io.WriteCloser, string, error) {
		t.Error("created session log")
		return nil, "", errors.New("unexpected log creation")
	}
	restored := false
	rt.snapshot = func(*os.File) (func() error, error) { return func() error { restored = true; return nil }, nil }
	err := runTUIDevWithRuntime([]Service{{Name: "backend", Path: "background", Dev: "background"}}, Service{Name: "terminal", Path: "terminal", Archetype: "go-tui", Dev: "terminal"}, rt)
	if err == nil || !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("expected startup failure: %v", err)
	}
	if !restored || background == nil || background.ProcessState == nil {
		t.Fatal("terminal or background cleanup incomplete")
	}
	if strings.Contains(readTestFile(t, stdout.Name()), "TUI session log") {
		t.Fatal("reported a session log")
	}
	receiver.assert(t, map[string][]string{})
}
