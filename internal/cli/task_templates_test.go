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
	if _, ok, _ := taskCommandForService(service, TaskPackage); !ok {
		t.Fatalf("expected go service to support package")
	}
	if _, ok, _ := taskCommandForService(pkg, TaskPackage); ok {
		t.Fatalf("expected go package to skip package")
	}
	if _, ok, _ := taskCommandForService(service, TaskAudit); !ok {
		t.Fatalf("expected go service to support audit")
	}
	if _, ok, _ := taskCommandForService(pkg, TaskAudit); !ok {
		t.Fatalf("expected go package to support audit")
	}
	if _, ok, _ := taskCommandForService(service, TaskDeploy); ok {
		t.Fatalf("expected go service to skip deploy until deploy template exists")
	}
}

func TestAvailableTasksForUnknownArchetype(t *testing.T) {
	t.Parallel()

	tasks := availableTasksForService(Service{Name: "x", Kind: "service", Archetype: "unknown"})
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks for unknown archetype, got %v", tasks)
	}
}

func TestTaskTemplateCommandsAreIntentional(t *testing.T) {
	t.Parallel()

	goService := Service{Name: "api", Kind: "service", Archetype: "go"}
	goPkg := Service{Name: "lib", Kind: "package", Archetype: "go"}
	reactService := Service{Name: "web", Kind: "service", Archetype: "react"}

	cases := []struct {
		svc  Service
		task TaskName
		want string
	}{
		{svc: goService, task: TaskLint, want: "go tool golangci-lint run ./..."},
		{svc: goService, task: TaskTypecheck, want: "go test -run=^$ ./..."},
		{svc: goPkg, task: TaskLint, want: "go tool golangci-lint run ./..."},
		{svc: goService, task: TaskAudit, want: "go tool govulncheck ./..."},
		{svc: reactService, task: TaskTypecheck, want: "pnpm typecheck"},
		{svc: reactService, task: TaskAudit, want: "pnpm audit"},
	}

	for _, tc := range cases {
		got, ok, reason := taskCommandForService(tc.svc, tc.task)
		if !ok {
			t.Fatalf("expected task support for %s %s, got skip reason %q", tc.svc.Name, tc.task, reason)
		}
		if got != tc.want {
			t.Fatalf("unexpected command for %s %s: got %q want %q", tc.svc.Name, tc.task, got, tc.want)
		}
	}
}
