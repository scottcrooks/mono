package cli

import "testing"

func TestTaskTemplatesServiceVsPackage(t *testing.T) {
	t.Parallel()

	service := Service{Name: "api", Kind: "service", Archetype: "go"}
	pkg := Service{Name: "lib", Kind: "package", Archetype: "go"}

	if _, ok, _ := taskCommandForService(service, TaskTypecheck); !ok {
		t.Fatalf("expected go service to support typecheck")
	}
	if _, ok, _ := taskCommandForService(pkg, TaskTypecheck); ok {
		t.Fatalf("expected go package to skip typecheck")
	}
}

func TestAvailableTasksForUnknownArchetype(t *testing.T) {
	t.Parallel()

	tasks := availableTasksForService(Service{Name: "x", Kind: "service", Archetype: "unknown"})
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks for unknown archetype, got %v", tasks)
	}
}
