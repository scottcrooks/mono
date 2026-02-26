package meta

import (
	"fmt"

	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/tasks"
)

type listCommand struct{}

func init() {
	registerCommand("list", &listCommand{})
}

func (c *listCommand) Run(_ []string) error {
	cfg, err := core.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	core.ListServices(cfg, tasks.AvailableTasksForService)
	return nil
}
