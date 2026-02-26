package tasks

import (
	"fmt"
	"sort"
	"strings"
)

// TaskName is the normalized orchestration task identifier.
type TaskName string

const (
	TaskBuild     TaskName = "build"
	TaskLint      TaskName = "lint"
	TaskTypecheck TaskName = "typecheck"
	TaskTest      TaskName = "test"
	TaskAudit     TaskName = "audit"
	TaskPackage   TaskName = "package"
	TaskDeploy    TaskName = "deploy"
)

var orchestratedTaskOrder = []TaskName{
	TaskBuild,
	TaskLint,
	TaskTypecheck,
	TaskTest,
	TaskAudit,
	TaskPackage,
	TaskDeploy,
}

var orchestratedTaskSet = map[TaskName]struct{}{
	TaskBuild:     {},
	TaskLint:      {},
	TaskTypecheck: {},
	TaskTest:      {},
	TaskAudit:     {},
	TaskPackage:   {},
	TaskDeploy:    {},
}

// TaskNode is the smallest execution unit in the orchestrator.
type TaskNode struct {
	Service string
	Task    TaskName
}

func (n TaskNode) String() string {
	return fmt.Sprintf("%s:%s", n.Service, n.Task)
}

func ParseTaskName(raw string) (TaskName, bool) {
	task := TaskName(strings.TrimSpace(raw))
	_, ok := orchestratedTaskSet[task]
	return task, ok
}

func sortedTaskNames() []string {
	out := make([]string, 0, len(orchestratedTaskSet))
	for _, task := range orchestratedTaskOrder {
		out = append(out, string(task))
	}
	sort.Strings(out)
	return out
}
