package cli

import (
	"strings"
	"testing"
)

func TestAffectedCommand(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&affectedCommand{}).Run([]string{"mono", "affected", "--base", "main"}); err != nil {
			t.Fatalf("affected command returned error: %v", err)
		}
	})

	if stdout != "api\nlib\nweb\n" {
		t.Fatalf("unexpected affected output: %q", stdout)
	}
}

func TestAffectedCommandExplain(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&affectedCommand{}).Run([]string{"mono", "affected", "--base", "main", "--explain"}); err != nil {
			t.Fatalf("affected command returned error: %v", err)
		}
	})

	if !strings.Contains(stdout, "lib -> api") {
		t.Fatalf("expected explain chain in output, got %q", stdout)
	}
}

func TestAffectedCommandNoChanges(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	stdout := captureStdout(t, func() {
		if err := (&affectedCommand{}).Run([]string{"mono", "affected", "--base", "HEAD"}); err != nil {
			t.Fatalf("affected command returned error: %v", err)
		}
	})

	if stdout != "No affected projects.\n" {
		t.Fatalf("expected success message when there are no changes, got %q", stdout)
	}
}
