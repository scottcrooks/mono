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

func TestParseTaskInvocationArgsUnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, err := parseTaskInvocationArgs([]string{"--integrationx"}, 2)
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
}
