package core

import "sort"

// Command is the strategy interface for mono commands.
type Command interface {
	Run(args []string) error
}

// Registry stores registered top-level commands.
type Registry struct {
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: map[string]Command{}}
}

func (r *Registry) Register(name string, cmd Command) {
	r.commands[name] = cmd
}

func (r *Registry) Lookup(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.commands))
	for name := range r.commands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
