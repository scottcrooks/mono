package selection

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/core"
)

func ResolveTargetServices(cfg *core.Config, explicit []string, baseRef string, all bool) ([]string, error) {
	if all && len(explicit) > 0 {
		return nil, fmt.Errorf("--all cannot be combined with explicit service names")
	}
	if len(explicit) > 0 {
		return append([]string(nil), explicit...), nil
	}
	if all {
		return allServiceNames(cfg), nil
	}

	report, err := buildImpactReport(cfg, baseRef)
	if err != nil {
		return nil, err
	}
	return report.Impacted, nil
}

func allServiceNames(cfg *core.Config) []string {
	names := make([]string, 0, len(cfg.Services))
	for _, svc := range cfg.Services {
		names = append(names, svc.Name)
	}
	sort.Strings(names)
	return names
}

type impactReport struct {
	Impacted []string
}

const localDiffBaseRef = "local"

func buildImpactReport(cfg *core.Config, baseFlag string) (*impactReport, error) {
	baseRef, err := resolveBaseRef(baseFlag)
	if err != nil {
		return nil, err
	}

	files, err := changedFiles(baseRef)
	if err != nil {
		return nil, err
	}

	changed := mapFilesToChangedServices(cfg, files)
	reverse := buildReverseDeps(cfg)
	return &impactReport{Impacted: computeImpactedClosure(changed, reverse)}, nil
}

func resolveBaseRef(baseFlag string) (string, error) {
	if baseFlag != "" {
		if _, err := gitOutput("rev-parse", "--verify", "--quiet", baseFlag+"^{commit}"); err != nil {
			return "", fmt.Errorf("invalid --base %q: %w", baseFlag, err)
		}
		return baseFlag, nil
	}
	baseRef, err := defaultMergeBaseBranch()
	if err != nil {
		return localDiffBaseRef, nil
	}
	return baseRef, nil
}

func changedFiles(baseRef string) ([]string, error) {
	committed := []string(nil)
	if baseRef != localDiffBaseRef {
		var err error
		committed, err = gitNameOnlyDiff(baseRef + "...HEAD")
		if err != nil {
			return nil, err
		}
	}
	staged, err := gitStagedFiles()
	if err != nil {
		return nil, err
	}
	unstaged, err := gitUnstagedFiles()
	if err != nil {
		return nil, err
	}
	untracked, err := gitUntrackedFiles()
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(committed)+len(staged)+len(unstaged)+len(untracked))
	for _, file := range committed {
		set[file] = struct{}{}
	}
	for _, file := range staged {
		set[file] = struct{}{}
	}
	for _, file := range unstaged {
		set[file] = struct{}{}
	}
	for _, file := range untracked {
		set[file] = struct{}{}
	}

	files := sortedKeys(set)
	sort.Strings(files)
	return files, nil
}

func gitNameOnlyDiff(refRange string) ([]string, error) {
	out, err := gitOutput("diff", "--name-only", refRange)
	if err != nil {
		return nil, err
	}
	return parseNameOnlyLines(out), nil
}

func gitStagedFiles() ([]string, error) {
	out, err := gitOutput("diff", "--name-only", "--cached")
	if err != nil {
		return nil, err
	}
	return parseNameOnlyLines(out), nil
}

func gitUnstagedFiles() ([]string, error) {
	out, err := gitOutput("diff", "--name-only")
	if err != nil {
		return nil, err
	}
	return parseNameOnlyLines(out), nil
}

func gitUntrackedFiles() ([]string, error) {
	out, err := gitOutput("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return parseNameOnlyLines(out), nil
}

func parseNameOnlyLines(raw string) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		files = append(files, filepath.ToSlash(trimmed))
	}
	return files
}

func mapFilesToChangedServices(cfg *core.Config, files []string) []string {
	changed := make(map[string]struct{})
	for _, file := range files {
		if service := owningService(cfg, file); service != "" {
			changed[service] = struct{}{}
		}
	}
	return sortedKeys(changed)
}

func owningService(cfg *core.Config, file string) string {
	file = filepath.ToSlash(file)

	bestName := ""
	bestLen := -1
	for _, svc := range cfg.Services {
		prefix := normalizeServicePath(svc.Path)
		if prefix == "" {
			continue
		}
		if file == prefix || strings.HasPrefix(file, prefix+"/") {
			if len(prefix) > bestLen {
				bestName = svc.Name
				bestLen = len(prefix)
			}
		}
	}
	return bestName
}

func normalizeServicePath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}

func buildReverseDeps(cfg *core.Config) map[string][]string {
	reverse := make(map[string][]string, len(cfg.Services))
	for _, svc := range cfg.Services {
		if _, ok := reverse[svc.Name]; !ok {
			reverse[svc.Name] = nil
		}
		for _, dep := range svc.Depends {
			reverse[dep] = append(reverse[dep], svc.Name)
		}
	}
	for key := range reverse {
		sort.Strings(reverse[key])
	}
	return reverse
}

func computeImpactedClosure(changed []string, reverse map[string][]string) []string {
	visited := make(map[string]struct{}, len(changed))
	queue := append([]string(nil), changed...)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if _, ok := visited[curr]; ok {
			continue
		}
		visited[curr] = struct{}{}

		for _, dep := range reverse[curr] {
			if _, ok := visited[dep]; !ok {
				queue = append(queue, dep)
			}
		}
	}
	return sortedKeys(visited)
}

func sortedKeys[T any](set map[string]T) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func defaultMergeBaseBranch() (string, error) {
	if out, err := gitOutput("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		branch := strings.TrimSpace(out)
		if branch != "" {
			return branch, nil
		}
	}

	for _, candidate := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := gitOutput("rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not determine default base branch (tried origin/HEAD, main, master)")
}
