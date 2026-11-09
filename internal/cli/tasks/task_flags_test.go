package tasks

import "testing"

func TestParseTaskInvocationArgsIntegrationFlag(t *testing.T) {
	t.Parallel()

	services, opts, err := parseTaskInvocationArgs([]string{"--integration", "api"}, 4)
	if err != nil {
		t.Fatalf("parseTaskInvocationArgs returned error: %v", err)
	}
	if !opts.Integration {
		t.Fatalf("expected Integration=true")
	}
	if len(services) != 1 || services[0] != "api" {
		t.Fatalf("unexpected services: %v", services)
	}
}

func TestParseTaskInvocationArgsSupportsBaseAndAll(t *testing.T) {
	t.Parallel()

	services, opts, err := parseTaskInvocationArgs([]string{"--base", "main", "--all", "--concurrency=2"}, 4)
	if err != nil {
		t.Fatalf("parseTaskInvocationArgs returned error: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("unexpected services: %v", services)
	}
	if opts.BaseRef != "main" {
		t.Fatalf("unexpected base ref: %q", opts.BaseRef)
	}
	if !opts.All {
		t.Fatalf("expected All=true")
	}
	if opts.Concurrency != 2 {
		t.Fatalf("unexpected concurrency: %d", opts.Concurrency)
	}
}

func TestParseTaskInvocationArgsUnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, err := parseTaskInvocationArgs([]string{"--integrationx"}, 2)
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func TestParseTaskInvocationArgsFocusedIntegrationOptions(t *testing.T) {
	services, opts, err := parseTaskInvocationArgs([]string{"--integration", "--package", "./internal/...", "--package=./cmd/...", "--run", "^TestStore$", "--list"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 0 || len(opts.Packages) != 2 || opts.Packages[0] != "./internal/..." || opts.Packages[1] != "./cmd/..." || opts.Run != "^TestStore$" || !opts.List {
		t.Fatalf("unexpected parse: services=%v opts=%+v", services, opts)
	}
}

func TestParseTaskInvocationArgsRejectsInvalidFocus(t *testing.T) {
	for _, args := range [][]string{
		{"--integration", "--package="},
		{"--integration", "--run", "["},
		{"--integration", "--run=x", "--run=y"},
		{"--integration", "--full-suite", "--package", "./..."},
		{"--package", "./..."},
	} {
		if _, _, err := parseTaskInvocationArgs(args, 4); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}

func TestParseTaskInvocationArgsGoConcurrency(t *testing.T) {
	_, opts, err := parseTaskInvocationArgs([]string{"--integration", "--go-concurrency=3", "--concurrency", "2"}, 8)
	if err != nil || opts.GoConcurrency != 3 || opts.Concurrency != 2 {
		t.Fatalf("opts=%+v err=%v", opts, err)
	}
	for _, args := range [][]string{{"--integration", "--go-concurrency", "0"}, {"--integration", "--go-concurrency=x"}, {"--integration", "--go-concurrency=2", "--go-concurrency=3"}, {"--go-concurrency=2"}} {
		if _, _, err := parseTaskInvocationArgs(args, 4); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}
