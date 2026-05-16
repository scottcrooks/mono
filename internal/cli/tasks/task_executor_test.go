package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
