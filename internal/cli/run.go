package cli

import (
	"fmt"
	"os"

	"github.com/scottcrooks/mono/internal/version"
)

// Run executes the mono CLI and returns a process exit code.
func Run(args []string) int {
	if len(args) < 2 {
		printUsage()
		return 1
	}

	switch args[1] {
	case "--help", "-h", "help":
		printUsage()
		return 0
	case "--version", "version":
		printVersion()
		return 0
	}

	command := args[1]
	if cmd, ok := commands[command]; ok {
		if err := cmd.Run(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := runServiceCommand(command, args); err != nil {
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
