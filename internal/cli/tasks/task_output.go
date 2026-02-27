package tasks

import (
	"fmt"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type TaskRunStatus string

const (
	TaskStatusSucceeded TaskRunStatus = "succeeded"
	TaskStatusFailed    TaskRunStatus = "failed"
	TaskStatusSkipped   TaskRunStatus = "skipped"
)

type TaskRunResult struct {
	Node       TaskNode
	Status     TaskRunStatus
	Err        error
	SkipReason string
	Cached     bool
}

type TaskRunSummary struct {
	Succeeded int
	Failed    int
	Skipped   int
}

func summarizeTaskResults(results []TaskRunResult) TaskRunSummary {
	summary := TaskRunSummary{}
	for _, result := range results {
		switch result.Status {
		case TaskStatusSucceeded:
			summary.Succeeded++
		case TaskStatusFailed:
			summary.Failed++
		case TaskStatusSkipped:
			summary.Skipped++
		}
	}
	return summary
}

func PrintTaskSummary(results []TaskRunResult) {
	summary := summarizeTaskResults(results)
	printer := output.DefaultPrinter()
	printer.Blank()
	printer.Summary(fmt.Sprintf("Task summary: succeeded=%d failed=%d skipped=%d", summary.Succeeded, summary.Failed, summary.Skipped))
}
