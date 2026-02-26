package tasks

import (
	"path/filepath"
	"testing"
)

func TestTaskCacheStoreAndLoad(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	cache := newTaskCache()
	entry := taskCacheEntry{Key: "abc", Service: "api", Task: TaskBuild}
	if err := cache.store(entry); err != nil {
		t.Fatalf("store: %v", err)
	}
	loaded, ok, err := cache.load("abc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok || loaded.Service != "api" || loaded.Task != TaskBuild {
		t.Fatalf("unexpected load result: ok=%v entry=%+v", ok, loaded)
	}
}

func TestBuildTaskCacheKeyChangesOnContentChange(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "apps", "api", "main.go"), "package main\n")
	svc := Service{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"}

	k1, err := buildTaskCacheKey(svc, TaskBuild, "go build ./...")
	if err != nil {
		t.Fatalf("buildTaskCacheKey(1): %v", err)
	}
	mustWrite(t, filepath.Join(repo, "apps", "api", "main.go"), "package main\n// changed\n")
	k2, err := buildTaskCacheKey(svc, TaskBuild, "go build ./...")
	if err != nil {
		t.Fatalf("buildTaskCacheKey(2): %v", err)
	}
	if k1 == k2 {
		t.Fatalf("expected cache key to change when content changes")
	}
}

func TestBuildTaskCacheKeyChangesOnCommandChange(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "apps", "api", "main.go"), "package main\n")
	svc := Service{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"}

	k1, err := buildTaskCacheKey(svc, TaskTest, "go test ./...")
	if err != nil {
		t.Fatalf("buildTaskCacheKey(1): %v", err)
	}
	k2, err := buildTaskCacheKey(svc, TaskTest, "go test -v ./...")
	if err != nil {
		t.Fatalf("buildTaskCacheKey(2): %v", err)
	}
	if k1 == k2 {
		t.Fatalf("expected cache key to change when command changes")
	}
}
