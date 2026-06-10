package tasks

import (
	"path/filepath"
	"testing"
)

func TestTaskTemplatesServiceVsPackage(t *testing.T) {
	t.Parallel()

	service := Service{Name: "api", Kind: "service", Archetype: "go"}
	pkg := Service{Name: "lib", Kind: "package", Archetype: "go"}

	if _, ok, _ := TaskCommandForService(service, TaskTypecheck); !ok {
		t.Fatalf("expected go service to support typecheck")
	}
	if _, ok, _ := TaskCommandForService(pkg, TaskTypecheck); ok {
		t.Fatalf("expected go package to skip typecheck")
	}
	if _, ok, _ := TaskCommandForService(service, TaskPackage); !ok {
		t.Fatalf("expected go service to support package")
	}
	if _, ok, _ := TaskCommandForService(pkg, TaskPackage); ok {
		t.Fatalf("expected go package to skip package")
	}
	if _, ok, _ := TaskCommandForService(service, TaskAudit); !ok {
		t.Fatalf("expected go service to support audit")
	}
	if _, ok, _ := TaskCommandForService(pkg, TaskAudit); !ok {
		t.Fatalf("expected go package to support audit")
	}
	if _, ok, _ := TaskCommandForService(service, TaskFormat); !ok {
		t.Fatalf("expected go service to support format")
	}
	if _, ok, _ := TaskCommandForService(pkg, TaskFormat); !ok {
		t.Fatalf("expected go package to support format")
	}
	if _, ok, _ := TaskCommandForService(service, TaskDeploy); ok {
		t.Fatalf("expected go service to skip deploy until deploy template exists")
	}
}

func TestAvailableTasksForUnknownArchetype(t *testing.T) {
	t.Parallel()

	tasks := AvailableTasksForService(Service{Name: "x", Kind: "service", Archetype: "unknown"})
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks for unknown archetype, got %v", tasks)
	}
}

func TestTaskTemplateCommandsAreIntentional(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{
  "scripts": {
    "format": "prettier --write .",
    "typecheck": "tsc --noEmit",
    "audit": "pnpm audit"
  }
}
`)
	writeFile(t, repo, filepath.Join("apps", "sonata", "package.json"), `{
  "scripts": {
    "build": "tsc -b",
    "lint": "eslint .",
    "typecheck": "tsc -b",
    "test": "vitest run --passWithNoTests",
    "audit": "pnpm audit --prod"
  }
}
`)

	goService := Service{Name: "api", Kind: "service", Archetype: "go"}
	goPkg := Service{Name: "lib", Kind: "package", Archetype: "go"}
	reactService := Service{Name: "web", Kind: "service", Archetype: "react", Path: "apps/web"}
	tsNodeService := Service{Name: "sonata", Kind: "service", Archetype: "ts-node", Path: "apps/sonata"}
	tsLibPackage := Service{Name: "core", Kind: "package", Archetype: "ts-lib", Path: "packages/core"}

	cases := []struct {
		svc  Service
		task TaskName
		want string
	}{
		{svc: goService, task: TaskLint, want: "go tool golangci-lint run ./..."},
		{svc: goService, task: TaskFormat, want: "gofmt -w"},
		{svc: goService, task: TaskTypecheck, want: "go test -run=^$ ./..."},
		{svc: goPkg, task: TaskLint, want: "go tool golangci-lint run ./..."},
		{svc: goPkg, task: TaskFormat, want: "gofmt -w"},
		{svc: goService, task: TaskAudit, want: "go tool govulncheck ./..."},
		{svc: reactService, task: TaskFormat, want: "pnpm format"},
		{svc: reactService, task: TaskTypecheck, want: "pnpm typecheck"},
		{svc: reactService, task: TaskAudit, want: "pnpm audit"},
		{svc: tsNodeService, task: TaskFormat, want: "pnpm format"},
		{svc: tsNodeService, task: TaskTypecheck, want: "pnpm typecheck"},
		{svc: tsLibPackage, task: TaskBuild, want: "pnpm build"},
		{svc: tsLibPackage, task: TaskFormat, want: "pnpm format"},
	}

	for _, tc := range cases {
		got, ok, reason := TaskCommandForService(tc.svc, tc.task)
		if !ok {
			t.Fatalf("expected task support for %s %s, got skip reason %q", tc.svc.Name, tc.task, reason)
		}
		if got != tc.want {
			t.Fatalf("unexpected command for %s %s: got %q want %q", tc.svc.Name, tc.task, got, tc.want)
		}
	}
}

func TestReactTaskSupportRequiresScript(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{
  "scripts": {
    "format": "prettier --write .",
    "lint": "eslint ."
  }
}
`)

	reactService := Service{Name: "web", Kind: "service", Archetype: "react", Path: "apps/web"}

	if _, ok, _ := TaskCommandForService(reactService, TaskLint); !ok {
		t.Fatalf("expected lint to be supported when script exists")
	}
	if _, ok, _ := TaskCommandForService(reactService, TaskFormat); !ok {
		t.Fatalf("expected format to be supported when script exists")
	}
	if _, ok, reason := TaskCommandForService(reactService, TaskTypecheck); ok {
		t.Fatalf("expected typecheck to be skipped when script is missing")
	} else if reason == "" {
		t.Fatalf("expected skip reason when typecheck script is missing")
	}
}

func TestReactIntegrationCommandIsNonInteractive(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "web", "package.json"), `{
  "scripts": {
    "test:integration": "playwright test"
  }
}
`)

	reactService := Service{Name: "web", Kind: "service", Archetype: "react", Path: "apps/web"}
	got, ok, reason := TaskCommandForServiceWithOptions(reactService, TaskTest, true)
	if !ok {
		t.Fatalf("expected integration test task support, got reason: %q", reason)
	}
	want := "pnpm test:integration -- --reporter=line"
	if got != want {
		t.Fatalf("unexpected integration command: got %q want %q", got, want)
	}
}
