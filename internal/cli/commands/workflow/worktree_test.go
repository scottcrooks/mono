package workflow

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestRunWorktreeRequirementsMissingServicesYAML(t *testing.T) {
	t.Parallel()

	worktreePath := t.TempDir()
	if err := runWorktreeRequirements(worktreePath); err != nil {
		t.Fatalf("expected missing services.yaml to be skipped, got error: %v", err)
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

func TestParseTagArgs(t *testing.T) {
	t.Parallel()

	status, err := parseTagArgs([]string{"in_progress"})
	if err != nil {
		t.Fatalf("parseTagArgs returned error: %v", err)
	}
	if status != workflowStatusInProgress {
		t.Fatalf("unexpected status: %q", status)
	}
}

func TestParseTagArgsInvalid(t *testing.T) {
	t.Parallel()

	_, err := parseTagArgs([]string{"blocked"})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
}

func TestParseTagArgsMissing(t *testing.T) {
	t.Parallel()

	_, err := parseTagArgs(nil)
	if err == nil {
		t.Fatal("expected usage error for missing status")
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

func TestLoadAndSaveWorktreeStatusStoreFromPath(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "mono-worktree-statuses.yaml")

	store, err := loadWorktreeStatusStoreFromPath(storePath)
	if err != nil {
		t.Fatalf("loadWorktreeStatusStoreFromPath missing file: %v", err)
	}
	if len(store.Worktrees) != 0 {
		t.Fatalf("expected empty store for missing file, got %d entries", len(store.Worktrees))
	}

	store.Worktrees["/tmp/worktree-a"] = workflowStatusNeedsInput
	if err := saveWorktreeStatusStoreToPath(storePath, store); err != nil {
		t.Fatalf("saveWorktreeStatusStoreToPath: %v", err)
	}

	reloaded, err := loadWorktreeStatusStoreFromPath(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if got := reloaded.Worktrees["/tmp/worktree-a"]; got != workflowStatusNeedsInput {
		t.Fatalf("unexpected reloaded status: %q", got)
	}
}

func TestLoadWorktreeStatusStoreFromPathEmptyFile(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "mono-worktree-statuses.yaml")
	if err := os.WriteFile(storePath, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write empty store file: %v", err)
	}

	store, err := loadWorktreeStatusStoreFromPath(storePath)
	if err != nil {
		t.Fatalf("loadWorktreeStatusStoreFromPath empty file: %v", err)
	}
	if len(store.Worktrees) != 0 {
		t.Fatalf("expected empty map for empty file, got %d", len(store.Worktrees))
	}
}

func TestPrintWorktreeUsageIncludesTag(t *testing.T) {
	t.Parallel()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	printWorktreeUsage()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read usage output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if !strings.Contains(string(out), "mono worktree tag <IN_PROGRESS|DONE|NEEDS_INPUT>") {
		t.Fatalf("usage output missing tag command: %q", string(out))
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

func TestIsBranchMergedIntoBase(t *testing.T) {
	repo := initTestGitRepo(t)
	writeFile(t, repo, "README.md", "root\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")

	gitRun(t, repo, "checkout", "-b", "feature/demo")
	writeFile(t, repo, "feature.txt", "feature\n")
	gitRun(t, repo, "add", "feature.txt")
	gitRun(t, repo, "commit", "-m", "feature")
	gitRun(t, repo, "checkout", "main")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("os.Chdir repo: %v", err)
	}

	merged, err := isBranchMergedIntoBase("feature/demo", "main")
	if err != nil {
		t.Fatalf("isBranchMergedIntoBase returned error: %v", err)
	}
	if merged {
		t.Fatal("expected feature/demo to be unmerged before merge commit")
	}

	gitRun(t, repo, "merge", "--no-ff", "feature/demo", "-m", "merge feature")

	merged, err = isBranchMergedIntoBase("feature/demo", "main")
	if err != nil {
		t.Fatalf("isBranchMergedIntoBase returned error after merge: %v", err)
	}
	if !merged {
		t.Fatal("expected feature/demo to be merged after merge commit")
	}
}

func TestDefaultMergeBaseBranchFallsBackToMain(t *testing.T) {
	repo := initTestGitRepo(t)
	writeFile(t, repo, "README.md", "root\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("os.Chdir repo: %v", err)
	}

	branch, err := defaultMergeBaseBranch()
	if err != nil {
		t.Fatalf("defaultMergeBaseBranch returned error: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected default merge base to be main, got %q", branch)
	}
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.name", "Mono Test")
	gitRun(t, repo, "config", "user.email", "mono-test@example.com")
	return repo
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", args, err, string(out))
	}
}

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(absPath), err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}
}
