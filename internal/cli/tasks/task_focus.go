package tasks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/core"
)

type focusedTest struct{ Service, Package, Name string }

// resolveFocusedGoSelection discovers every candidate before the executor can
// launch a test node. It also replaces each Go node's package target with the
// resolved import paths so the reported selection and execution agree.
func resolveFocusedGoSelection(graph *taskGraph, opts TaskRunOptions) error {
	if len(opts.Packages) == 0 && opts.Run == "" && !opts.List {
		return nil
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	var requested *regexp.Regexp
	if opts.Run != "" {
		first := strings.Split(opts.Run, "/")[0]
		requested, err = regexp.Compile(first)
		if err != nil {
			return fmt.Errorf("invalid --run regular expression")
		}
	}
	for node, resolved := range graph.nodes {
		if resolved.SkipReason != "" {
			continue
		}
		if resolved.Service.Archetype != "go" && resolved.Service.Archetype != "go-tui" {
			return fmt.Errorf("focused integration selection does not support runnable service %q (%s)", node.Service, resolved.Service.Archetype)
		}
	}
	runnableGoNodes := 0
	for _, resolved := range graph.nodes {
		if resolved.SkipReason == "" {
			runnableGoNodes++
		}
	}
	if runnableGoNodes == 0 {
		return fmt.Errorf("focused integration selection has no runnable Go nodes")
	}
	keys := make([]TaskNode, 0, len(graph.nodes))
	allSelected := make([]focusedTest, 0)
	for node := range graph.nodes {
		keys = append(keys, node)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, node := range keys {
		resolved := graph.nodes[node]
		if resolved.SkipReason != "" {
			continue
		}
		serviceDir, err := filepath.Abs(resolved.Service.Path)
		if err != nil {
			return err
		}
		env, err := core.BuildEnvironment(root, serviceDir, "test.env", os.Environ())
		if err != nil {
			return fmt.Errorf("[%s] integration environment: %w", node, err)
		}
		env = core.WithDefaultCI(env)
		resolved.Environment = append([]string(nil), env...)
		packages := opts.Packages
		if len(packages) == 0 {
			packages = []string{"./..."}
		}
		listArgs := []string{"list", "-e", "-f", "{{if not .Incomplete}}{{.ImportPath}}{{end}}"}
		if opts.GoConcurrency > 0 {
			listArgs = []string{"list", "-p", fmt.Sprint(opts.GoConcurrency), "-e", "-f", "{{if not .Incomplete}}{{.ImportPath}}{{end}}"}
		}
		listArgs = append(listArgs, packages...)
		importOutput, err := focusedGoCommand(ctx, serviceDir, env, listArgs)
		if err != nil {
			return fmt.Errorf("[%s] Go package discovery failed: %w", node, err)
		}
		imports := nonEmptyLines(importOutput)
		if len(imports) == 0 {
			if graph.directServices[node.Service] {
				return fmt.Errorf("[%s] package selection matched no Go packages", node)
			}
			resolved.SkipReason = "no matching focused packages"
			graph.nodes[node] = resolved
			continue
		}
		selected := make([]focusedTest, 0)
		for _, pkg := range imports {
			candidateArgs := []string{"test", "-list", ".", pkg}
			if opts.GoConcurrency > 0 {
				candidateArgs = []string{"test", "-p", fmt.Sprint(opts.GoConcurrency), "-list", ".", pkg}
			}
			output, err := focusedGoCommand(ctx, serviceDir, env, candidateArgs)
			if err != nil {
				return fmt.Errorf("[%s] Go test discovery failed: %w", node, err)
			}
			for _, line := range nonEmptyLines(output) {
				if !strings.HasPrefix(line, "Test") || len(line) == 4 || line[4] < 'A' || line[4] > 'Z' {
					continue
				}
				if requested != nil && !requested.MatchString(line) {
					continue
				}
				selected = append(selected, focusedTest{Package: pkg, Name: line})
			}
		}
		if len(selected) == 0 {
			if graph.directServices[node.Service] {
				return fmt.Errorf("[%s] selection matched no top-level tests", node)
			}
			resolved.SkipReason = "no matching focused tests"
			graph.nodes[node] = resolved
			continue
		}
		sort.Slice(selected, func(i, j int) bool {
			if selected[i].Package == selected[j].Package {
				return selected[i].Name < selected[j].Name
			}
			return selected[i].Package < selected[j].Package
		})
		for _, test := range selected {
			test.Service = node.Service
			allSelected = append(allSelected, test)
		}
		args := make([]string, 0, len(imports)+4)
		if opts.GoConcurrency > 0 {
			args = append(args, "-p", fmt.Sprint(opts.GoConcurrency))
		}
		if requested != nil {
			args = append(args, "-run", opts.Run)
		}
		args = append(args, imports...)
		resolved.Command = focusedGoTestCommand(resolved.Command)
		resolved.CommandArgs = args
		graph.nodes[node] = resolved
	}
	sort.Slice(allSelected, func(i, j int) bool {
		if allSelected[i].Service != allSelected[j].Service {
			return allSelected[i].Service < allSelected[j].Service
		}
		if allSelected[i].Package != allSelected[j].Package {
			return allSelected[i].Package < allSelected[j].Package
		}
		return allSelected[i].Name < allSelected[j].Name
	})
	for _, test := range allSelected {
		fmt.Printf("[%s] %s %s\n", test.Service, test.Package, test.Name)
	}
	return nil
}

func focusedGoTestCommand(command string) string {
	parts := strings.Fields(command)
	if len(parts) < 2 || parts[0] != "go" || parts[1] != "test" {
		return command
	}
	filtered := parts[:2]
	for _, part := range parts[2:] {
		if strings.HasPrefix(part, "-") {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, " ")
}

func focusedGoCommand(ctx context.Context, dir string, env []string, args []string) (string, error) {
	cmd, err := core.CommandFromParts(ctx, append([]string{"go"}, args...))
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
