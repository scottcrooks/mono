package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
	"gopkg.in/yaml.v3"
)

type DependencyInstallTarget struct {
	Archetype string
	Dir       string
	Command   string
	Services  []string
}

type DependencyInstallResult struct {
	Target DependencyInstallTarget
	Status TaskRunStatus
	Err    error
}

var runDependencyInstallCommand = executeDependencyInstallCommand

func DependencyInstallTargetsForServices(cfg *Config, services []string) ([]DependencyInstallTarget, error) {
	selected, err := selectServicesExact(cfg, services)
	if err != nil {
		return nil, err
	}

	targetsByKey := make(map[string]*DependencyInstallTarget)
	for _, svc := range selected {
		target, ok, err := dependencyInstallTargetForService(svc)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		key := target.Archetype + "\x00" + filepath.Clean(target.Dir) + "\x00" + target.Command
		existing, found := targetsByKey[key]
		if !found {
			copyTarget := target
			targetsByKey[key] = &copyTarget
			existing = &copyTarget
		}
		existing.Services = append(existing.Services, svc.Name)
	}

	targets := make([]DependencyInstallTarget, 0, len(targetsByKey))
	for _, target := range targetsByKey {
		sort.Strings(target.Services)
		targets = append(targets, *target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Dir != targets[j].Dir {
			return targets[i].Dir < targets[j].Dir
		}
		if targets[i].Archetype != targets[j].Archetype {
			return targets[i].Archetype < targets[j].Archetype
		}
		return targets[i].Command < targets[j].Command
	})

	return targets, nil
}

func RunDependencyInstallsWithConfig(cfg *Config, services []string) ([]DependencyInstallResult, error) {
	targets, err := DependencyInstallTargetsForServices(cfg, services)
	if err != nil {
		return nil, err
	}

	printer := output.DefaultPrinter()
	results := make([]DependencyInstallResult, 0, len(targets))
	failures := 0

	for _, target := range targets {
		label := fmt.Sprintf("deps:%s", target.Archetype)
		details := fmt.Sprintf("%s (%s)", target.Dir, strings.Join(target.Services, ", "))
		printer.StepStart(label, details)

		if err := runDependencyInstallCommand(context.Background(), target); err != nil {
			printer.StepErr(label, "failed")
			results = append(results, DependencyInstallResult{
				Target: target,
				Status: TaskStatusFailed,
				Err:    err,
			})
			failures++
			continue
		}

		printer.StepOK(label, "completed")
		results = append(results, DependencyInstallResult{
			Target: target,
			Status: TaskStatusSucceeded,
		})
	}

	if failures > 0 {
		return results, fmt.Errorf("%d dependency install target(s) failed", failures)
	}
	return results, nil
}

func PrintDependencyInstallSummary(results []DependencyInstallResult) {
	summary := TaskRunSummary{}
	for _, result := range results {
		switch result.Status {
		case TaskStatusSucceeded:
			summary.Succeeded++
		case TaskStatusFailed:
			summary.Failed++
		case TaskStatusSkipped:
			summary.Skipped++
		}
	}

	printer := output.DefaultPrinter()
	printer.Blank()
	printer.Summary(fmt.Sprintf("Dependency install summary: succeeded=%d failed=%d skipped=%d", summary.Succeeded, summary.Failed, summary.Skipped))
}

func dependencyInstallTargetForService(svc Service) (DependencyInstallTarget, bool, error) {
	switch svc.Archetype {
	case "go":
		dir, ok, err := findNearestRepoFileDir(svc.Path, "go.mod")
		if err != nil || !ok {
			return DependencyInstallTarget{}, ok, err
		}
		return DependencyInstallTarget{
			Archetype: svc.Archetype,
			Dir:       dir,
			Command:   "go mod download",
		}, true, nil
	case "react":
		dir, ok, err := reactWorkspaceInstallDir(svc.Path)
		if err != nil {
			return DependencyInstallTarget{}, false, err
		}
		if ok {
			return DependencyInstallTarget{
				Archetype: svc.Archetype,
				Dir:       dir,
				Command:   "pnpm install",
			}, true, nil
		}

		dir, ok, err = findNearestRepoFileDir(svc.Path, "package.json")
		if err != nil || !ok {
			return DependencyInstallTarget{}, ok, err
		}
		return DependencyInstallTarget{
			Archetype: svc.Archetype,
			Dir:       dir,
			Command:   "pnpm install",
		}, true, nil
	default:
		return DependencyInstallTarget{}, false, nil
	}
}

type pnpmWorkspace struct {
	Packages []string `yaml:"packages"`
}

func reactWorkspaceInstallDir(servicePath string) (string, bool, error) {
	dir, ok, err := findNearestRepoFileDir(servicePath, "pnpm-workspace.yaml")
	if err != nil || !ok {
		return dir, ok, err
	}

	included, err := pnpmWorkspaceIncludesService(dir, servicePath)
	if err != nil {
		return "", false, err
	}
	if !included {
		return "", false, nil
	}
	return dir, true, nil
}

func pnpmWorkspaceIncludesService(workspaceDir, servicePath string) (bool, error) {
	workspaceFile := filepath.Join(workspaceDir, "pnpm-workspace.yaml")
	data, err := os.ReadFile(workspaceFile)
	if err != nil {
		return false, err
	}

	var cfg pnpmWorkspace
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse %s: %w", workspaceFile, err)
	}

	relativePath := normalizeServicePath(servicePath)
	if workspaceDir != "." {
		relativePath, err = filepath.Rel(workspaceDir, servicePath)
		if err != nil {
			return false, fmt.Errorf("resolve %s relative to %s: %w", servicePath, workspaceDir, err)
		}
		relativePath = normalizeServicePath(relativePath)
	}

	included := false
	for _, pattern := range cfg.Packages {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		exclude := strings.HasPrefix(pattern, "!")
		if exclude {
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
		}
		if pattern == "" {
			continue
		}

		if !matchPathPattern(normalizeServicePath(pattern), relativePath) {
			continue
		}
		if exclude {
			included = false
			continue
		}
		included = true
	}

	return included, nil
}

func matchPathPattern(pattern, path string) bool {
	patternParts := splitPathParts(pattern)
	pathParts := splitPathParts(path)
	return matchPathPatternParts(patternParts, pathParts)
}

func splitPathParts(path string) []string {
	normalized := normalizeServicePath(path)
	if normalized == "" || normalized == "." {
		return nil
	}
	return strings.Split(normalized, "/")
}

func matchPathPatternParts(patternParts, pathParts []string) bool {
	if len(patternParts) == 0 {
		return len(pathParts) == 0
	}

	if patternParts[0] == "**" {
		if matchPathPatternParts(patternParts[1:], pathParts) {
			return true
		}
		if len(pathParts) == 0 {
			return false
		}
		return matchPathPatternParts(patternParts, pathParts[1:])
	}

	if len(pathParts) == 0 {
		return false
	}

	matched, err := filepath.Match(patternParts[0], pathParts[0])
	if err != nil || !matched {
		return false
	}
	return matchPathPatternParts(patternParts[1:], pathParts[1:])
}

func findNearestRepoFileDir(startPath, filename string) (string, bool, error) {
	curr := filepath.Clean(strings.TrimSpace(startPath))
	if curr == "" {
		curr = "."
	}

	for {
		candidate := filepath.Join(curr, filename)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return curr, true, nil
			}
			return "", false, fmt.Errorf("%s exists but is not a file", candidate)
		}
		if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}

		if curr == "." {
			return "", false, nil
		}

		next := filepath.Dir(curr)
		if next == curr {
			return "", false, nil
		}
		curr = next
	}
}

func executeDependencyInstallCommand(ctx context.Context, target DependencyInstallTarget) error {
	parts := strings.Fields(target.Command)
	cmd, err := commandFromParts(ctx, parts)
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(target.Dir)
	if err != nil {
		return err
	}
	cmd.Dir = absPath
	cmd.Stdout = output.NewPrefixWriter(fmt.Sprintf("[deps:%s]", target.Archetype), os.Stdout)
	cmd.Stderr = output.NewPrefixWriter(fmt.Sprintf("[deps:%s]", target.Archetype), os.Stderr)
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s in %s: %w", target.Command, target.Dir, err)
	}
	return nil
}
