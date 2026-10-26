package quality

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/tasks"
)

func TestParseCheckArgs(t *testing.T) {
	base, all, opts, err := parseCheckArgs([]string{"--base", "main", "--all", "--no-cache", "--concurrency", "3"})
	if err != nil {
		t.Fatalf("parseCheckArgs returned error: %v", err)
	}
	if base != "main" {
		t.Fatalf("unexpected base ref: %q", base)
	}
	if !all {
		t.Fatalf("expected all=true")
	}
	if !opts.NoCache {
		t.Fatalf("expected NoCache=true")
	}
	if opts.Concurrency != 3 {
		t.Fatalf("unexpected concurrency: %d", opts.Concurrency)
	}
	if !opts.BufferOutput {
		t.Fatalf("expected BufferOutput=true by default")
	}
	if !opts.ContinueOnFailure {
		t.Fatalf("expected ContinueOnFailure=true by default")
	}
}

func TestParseCheckArgsNoBuffer(t *testing.T) {
	_, _, opts, err := parseCheckArgs([]string{"--no-buffer"})
	if err != nil {
		t.Fatalf("parseCheckArgs returned error: %v", err)
	}
	if opts.BufferOutput {
		t.Fatalf("expected BufferOutput=false")
	}
}

func TestParseCheckArgsRejectsUnknownArg(t *testing.T) {
	_, _, _, err := parseCheckArgs([]string{"api"})
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

func TestCheckCommandBuildsFixPhaseFirst(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	plan := buildPendingCheckPlan(cfg, []string{"lib", "api"})
	if len(plan.Phases) != 5 {
		t.Fatalf("expected 5 phases, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Task != TaskFix {
		t.Fatalf("expected fix to be planned first, got %+v", plan.Phases[0])
	}
}

func TestCheckCommandExecutesPhasesInOrder(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	type phaseCall struct {
		kind        string
		task        TaskName
		services    []string
		fileTargets map[string][]string
	}
	calls := make([]phaseCall, 0, 6)

	original := runCheckTaskPhase
	runCheckTaskPhase = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		calls = append(calls, phaseCall{
			kind:        "task",
			task:        req.Task,
			services:    append([]string(nil), req.Services...),
			fileTargets: req.FileTargets,
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

	if len(calls) != 6 {
		t.Fatalf("expected dependency installation plus 5 task phases, got %d calls", len(calls))
	}
	want := []phaseCall{
		{kind: "deps", services: []string{"api", "lib"}},
		{kind: "task", task: TaskFix, services: []string{"api", "lib"}},
		{kind: "task", task: TaskFormat, services: []string{"api", "lib"}, fileTargets: map[string][]string{"lib": []string{"lib.go"}}},
		{kind: "task", task: TaskLint, services: []string{"api", "lib"}},
		{kind: "task", task: TaskTypecheck, services: []string{"api"}},
		{kind: "task", task: TaskTest, services: []string{"api", "lib"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected phase calls: got %+v want %+v", calls, want)
	}
}

func TestCheckPreparesAllPhasesBeforeUsingOneProgressSession(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)
	originalInstalls := runCheckDependencyInstalls
	runCheckDependencyInstalls = func(_ *Config, _ []string) ([]DependencyInstallResult, error) { return nil, nil }
	t.Cleanup(func() { runCheckDependencyInstalls = originalInstalls })
	originalPrepare := prepareCheckTaskRun
	prepareCount := 0
	prepareCheckTaskRun = func(cfg *Config, req TaskRequest) (*tasks.PreparedTaskRun, error) {
		prepareCount++
		return originalPrepare(cfg, req)
	}
	t.Cleanup(func() { prepareCheckTaskRun = originalPrepare })
	originalPhase := runCheckTaskPhase
	var shared *tasks.TaskProgressSession
	lastElapsed := time.Duration(0)
	phaseCount := 0
	runCheckTaskPhase = func(_ *Config, req TaskRequest, opts TaskRunOptions) ([]TaskRunResult, error) {
		phaseCount++
		if prepareCount != 5 {
			t.Fatalf("first task started before all phases were prepared: %d", prepareCount)
		}
		if opts.PreparedRun == nil || opts.Progress == nil {
			t.Fatalf("prepared phase missing shared state: %+v", opts)
		}
		if shared == nil {
			shared = opts.Progress
		} else if shared != opts.Progress {
			t.Fatal("check recreated progress session between phases")
		}
		if got := opts.Progress.TotalNodes(); got < 7 {
			t.Fatalf("denominator missing later phase nodes: %d", got)
		}
		if elapsed := opts.Progress.Elapsed(); elapsed < lastElapsed {
			t.Fatalf("elapsed reset between phases: %s then %s", lastElapsed, elapsed)
		} else {
			lastElapsed = elapsed
		}
		return []TaskRunResult{{Node: tasks.TaskNode{Service: req.Services[0], Task: req.Task}, Status: tasks.TaskStatusSucceeded}}, nil
	}
	t.Cleanup(func() { runCheckTaskPhase = originalPhase })
	if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "main", "--no-cache"}); err != nil {
		t.Fatal(err)
	}
	if phaseCount != 5 {
		t.Fatalf("ran %d phases, want 5", phaseCount)
	}
}

func TestCheckNoBufferStreamsTasksButKeepsInstallBuffered(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	writeFile(t, repo, "go.mod", "module example.com/check\n\ngo 1.24\n")
	withWorkingDir(t, repo)
	tools := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go.log")
	fakeGo := filepath.Join(tools, "go")
	writeFile(t, tools, "go", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$MONO_FAKE_GO_LOG\"\nif [ \"$1\" = mod ]; then echo install-output; else echo task-output; fi\n")
	if err := os.Chmod(fakeGo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MONO_FAKE_GO_LOG", logPath)
	stdout, stderr := captureCheckStreams(t, func() {
		if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "main", "--all", "--no-cache", "--no-buffer"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "[lib:fix] task-output") || !strings.Contains(stdout, "Check complete:") {
		t.Fatalf("task stream or summary missing: %q", stdout)
	}
	if strings.Contains(stdout, "install-output") {
		t.Fatalf("successful install output should remain buffered: %q", stdout)
	}
	if strings.Contains(stdout+stderr, "\x1b[2K") || strings.Contains(stdout+stderr, "\r[") {
		t.Fatalf("no-buffer run emitted progress controls: stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Count(stdout, "Check complete:") != 1 {
		t.Fatalf("expected one check summary: %q", stdout)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "mod download") || !strings.Contains(string(log), "test") {
		t.Fatalf("fake install/task commands did not execute: %s", log)
	}
}

func captureCheckStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var out, stderr bytes.Buffer
	_, _ = io.Copy(&out, rOut)
	_, _ = io.Copy(&stderr, rErr)
	_ = rOut.Close()
	_ = rErr.Close()
	return out.String(), stderr.String()
}

func TestCheckCommandPrintsSingleFinalSummary(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	original := runCheckDependencyInstalls
	runCheckDependencyInstalls = func(_ *Config, services []string) ([]DependencyInstallResult, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		runCheckDependencyInstalls = original
	})

	originalTask := runCheckTaskPhase
	runCheckTaskPhase = func(_ *Config, req TaskRequest, _ TaskRunOptions) ([]TaskRunResult, error) {
		switch req.Task {
		case TaskFix:
			return []TaskRunResult{
				{Status: tasks.TaskStatusSucceeded},
				{Status: tasks.TaskStatusSucceeded},
			}, nil
		case TaskFormat:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		case TaskLint:
			return []TaskRunResult{
				{Status: tasks.TaskStatusSucceeded},
				{Status: tasks.TaskStatusSkipped},
			}, nil
		case TaskTypecheck:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		case TaskTest:
			return []TaskRunResult{
				{Status: tasks.TaskStatusSucceeded},
				{Status: tasks.TaskStatusSucceeded},
			}, nil
		default:
			t.Fatalf("unexpected task: %s", req.Task)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		runCheckTaskPhase = originalTask
	})

	stdout := captureStdout(t, func() {
		if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "main"}); err != nil {
			t.Fatalf("check command returned error: %v", err)
		}
	})

	if strings.Count(stdout, "Task summary:") != 0 {
		t.Fatalf("unexpected per-phase summary chatter: %q", stdout)
	}
	if !strings.Contains(stdout, "Check complete: impacted=2 phases=5 succeeded=7 failed=0 skipped=1") {
		t.Fatalf("unexpected final summary: %q", stdout)
	}
}

func TestCheckCommandReturnsErrorAfterRunningAllPhasesOnFailures(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	original := runCheckDependencyInstalls
	runCheckDependencyInstalls = func(_ *Config, services []string) ([]DependencyInstallResult, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		runCheckDependencyInstalls = original
	})

	calls := make([]TaskName, 0, 5)
	originalTask := runCheckTaskPhase
	runCheckTaskPhase = func(_ *Config, req TaskRequest, opts TaskRunOptions) ([]TaskRunResult, error) {
		calls = append(calls, req.Task)
		if !opts.ContinueOnFailure {
			t.Fatalf("expected ContinueOnFailure=true")
		}
		switch req.Task {
		case TaskFix:
			return []TaskRunResult{{Status: tasks.TaskStatusFailed}}, fmt.Errorf("fix failed")
		case TaskFormat:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		case TaskLint:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		case TaskTypecheck:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		case TaskTest:
			return []TaskRunResult{{Status: tasks.TaskStatusSucceeded}}, nil
		default:
			t.Fatalf("unexpected task: %s", req.Task)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		runCheckTaskPhase = originalTask
	})

	stdout := captureStdout(t, func() {
		err := (&checkCLICommand{}).Run([]string{"mono", "check", "--base", "main"})
		if err == nil {
			t.Fatalf("expected check command to fail")
		}
		if !strings.Contains(err.Error(), "1 phase(s) had task failures") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	wantCalls := []TaskName{TaskFix, TaskFormat, TaskLint, TaskTypecheck, TaskTest}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("expected all phases to run: got %+v want %+v", calls, wantCalls)
	}
	if !strings.Contains(stdout, "Check complete: impacted=2 phases=5 succeeded=4 failed=1 skipped=0") {
		t.Fatalf("unexpected final summary: %q", stdout)
	}
}

func TestCheckCommandAllRunsEvenWithoutImpactedServices(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	type phaseCall struct {
		kind     string
		task     TaskName
		services []string
	}
	calls := make([]phaseCall, 0, 6)

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

	if err := (&checkCLICommand{}).Run([]string{"mono", "check", "--all", "--base", "HEAD"}); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	want := []phaseCall{
		{kind: "deps", services: []string{"api", "lib"}},
		{kind: "task", task: TaskFix, services: []string{"api", "lib"}},
		{kind: "task", task: TaskFormat, services: []string{"api", "lib"}},
		{kind: "task", task: TaskLint, services: []string{"api", "lib"}},
		{kind: "task", task: TaskTypecheck, services: []string{"api"}},
		{kind: "task", task: TaskTest, services: []string{"api", "lib"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected phase calls: got %+v want %+v", calls, want)
	}
}

func TestCheckCommandFallsBackToLocalDiffWhenBaseBranchMissing(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	detachHeadWithoutBaseRefs(t, repo)
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "api", "api.go"), "package api\n\n// staged local change\n")
	gitRun(t, repo, "add", "apps/api/api.go")
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n\n// unstaged local change\n")

	type phaseCall struct {
		kind     string
		task     TaskName
		services []string
	}
	calls := make([]phaseCall, 0, 6)

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

	if err := (&checkCLICommand{}).Run([]string{"mono", "check"}); err != nil {
		t.Fatalf("check command returned error: %v", err)
	}

	want := []phaseCall{
		{kind: "deps", services: []string{"api", "lib"}},
		{kind: "task", task: TaskFix, services: []string{"api", "lib"}},
		{kind: "task", task: TaskFormat, services: []string{"api", "lib"}},
		{kind: "task", task: TaskLint, services: []string{"api", "lib"}},
		{kind: "task", task: TaskTypecheck, services: []string{"api"}},
		{kind: "task", task: TaskTest, services: []string{"api", "lib"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected phase calls: got %+v want %+v", calls, want)
	}
}

func TestCheckCommandFallbackNoLocalChanges(t *testing.T) {
	repo := initCheckRepoWithFeatureChange(t)
	detachHeadWithoutBaseRefs(t, repo)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&checkCLICommand{}).Run([]string{"mono", "check"}); err != nil {
			t.Fatalf("check command returned error: %v", err)
		}
	})

	if !strings.Contains(stdout, "No impacted services. Nothing to check.") {
		t.Fatalf("unexpected output: %q", stdout)
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
