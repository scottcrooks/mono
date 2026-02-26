package tasks

import (
	"fmt"
	"strconv"
	"strings"
)

type TaskRunOptions struct {
	NoCache     bool
	Concurrency int
	Integration bool
}

func parseTaskInvocationArgs(args []string, defaultConcurrency int) ([]string, TaskRunOptions, error) {
	opts := TaskRunOptions{Concurrency: defaultConcurrency}
	services := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-cache":
			opts.NoCache = true
		case arg == "--integration":
			opts.Integration = true
		case arg == "--concurrency":
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--concurrency requires a value")
			}
			v, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if err != nil || v <= 0 {
				return nil, opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
			i++
		case strings.HasPrefix(arg, "--concurrency="):
			v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--concurrency=")))
			if err != nil || v <= 0 {
				return nil, opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
		case strings.HasPrefix(arg, "--"):
			return nil, opts, fmt.Errorf("unknown argument %q", arg)
		default:
			services = append(services, arg)
		}
	}

	return services, opts, nil
}
