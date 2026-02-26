package quality

import (
	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/impact"
	"github.com/scottcrooks/mono/internal/cli/tasks"
)

type registeredCommand struct {
	name string
	cmd  core.Command
}

var pending []registeredCommand

func registerCommand(name string, cmd core.Command) {
	pending = append(pending, registeredCommand{name: name, cmd: cmd})
}

func Register(reg *core.Registry) {
	for _, c := range pending {
		reg.Register(c.name, c.cmd)
	}
}

type (
	Config         = core.Config
	TaskName       = tasks.TaskName
	TaskRequest    = tasks.TaskRequest
	TaskRunOptions = tasks.TaskRunOptions
	TaskRunResult  = tasks.TaskRunResult
)

const (
	TaskLint      = tasks.TaskLint
	TaskTypecheck = tasks.TaskTypecheck
	TaskTest      = tasks.TaskTest
)

func defaultTaskConcurrency() int { return tasks.DefaultTaskConcurrency() }

func runOrchestratedTaskRequestWithConfig(cfg *core.Config, req TaskRequest, opts TaskRunOptions) ([]TaskRunResult, error) {
	return tasks.RunOrchestratedTaskRequestWithConfig(cfg, req, opts)
}

func printTaskSummary(results []TaskRunResult) { tasks.PrintTaskSummary(results) }

func loadConfig() (*core.Config, error) { return core.LoadConfig() }

func buildImpactReport(cfg *core.Config, baseRef string, explain bool) (*impact.ImpactReport, error) {
	return impact.BuildImpactReport(cfg, baseRef, explain)
}

func buildPendingCheckPlan(cfg *core.Config, impacted []string) impact.PendingCheckPlan {
	return impact.BuildPendingCheckPlan(cfg, impacted)
}
