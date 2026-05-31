package quality

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
	"github.com/scottcrooks/mono/internal/cli/selection"
	"github.com/scottcrooks/mono/internal/cli/tasks"
)

type checkCLICommand struct{}

var runCheckTaskPhase = runOrchestratedTaskRequestWithConfig
var runCheckDependencyInstalls = runDependencyInstallsWithConfig

func init() {
	registerCommand("check", &checkCLICommand{})
}

func (c *checkCLICommand) Run(args []string) error {
	printer := output.DefaultPrinter()

	baseRef, all, opts, err := parseCheckArgs(args[2:])
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	targetServices, err := selection.ResolveTargetServices(cfg, nil, baseRef, all)
	if err != nil {
		return err
	}

	if len(targetServices) == 0 {
		if all {
			printer.Summary("No services configured. Nothing to check.")
			return nil
		}
		printer.Summary("No impacted services. Nothing to check.")
		return nil
	}

	installResults, err := runCheckDependencyInstalls(cfg, targetServices)
	if err != nil {
		printDependencyInstallSummary(installResults)
		return fmt.Errorf("check dependency installs failed: %w", err)
	}
	if len(installResults) > 0 {
		printDependencyInstallSummary(installResults)
	}

	plan := buildPendingCheckPlan(cfg, targetServices)
	aggregateSummary := tasks.TaskRunSummary{}
	phaseCount := 0
	var phaseErrCount int
	for _, phase := range plan.Phases {
		if len(phase.Services) == 0 {
			continue
		}

		phaseCount++
		results, phaseErr := runCheckTaskPhase(cfg, TaskRequest{
			Task:          phase.Task,
			Services:      phase.Services,
			ExactServices: true,
		}, opts)
		phaseSummary := tasks.SummarizeTaskResults(results)
		aggregateSummary.Succeeded += phaseSummary.Succeeded
		aggregateSummary.Failed += phaseSummary.Failed
		aggregateSummary.Skipped += phaseSummary.Skipped
		if phaseErr != nil {
			phaseErrCount++
		}
	}

	if phaseCount == 0 {
		if all {
			printer.Summary("No pending check tasks for configured services.")
			return nil
		}
		printer.Summary("No pending check tasks for impacted services.")
		return nil
	}

	label := "impacted"
	if all {
		label = "services"
	}
	printer.Summary(fmt.Sprintf("Check complete: %s=%d phases=%d succeeded=%d failed=%d skipped=%d", label, len(plan.ImpactedServices), phaseCount, aggregateSummary.Succeeded, aggregateSummary.Failed, aggregateSummary.Skipped))
	if phaseErrCount > 0 {
		return fmt.Errorf("check failed: %d phase(s) had task failures", phaseErrCount)
	}
	return nil
}

func parseCheckArgs(args []string) (baseRef string, all bool, opts TaskRunOptions, err error) {
	opts = TaskRunOptions{Concurrency: defaultTaskConcurrency(), BufferOutput: true, ContinueOnFailure: true}
	sawBase := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-cache":
			opts.NoCache = true
		case arg == "--no-buffer":
			opts.BufferOutput = false
		case arg == "--all":
			all = true
		case arg == "--base":
			sawBase = true
			if i+1 >= len(args) {
				return "", all, opts, fmt.Errorf("--base requires a value")
			}
			baseRef = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--base="):
			sawBase = true
			baseRef = strings.TrimSpace(strings.TrimPrefix(arg, "--base="))
		case arg == "--concurrency":
			if i+1 >= len(args) {
				return "", all, opts, fmt.Errorf("--concurrency requires a value")
			}
			v, convErr := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if convErr != nil || v <= 0 {
				return "", all, opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
			i++
		case strings.HasPrefix(arg, "--concurrency="):
			v, convErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--concurrency=")))
			if convErr != nil || v <= 0 {
				return "", all, opts, fmt.Errorf("--concurrency requires a positive integer")
			}
			opts.Concurrency = v
		default:
			return "", all, opts, fmt.Errorf("unknown argument %q (usage: mono check [--base <ref>] [--all] [--no-cache] [--no-buffer] [--concurrency N])", arg)
		}
	}

	if sawBase && strings.TrimSpace(baseRef) == "" {
		return "", all, opts, fmt.Errorf("--base requires a non-empty value")
	}

	return baseRef, all, opts, nil
}
