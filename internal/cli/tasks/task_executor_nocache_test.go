package tasks

import "testing"

func TestCommandForExecutionAddsNoCacheForGoTest(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskTest}

	got := commandForExecution(svc, node, "go test ./...", nil, TaskRunOptions{NoCache: true})
	if got != "go test ./... -count=1" {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionNoopForNonTestTask(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskLint}

	got := commandForExecution(svc, node, "go tool golangci-lint run ./...", nil, TaskRunOptions{NoCache: true})
	if got != "go tool golangci-lint run ./..." {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionNoopWhenCountAlreadySet(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskTest}

	got := commandForExecution(svc, node, "go test -v ./... -count=1", nil, TaskRunOptions{NoCache: true})
	if got != "go test -v ./... -count=1" {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionFormatsAffectedGoServiceByPackage(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskFormat}

	got := commandForExecution(svc, node, "go fmt ./...", []string{"main.go", "migrations/000001_create_items.sql"}, TaskRunOptions{})
	if got != "go fmt ./..." {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionFormatsGoServiceAffectedOnlyBySQL(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskFormat}

	got := commandForExecution(svc, node, "go fmt ./...", []string{"migrations/000001_create_items.sql"}, TaskRunOptions{})
	if got != "go fmt ./..." {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionAppendsPnpmFormatFiles(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "web", Archetype: "react"}
	node := TaskNode{Service: "web", Task: TaskFormat}

	got := commandForExecution(svc, node, "pnpm format", []string{"src/app.tsx", "src/lib.ts"}, TaskRunOptions{})
	if got != "pnpm format -- src/app.tsx src/lib.ts" {
		t.Fatalf("unexpected command: %q", got)
	}
}
