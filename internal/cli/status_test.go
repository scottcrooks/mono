package cli

import (
	"strings"
	"testing"
)

func TestStatusCommand(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&statusCommand{}).Run([]string{"mono", "status", "--base", "main"}); err != nil {
			t.Fatalf("status command returned error: %v", err)
		}
	})

	requiredSnippets := []string{
		"Changed services:",
		"  - lib",
		"Impacted services:",
		"  - api",
		"  - lib",
		"  - web",
		"Planned check tasks:",
		"  - api: run [lint, typecheck, test], skip [none]",
		"  - lib: run [lint, test], skip [typecheck]",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(stdout, snippet) {
			t.Fatalf("expected %q in output, got %q", snippet, stdout)
		}
	}
}

func TestStatusCommandNoChanges(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&statusCommand{}).Run([]string{"mono", "status", "--base", "HEAD"}); err != nil {
			t.Fatalf("status command returned error: %v", err)
		}
	})

	if !strings.Contains(stdout, "Changed services:\n  (none)") {
		t.Fatalf("expected empty changed section, got %q", stdout)
	}
	if !strings.Contains(stdout, "Impacted services:\n  (none)") {
		t.Fatalf("expected empty impacted section, got %q", stdout)
	}
	if !strings.Contains(stdout, "Planned check tasks:\n  (none)") {
		t.Fatalf("expected empty planned tasks section, got %q", stdout)
	}
}
