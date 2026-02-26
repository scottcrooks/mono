package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
			if ok, reason := hasReactScriptSupport(svc, "test:integration"); !ok {
				return "", false, reason
			}
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
			if svc.Archetype == "react" {
				script := reactScriptForTask(task)
				if script != "" {
					if ok, reason := hasReactScriptSupport(svc, script); !ok {
						return "", false, reason
					}
				}
			}
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

func reactScriptForTask(task TaskName) string {
	switch task {
	case TaskBuild:
		return "build"
	case TaskLint:
		return "lint"
	case TaskTypecheck:
		return "typecheck"
	case TaskTest:
		return "test"
	case TaskAudit:
		return "audit"
	case TaskPackage:
		return "build"
	default:
		return ""
	}
}

func hasReactScriptSupport(svc Service, script string) (bool, string) {
	if strings.TrimSpace(svc.Path) == "" {
		return true, ""
	}

	pkgJSONPath := filepath.Join(svc.Path, "package.json")
	data, err := os.ReadFile(pkgJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Sprintf("react script %q is not supported (missing package.json)", script)
		}
		return false, fmt.Sprintf("react script %q is not supported (read error: %v)", script, err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, fmt.Sprintf("react script %q is not supported (invalid package.json)", script)
	}

	if strings.TrimSpace(pkg.Scripts[script]) == "" {
		return false, fmt.Sprintf("react script %q is not defined", script)
	}
	return true, ""
}
