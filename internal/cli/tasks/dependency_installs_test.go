package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func TestExecuteDependencyInstallCommandChildEnvironment(t *testing.T) {
	for _, ci := range []struct{ name, value, want string }{{"absent", "__unset__", "true"}, {"empty", "", ""}, {"false", "false", "false"}} {
		t.Run(ci.name, func(t *testing.T) {
			bin := t.TempDir()
			capture := filepath.Join(t.TempDir(), "env")
			writeFile(t, bin, "pnpm", "#!/bin/sh\nprintf '%s|%s' \"${CI-unset}\" \"$MARKER\" > \"$CAPTURE\"\n")
			if err := os.Chmod(filepath.Join(bin, "pnpm"), 0o755); err != nil {
				t.Fatal(err)
			}
			oldPath := os.Getenv("PATH")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+oldPath)
			t.Setenv("MARKER", "preserved")
			if ci.value == "__unset__" {
				_ = os.Unsetenv("CI")
				t.Cleanup(func() { _ = os.Setenv("CI", ci.want) })
			} else {
				t.Setenv("CI", ci.value)
			}
			t.Setenv("CAPTURE", capture)
			parentCI, parentPresent := os.LookupEnv("CI")
			err := executeDependencyInstallCommand(context.Background(), DependencyInstallTarget{Archetype: "ts-node", Dir: bin, Command: "pnpm install"}, io.Discard, io.Discard)
			if err != nil {
				t.Fatalf("install failed: %v", err)
			}
			got, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != ci.want+"|preserved" {
				t.Fatalf("child env = %q", got)
			}
			afterCI, afterPresent := os.LookupEnv("CI")
			if afterCI != parentCI || afterPresent != parentPresent {
				t.Fatalf("parent CI changed: before %q/%v after %q/%v", parentCI, parentPresent, afterCI, afterPresent)
			}
		})
	}
	t.Run("failure", func(t *testing.T) {
		bin := t.TempDir()
		writeFile(t, bin, "pnpm", "#!/bin/sh\nexit 17\n")
		_ = os.Chmod(filepath.Join(bin, "pnpm"), 0o755)
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		err := executeDependencyInstallCommand(context.Background(), DependencyInstallTarget{Dir: bin, Command: "pnpm install"}, io.Discard, io.Discard)
		if err == nil {
			t.Fatal("expected install failure")
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("failure not propagated: %v", err)
		}
	})
}

func TestDependencyInstallTargetsForServicesDedupesByResolvedRoot(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "go.mod", "module example.com/mono\n")
	writeFile(t, repo, "pnpm-workspace.yaml", "packages:\n  - apps/*\n")
	writeFile(t, repo, "package.json", `{"name":"repo-root","private":true}`)
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{"name":"web"}`)
	writeFile(t, repo, filepath.Join("apps", "admin", "package.json"), `{"name":"admin"}`)

	targets, err := DependencyInstallTargetsForServices(&Config{
		Services: []Service{
			{Name: "api", Path: "apps/api", Archetype: "go"},
			{Name: "worker", Path: "apps/worker", Archetype: "go"},
			{Name: "web", Path: "apps/web", Archetype: "react"},
			{Name: "admin", Path: "apps/admin", Archetype: "react"},
		},
	}, []string{"api", "worker", "web", "admin"})
	if err != nil {
		t.Fatalf("DependencyInstallTargetsForServices returned error: %v", err)
	}

	want := []DependencyInstallTarget{
		{Archetype: "go", Dir: ".", Command: "go mod download", Services: []string{"api", "worker"}},
		{Archetype: "react", Dir: ".", Command: "pnpm install", Services: []string{"admin", "web"}},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets: got %+v want %+v", targets, want)
	}
}

func TestDependencyInstallTargetsDedupesGoAndGoTUI(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	writeFile(t, repo, "go.mod", "module example.com/mono\n")

	targets, err := DependencyInstallTargetsForServices(&Config{
		Services: []Service{
			{Name: "api", Path: "apps/api", Archetype: "go"},
			{Name: "terminal", Path: "apps/terminal", Archetype: "go-tui"},
		},
	}, []string{"api", "terminal"})
	if err != nil {
		t.Fatalf("DependencyInstallTargetsForServices returned error: %v", err)
	}
	want := []DependencyInstallTarget{{
		Archetype: "go",
		Dir:       ".",
		Command:   "go mod download",
		Services:  []string{"api", "terminal"},
	}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected shared Go install target: got %+v want %+v", targets, want)
	}
}

func TestDependencyInstallTargetsForServicesDoesNotShareReactWithoutWorkspace(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{"name":"web"}`)
	writeFile(t, repo, filepath.Join("apps", "admin", "package.json"), `{"name":"admin"}`)
	writeFile(t, repo, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")

	targets, err := DependencyInstallTargetsForServices(&Config{
		Services: []Service{
			{Name: "web", Path: "apps/web", Archetype: "react"},
			{Name: "admin", Path: "apps/admin", Archetype: "react"},
		},
	}, []string{"web", "admin"})
	if err != nil {
		t.Fatalf("DependencyInstallTargetsForServices returned error: %v", err)
	}

	want := []DependencyInstallTarget{
		{Archetype: "react", Dir: "apps/admin", Command: "pnpm install", Services: []string{"admin"}},
		{Archetype: "react", Dir: "apps/web", Command: "pnpm install", Services: []string{"web"}},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets without workspace: got %+v want %+v", targets, want)
	}
}

func TestDependencyInstallTargetsForServicesFallsBackWhenWorkspaceExcludesService(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "pnpm-workspace.yaml", "packages:\n  - apps/other/*\n")
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{"name":"web"}`)

	targets, err := DependencyInstallTargetsForServices(&Config{
		Services: []Service{
			{Name: "web", Path: "apps/web", Archetype: "react"},
		},
	}, []string{"web"})
	if err != nil {
		t.Fatalf("DependencyInstallTargetsForServices returned error: %v", err)
	}

	want := []DependencyInstallTarget{
		{Archetype: "react", Dir: "apps/web", Command: "pnpm install", Services: []string{"web"}},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets when workspace excludes service: got %+v want %+v", targets, want)
	}
}

func TestDependencyInstallTargetsForServicesDedupesPnpmAcrossArchetypes(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "pnpm-workspace.yaml", "packages:\n  - apps/*\n  - packages/*\n")
	writeFile(t, repo, "package.json", `{"name":"repo-root","private":true}`)
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{"name":"web"}`)
	writeFile(t, repo, filepath.Join("apps", "daemon", "package.json"), `{"name":"daemon"}`)
	writeFile(t, repo, filepath.Join("packages", "core", "package.json"), `{"name":"core"}`)

	targets, err := DependencyInstallTargetsForServices(&Config{
		Services: []Service{
			{Name: "web", Path: "apps/web", Archetype: "react"},
			{Name: "daemon", Path: "apps/daemon", Archetype: "ts-node"},
			{Name: "core", Path: "packages/core", Archetype: "ts-lib"},
		},
	}, []string{"web", "daemon", "core"})
	if err != nil {
		t.Fatalf("DependencyInstallTargetsForServices returned error: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected a single pnpm install target, got %+v", targets)
	}
	if targets[0].Dir != "." || targets[0].Command != "pnpm install" {
		t.Fatalf("unexpected mixed target location/command: %+v", targets[0])
	}
	if !reflect.DeepEqual(targets[0].Services, []string{"core", "daemon", "web"}) {
		t.Fatalf("unexpected mixed target services: %+v", targets[0].Services)
	}
}

func TestRunDependencyInstallsWithConfigExecutesResolvedTargets(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "go.mod", "module example.com/mono\n")
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{"name":"web"}`)

	original := runDependencyInstallCommand
	calls := make([]DependencyInstallTarget, 0, 2)
	runDependencyInstallCommand = func(_ context.Context, target DependencyInstallTarget, _, _ io.Writer) error {
		calls = append(calls, target)
		return nil
	}
	t.Cleanup(func() {
		runDependencyInstallCommand = original
	})

	results, err := RunDependencyInstallsWithConfig(&Config{
		Services: []Service{
			{Name: "api", Path: "apps/api", Archetype: "go"},
			{Name: "web", Path: "apps/web", Archetype: "react"},
		},
	}, []string{"api", "web"})
	if err != nil {
		t.Fatalf("RunDependencyInstallsWithConfig returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !reflect.DeepEqual(calls[0], DependencyInstallTarget{
		Archetype: "go",
		Dir:       ".",
		Command:   "go mod download",
		Services:  []string{"api"},
	}) {
		t.Fatalf("unexpected first call: %+v", calls[0])
	}
	if !reflect.DeepEqual(calls[1], DependencyInstallTarget{
		Archetype: "react",
		Dir:       "apps/web",
		Command:   "pnpm install",
		Services:  []string{"web"},
	}) {
		t.Fatalf("unexpected second call: %+v", calls[1])
	}
}

func TestRunDependencyInstallsWithConfigBufferedStaysQuietOnSuccess(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "go.mod", "module example.com/mono\n")

	stdout, stderr := captureStreams(t, func() {
		results, err := RunDependencyInstallsWithConfigBuffered(&Config{
			Services: []Service{
				{Name: "api", Path: "apps/api", Archetype: "go"},
			},
		}, []string{"api"})
		if err != nil {
			t.Fatalf("RunDependencyInstallsWithConfigBuffered returned error: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected quiet stdout, got %q", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected quiet stderr, got %q", stderr)
	}
}

func TestRunDependencyInstallsReturnsContextualFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	writeFile(t, repo, "apps/api/go.mod", "module example.com/api\n")
	cause := errors.New("download unavailable")
	original := runDependencyInstallCommand
	runDependencyInstallCommand = func(_ context.Context, _ DependencyInstallTarget, _, _ io.Writer) error { return cause }
	t.Cleanup(func() { runDependencyInstallCommand = original })

	var results []DependencyInstallResult
	_, _ = captureStreams(t, func() {
		var err error
		results, err = RunDependencyInstallsWithConfigBuffered(&Config{Services: []Service{{Name: "api", Path: "apps/api", Archetype: "go"}}}, []string{"api"})
		if err == nil {
			t.Fatal("expected dependency install error")
		}
		for _, part := range []string{"1 dependency install target(s) failed", "go target", "apps/api", "go mod download", "download unavailable"} {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("returned error %q does not contain %q", err, part)
			}
		}
		if !errors.Is(err, cause) {
			t.Errorf("returned error does not preserve cause: %v", err)
		}
	})
	if len(results) != 1 || results[0].Status != TaskStatusFailed || !errors.Is(results[0].Err, cause) {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestRunDependencyInstallsReturnsEveryFailureAndSuccessfulStatus(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	writeFile(t, repo, "apps/api/go.mod", "module example.com/api\n")
	writeFile(t, repo, "apps/worker/go.mod", "module example.com/worker\n")
	writeFile(t, repo, "apps/web/package.json", "{}\n")
	apiCause := errors.New("api download failed")
	workerCause := errors.New("worker download failed")
	original := runDependencyInstallCommand
	runDependencyInstallCommand = func(_ context.Context, target DependencyInstallTarget, _, _ io.Writer) error {
		switch target.Dir {
		case "apps/api":
			return apiCause
		case "apps/worker":
			return workerCause
		default:
			return nil
		}
	}
	t.Cleanup(func() { runDependencyInstallCommand = original })

	var results []DependencyInstallResult
	_, _ = captureStreams(t, func() {
		var err error
		results, err = RunDependencyInstallsWithConfigBuffered(&Config{Services: []Service{
			{Name: "api", Path: "apps/api", Archetype: "go"},
			{Name: "worker", Path: "apps/worker", Archetype: "go"},
			{Name: "web", Path: "apps/web", Archetype: "react"},
		}}, []string{"api", "worker", "web"})
		if err == nil {
			t.Fatal("expected dependency install error")
		}
		for _, part := range []string{"2 dependency install target(s) failed", "apps/api", "api download failed", "apps/worker", "worker download failed"} {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("returned error %q does not contain %q", err, part)
			}
		}
		if !errors.Is(err, apiCause) || !errors.Is(err, workerCause) {
			t.Errorf("returned error does not preserve both causes: %v", err)
		}
	})
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(results), results)
	}
	statuses := map[string]TaskRunStatus{}
	for _, result := range results {
		statuses[result.Target.Dir] = result.Status
	}
	if statuses["apps/api"] != TaskStatusFailed || statuses["apps/worker"] != TaskStatusFailed || statuses["apps/web"] != TaskStatusSucceeded {
		t.Fatalf("unexpected result statuses: %+v", statuses)
	}
}

func TestRunDependencyInstallsWithConfigBufferedReplaysFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, "go.mod", "module example.com/mono\n")

	original := runDependencyInstallCommand
	runDependencyInstallCommand = func(_ context.Context, target DependencyInstallTarget, stdoutWriter, stderrWriter io.Writer) error {
		if stdoutWriter != nil {
			_, _ = stdoutWriter.Write([]byte("[deps:" + target.Archetype + "] install-stdout\n"))
		}
		if stderrWriter != nil {
			_, _ = stderrWriter.Write([]byte("[deps:" + target.Archetype + "] install-stderr\n"))
		}
		return io.EOF
	}
	t.Cleanup(func() {
		runDependencyInstallCommand = original
	})

	stdout, stderr := captureStreams(t, func() {
		results, err := RunDependencyInstallsWithConfigBuffered(&Config{
			Services: []Service{
				{Name: "api", Path: "apps/api", Archetype: "go"},
			},
		}, []string{"api"})
		if err == nil {
			t.Fatalf("expected RunDependencyInstallsWithConfigBuffered error")
		}
		if len(results) != 1 || results[0].Status != TaskStatusFailed {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if !strings.Contains(stdout, "[deps:go] install-stdout") {
		t.Fatalf("expected replayed stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "[deps:go] install-stderr") {
		t.Fatalf("expected replayed stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "[deps:go] failed") {
		t.Fatalf("expected failure line, got %q", stderr)
	}
}

func TestRunDependencyInstallsProgressClearsForFailureReplay(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	writeFile(t, repo, "go.mod", "module example.com/mono\n")
	original := runDependencyInstallCommand
	runDependencyInstallCommand = func(_ context.Context, _ DependencyInstallTarget, stdoutWriter, stderrWriter io.Writer) error {
		_, _ = io.WriteString(stdoutWriter, "install stdout transcript\n")
		_, _ = io.WriteString(stderrWriter, "install stderr transcript\n")
		return io.EOF
	}
	t.Cleanup(func() { runDependencyInstallCommand = original })
	var progressOutput bytes.Buffer
	line := output.NewProgressLine(output.ProgressOptions{Writer: &progressOutput, Mode: output.ModeInteractive, IsTTY: func() bool { return true }, Term: func() string { return "xterm" }, Width: func() (int, error) { return 80, nil }, TickInterval: time.Hour})
	line.Update(output.ProgressView{Kind: output.ProgressInstall})
	stdout, stderr := captureStreams(t, func() {
		results, err := RunDependencyInstallsWithConfigBufferedProgress(&Config{Services: []Service{{Name: "api", Path: "apps/api", Archetype: "go"}}}, []string{"api"}, line)
		if err == nil || len(results) != 1 || results[0].Status != TaskStatusFailed {
			t.Fatalf("unexpected install result: %+v, %v", results, err)
		}
	})
	line.Close()
	if !strings.Contains(stdout, "install stdout transcript") || !strings.Contains(stderr, "install stderr transcript") {
		t.Fatalf("install transcript missing: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(progressOutput.String(), "\x1b[2K") {
		t.Fatalf("install progress line was not cleared around replay: %q", progressOutput.String())
	}
	if !strings.HasSuffix(progressOutput.String(), "\n") {
		t.Fatalf("progress line was not terminated on close: %q", progressOutput.String())
	}
}
