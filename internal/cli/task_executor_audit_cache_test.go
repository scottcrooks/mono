package cli

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTaskExecutorAuditDoesNotUseCache(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "svc", "go.mod"), "module svc\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "svc", "main.go"), "package main\n")

	cfg := &Config{Services: []Service{
		{Name: "svc", Path: "svc", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "svc", Task: TaskAudit}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	first, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 1})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 1})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}

	if len(first) != 1 || first[0].Status != TaskStatusSucceeded {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(second) != 1 || second[0].Status != TaskStatusSucceeded {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if second[0].Cached {
		t.Fatalf("expected audit result to never be cached")
	}
}
