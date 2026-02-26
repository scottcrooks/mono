package validation

import (
	"fmt"
	"sort"
	"strings"
)

func validateGraphRules(services []manifestService, local *manifestLocal, info map[int]serviceNodeInfo, report *Report) {
	serviceNames := make(map[string]int, len(services))
	for i, svc := range services {
		if name := strings.TrimSpace(svc.Name); name != "" {
			serviceNames[name] = i
		}
	}

	infraNames := map[string]struct{}{}
	if local != nil {
		for _, resource := range local.Resources {
			if name := strings.TrimSpace(resource.Name); name != "" {
				infraNames[name] = struct{}{}
			}
		}
	}

	edges := map[string][]string{}
	for i, svc := range services {
		from := strings.TrimSpace(svc.Name)
		if from == "" {
			continue
		}
		for _, dep := range svc.Depends {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if _, ok := serviceNames[dep]; ok {
				edges[from] = append(edges[from], dep)
				continue
			}
			if _, ok := infraNames[dep]; ok {
				continue
			}

			p := position{}
			if sInfo, ok := info[i]; ok {
				p = requiredFieldPos(sInfo, "depends")
			}
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "graph.unknown_dependency",
				Path:     fmt.Sprintf("services[%d].depends", i),
				Message:  fmt.Sprintf("unknown dependency %q (must reference a service or local resource)", dep),
				Service:  serviceLabel(i, svc),
				Line:     p.line,
				Column:   p.column,
			})
		}
	}

	if cycle := detectServiceCycle(serviceNames, edges); len(cycle) > 0 {
		ownerIdx := serviceNames[cycle[0]]
		p := position{}
		if sInfo, ok := info[ownerIdx]; ok {
			p = requiredFieldPos(sInfo, "depends")
		}
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "graph.cycle",
			Path:     fmt.Sprintf("services[%d].depends", ownerIdx),
			Message:  fmt.Sprintf("dependency cycle detected: %s", strings.Join(cycle, " -> ")),
			Service:  serviceLabel(ownerIdx, services[ownerIdx]),
			Line:     p.line,
			Column:   p.column,
		})
	}
}

func detectServiceCycle(serviceNames map[string]int, edges map[string][]string) []string {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)

	state := map[string]int{}
	stack := []string{}
	keys := make([]string, 0, len(serviceNames))
	for name := range serviceNames {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, from := range keys {
		sort.Strings(edges[from])
	}

	var visit func(string) []string
	visit = func(node string) []string {
		state[node] = visiting
		stack = append(stack, node)
		for _, next := range edges[node] {
			if state[next] == visiting {
				start := 0
				for i := range stack {
					if stack[i] == next {
						start = i
						break
					}
				}
				cycle := append([]string(nil), stack[start:]...)
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

	for _, name := range keys {
		if state[name] == unvisited {
			if cycle := visit(name); len(cycle) > 0 {
				return cycle
			}
		}
	}

	return nil
}
