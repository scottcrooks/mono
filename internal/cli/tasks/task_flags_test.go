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
