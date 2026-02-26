package tasks

import "testing"

func TestCommandForExecutionAddsNoCacheForGoTest(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskTest}

	got := commandForExecution(svc, node, "go test ./...", TaskRunOptions{NoCache: true})
	if got != "go test ./... -count=1" {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionNoopForNonTestTask(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskLint}

	got := commandForExecution(svc, node, "go tool golangci-lint run ./...", TaskRunOptions{NoCache: true})
	if got != "go tool golangci-lint run ./..." {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestCommandForExecutionNoopWhenCountAlreadySet(t *testing.T) {
	t.Parallel()

	svc := Service{Name: "pythia", Archetype: "go"}
	node := TaskNode{Service: "pythia", Task: TaskTest}

	got := commandForExecution(svc, node, "go test -v ./... -count=1", TaskRunOptions{NoCache: true})
	if got != "go test -v ./... -count=1" {
		t.Fatalf("unexpected command: %q", got)
	}
}
