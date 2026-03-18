package quality

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type checkCLICommand struct{}

var runCheckTaskPhase = runOrchestratedTaskRequestWithConfig
var runCheckDependencyInstalls = runDependencyInstallsWithConfig

func init() {
	registerCommand("check", &checkCLICommand{})
}

func (c *checkCLICommand) Run(args []string) error {
	printer := output.DefaultPrinter()

	baseRef, opts, err := parseCheckArgs(args[2:])
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	report, err := buildImpactReport(cfg, baseRef, false)
	if err != nil {
		return err
	}

	if len(report.Impacted) == 0 {
		printer.Summary("No impacted services. Nothing to check.")
		return nil
	}

	installResults, err := runCheckDependencyInstalls(cfg, report.Impacted)
	if err != nil {
		printDependencyInstallSummary(installResults)
		return fmt.Errorf("check dependency installs failed: %w", err)
	}
	if len(installResults) > 0 {
		printDependencyInstallSummary(installResults)
	}

	plan := buildPendingCheckPlan(cfg, report.Impacted)
	phaseCount := 0
	for _, phase := range plan.Phases {
		if len(phase.Services) == 0 {
			continue
		}

		phaseCount++
		printer.StepStart("check phase", fmt.Sprintf("%s (%d service(s))", phase.Task, len(phase.Services)))

		results, phaseErr := runCheckTaskPhase(cfg, TaskRequest{
			Task:          phase.Task,
			Services:      phase.Services,
			ExactServices: true,
		}, opts)
		printTaskSummary(results)
		if phaseErr != nil {
			return fmt.Errorf("check phase %q failed: %w", phase.Task, phaseErr)
		}
	}

	if phaseCount == 0 {
		printer.Summary("No pending check tasks for impacted services.")
		return nil
	}

	printer.Summary(fmt.Sprintf("Check complete: impacted=%d phases=%d", len(plan.ImpactedServices), phaseCount))
	return nil
}

func parseCheckArgs(args []string) (baseRef string, opts TaskRunOptions, err error) {
	opts = TaskRunOptions{Concurrency: defaultTaskConcurrency()}
	sawBase := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-cache":
			opts.NoCache = true
		case arg == "--base":
			sawBase = true
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--base requires a value")
			}
			baseRef = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--base="):
			sawBase = true
			baseRef = strings.TrimSpace(strings.TrimPrefix(arg, "--base="))
		case arg == "--concurrency":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--concurrency requires a value")
			}
			v, convErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if convErr != nil || v <= 0 {
				return "", opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
			i++
		case strings.HasPrefix(arg, "--concurrency="):
			v, convErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--concurrency=")))
			if convErr != nil || v <= 0 {
				return "", opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
		default:
			return "", opts, fmt.Errorf("unknown argument %q (usage: mono check [--base <ref>] [--no-cache] [--concurrency N])", arg)
		}
	}

	if sawBase && strings.TrimSpace(baseRef) == "" {
		return "", opts, fmt.Errorf("--base requires a non-empty value")
	}

	return baseRef, opts, nil
}
