package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func TestBuildTaskGraphDetectsCycle(t *testing.T) {
	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go", Depends: []string{"b"}},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}

	_, err := buildTaskGraph(cfg, resolved)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestTaskExecutorHonorsDependencyOrdering(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "lib", "go.mod"), "module lib\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n")
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
		{Node: TaskNode{Service: "lib", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	byNode := map[string]TaskRunStatus{}
	for _, r := range results {
		byNode[r.Node.String()] = r.Status
	}
	if byNode["lib:build"] != TaskStatusSucceeded {
		t.Fatalf("expected lib success, got %s", byNode["lib:build"])
	}
	if byNode["api:build"] != TaskStatusSucceeded {
		t.Fatalf("expected api success, got %s", byNode["api:build"])
	}
}

func TestTaskExecutorBufferedSuccessStaysQuiet(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-success.js"), "process.stdout.write('success-stdout\\n')\nprocess.stderr.write('success-stderr\\n')\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-success.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestTaskExecutorBufferedCacheMissStaysQuiet(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 1, BufferOutput: true})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestTaskExecutorBufferedFailureReplaysTranscript(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-fail.js"), "process.stdout.write('fail-stdout\\n')\nprocess.stderr.write('fail-stderr\\n')\nprocess.exit(1)\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-fail.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
		if err == nil {
			t.Fatalf("expected execute error")
		}
		if len(results) != 1 || results[0].Status != TaskStatusFailed {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if !strings.Contains(stdout, "[api:build] start") {
		t.Fatalf("expected buffered step start in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "[api:build] fail-stdout") {
		t.Fatalf("expected buffered stdout replay, got %q", stdout)
	}
	if !strings.Contains(stderr, "[api:build] fail-stderr") {
		t.Fatalf("expected buffered stderr replay, got %q", stderr)
	}
	if !strings.Contains(stderr, "[api:build] failed") {
		t.Fatalf("expected failure line in stderr, got %q", stderr)
	}
}

func TestTaskExecutorLivePathStreamsOutput(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-live.js"), "process.stdout.write('live-stdout\\n')\nprocess.stderr.write('live-stderr\\n')\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-live.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if !strings.Contains(stdout, "[api:build] live-stdout") {
		t.Fatalf("expected live stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "[api:build] live-stderr") {
		t.Fatalf("expected live stderr, got %q", stderr)
	}
}

func TestTaskExecutorFailureSkipsRemaining(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:build"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:build"])
	}
	if byNode["b:build"].Status != TaskStatusSkipped {
		t.Fatalf("expected b to be skipped, got %+v", byNode["b:build"])
	}
}

func TestTaskExecutorContinueOnFailureRunsDependents(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2, ContinueOnFailure: true})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:build"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:build"])
	}
	if byNode["b:build"].Status != TaskStatusSucceeded {
		t.Fatalf("expected b to run despite failure, got %+v", byNode["b:build"])
	}
}

func TestTaskExecutorDependencyChangeInvalidatesDependentCache(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "lib", "go.mod"), "module lib\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n")
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
		{Node: TaskNode{Service: "lib", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	first, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	for _, r := range first {
		if r.Status == TaskStatusFailed {
			t.Fatalf("unexpected failure in first run: %+v", r)
		}
	}

	second, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	byNodeSecond := map[string]TaskRunStatus{}
	for _, r := range second {
		byNodeSecond[r.Node.String()] = r.Status
	}
	if byNodeSecond["lib:build"] != TaskStatusSkipped || byNodeSecond["api:build"] != TaskStatusSkipped {
		t.Fatalf("expected both nodes cached on second run, got %+v", byNodeSecond)
	}

	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n// changed\n")
	third, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("third execute: %v", err)
	}
	byNodeThird := map[string]TaskRunStatus{}
	for _, r := range third {
		byNodeThird[r.Node.String()] = r.Status
	}
	if byNodeThird["lib:build"] != TaskStatusSucceeded {
		t.Fatalf("expected lib to rerun after lib change, got %+v", byNodeThird)
	}
	if byNodeThird["api:build"] != TaskStatusSucceeded {
		t.Fatalf("expected api to rerun after dependency change, got %+v", byNodeThird)
	}
}

func TestTaskExecutorAuditContinuesOnFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskAudit}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskAudit}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:audit"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:audit"])
	}
	if byNode["b:audit"].Status != TaskStatusSucceeded {
		t.Fatalf("expected b to succeed despite dependency failure, got %+v", byNode["b:audit"])
	}
}

func TestPreparedTaskRunPersistsOnlyStartedCommandsAndSeparatesVariants(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api_test.go"), "package api\nimport \"testing\"\nfunc TestOK(t *testing.T) {}\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	keyRegular := taskTimingKey{Service: "api", Task: string(TaskTest)}
	keyIntegration := taskTimingKey{Service: "api", Task: string(TaskTest), Integration: true}
	for _, variant := range []bool{false, true} {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true, Integration: variant})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskTest}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("variant %v run: %+v, %v", variant, results, err)
		}
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[keyRegular]) != 1 || len(loaded[keyIntegration]) != 1 {
		t.Fatalf("variant observations missing: %#v", loaded)
	}

	failed, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	buildNode := TaskNode{Service: "api", Task: TaskBuild}
	resolved := failed.graph.nodes[buildNode]
	resolved.Command = "definitely-not-a-real-command"
	failed.graph.nodes[buildNode] = resolved
	_, runErr := RunPreparedTaskRun(failed, TaskRunOptions{NoCache: true, Concurrency: 1})
	if runErr == nil {
		t.Fatal("expected command construction failure")
	}
	loaded, err = newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
		t.Fatal("command not started contributed a timing sample")
	}
}

func TestPreparedTaskRunLearnsStreamedSuccessAndFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	for _, tc := range []struct {
		task       TaskName
		command    string
		wantStatus TaskRunStatus
	}{
		{TaskBuild, "go version", TaskStatusSucceeded},
		{TaskAudit, "go definitely-not-a-command", TaskStatusFailed},
	} {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: tc.task, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: tc.task}
		resolved := prepared.graph.nodes[node]
		resolved.Command = tc.command
		prepared.graph.nodes[node] = resolved
		_, runErr := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: false})
		if tc.wantStatus == TaskStatusSucceeded && runErr != nil {
			t.Fatal(runErr)
		}
		if tc.wantStatus == TaskStatusFailed && runErr == nil {
			t.Fatal("expected command failure")
		}
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 1 || len(loaded[taskTimingKey{Service: "api", Task: string(TaskAudit)}]) != 1 {
		t.Fatalf("streamed observations missing: %#v", loaded)
	}
}

func TestPreparedTaskRunCacheHitDoesNotAddTiming(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepare := func() *PreparedTaskRun {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskBuild}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		return prepared
	}
	first, err := RunPreparedTaskRun(prepare(), TaskRunOptions{Concurrency: 1})
	if err != nil || first[0].Status != TaskStatusSucceeded {
		t.Fatalf("first run: %+v, %v", first, err)
	}
	second, err := RunPreparedTaskRun(prepare(), TaskRunOptions{Concurrency: 1})
	if err != nil || second[0].Status != TaskStatusSkipped || !second[0].Cached {
		t.Fatalf("cache run: %+v, %v", second, err)
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]; len(got) != 1 {
		t.Fatalf("cache hit added timing sample: %v", got)
	}
}

func TestCompatibilityRunnerUsesRegisteredPreparedPhases(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	var runs []*PreparedTaskRun
	for _, task := range []TaskName{TaskFix, TaskTest} {
		run, err := PrepareTaskRun(cfg, TaskRequest{Task: task, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		for node, resolved := range run.graph.nodes {
			resolved.Command = "go version"
			run.graph.nodes[node] = resolved
		}
		runs = append(runs, run)
	}
	opts := TaskRunOptions{NoCache: true, BufferOutput: true, Concurrency: 1}
	session := NewTaskProgressSession(runs, opts)
	defer session.Close()
	for i, run := range runs {
		opts.PreparedRun, opts.Progress = run, session
		if _, err := RunOrchestratedTaskRequestWithConfig(cfg, TaskRequest{Task: []TaskName{TaskFix, TaskTest}[i], Services: []string{"api"}, ExactServices: true}, opts); err != nil {
			t.Fatal(err)
		}
		got := session.model.Snapshot()
		if got.Total != 2 || got.Settled != i+1 || got.Percent != (i+1)*50 {
			t.Fatalf("phase %d did not advance shared progress: %+v", i, got)
		}
	}
}

func TestPreparedTaskRunHistoryFailuresRemainAdvisory(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	if err := os.Mkdir(filepath.Join(repo, taskTimingFile), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskBuild}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "go version"
	prepared.graph.nodes[node] = resolved
	var results []TaskRunResult
	var runErr error
	stdout, stderr := captureStreams(t, func() {
		results, runErr = RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
	})
	if runErr != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
		t.Fatalf("history failure changed task outcome: %+v, %v", results, runErr)
	}
	if !strings.Contains(stderr, "timing history load failed") || !strings.Contains(stderr, "timing history save failed") {
		t.Fatalf("missing advisory history warnings: %q", stderr)
	}
	if strings.Contains(stdout+stderr, "\x1b[2K") {
		t.Fatalf("buffered pipe wrote progress controls: stdout=%q stderr=%q", stdout, stderr)
	}

	failing, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskAudit, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	failedNode := TaskNode{Service: "api", Task: TaskAudit}
	resolved = failing.graph.nodes[failedNode]
	resolved.Command = "go definitely-not-a-command"
	failing.graph.nodes[failedNode] = resolved
	failedResults, runErr := RunPreparedTaskRun(failing, TaskRunOptions{NoCache: true, Concurrency: 1})
	if runErr == nil || len(failedResults) != 1 || failedResults[0].Err == nil || !strings.Contains(failedResults[0].Err.Error(), "exit status") {
		t.Fatalf("history failure replaced task error: results=%+v error=%v", failedResults, runErr)
	}
}

func TestPreparedTaskRunDoesNotLearnFormatSkipsBlockedNodesOrCacheErrors(t *testing.T) {
	t.Run("format skip", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskFormat, Services: []string{"api"}, ExactServices: true, FileTargets: map[string][]string{}})
		if err != nil {
			t.Fatal(err)
		}
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
		if err != nil || len(results) != 1 || results[0].Status != TaskStatusSkipped {
			t.Fatalf("skip result: %+v, %v", results, err)
		}
		if _, err := os.Stat(filepath.Join(repo, taskTimingFile)); !os.IsNotExist(err) {
			t.Fatalf("planned skip created timing history: %v", err)
		}
	})
	t.Run("blocked dependency", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
		mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
		cfg := &Config{Services: []Service{{Name: "a", Path: "a", Kind: "service", Archetype: "go"}, {Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"b"}})
		if err != nil {
			t.Fatal(err)
		}
		for node, resolved := range prepared.graph.nodes {
			resolved.Command = "go version"
			if node.Service == "a" {
				resolved.Command = "go definitely-not-a-command"
			}
			prepared.graph.nodes[node] = resolved
		}
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err == nil {
			t.Fatal("expected failed dependency")
		}
		statuses := map[string]TaskRunStatus{}
		for _, result := range results {
			statuses[result.Node.Service] = result.Status
		}
		if statuses["a"] != TaskStatusFailed || statuses["b"] != TaskStatusSkipped {
			t.Fatalf("unexpected dependency statuses: %v", statuses)
		}
		loaded, err := newTaskTimingStore(repo).load()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded[taskTimingKey{Service: "a", Task: string(TaskBuild)}]) != 1 || len(loaded[taskTimingKey{Service: "b", Task: string(TaskBuild)}]) != 0 {
			t.Fatalf("blocked node affected history: %#v", loaded)
		}
	})
	t.Run("cache load error", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
		cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskBuild}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		key, err := buildTaskCacheKey(resolved.Service, node.Task, resolved.Command)
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(repo, ".cache", "mono", "tasks", key+".json"), "invalid json")
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
		if err == nil || results[0].Status != TaskStatusFailed {
			t.Fatalf("expected cache failure: %+v, %v", results, err)
		}
		loaded, err := newTaskTimingStore(repo).load()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
			t.Fatalf("cache failure produced timing sample: %#v", loaded)
		}
	})
}

func TestPreparedTaskRunStartupFailureDoesNotLearn(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	tools := t.TempDir()
	mustWrite(t, filepath.Join(tools, "node"), "not executable")
	if err := os.Chmod(filepath.Join(tools, "node"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskBuild}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node command.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
	if err == nil || results[0].Status != TaskStatusFailed {
		t.Fatalf("expected subprocess startup failure: %+v, %v", results, err)
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
		t.Fatalf("failed startup produced timing sample: %#v", loaded)
	}
}

func TestTaskExecutorSerializesConcurrentFailureReplayWithProgress(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	cfg := &Config{Services: []Service{{Name: "a", Path: "a", Kind: "service", Archetype: "go"}, {Name: "b", Path: "b", Kind: "service", Archetype: "go"}}}
	for _, name := range []string{"a", "b"} {
		mustWrite(t, filepath.Join(repo, name, "go.mod"), "module "+name+"\n\ngo 1.24\n")
		mustWrite(t, filepath.Join(repo, name, "fail.js"), "const name = "+strconv.Quote(strings.ToUpper(name))+"; process.stdout.write(name+'-START\\n'); setTimeout(() => { process.stdout.write(name+'-END\\n'); process.stderr.write(name+'-ERR\\n'); process.exit(1) }, 20);\n")
	}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskAudit, Services: []string{"a", "b"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	for node, resolved := range prepared.graph.nodes {
		resolved.Command = "node fail.js"
		prepared.graph.nodes[node] = resolved
	}
	var events bytes.Buffer
	line := output.NewProgressLine(output.ProgressOptions{Writer: &events, Mode: output.ModeInteractive, IsTTY: func() bool { return true }, Term: func() string { return "xterm" }, Width: func() (int, error) { return 100, nil }, TickInterval: time.Hour})
	session := newTaskProgressSession([]*PreparedTaskRun{prepared}, TaskRunOptions{BufferOutput: true, Concurrency: 2}, line)
	options := TaskRunOptions{NoCache: true, BufferOutput: true, Concurrency: 2, Progress: session, progressIDs: session.ids[prepared]}
	executor := taskExecutor{cache: newTaskCache(), printer: output.NewPrinterWithMode(&events, &events, output.ModePlain), stdout: &events, stderr: &events}
	_, runErr := executor.execute(context.Background(), prepared.graph, options)
	session.Close()
	if runErr == nil {
		t.Fatal("expected concurrent command failures")
	}
	text := events.String()
	clear := strings.Index(text, "\x1b[2K")
	if clear < 0 {
		t.Fatalf("progress line was not cleared before transcript replay: %q", text)
	}
	for _, name := range []string{"A", "B"} {
		start := strings.Index(text, name+"-START")
		end := strings.Index(text, name+"-END")
		errAt := strings.Index(text, name+"-ERR")
		if start < clear || end < start || errAt < end {
			t.Fatalf("missing or reordered %s transcript: %q", name, text)
		}
		other := "A"
		if name == "A" {
			other = "B"
		}
		otherStart := strings.Index(text, other+"-START")
		if otherStart > start && otherStart < errAt {
			t.Fatalf("concurrent transcripts interleaved: %q", text)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
