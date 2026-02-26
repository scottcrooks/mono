package workflow

import (
	"context"
	"os/exec"

	"github.com/scottcrooks/mono/internal/cli/core"
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

type Config = core.Config

func loadConfig() (*core.Config, error) { return core.LoadConfig() }
func commandFromParts(ctx context.Context, parts []string) (*exec.Cmd, error) {
	return core.CommandFromParts(ctx, parts)
}

func listServices() error {
	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}
	core.ListServices(cfg, tasks.AvailableTasksForService)
	return nil
}
