package cli

import (
	"fmt"
	"sort"
)

type archetypeTemplate struct {
	serviceTasks map[TaskName]string
	packageTasks map[TaskName]string
}

var taskTemplates = map[string]archetypeTemplate{
	"go": {
		serviceTasks: map[TaskName]string{
			TaskBuild:     "go build ./...",
			TaskLint:      "go test ./...",
			TaskTypecheck: "go test ./...",
			TaskTest:      "go test ./...",
			TaskPackage:   "go build ./...",
			TaskDeploy:    "go test ./...",
		},
		packageTasks: map[TaskName]string{
			TaskBuild:   "go build ./...",
			TaskLint:    "go test ./...",
			TaskTest:    "go test ./...",
			TaskPackage: "go build ./...",
		},
	},
	"react": {
		serviceTasks: map[TaskName]string{
			TaskBuild:     "pnpm build",
			TaskLint:      "pnpm lint",
			TaskTypecheck: "pnpm typecheck",
			TaskTest:      "pnpm test",
			TaskPackage:   "pnpm build",
		},
		packageTasks: map[TaskName]string{
			TaskBuild:     "pnpm build",
			TaskLint:      "pnpm lint",
			TaskTypecheck: "pnpm typecheck",
			TaskTest:      "pnpm test",
			TaskPackage:   "pnpm build",
		},
	},
}

func taskCommandForService(svc Service, task TaskName) (string, bool, string) {
	if tpl, ok := taskTemplates[svc.Archetype]; ok {
		tasks := tpl.serviceTasks
		if svc.Kind == "package" {
			tasks = tpl.packageTasks
		}
		if cmd, exists := tasks[task]; exists {
			return cmd, true, ""
		}
	}

	if svc.Archetype == "" {
		return "", false, fmt.Sprintf("task %q is not supported (missing archetype)", task)
	}
	return "", false, fmt.Sprintf("task %q is not supported for %s/%s", task, svc.Archetype, svc.Kind)
}

func availableTasksForService(svc Service) []string {
	out := make([]string, 0, len(orchestratedTaskOrder))
	for _, task := range orchestratedTaskOrder {
		if _, supported, _ := taskCommandForService(svc, task); supported {
			out = append(out, string(task))
		}
	}
	sort.Strings(out)
	return out
}
