package tasks

import (
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
