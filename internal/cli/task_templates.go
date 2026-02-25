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
			TaskLint:      "go tool golangci-lint run ./...",
			TaskTypecheck: "go test -run=^$ ./...",
			TaskTest:      "go test ./...",
			TaskAudit:     "go tool govulncheck ./...",
			TaskPackage:   "go build ./...",
		},
		packageTasks: map[TaskName]string{
			TaskBuild: "go build ./...",
			TaskLint:  "go tool golangci-lint run ./...",
			TaskTest:  "go test ./...",
			TaskAudit: "go tool govulncheck ./...",
		},
	},
	"react": {
		serviceTasks: map[TaskName]string{
			TaskBuild:     "pnpm build",
			TaskLint:      "pnpm lint",
			TaskTypecheck: "pnpm typecheck",
			TaskTest:      "pnpm test",
			TaskAudit:     "pnpm audit",
			TaskPackage:   "pnpm build",
		},
		packageTasks: map[TaskName]string{
			TaskBuild:     "pnpm build",
			TaskLint:      "pnpm lint",
			TaskTypecheck: "pnpm typecheck",
			TaskTest:      "pnpm test",
			TaskAudit:     "pnpm audit",
		},
	},
}

func taskCommandForService(svc Service, task TaskName) (string, bool, string) {
	return taskCommandForServiceWithOptions(svc, task, false)
}

func taskCommandForServiceWithOptions(svc Service, task TaskName, integration bool) (string, bool, string) {
	if integration {
		if task != TaskTest {
			return "", false, fmt.Sprintf("task %q does not support --integration", task)
		}
		switch svc.Archetype {
		case "go":
			return "go test -v ./...", true, ""
		case "react":
			return "pnpm test:integration", true, ""
		case "":
			return "", false, "task \"test\" is not supported (missing archetype)"
		default:
			return "", false, fmt.Sprintf("task %q is not supported for %s/%s", task, svc.Archetype, svc.Kind)
		}
	}

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
