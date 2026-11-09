package tasks

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type TaskRunOptions struct {
	NoCache           bool
	JUnitDir          string
	Concurrency       int
	Integration       bool
	Packages          []string
	Run               string
	List              bool
	FullSuite         bool
	GoConcurrency     int
	Context           context.Context
	BaseRef           string
	All               bool
	BufferOutput      bool
	ContinueOnFailure bool
	Progress          *TaskProgressSession
	PreparedRun       *PreparedTaskRun
	progressIDs       map[TaskNode]string
}

func parseTaskInvocationArgs(args []string, defaultConcurrency int) ([]string, TaskRunOptions, error) {
	opts := TaskRunOptions{Concurrency: defaultConcurrency}
	services := make([]string, 0, len(args))
	sawBase := false
	sawRun := false
	sawGoConcurrency := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-cache":
			opts.NoCache = true
		case arg == "--all":
			opts.All = true
		case arg == "--integration":
			opts.Integration = true
		case arg == "--list":
			opts.List = true
		case arg == "--full-suite":
			opts.FullSuite = true
		case arg == "--package":
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--package requires a value")
			}
			v := strings.TrimSpace(args[i+1])
			if v == "" {
				return nil, opts, fmt.Errorf("--package requires a non-empty value")
			}
			opts.Packages = append(opts.Packages, v)
			i++
		case strings.HasPrefix(arg, "--package="):
			v := strings.TrimSpace(strings.TrimPrefix(arg, "--package="))
			if v == "" {
				return nil, opts, fmt.Errorf("--package requires a non-empty value")
			}
			opts.Packages = append(opts.Packages, v)
		case arg == "--run":
			if sawRun {
				return nil, opts, fmt.Errorf("--run may only be specified once")
			}
			sawRun = true
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--run requires a value")
			}
			opts.Run = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--run="):
			if sawRun {
				return nil, opts, fmt.Errorf("--run may only be specified once")
			}
			sawRun = true
			opts.Run = strings.TrimSpace(strings.TrimPrefix(arg, "--run="))
		case arg == "--go-concurrency":
			if sawGoConcurrency {
				return nil, opts, fmt.Errorf("--go-concurrency may only be specified once")
			}
			sawGoConcurrency = true
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--go-concurrency requires a value")
			}
			v, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if err != nil || v <= 0 {
				return nil, opts, fmt.Errorf("--go-concurrency requires a positive integer")
			}
			opts.GoConcurrency = v
			i++
		case strings.HasPrefix(arg, "--go-concurrency="):
			if sawGoConcurrency {
				return nil, opts, fmt.Errorf("--go-concurrency may only be specified once")
			}
			sawGoConcurrency = true
			v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--go-concurrency=")))
			if err != nil || v <= 0 {
				return nil, opts, fmt.Errorf("--go-concurrency requires a positive integer")
			}
			opts.GoConcurrency = v
		case arg == "--base":
			sawBase = true
			if i+1 >= len(args) {
				return nil, opts, fmt.Errorf("--base requires a value")
			}
			opts.BaseRef = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--base="):
			sawBase = true
			opts.BaseRef = strings.TrimSpace(strings.TrimPrefix(arg, "--base="))
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

	if sawBase && opts.BaseRef == "" {
		return nil, opts, fmt.Errorf("--base requires a non-empty value")
	}
	if sawRun {
		if opts.Run == "" {
			return nil, opts, fmt.Errorf("--run requires a non-empty value")
		}
		if _, err := regexp.Compile(opts.Run); err != nil {
			return nil, opts, fmt.Errorf("invalid --run regular expression")
		}
	}
	if (len(opts.Packages) > 0 || sawRun || opts.List || opts.FullSuite || sawGoConcurrency) && !opts.Integration {
		return nil, opts, fmt.Errorf("--package, --run, --list, --full-suite, and --go-concurrency require --integration")
	}
	if opts.FullSuite && (len(opts.Packages) > 0 || sawRun || opts.List) {
		return nil, opts, fmt.Errorf("--full-suite cannot be combined with focused selection or --list")
	}

	return services, opts, nil
}
