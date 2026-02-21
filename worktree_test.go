package main

import (
	"os"
	"path/filepath"
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

func TestFindProjectEnvFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "apps", "pythia"), 0o755); err != nil {
		t.Fatalf("mkdir apps/pythia: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "packages", "polaris"), 0o755); err != nil {
		t.Fatalf("mkdir packages/polaris: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "apps", "pythia", ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("write apps env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "packages", "polaris", ".env"), []byte("B=2\n"), 0o600); err != nil {
		t.Fatalf("write packages env: %v", err)
	}
	// This should not be included because only apps/ and packages/ are scanned.
	if err := os.MkdirAll(filepath.Join(repoRoot, "tools", "mono"), 0o755); err != nil {
		t.Fatalf("mkdir tools/mono: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tools", "mono", ".env"), []byte("NO=1\n"), 0o600); err != nil {
		t.Fatalf("write tools env: %v", err)
	}

	paths, err := findProjectEnvFiles(repoRoot)
	if err != nil {
		t.Fatalf("findProjectEnvFiles returned error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 env files, got %d (%v)", len(paths), paths)
	}
	if paths[0] != filepath.Join("apps", "pythia", ".env") {
		t.Fatalf("unexpected first path: %q", paths[0])
	}
	if paths[1] != filepath.Join("packages", "polaris", ".env") {
		t.Fatalf("unexpected second path: %q", paths[1])
	}
}

func TestCopyProjectEnvFiles(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	destRoot := t.TempDir()

	sourceEnv := filepath.Join(sourceRoot, "apps", "pythia", ".env")
	if err := os.MkdirAll(filepath.Dir(sourceEnv), 0o755); err != nil {
		t.Fatalf("mkdir source env dir: %v", err)
	}
	if err := os.WriteFile(sourceEnv, []byte("PYTHIA_DB_URL=postgres://localhost\n"), 0o600); err != nil {
		t.Fatalf("write source env: %v", err)
	}

	copied, err := copyProjectEnvFiles(sourceRoot, destRoot)
	if err != nil {
		t.Fatalf("copyProjectEnvFiles returned error: %v", err)
	}
	if copied != 1 {
		t.Fatalf("expected copied=1, got %d", copied)
	}

	destEnv := filepath.Join(destRoot, "apps", "pythia", ".env")
	data, err := os.ReadFile(destEnv)
	if err != nil {
		t.Fatalf("read copied env: %v", err)
	}
	if string(data) != "PYTHIA_DB_URL=postgres://localhost\n" {
		t.Fatalf("unexpected copied env content: %q", string(data))
	}
}

func TestCopyProjectEnvFilesSkipsExistingDest(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	destRoot := t.TempDir()

	sourceEnv := filepath.Join(sourceRoot, "apps", "pythia", ".env")
	destEnv := filepath.Join(destRoot, "apps", "pythia", ".env")

	if err := os.MkdirAll(filepath.Dir(sourceEnv), 0o755); err != nil {
		t.Fatalf("mkdir source env dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(destEnv), 0o755); err != nil {
		t.Fatalf("mkdir dest env dir: %v", err)
	}
	if err := os.WriteFile(sourceEnv, []byte("SOURCE=1\n"), 0o600); err != nil {
		t.Fatalf("write source env: %v", err)
	}
	if err := os.WriteFile(destEnv, []byte("DEST=1\n"), 0o600); err != nil {
		t.Fatalf("write dest env: %v", err)
	}

	copied, err := copyProjectEnvFiles(sourceRoot, destRoot)
	if err != nil {
		t.Fatalf("copyProjectEnvFiles returned error: %v", err)
	}
	if copied != 0 {
		t.Fatalf("expected copied=0, got %d", copied)
	}

	data, err := os.ReadFile(destEnv)
	if err != nil {
		t.Fatalf("read dest env: %v", err)
	}
	if string(data) != "DEST=1\n" {
		t.Fatalf("destination env should be unchanged, got %q", string(data))
	}
}
