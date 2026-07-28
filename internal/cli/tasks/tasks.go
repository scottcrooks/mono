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
	TaskFix       TaskName = "fix"
	TaskFormat    TaskName = "format"
	TaskLint      TaskName = "lint"
	TaskTypecheck TaskName = "typecheck"
	TaskTest      TaskName = "test"
	TaskAudit     TaskName = "audit"
	TaskPackage   TaskName = "package"
	TaskDeploy    TaskName = "deploy"
)

var orchestratedTaskOrder = []TaskName{
	TaskBuild,
	TaskFormat,
	TaskLint,
	TaskTypecheck,
	TaskTest,
	TaskAudit,
	TaskPackage,
	TaskDeploy,
}

var orchestratedTaskSet = map[TaskName]struct{}{
	TaskBuild:     {},
	TaskFormat:    {},
	TaskLint:      {},
	TaskTypecheck: {},
	TaskTest:      {},
	TaskAudit:     {},
	TaskPackage:   {},
	TaskDeploy:    {},
}

var internalTaskSet = map[TaskName]struct{}{
	TaskFix: {},
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

func isResolvableTask(task TaskName) bool {
	if _, ok := orchestratedTaskSet[task]; ok {
		return true
	}
	_, ok := internalTaskSet[task]
	return ok
}

func sortedTaskNames() []string {
	out := make([]string, 0, len(orchestratedTaskSet))
	for _, task := range orchestratedTaskOrder {
		out = append(out, string(task))
	}
	sort.Strings(out)
	return out
}
