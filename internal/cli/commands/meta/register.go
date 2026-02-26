package meta

import "github.com/scottcrooks/mono/internal/cli/core"

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
