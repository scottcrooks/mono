package tasks

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type TaskRequest struct {
	Task          TaskName
	Services      []string
	ExactServices bool
	Integration   bool
	FileTargets   map[string][]string
}

type ResolvedTaskNode struct {
	Node          TaskNode
	Service       Service
	Command       string
	AffectedFiles []string
	SkipReason    string
}

type TaskResolution struct {
	Task  TaskName
	Nodes []ResolvedTaskNode
}

func resolveTaskRequest(cfg *Config, req TaskRequest) (*TaskResolution, error) {
	if !isResolvableTask(req.Task) {
		return nil, fmt.Errorf("unsupported task %q", req.Task)
	}

	selected, err := selectServicesForRequest(cfg, req.Services, req.ExactServices)
	if err != nil {
		return nil, err
	}

	nodes := make([]ResolvedTaskNode, 0, len(selected))
	for _, svc := range selected {
		node := ResolvedTaskNode{
			Node:    TaskNode{Service: svc.Name, Task: req.Task},
			Service: svc,
		}
		if cmd, ok, reason := TaskCommandForServiceWithOptions(svc, req.Task, req.Integration); ok {
			node.Command = cmd
			if req.Task == TaskFormat && req.FileTargets != nil {
				files := formatTargetsForService(svc, req.FileTargets[svc.Name])
				if len(files) == 0 {
					node.SkipReason = "no changed files"
				} else {
					node.AffectedFiles = files
				}
			}
		} else {
			node.SkipReason = reason
		}
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Node.Service < nodes[j].Node.Service
	})

	return &TaskResolution{Task: req.Task, Nodes: nodes}, nil
}

func formatTargetsForService(svc Service, files []string) []string {
	if len(files) == 0 {
		return nil
	}

	prefix := normalizeTaskServicePath(svc.Path)
	targets := make([]string, 0, len(files))
	for _, file := range files {
		normalized := filepath.ToSlash(strings.TrimSpace(file))
		if normalized == "" {
			continue
		}
		if prefix != "" {
			if normalized == prefix {
				continue
			}
			if strings.HasPrefix(normalized, prefix+"/") {
				normalized = strings.TrimPrefix(normalized, prefix+"/")
			}
		}
		if normalized != "" {
			targets = append(targets, normalized)
		}
	}
	sort.Strings(targets)
	return targets
}

func normalizeTaskServicePath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}

func selectServicesForRequest(cfg *Config, requested []string, exact bool) ([]Service, error) {
	if exact {
		return selectServicesExact(cfg, requested)
	}
	return selectServicesWithDependencyClosure(cfg, requested)
}

func selectServicesWithDependencyClosure(cfg *Config, requested []string) ([]Service, error) {
	if len(requested) == 0 {
		all := append([]Service(nil), cfg.Services...)
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		return all, nil
	}

	index := make(map[string]Service, len(cfg.Services))
	for _, svc := range cfg.Services {
		index[svc.Name] = svc
	}

	selected := make(map[string]Service)
	queue := append([]string(nil), requested...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := selected[name]; ok {
			continue
		}
		svc, ok := index[name]
		if !ok {
			return nil, fmt.Errorf("unknown service %q", name)
		}
		selected[name] = svc
		for _, dep := range svc.Depends {
			if _, exists := index[dep]; exists {
				queue = append(queue, dep)
			}
		}
	}

	out := make([]Service, 0, len(selected))
	for _, svc := range selected {
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func selectServicesExact(cfg *Config, requested []string) ([]Service, error) {
	if len(requested) == 0 {
		return []Service{}, nil
	}

	index := make(map[string]Service, len(cfg.Services))
	for _, svc := range cfg.Services {
		index[svc.Name] = svc
	}

	selected := make([]Service, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		if _, dup := seen[name]; dup {
			continue
		}
		svc, ok := index[name]
		if !ok {
			return nil, fmt.Errorf("unknown service %q", name)
		}
		selected = append(selected, svc)
		seen[name] = struct{}{}
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	return selected, nil
}
