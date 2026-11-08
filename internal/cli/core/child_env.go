package core

import (
	"context"
	"os/exec"
	"strings"
)

// WithDefaultCI adds CI=true to a child environment when CI is not present.
// An explicitly empty or false value is preserved.
func WithDefaultCI(env []string) []string {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "CI" {
			return env
		}
	}
	return append(append([]string(nil), env...), "CI=true")
}

// CommandWithDefaultCI constructs a subprocess command with the default child CI policy.
func CommandWithDefaultCI(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = WithDefaultCI(cmd.Environ())
	return cmd
}

// CommandContextWithDefaultCI constructs a context-bound subprocess command with the default child CI policy.
func CommandContextWithDefaultCI(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = WithDefaultCI(cmd.Environ())
	return cmd
}
