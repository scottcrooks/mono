package cli

import (
	"fmt"
	"os"

	"github.com/scottcrooks/mono/internal/cli/commands/insights"
	metacmd "github.com/scottcrooks/mono/internal/cli/commands/meta"
	"github.com/scottcrooks/mono/internal/cli/commands/quality"
	runtimecmd "github.com/scottcrooks/mono/internal/cli/commands/runtime"
	"github.com/scottcrooks/mono/internal/cli/commands/workflow"
	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/tasks"
	"github.com/scottcrooks/mono/internal/version"
)

var registry = core.NewRegistry()
var commandsRegistered bool

func registerCommands() {
	if commandsRegistered {
		return
	}
	insights.Register(registry)
	quality.Register(registry)
	runtimecmd.Register(registry)
	workflow.Register(registry)
	metacmd.Register(registry)
	commandsRegistered = true
}

// Run executes the mono CLI and returns a process exit code.
func Run(args []string) int {
	registerCommands()

	if len(args) < 2 {
		core.PrintUsage()
		return 1
	}

	switch args[1] {
	case "--help", "-h", "help":
		core.PrintUsage()
		return 0
	case "--version", "version":
		printVersion()
		return 0
	}

	command := args[1]
	if cmd, ok := registry.Lookup(command); ok {
		if err := cmd.Run(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if _, ok := tasks.ParseTaskName(command); ok {
		if err := tasks.RunOrchestratedTask(command, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := core.RunServiceCommand(command, args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func printVersion() {
	fmt.Printf("Version: %s\n", version.Version)
	fmt.Printf("Commit: %s\n", version.Commit)
	fmt.Printf("Date: %s\n", version.Date)
}
