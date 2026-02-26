package impact

import (
	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/tasks"
)

type (
	Config   = core.Config
	Service  = core.Service
	TaskName = tasks.TaskName
)

const (
	TaskLint      = tasks.TaskLint
	TaskTypecheck = tasks.TaskTypecheck
	TaskTest      = tasks.TaskTest
)

func loadConfig() (*Config, error)                  { return core.LoadConfig() }
func findService(cfg *Config, name string) *Service { return core.FindService(cfg, name) }
