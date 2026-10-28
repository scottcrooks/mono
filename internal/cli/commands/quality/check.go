package quality

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
	"github.com/scottcrooks/mono/internal/cli/selection"
	"github.com/scottcrooks/mono/internal/cli/tasks"
)

type checkCLICommand struct{}

var runCheckTaskPhase = runOrchestratedTaskRequestWithConfig
var runCheckDependencyInstalls = runDependencyInstallsWithConfig
var runCheckDependencyInstallsWithProgress = runDependencyInstallsWithProgress
var prepareCheckTaskRun = tasks.PrepareTaskRun

func init() {
	registerCommand("check", &checkCLICommand{})
}

func (c *checkCLICommand) Run(args []string) error {
	printer := output.DefaultPrinter()

	baseRef, all, opts, err := parseCheckArgs(args[2:])
	if err != nil {
		return err
	}
	if opts.JUnitDir != "" {
		if err := validateCheckJUnitDir(&opts); err != nil {
			return err
		}
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

	mode := output.DetectMode(os.Stdout)
	var installLine *output.ProgressLine
	if opts.BufferOutput && output.TerminalProgressSupported(os.Stderr, mode) {
		installLine = output.NewProgressLine(output.ProgressOptions{Writer: os.Stderr, Mode: mode})
	}
	installResults, err := runCheckDependencyInstallsWithProgress(cfg, targetServices, installLine)
	if installLine != nil {
		installLine.Close()
	}
	if err != nil {
		printDependencyInstallSummary(installResults)
		return fmt.Errorf("check dependency installs failed: %w", err)
	}
	if len(installResults) > 0 {
		printDependencyInstallSummary(installResults)
	}

	formatTargets, err := buildChangedFilesByService(cfg, baseRef)
	if err != nil {
		return fmt.Errorf("building changed file targets: %w", err)
	}

	plan := buildPendingCheckPlan(cfg, targetServices)
	aggregateSummary := tasks.TaskRunSummary{}
	type preparedPhase struct {
		request TaskRequest
		run     *tasks.PreparedTaskRun
	}
	preparedPhases := make([]preparedPhase, 0, len(plan.Phases))
	preparedRuns := make([]*tasks.PreparedTaskRun, 0, len(plan.Phases))
	for _, phase := range plan.Phases {
		if len(phase.Services) == 0 {
			continue
		}
		req := TaskRequest{Task: phase.Task, Services: phase.Services, ExactServices: true}
		if phase.Task == TaskFormat {
			req.FileTargets = formatTargets
		}
		prepared, prepareErr := prepareCheckTaskRun(cfg, req)
		if prepareErr != nil {
			return fmt.Errorf("preparing check phase %s: %w", phase.Task, prepareErr)
		}
		preparedPhases = append(preparedPhases, preparedPhase{request: req, run: prepared})
		preparedRuns = append(preparedRuns, prepared)
	}
	phaseCount := len(preparedPhases)
	var phaseErrCount int
	var session *tasks.TaskProgressSession
	if phaseCount > 0 {
		session = tasks.NewTaskProgressSession(preparedRuns, opts)
		defer session.Close()
	}
	for _, phase := range preparedPhases {
		phaseOpts := opts
		phaseOpts.Progress = session
		phaseOpts.PreparedRun = phase.run
		results, phaseErr := runCheckTaskPhase(cfg, phase.request, phaseOpts)
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
	if session != nil {
		session.Close()
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
		case arg == "--junit":
			if i+1 >= len(args) {
				return "", all, opts, fmt.Errorf("--junit requires a directory")
			}
			opts.JUnitDir = strings.TrimSpace(args[i+1])
			if opts.JUnitDir == "" || strings.HasPrefix(opts.JUnitDir, "--") {
				return "", all, opts, fmt.Errorf("--junit requires a non-empty directory")
			}
			opts.NoCache = true
			i++
		case strings.HasPrefix(arg, "--junit="):
			opts.JUnitDir = strings.TrimSpace(strings.TrimPrefix(arg, "--junit="))
			if opts.JUnitDir == "" {
				return "", all, opts, fmt.Errorf("--junit requires a non-empty directory")
			}
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
			return "", all, opts, fmt.Errorf("unknown argument %q (usage: mono check [--base <ref>] [--all] [--no-cache] [--junit DIR] [--no-buffer] [--concurrency N])", arg)
		}
	}

	if sawBase && strings.TrimSpace(baseRef) == "" {
		return "", all, opts, fmt.Errorf("--base requires a non-empty value")
	}

	return baseRef, all, opts, nil
}

func validateCheckJUnitDir(opts *TaskRunOptions) error {
	abs, err := filepath.Abs(opts.JUnitDir)
	if err != nil {
		return fmt.Errorf("resolving --junit directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("creating --junit directory: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("reading --junit directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("--junit directory must be empty: %s", abs)
	}
	opts.JUnitDir = abs
	opts.NoCache = true
	return nil
}
