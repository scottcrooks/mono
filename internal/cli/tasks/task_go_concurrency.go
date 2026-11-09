package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/core"
)

func configureGoConcurrency(graph *taskGraph, opts TaskRunOptions) error {
	if opts.GoConcurrency == 0 {
		return nil
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	for node, resolved := range graph.nodes {
		if resolved.SkipReason != "" || (resolved.Service.Archetype != "go" && resolved.Service.Archetype != "go-tui") {
			continue
		}
		dir, err := filepath.Abs(resolved.Service.Path)
		if err != nil {
			return err
		}
		env, err := core.BuildEnvironment(root, dir, "test.env", os.Environ())
		if err != nil {
			return fmt.Errorf("[%s] integration environment: %w", node, err)
		}
		words, err := splitGoFlags(environmentValue(env, "GOFLAGS"))
		if err != nil {
			return fmt.Errorf("[%s] malformed GOFLAGS quoting", node)
		}
		for _, word := range words {
			if word == "-p" || strings.HasPrefix(word, "-p=") {
				return fmt.Errorf("[%s] --go-concurrency conflicts with GOFLAGS", node)
			}
		}
		if len(opts.Packages) > 0 || opts.Run != "" || opts.List {
			resolved.CommandArgs = append([]string{"-p", fmt.Sprint(opts.GoConcurrency)}, resolved.CommandArgs...)
		} else {
			resolved.Command = insertGoTestFlag(resolved.Command, "-p", fmt.Sprint(opts.GoConcurrency))
		}
		graph.nodes[node] = resolved
	}
	return nil
}
func environmentValue(env []string, key string) string {
	value := ""
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok && k == key {
			value = v
		}
	}
	return value
}

// splitGoFlags follows cmd/internal/quoted.Split: quotes delimit fields and
// are removed; malformed quotes fail closed without exposing the value.
func splitGoFlags(s string) ([]string, error) {
	var fields []string
	for len(s) > 0 {
		for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if s[0] == '\'' || s[0] == '"' {
			q := s[0]
			s = s[1:]
			i := strings.IndexByte(s, q)
			if i < 0 {
				return nil, fmt.Errorf("unterminated quote")
			}
			fields = append(fields, s[:i])
			s = s[i+1:]
			continue
		}
		i := 0
		for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			i++
		}
		fields = append(fields, s[:i])
		s = s[i:]
	}
	return fields, nil
}
func insertGoTestFlag(command, flag, value string) string {
	if strings.HasPrefix(command, "go test") {
		return "go test " + flag + " " + value + strings.TrimPrefix(command, "go test")
	}
	return command
}
