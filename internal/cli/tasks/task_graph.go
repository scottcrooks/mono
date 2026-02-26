package tasks

import (
	"fmt"
	"sort"
	"strings"
)

type taskGraph struct {
	nodes    map[TaskNode]ResolvedTaskNode
	edges    map[TaskNode][]TaskNode
	reverse  map[TaskNode][]TaskNode
	inDegree map[TaskNode]int
}

func buildTaskGraph(cfg *Config, resolved []ResolvedTaskNode) (*taskGraph, error) {
	g := &taskGraph{
		nodes:    make(map[TaskNode]ResolvedTaskNode, len(resolved)),
		edges:    make(map[TaskNode][]TaskNode, len(resolved)),
		reverse:  make(map[TaskNode][]TaskNode, len(resolved)),
		inDegree: make(map[TaskNode]int, len(resolved)),
	}

	byService := make(map[string]ResolvedTaskNode, len(resolved))
	for _, node := range resolved {
		g.nodes[node.Node] = node
		g.inDegree[node.Node] = 0
		if node.SkipReason == "" {
			byService[node.Service.Name] = node
		}
	}

	for _, node := range resolved {
		if node.SkipReason != "" {
			continue
		}
		for _, dep := range node.Service.Depends {
			depNode, ok := byService[dep]
			if !ok {
				continue
			}
			g.edges[depNode.Node] = append(g.edges[depNode.Node], node.Node)
			g.reverse[node.Node] = append(g.reverse[node.Node], depNode.Node)
			g.inDegree[node.Node]++
		}
	}

	for from := range g.edges {
		sort.Slice(g.edges[from], func(i, j int) bool {
			return g.edges[from][i].Service < g.edges[from][j].Service
		})
	}

	if cycle := detectTaskCycle(g); len(cycle) > 0 {
		parts := make([]string, 0, len(cycle))
		for _, n := range cycle {
			parts = append(parts, n.String())
		}
		return nil, fmt.Errorf("dependency cycle detected: %s", strings.Join(parts, " -> "))
	}

	return g, nil
}

func detectTaskCycle(g *taskGraph) []TaskNode {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)

	state := make(map[TaskNode]int, len(g.nodes))
	stack := make([]TaskNode, 0, len(g.nodes))

	keys := make([]TaskNode, 0, len(g.nodes))
	for n, resolved := range g.nodes {
		if resolved.SkipReason != "" {
			continue
		}
		keys = append(keys, n)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})

	var visit func(TaskNode) []TaskNode
	visit = func(node TaskNode) []TaskNode {
		state[node] = visiting
		stack = append(stack, node)

		for _, next := range g.edges[node] {
			if state[next] == visiting {
				start := 0
				for i := range stack {
					if stack[i] == next {
						start = i
						break
					}
				}
				cycle := append([]TaskNode(nil), stack[start:]...)
				cycle = append(cycle, next)
				return cycle
			}
			if state[next] == unvisited {
				if cycle := visit(next); len(cycle) > 0 {
					return cycle
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[node] = done
		return nil
	}

	for _, n := range keys {
		if state[n] == unvisited {
			if cycle := visit(n); len(cycle) > 0 {
				return cycle
			}
		}
	}

	return nil
}
