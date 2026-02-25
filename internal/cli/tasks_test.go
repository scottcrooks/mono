package cli

import "testing"

func TestParseTaskNameAudit(t *testing.T) {
	t.Parallel()

	task, ok := parseTaskName("audit")
	if !ok {
		t.Fatal("expected audit to be a recognized task")
	}
	if task != TaskAudit {
		t.Fatalf("unexpected task: %q", task)
	}
}
