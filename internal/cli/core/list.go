package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
)

// ListServices prints all configured services and commands.
func ListServices(config *Config, availableTasks func(Service) []string) {
	p := output.DefaultPrinter()
	p.Summary("Available services:")
	p.Blank()

	for _, svc := range config.Services {
		p.Summary(fmt.Sprintf("  %s - %s", svc.Name, svc.Description))
		p.Summary(fmt.Sprintf("    Path: %s", svc.Path))

		if len(svc.Depends) > 0 {
			p.Summary(fmt.Sprintf("    Depends: %s", strings.Join(svc.Depends, ", ")))
		}
		if len(svc.DevDepends) > 0 {
			p.Summary(fmt.Sprintf("    DevDepends: %s", strings.Join(svc.DevDepends, ", ")))
		}

		cmds := make([]string, 0, len(svc.Commands)+8)
		for _, task := range availableTasks(svc) {
			cmds = append(cmds, task)
		}
		for cmdName := range svc.Commands {
			cmds = append(cmds, cmdName)
		}
		if strings.TrimSpace(svc.Dev) != "" {
			cmds = append(cmds, "dev")
		}
		sort.Strings(cmds)

		p.Summary(fmt.Sprintf("    Commands: %s", strings.Join(cmds, ", ")))
		p.Blank()
	}

	if config.Local != nil && len(config.Local.Resources) > 0 {
		p.Summary("Local infrastructure resources:")
		p.Blank()
		for _, res := range config.Local.Resources {
			p.Summary(fmt.Sprintf("  %s - %s", res.Name, res.Description))
			if res.PortForward != nil {
				p.Summary(fmt.Sprintf("    Port-forward: localhost:%d -> %s:%d", res.PortForward.LocalPort, res.PortForward.Target, res.PortForward.TargetPort))
			}
			p.Blank()
		}
	}
}
