package cli

import "fmt"

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

func printTaskSummary(results []TaskRunResult) {
	summary := summarizeTaskResults(results)
	fmt.Println()
	fmt.Printf("Task summary: succeeded=%d failed=%d skipped=%d\n", summary.Succeeded, summary.Failed, summary.Skipped)
}
