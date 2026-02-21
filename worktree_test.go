package main

import (
	"testing"
	"time"
)

func TestParseCreateArgs(t *testing.T) {
	t.Parallel()

	branch, fromRef, uniqueID, noBootstrap, err := parseCreateArgs([]string{
		"feature/my-work",
		"--from", "main",
		"--id", "feature-my-work-1",
		"--no-bootstrap",
	})
	if err != nil {
		t.Fatalf("parseCreateArgs returned error: %v", err)
	}

	if branch != "feature/my-work" {
		t.Fatalf("unexpected branch: %q", branch)
	}
	if fromRef != "main" {
		t.Fatalf("unexpected fromRef: %q", fromRef)
	}
	if uniqueID != "feature-my-work-1" {
		t.Fatalf("unexpected uniqueID: %q", uniqueID)
	}
	if !noBootstrap {
		t.Fatal("expected noBootstrap=true")
	}
}

func TestParseCreateArgsFlagFirst(t *testing.T) {
	t.Parallel()

	branch, fromRef, uniqueID, noBootstrap, err := parseCreateArgs([]string{
		"--from=main",
		"--id=feature-my-work-2",
		"feature/my-work",
	})
	if err != nil {
		t.Fatalf("parseCreateArgs returned error: %v", err)
	}

	if branch != "feature/my-work" {
		t.Fatalf("unexpected branch: %q", branch)
	}
	if fromRef != "main" {
		t.Fatalf("unexpected fromRef: %q", fromRef)
	}
	if uniqueID != "feature-my-work-2" {
		t.Fatalf("unexpected uniqueID: %q", uniqueID)
	}
	if noBootstrap {
		t.Fatal("expected noBootstrap=false")
	}
}

func TestParseCreateArgsInvalidID(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := parseCreateArgs([]string{"feature/my-work", "--id", "Bad_ID"})
	if err == nil {
		t.Fatal("expected error for non slug-safe unique id")
	}
}

func TestParseRemoveArgs(t *testing.T) {
	t.Parallel()

	identifier, force, err := parseRemoveArgs([]string{"feature/my-work", "--force"})
	if err != nil {
		t.Fatalf("parseRemoveArgs returned error: %v", err)
	}
	if identifier != "feature/my-work" {
		t.Fatalf("unexpected identifier: %q", identifier)
	}
	if !force {
		t.Fatal("expected force=true")
	}
}

func TestSanitizeSlug(t *testing.T) {
	t.Parallel()

	got := sanitizeSlug("Feature/Foo_Bar.BAZ")
	want := "feature-foo-bar-baz"
	if got != want {
		t.Fatalf("sanitizeSlug mismatch: got %q, want %q", got, want)
	}
}

func TestDefaultWorktreeID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 21, 15, 30, 45, 0, time.UTC)
	got := defaultWorktreeID("feature/Foo", now)
	want := "feature-foo-20260221-153045"
	if got != want {
		t.Fatalf("defaultWorktreeID mismatch: got %q, want %q", got, want)
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	t.Parallel()

	output := `worktree /repo
HEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
branch refs/heads/main

worktree /tmp/wt
HEAD bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
branch refs/heads/feature-x
`

	entries, err := parseWorktreePorcelain(output)
	if err != nil {
		t.Fatalf("parseWorktreePorcelain returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Path != "/repo" || entries[0].Branch != "main" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Path != "/tmp/wt" || entries[1].Branch != "feature-x" {
		t.Fatalf("unexpected second entry: %+v", entries[1])
	}
}

func TestParseWorktreePorcelainMissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseWorktreePorcelain("HEAD deadbeef\n")
	if err == nil {
		t.Fatal("expected parse error for missing worktree path")
	}
}

func TestResolveWorktreeByIdentifier(t *testing.T) {
	t.Parallel()

	entries := []gitWorktreeEntry{
		{Path: "/tmp/repo-main", Branch: "main"},
		{Path: "/home/u/.worktrees/repo/feature-1", Branch: "feature-1"},
	}

	entry, err := resolveWorktreeFromEntries(entries, "feature-1")
	if err != nil {
		t.Fatalf("resolveWorktreeFromEntries returned error: %v", err)
	}
	if entry.Path != "/home/u/.worktrees/repo/feature-1" {
		t.Fatalf("unexpected resolved path: %s", entry.Path)
	}
}

func TestResolveWorktreePrefersBranchOverBasename(t *testing.T) {
	t.Parallel()

	entries := []gitWorktreeEntry{
		{Path: "/home/u/.worktrees/repo/main", Branch: "feature-main"},
		{Path: "/repo", Branch: "main"},
	}

	entry, err := resolveWorktreeFromEntries(entries, "main")
	if err != nil {
		t.Fatalf("resolveWorktreeFromEntries returned error: %v", err)
	}
	if entry.Path != "/repo" {
		t.Fatalf("expected branch match to win, got %s", entry.Path)
	}
}
