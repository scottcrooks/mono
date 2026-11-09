package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigureGoConcurrencyUsesEffectiveEnvironmentAndRedacts(t *testing.T) {
	repo := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	service := filepath.Join(repo, "svc")
	if err := os.MkdirAll(service, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "test.env"), []byte("GOFLAGS='-tags value with spaces'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "test.env"), []byte("GOFLAGS=-p=91\n"), 0600); err != nil {
		t.Fatal(err)
	}
	graph := &taskGraph{nodes: map[TaskNode]ResolvedTaskNode{{Service: "svc", Task: TaskTest}: {Node: TaskNode{Service: "svc", Task: TaskTest}, Service: Service{Name: "svc", Path: "svc", Archetype: "go"}, Command: "go test ./..."}}}
	err = configureGoConcurrency(graph, TaskRunOptions{GoConcurrency: 2})
	if err == nil || !strings.Contains(err.Error(), "conflicts with GOFLAGS") || strings.Contains(err.Error(), "91") {
		t.Fatalf("conflict diagnostic leaked or missing: %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "test.env")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", `"secret-unclosed`)
	err = configureGoConcurrency(graph, TaskRunOptions{GoConcurrency: 2})
	if err == nil || !strings.Contains(err.Error(), "malformed GOFLAGS") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("malformed diagnostic leaked or missing: %v", err)
	}
	if err := os.Remove(filepath.Join(service, "test.env")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", `'-tags value with spaces'`)
	if err := configureGoConcurrency(graph, TaskRunOptions{GoConcurrency: 2}); err != nil {
		t.Fatal(err)
	}
	if got := graph.nodes[TaskNode{Service: "svc", Task: TaskTest}].Command; got != "go test -p 2 ./..." {
		t.Fatalf("command=%q", got)
	}
}

func TestSplitGoFlagsQuotedFields(t *testing.T) {
	got, err := splitGoFlags(`-tags 'alpha beta' -mod=readonly`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-tags", "alpha beta", "-mod=readonly"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitGoFlags=%q want %q", got, want)
	}
	for _, raw := range []string{`-tags "unfinished`, `-tags 'unfinished`} {
		if _, err := splitGoFlags(raw); err == nil {
			t.Errorf("expected malformed quote error for %q", raw)
		}
	}
}

func TestGoConcurrencyConflictForms(t *testing.T) {
	for _, flags := range []string{"-p 4", "-p=4", `-tags 'ok value' -p=2`} {
		words, err := splitGoFlags(flags)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, word := range words {
			if word == "-p" || len(word) > 3 && word[:3] == "-p=" {
				found = true
			}
		}
		if !found {
			t.Errorf("failed to detect package concurrency in %q", flags)
		}
	}
}

func TestGoConcurrencyAddsSeparateExecutionArguments(t *testing.T) {
	graph := &taskGraph{nodes: map[TaskNode]ResolvedTaskNode{{Service: "svc", Task: TaskTest}: {Node: TaskNode{Service: "svc", Task: TaskTest}, Service: Service{Name: "svc", Path: "svc", Archetype: "go"}, Command: "go test ./..."}}}
	// A focused graph has already replaced package operands by resolved imports.
	node := TaskNode{Service: "svc", Task: TaskTest}
	resolved := graph.nodes[node]
	resolved.CommandArgs = []string{"-run", "^Test Name$", "example.test/svc/pkg"}
	graph.nodes[node] = resolved
	if err := configureGoConcurrency(graph, TaskRunOptions{GoConcurrency: 3, Packages: []string{"./pkg"}, Run: "^Test Name$"}); err != nil {
		t.Fatal(err)
	}
	got := graph.nodes[node].CommandArgs
	want := []string{"-p", "3", "-run", "^Test Name$", "example.test/svc/pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%q want %q", got, want)
	}
}
