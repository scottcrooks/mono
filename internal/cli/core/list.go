package core

import (
	"fmt"
	"sort"
	"strings"
)

// ListServices prints all configured services and commands.
func ListServices(config *Config, availableTasks func(Service) []string) {
	fmt.Println("Available services:")
	fmt.Println()

	for _, svc := range config.Services {
		fmt.Printf("  %s - %s\n", svc.Name, svc.Description)
		fmt.Printf("    Path: %s\n", svc.Path)

		if len(svc.Depends) > 0 {
			fmt.Printf("    Depends: %s\n", strings.Join(svc.Depends, ", "))
		}

		fmt.Printf("    Commands: ")

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

		if len(cmds) > 0 {
			for i, cmd := range cmds {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(cmd)
			}
		}
		fmt.Println()
		fmt.Println()
	}

	if config.Local != nil && len(config.Local.Resources) > 0 {
		fmt.Println("Local infrastructure resources:")
		fmt.Println()
		for _, res := range config.Local.Resources {
			fmt.Printf("  %s - %s\n", res.Name, res.Description)
			if res.PortForward != nil {
				fmt.Printf("    Port-forward: localhost:%d -> %s:%d\n", res.PortForward.LocalPort, res.PortForward.Target, res.PortForward.TargetPort)
			}
			fmt.Println()
		}
	}
}
