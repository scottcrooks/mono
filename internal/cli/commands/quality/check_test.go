package quality

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseCheckArgs(t *testing.T) {
	base, opts, err := parseCheckArgs([]string{"--base", "main", "--no-cache", "--concurrency", "3"})
	if err != nil {
		t.Fatalf("parseCheckArgs returned error: %v", err)
	}
	if base != "main" {
		t.Fatalf("unexpected base ref: %q", base)
	}
	if !opts.NoCache {
		t.Fatalf("expected NoCache=true")
	}
	if opts.Concurrency != 3 {
		t.Fatalf("unexpected concurrency: %d", opts.Concurrency)
	}
}

func TestParseCheckArgsRejectsUnknownArg(t *testing.T) {
	_, _, err := parseCheckArgs([]string{"api"})
	if err == nil {
		t.Fatalf("expected unknown argument error")
	}
}

func TestCheckCommandNoImpactedServices(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "HEAD"}); err != nil {
			t.Fatalf("check command returned error: %v", err)
		}
	})

	if !strings.Contains(stdout, "No impacted services. Nothing to check.") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestCheckCommandExecutesPhasesInOrder(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	type phaseCall struct {
		kind     string
		task     TaskName
		services []string
	}
	calls := make([]phaseCall, 0, 4)

	original := runCheckTaskPhase
	runCheckTaskPhase = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		calls = append(calls, phaseCall{
			kind:     "task",
			task:     req.Task,
			services: append([]string(nil), req.Services...),
		})
		return []TaskRunResult{}, nil
	}
	t.Cleanup(func() {
		runCheckTaskPhase = original
	})

	originalInstalls := runCheckDependencyInstalls
	runCheckDependencyInstalls = func(_ *Config, services []string) ([]DependencyInstallResult, error) {
		calls = append(calls, phaseCall{
			kind:     "deps",
			services: append([]string(nil), services...),
		})
		return nil, nil
	}
	t.Cleanup(func() {
		runCheckDependencyInstalls = originalInstalls
	})

	if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "main", "--concurrency", "1", "--no-cache"}); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	if len(calls) != 4 {
		t.Fatalf("expected 4 phases, got %d", len(calls))
	}
	want := []phaseCall{
		{kind: "deps", services: []string{"api", "lib"}},
		{kind: "task", task: TaskLint, services: []string{"api", "lib"}},
		{kind: "task", task: TaskTypecheck, services: []string{"api"}},
		{kind: "task", task: TaskTest, services: []string{"api", "lib"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected phase calls: got %+v want %+v", calls, want)
	}
}

func initCheckRepoWithFeatureChange(t *testing.T) string {
	t.Helper()

	repo := initTestGitRepo(t)
	writeFile(t, repo, "services.yaml", `services:
  - name: lib
    path: libs/lib
    description: Shared library
    kind: package
    archetype: go
  - name: api
    path: apps/api
    description: API service
    kind: service
    archetype: go
    depends: [lib]
`)
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n")
	writeFile(t, repo, filepath.Join("apps", "api", "api.go"), "package api\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial")

	gitRun(t, repo, "checkout", "-b", "feature/check")
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n\n// changed\n")
	gitRun(t, repo, "add", "libs/lib/lib.go")
	gitRun(t, repo, "commit", "-m", "change lib")

	return repo
}
