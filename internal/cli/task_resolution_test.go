package cli

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
