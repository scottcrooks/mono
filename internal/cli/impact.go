package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type ImpactReport struct {
	BaseRef  string
	Changed  []string
	Impacted []string
	Explain  map[string][]string
}

type CheckTaskPreview struct {
	Service string
	Present []string
	Missing []string
}

func buildImpactReport(cfg *Config, baseFlag string, includeExplain bool) (*ImpactReport, error) {
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
	impacted := computeImpactedClosure(changed, reverse)

	report := &ImpactReport{
		BaseRef:  baseRef,
		Changed:  changed,
		Impacted: impacted,
	}
	if includeExplain {
		report.Explain = computeExplainChains(changed, reverse)
	}

	return report, nil
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
		return "", err
	}
	return baseRef, nil
}

func changedFiles(baseRef string) ([]string, error) {
	committed, err := gitNameOnlyDiff(baseRef + "...HEAD")
	if err != nil {
		return nil, err
	}
	workingTree, err := gitNameOnlyDiff("HEAD")
	if err != nil {
		return nil, err
	}
	untracked, err := gitUntrackedFiles()
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(committed)+len(workingTree)+len(untracked))
	for _, file := range committed {
		set[file] = struct{}{}
	}
	for _, file := range workingTree {
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

func mapFilesToChangedServices(cfg *Config, files []string) []string {
	changed := make(map[string]struct{})
	for _, file := range files {
		if service := owningService(cfg, file); service != "" {
			changed[service] = struct{}{}
		}
	}
	return sortedKeys(changed)
}

func owningService(cfg *Config, file string) string {
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

func buildReverseDeps(cfg *Config) map[string][]string {
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

func computeExplainChains(changed []string, reverse map[string][]string) map[string][]string {
	result := make(map[string]map[string]struct{})
	for _, root := range changed {
		chains := map[string]string{
			root: root,
		}
		queue := []string{root}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]

			path := chains[curr]
			if _, ok := result[curr]; !ok {
				result[curr] = make(map[string]struct{})
			}
			result[curr][path] = struct{}{}

			for _, next := range reverse[curr] {
				nextChain := path + " -> " + next
				if _, seen := chains[next]; seen {
					continue
				}
				chains[next] = nextChain
				queue = append(queue, next)
			}
		}
	}

	out := make(map[string][]string, len(result))
	for svc, set := range result {
		chains := sortedKeys(set)
		out[svc] = chains
	}
	return out
}

func buildCheckTaskPreview(cfg *Config, impacted []string) []CheckTaskPreview {
	required := []string{"lint", "typecheck", "test"}
	rows := make([]CheckTaskPreview, 0, len(impacted))
	for _, name := range impacted {
		svc := findService(cfg, name)
		if svc == nil {
			continue
		}
		row := CheckTaskPreview{
			Service: name,
		}
		for _, cmd := range required {
			task := TaskName(cmd)
			_, supported, _ := taskCommandForService(*svc, task)
			if supported {
				row.Present = append(row.Present, cmd)
			} else {
				row.Missing = append(row.Missing, cmd)
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Service < rows[j].Service
	})
	return rows
}

func sortedKeys[T any](set map[string]T) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
