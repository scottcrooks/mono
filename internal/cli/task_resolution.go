package cli

import (
	"fmt"
	"sort"
)

type TaskRequest struct {
	Task     TaskName
	Services []string
}

type ResolvedTaskNode struct {
	Node       TaskNode
	Service    Service
	Command    string
	SkipReason string
}

type TaskResolution struct {
	Task  TaskName
	Nodes []ResolvedTaskNode
}

func resolveTaskRequest(cfg *Config, req TaskRequest) (*TaskResolution, error) {
	if _, ok := orchestratedTaskSet[req.Task]; !ok {
		return nil, fmt.Errorf("unsupported task %q", req.Task)
	}

	selected, err := selectServicesWithDependencyClosure(cfg, req.Services)
	if err != nil {
		return nil, err
	}

	nodes := make([]ResolvedTaskNode, 0, len(selected))
	for _, svc := range selected {
		node := ResolvedTaskNode{
			Node:    TaskNode{Service: svc.Name, Task: req.Task},
			Service: svc,
		}
		if cmd, ok, reason := taskCommandForService(svc, req.Task); ok {
			node.Command = cmd
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
