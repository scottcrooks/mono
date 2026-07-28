package tasks

import (
	"reflect"
	"testing"
)

func TestResolveTaskRequestIncludesDependencies(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "libs/lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}

	resolved, err := resolveTaskRequest(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}

	got := make([]string, 0, len(resolved.Nodes))
	for _, n := range resolved.Nodes {
		got = append(got, n.Node.Service)
	}
	want := []string{"api", "lib"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected services: got %v want %v", got, want)
	}
}

func TestResolveTaskRequestUnknownService(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"}}}
	_, err := resolveTaskRequest(cfg, TaskRequest{Task: TaskBuild, Services: []string{"missing"}})
	if err == nil {
		t.Fatalf("expected error for unknown service")
	}
}

func TestResolveTaskRequestUnsupportedTaskMarksSkipped(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{{Name: "lib", Path: "libs/lib", Kind: "package", Archetype: "go"}}}
	resolved, err := resolveTaskRequest(cfg, TaskRequest{Task: TaskTypecheck})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 1 {
		t.Fatalf("expected one node")
	}
	if resolved.Nodes[0].SkipReason == "" {
		t.Fatalf("expected unsupported task to be skipped")
	}
}

func TestResolveTaskRequestAllowsInternalFix(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"}}}
	resolved, err := resolveTaskRequest(cfg, TaskRequest{Task: TaskFix})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 1 || resolved.Nodes[0].Command != "go fix ./..." {
		t.Fatalf("unexpected fix resolution: %+v", resolved.Nodes)
	}
}

func TestResolveTaskRequestIntegrationUsesIntegrationCommand(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"}}}
	resolved, err := resolveTaskRequest(cfg, TaskRequest{Task: TaskTest, Integration: true})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 1 {
		t.Fatalf("expected one node")
	}
	if resolved.Nodes[0].Command != "go test -v ./..." {
		t.Fatalf("unexpected integration command: %q", resolved.Nodes[0].Command)
	}
}

func TestResolveTaskRequestExactServicesSkipsDependencyClosure(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "libs/lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}

	resolved, err := resolveTaskRequest(cfg, TaskRequest{
		Task:          TaskLint,
		Services:      []string{"api"},
		ExactServices: true,
	})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 1 || resolved.Nodes[0].Node.Service != "api" {
		t.Fatalf("expected only api in exact mode, got %+v", resolved.Nodes)
	}
}

func TestResolveTaskRequestFormatUsesFileTargets(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"},
		{Name: "web", Path: "apps/web", Kind: "service", Archetype: "go"},
	}}

	resolved, err := resolveTaskRequest(cfg, TaskRequest{
		Task:          TaskFormat,
		Services:      []string{"api", "web"},
		ExactServices: true,
		FileTargets: map[string][]string{
			"api": []string{"apps/api/main.go", "apps/api/util.go"},
			"web": []string{"apps/web/web.go"},
		},
	})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 2 {
		t.Fatalf("expected two nodes, got %+v", resolved.Nodes)
	}
	if got := resolved.Nodes[0].AffectedFiles; len(got) != 2 || got[0] != "main.go" || got[1] != "util.go" {
		t.Fatalf("unexpected api affected files: %+v", got)
	}
	if got := resolved.Nodes[1].AffectedFiles; len(got) != 1 || got[0] != "web.go" {
		t.Fatalf("unexpected web affected files: %+v", got)
	}
}

func TestResolveTaskRequestFormatSkipsWithoutFiles(t *testing.T) {
	t.Parallel()

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "apps/api", Kind: "service", Archetype: "go"},
	}}

	resolved, err := resolveTaskRequest(cfg, TaskRequest{
		Task:          TaskFormat,
		Services:      []string{"api"},
		ExactServices: true,
		FileTargets:   map[string][]string{},
	})
	if err != nil {
		t.Fatalf("resolveTaskRequest error: %v", err)
	}
	if len(resolved.Nodes) != 1 {
		t.Fatalf("expected one node, got %+v", resolved.Nodes)
	}
	if resolved.Nodes[0].SkipReason != "no changed files" {
		t.Fatalf("expected no-changed-files skip reason, got %+v", resolved.Nodes[0])
	}
}
