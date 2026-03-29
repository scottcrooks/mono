package tasks

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunOrchestratedTaskUsesImpactedServicesByDefault(t *testing.T) {
	repo := initTaskGitRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	var got TaskRequest
	original := runTaskRequestWithConfig
	runTaskRequestWithConfig = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		got = req
		return nil, nil
	}
	t.Cleanup(func() {
		runTaskRequestWithConfig = original
	})

	if err := RunOrchestratedTask("lint", []string{"mono", "lint", "--base", "main"}); err != nil {
		t.Fatalf("RunOrchestratedTask returned error: %v", err)
	}

	want := TaskRequest{
		Task:          TaskLint,
		Services:      []string{"api", "lib", "web"},
		ExactServices: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected request: got %+v want %+v", got, want)
	}
}

func TestRunOrchestratedTaskAllOverridesImpactedSelection(t *testing.T) {
	repo := initTaskGitRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	var got TaskRequest
	original := runTaskRequestWithConfig
	runTaskRequestWithConfig = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		got = req
		return nil, nil
	}
	t.Cleanup(func() {
		runTaskRequestWithConfig = original
	})

	if err := RunOrchestratedTask("test", []string{"mono", "test", "--all", "--base", "HEAD"}); err != nil {
		t.Fatalf("RunOrchestratedTask returned error: %v", err)
	}

	want := TaskRequest{
		Task:          TaskTest,
		Services:      []string{"api", "lib", "web"},
		ExactServices: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected request: got %+v want %+v", got, want)
	}
}

func TestRunOrchestratedTaskNoImpactedServices(t *testing.T) {
	repo := initTaskGitRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	called := false
	original := runTaskRequestWithConfig
	runTaskRequestWithConfig = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() {
		runTaskRequestWithConfig = original
	})

	stdout := captureStdout(t, func() {
		if err := RunOrchestratedTask("typecheck", []string{"mono", "typecheck", "--base", "HEAD"}); err != nil {
			t.Fatalf("RunOrchestratedTask returned error: %v", err)
		}
	})

	if called {
		t.Fatalf("expected task request not to run")
	}
	if !strings.Contains(stdout, "No impacted services. Nothing to typecheck.") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}

func TestRunOrchestratedTaskRejectsAllWithExplicitServices(t *testing.T) {
	repo := initTaskGitRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	err := RunOrchestratedTask("lint", []string{"mono", "lint", "api", "--all"})
	if err == nil || !strings.Contains(err.Error(), "--all cannot be combined") {
		t.Fatalf("expected --all conflict error, got %v", err)
	}
}

func initTaskGitRepoWithFeatureChange(t *testing.T) string {
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
  - name: web
    path: apps/web
    description: Web app
    kind: service
    archetype: react
    depends: [api]
`)
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n")
	writeFile(t, repo, filepath.Join("libs", "lib", "go.mod"), "module lib\n\ngo 1.24\n")
	writeFile(t, repo, filepath.Join("apps", "api", "api.go"), "package api\n")
	writeFile(t, repo, filepath.Join("apps", "api", "go.mod"), "module api\n\ngo 1.24\n")
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), "{\n  \"scripts\": {\n    \"lint\": \"echo lint\",\n    \"typecheck\": \"echo typecheck\",\n    \"test\": \"echo test\"\n  }\n}\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial")

	gitRun(t, repo, "checkout", "-b", "feature/task-targets")
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n\n// changed\n")
	gitRun(t, repo, "add", "libs/lib/lib.go")
	gitRun(t, repo, "commit", "-m", "change lib")

	return repo
}
