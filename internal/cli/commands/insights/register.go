package insights

import (
	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/impact"
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

type CheckTaskPreview = impact.CheckTaskPreview

func loadConfig() (*core.Config, error) { return core.LoadConfig() }

func buildImpactReport(cfg *core.Config, baseRef string, explain bool) (*impact.ImpactReport, error) {
	return impact.BuildImpactReport(cfg, baseRef, explain)
}

func buildCheckTaskPreview(cfg *core.Config, impacted []string) []impact.CheckTaskPreview {
	return impact.BuildCheckTaskPreview(cfg, impacted)
}
