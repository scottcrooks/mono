package tasks

import (
	"strings"
	"testing"
)

func TestSummarizeTaskResults(t *testing.T) {
	t.Parallel()

	results := []TaskRunResult{
		{Status: TaskStatusSucceeded},
		{Status: TaskStatusFailed},
		{Status: TaskStatusSkipped},
		{Status: TaskStatusSkipped},
	}
	s := summarizeTaskResults(results)
	if s.Succeeded != 1 || s.Failed != 1 || s.Skipped != 2 {
		t.Fatalf("unexpected summary: %+v", s)
	}
}

func TestPrintTaskSummary(t *testing.T) {
	t.Parallel()

	stdout := captureStdout(t, func() {
		PrintTaskSummary([]TaskRunResult{{Status: TaskStatusSucceeded}, {Status: TaskStatusSkipped}})
	})
	if !strings.Contains(stdout, "Task summary: succeeded=1 failed=0 skipped=1") {
		t.Fatalf("unexpected output: %q", stdout)
	}
}
