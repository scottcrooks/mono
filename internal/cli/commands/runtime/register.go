package runtimecmd

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

type (
	Config          = core.Config
	Service         = core.Service
	InfraSpec       = core.InfraSpec
	InfraResource   = core.InfraResource
	ReadyCheckSpec  = core.ReadyCheckSpec
	PortForwardSpec = core.PortForwardSpec
)

func loadConfig() (*Config, error)                  { return core.LoadConfig() }
func findService(cfg *Config, name string) *Service { return core.FindService(cfg, name) }
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
