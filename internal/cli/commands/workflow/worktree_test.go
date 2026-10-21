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

	branch, fromRef, uniqueID, noBootstrap, skipSync, err := parseCreateArgs([]string{
		"feature/my-work",
		"--from", "main",
		"--id", "feature-my-work-1",
		"--no-bootstrap",
		"--skip-sync",
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
	if !skipSync {
		t.Fatal("expected skipSync=true")
	}
}

func TestParseCreateArgsFlagFirst(t *testing.T) {
	t.Parallel()

	branch, fromRef, uniqueID, noBootstrap, skipSync, err := parseCreateArgs([]string{
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
	if skipSync {
		t.Fatal("expected skipSync=false")
	}
}

func TestParseCreateArgsInvalidID(t *testing.T) {
	t.Parallel()

	_, _, _, _, _, err := parseCreateArgs([]string{"feature/my-work", "--id", "Bad_ID"})
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

func TestParseListArgs(t *testing.T) {
	t.Parallel()

	state, err := parseListArgs([]string{"--state", "active"})
	if err != nil {
		t.Fatalf("parseListArgs returned error: %v", err)
	}
	if state != worktreeListStateActive {
		t.Fatalf("unexpected state: %q", state)
	}

	state, err = parseListArgs([]string{"--state=needs-input"})
	if err != nil {
		t.Fatalf("parseListArgs returned error: %v", err)
	}
	if state != worktreeListStateNeedsInput {
		t.Fatalf("unexpected state: %q", state)
	}

	state, err = parseListArgs(nil)
	if err != nil {
		t.Fatalf("parseListArgs nil returned error: %v", err)
	}
	if state != worktreeListStateAll {
		t.Fatalf("unexpected default state: %q", state)
	}
}

func TestParsePruneArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want worktreePruneOptions
	}{
		{args: []string{"--merged", "--dry-run"}, want: worktreePruneOptions{merged: true, dryRun: true}},
		{args: []string{"--all", "--dry-run"}, want: worktreePruneOptions{all: true, dryRun: true}},
		{args: []string{"-i"}, want: worktreePruneOptions{interactive: true}},
		{args: []string{"--interactive"}, want: worktreePruneOptions{interactive: true}},
		{args: []string{"--merged", "-i"}, want: worktreePruneOptions{merged: true, interactive: true}},
	}
	for _, tc := range cases {
		got, err := parsePruneArgs(tc.args)
		if err != nil || got != tc.want {
			t.Errorf("parsePruneArgs(%v) = %+v, %v; want %+v", tc.args, got, err, tc.want)
		}
	}
	if got, err := parsePruneArgs(nil); err != nil || got != (worktreePruneOptions{}) {
		t.Fatalf("expected unchanged no-argument mode, got %+v, %v", got, err)
	}
}

func TestParsePruneArgsRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--dry-run"}, {"--merged", "--merged"}, {"--all", "--all"},
		{"--merged", "--all"}, {"--merged", "--dry-run", "--dry-run"},
		{"-i", "--interactive"}, {"--all", "--interactive", "--dry-run"}, {"--force"},
	} {
		if _, err := parsePruneArgs(args); err == nil {
			t.Errorf("expected parsePruneArgs(%v) to fail", args)
		}
	}
}

func TestParseListArgsInvalid(t *testing.T) {
	t.Parallel()

	if _, err := parseListArgs([]string{"--state", "blocked"}); err == nil {
		t.Fatal("expected invalid state error")
	}
	if _, err := parseListArgs([]string{"--state"}); err == nil {
		t.Fatal("expected missing value error")
	}
	if _, err := parseListArgs([]string{"--bad-flag"}); err == nil {
		t.Fatal("expected unknown argument error")
	}
}

func TestIncludeWorktreeInList(t *testing.T) {
	t.Parallel()

	if !includeWorktreeInList(worktreeListStateActive, workflowStatusInProgress, "dirty", "no") {
		t.Fatal("expected active state to include IN_PROGRESS + unmerged worktree")
	}
	if includeWorktreeInList(worktreeListStateActive, workflowStatusInProgress, "clean", "yes") {
		t.Fatal("did not expect active state to include merged worktree")
	}
	if !includeWorktreeInList(worktreeListStateNeedsInput, workflowStatusNeedsInput, "clean", "yes") {
		t.Fatal("expected needs-input state to include NEEDS_INPUT workflow")
	}
	if !includeWorktreeInList(worktreeListStateNeedsInput, workflowStatusUnset, "clean", "unknown") {
		t.Fatal("expected needs-input state to include unknown merge status")
	}
	if !includeWorktreeInList(worktreeListStateDone, workflowStatusDone, "clean", "yes") {
		t.Fatal("expected done state to include DONE + merged + clean worktree")
	}
	if includeWorktreeInList(worktreeListStateDone, workflowStatusDone, "dirty", "yes") {
		t.Fatal("did not expect done state to include dirty worktree")
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

func TestFormatWorktreeHeader(t *testing.T) {
	t.Parallel()

	if got := formatWorktreeHeader("/tmp/wt", workflowStatusDone); got != "/tmp/wt [DONE]" {
		t.Fatalf("header mismatch: got %q", got)
	}
	if got := formatWorktreeHeader("/tmp/wt", workflowStatusInProgress); got != "/tmp/wt [IN_PROGRESS]" {
		t.Fatalf("header mismatch: got %q", got)
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
	if !strings.Contains(string(out), "mono worktree list [--state <active|needs-input|done>]") {
		t.Fatalf("usage output missing list state options: %q", string(out))
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

func TestRunWorktreePruneMerged(t *testing.T) {
	repo := initTestGitRepo(t)
	writeFile(t, repo, "README.md", "root\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")

	gitRun(t, repo, "checkout", "-b", "feature/merged")
	writeFile(t, repo, "merged.txt", "merged\n")
	gitRun(t, repo, "add", "merged.txt")
	gitRun(t, repo, "commit", "-m", "merged feature")
	gitRun(t, repo, "checkout", "main")
	gitRun(t, repo, "merge", "--no-ff", "feature/merged", "-m", "merge feature")

	gitRun(t, repo, "checkout", "-b", "feature/unmerged")
	writeFile(t, repo, "unmerged.txt", "unmerged\n")
	gitRun(t, repo, "add", "unmerged.txt")
	gitRun(t, repo, "commit", "-m", "unmerged feature")
	gitRun(t, repo, "checkout", "main")
	gitRun(t, repo, "branch", "feature/dirty")

	paths := map[string]string{
		"merged":   filepath.Join(t.TempDir(), "merged"),
		"unmerged": filepath.Join(t.TempDir(), "unmerged"),
		"dirty":    filepath.Join(t.TempDir(), "dirty"),
		"detached": filepath.Join(t.TempDir(), "detached"),
	}
	gitRun(t, repo, "worktree", "add", paths["merged"], "feature/merged")
	gitRun(t, repo, "worktree", "add", paths["unmerged"], "feature/unmerged")
	gitRun(t, repo, "worktree", "add", paths["dirty"], "feature/dirty")
	gitRun(t, repo, "worktree", "add", "--detach", paths["detached"], "main")
	writeFile(t, paths["dirty"], "untracked.txt", "dirty\n")

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
	for name, path := range paths {
		paths[name] = gitOutputString(t, path, "rev-parse", "--show-toplevel")
	}

	// The legacy invocation only prunes stale Git metadata and status keys.
	if err := runWorktreePrune(nil); err != nil {
		t.Fatalf("runWorktreePrune without options: %v", err)
	}
	for name, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("no-argument prune removed %s worktree: %v", name, err)
		}
	}
	gitRun(t, repo, "branch", "feature/uninspectable", "main")
	uninspectablePath := filepath.Join(t.TempDir(), "uninspectable")
	gitRun(t, repo, "worktree", "add", uninspectablePath, "feature/uninspectable")
	paths["uninspectable"] = gitOutputString(t, uninspectablePath, "rev-parse", "--show-toplevel")
	if err := os.RemoveAll(paths["uninspectable"]); err != nil {
		t.Fatalf("remove uninspectable worktree directory: %v", err)
	}

	storePath, err := worktreeStatusStorePath()
	if err != nil {
		t.Fatalf("worktreeStatusStorePath: %v", err)
	}
	store := &worktreeStatusStore{Worktrees: make(map[string]worktreeWorkflowStatus)}
	for _, path := range paths {
		store.Worktrees[normalizeWorktreePath(path)] = workflowStatusDone
	}
	if err := saveWorktreeStatusStoreToPath(storePath, store); err != nil {
		t.Fatalf("save status store: %v", err)
	}
	porcelainBefore := gitOutputString(t, repo, "worktree", "list", "--porcelain")
	branchesBefore := gitOutputString(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	statusesBefore, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read status store: %v", err)
	}
	preview, err := captureStdout(t, func() error {
		return runWorktreePrune([]string{"--merged", "--dry-run"})
	})
	if err != nil {
		t.Fatalf("dry-run merged prune: %v", err)
	}
	if want := "Would prune stale worktree metadata for " + paths["uninspectable"] + " because the worktree path is missing"; !strings.Contains(preview, want) {
		t.Fatalf("dry-run output did not explain missing worktree path; wanted %q in:\n%s", want, preview)
	}
	if got := gitOutputString(t, repo, "worktree", "list", "--porcelain"); got != porcelainBefore {
		t.Fatalf("dry-run changed worktree metadata\nbefore:\n%s\nafter:\n%s", porcelainBefore, got)
	}
	if got := gitOutputString(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"); got != branchesBefore {
		t.Fatalf("dry-run changed local branches\nbefore:\n%s\nafter:\n%s", branchesBefore, got)
	}
	statusesAfter, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read status store after dry-run: %v", err)
	}
	if string(statusesAfter) != string(statusesBefore) {
		t.Fatal("dry-run changed workflow status tags")
	}
	allPreview, err := captureStdout(t, func() error {
		return runWorktreePrune([]string{"--all", "--dry-run"})
	})
	if err != nil {
		t.Fatalf("all-worktree dry-run: %v", err)
	}
	for _, name := range []string{"merged", "unmerged", "detached"} {
		if !strings.Contains(allPreview, "Would remove "+paths[name]) {
			t.Errorf("all-worktree dry-run did not include %s worktree %s:\n%s", name, paths[name], allPreview)
		}
	}
	if strings.Contains(allPreview, "Would remove "+paths["dirty"]) || strings.Contains(allPreview, "Would remove "+repo) {
		t.Fatalf("all-worktree dry-run included dirty or primary worktree:\n%s", allPreview)
	}
	if got := gitOutputString(t, repo, "worktree", "list", "--porcelain"); got != porcelainBefore {
		t.Fatalf("all-worktree dry-run changed worktree metadata\nbefore:\n%s\nafter:\n%s", porcelainBefore, got)
	}
	if got := gitOutputString(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"); got != branchesBefore {
		t.Fatalf("all-worktree dry-run changed local branches\nbefore:\n%s\nafter:\n%s", branchesBefore, got)
	}
	statusesAfterAllPreview, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read status store after all-worktree dry-run: %v", err)
	}
	if string(statusesAfterAllPreview) != string(statusesBefore) {
		t.Fatal("all-worktree dry-run changed workflow status tags")
	}
	for name, path := range paths {
		if _, err := os.Stat(path); err != nil {
			if name != "uninspectable" || !os.IsNotExist(err) {
				t.Fatalf("dry-run changed %s worktree path: %v", name, err)
			}
		}
	}

	pruneOutput, err := captureStdout(t, func() error {
		return runWorktreePrune([]string{"--merged"})
	})
	if err != nil {
		t.Fatalf("merged prune: %v", err)
	}
	if want := "Pruned stale worktree metadata for " + paths["uninspectable"] + " because the worktree path was missing"; !strings.Contains(pruneOutput, want) {
		t.Fatalf("prune output did not confirm missing-path metadata cleanup; wanted %q in:\n%s", want, pruneOutput)
	}
	if _, err := os.Stat(paths["merged"]); !os.IsNotExist(err) {
		t.Fatalf("expected merged worktree to be removed, stat error: %v", err)
	}
	for _, name := range []string{"unmerged", "dirty", "detached"} {
		if _, err := os.Stat(paths[name]); err != nil {
			t.Fatalf("expected %s worktree to remain: %v", name, err)
		}
	}
	if got := gitOutputString(t, repo, "show-ref", "--verify", "refs/heads/feature/merged"); got == "" {
		t.Fatal("expected merged local branch to remain")
	}
	remaining, err := loadWorktreeStatusStoreFromPath(storePath)
	if err != nil {
		t.Fatalf("load status store after prune: %v", err)
	}
	if _, ok := remaining.Worktrees[normalizeWorktreePath(paths["uninspectable"])]; ok {
		t.Fatal("expected stale uninspectable worktree status tag to be pruned with stale Git metadata")
	}
	if _, ok := remaining.Worktrees[normalizeWorktreePath(paths["merged"])]; ok {
		t.Fatal("expected successfully removed worktree status tag to be cleared")
	}
	for _, name := range []string{"unmerged", "dirty", "detached"} {
		if _, ok := remaining.Worktrees[normalizeWorktreePath(paths[name])]; !ok {
			t.Errorf("expected %s worktree status tag to remain", name)
		}
	}
	allOutput, err := captureStdout(t, func() error {
		return runWorktreePrune([]string{"--all"})
	})
	if err != nil {
		t.Fatalf("all-worktree prune: %v", err)
	}
	for _, name := range []string{"unmerged", "detached"} {
		if _, err := os.Stat(paths[name]); !os.IsNotExist(err) {
			t.Errorf("expected --all to remove %s worktree, stat error: %v", name, err)
		}
		if !strings.Contains(allOutput, "Removed worktree "+paths[name]) {
			t.Errorf("--all output did not report removed %s worktree:\n%s", name, allOutput)
		}
	}
	if got := gitOutputString(t, repo, "show-ref", "--verify", "refs/heads/feature/unmerged"); got == "" {
		t.Error("expected --all to retain local branch for removed unmerged worktree")
	}
	if _, err := os.Stat(paths["dirty"]); err != nil {
		t.Errorf("expected --all to retain dirty worktree: %v", err)
	}
}

func TestRunWorktreePruneInteractiveDefaultsToKeep(t *testing.T) {
	repo := initTestGitRepo(t)
	writeFile(t, repo, "README.md", "root\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	for _, branch := range []string{"feature/remove", "feature/keep", "feature/dirty-keep", "feature/dirty-remove"} {
		gitRun(t, repo, "branch", branch)
	}
	pathsRoot := t.TempDir()
	removePath := filepath.Join(pathsRoot, "a-remove")
	keepPath := filepath.Join(pathsRoot, "b-keep")
	dirtyKeepPath := filepath.Join(pathsRoot, "c-dirty-keep")
	dirtyRemovePath := filepath.Join(pathsRoot, "d-dirty-remove")
	gitRun(t, repo, "worktree", "add", removePath, "feature/remove")
	gitRun(t, repo, "worktree", "add", keepPath, "feature/keep")
	gitRun(t, repo, "worktree", "add", dirtyKeepPath, "feature/dirty-keep")
	gitRun(t, repo, "worktree", "add", dirtyRemovePath, "feature/dirty-remove")
	writeFile(t, dirtyKeepPath, "untracked.txt", "keep this dirty data\n")
	writeFile(t, dirtyRemovePath, "untracked.txt", "explicitly remove this dirty data\n")

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

	oldStdin := os.Stdin
	oldStderr := os.Stderr
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := writePipe.WriteString("n\n\n\nn\n"); err != nil {
		t.Fatalf("write interactive answers: %v", err)
	}
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close interactive input: %v", err)
	}
	os.Stdin = readPipe
	stderrFile, err := os.CreateTemp(t.TempDir(), "interactive-stderr-")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	os.Stderr = stderrFile
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stderr = oldStderr
		_ = readPipe.Close()
		_ = stderrFile.Close()
	})
	output, err := captureStdout(t, func() error {
		return runWorktreePrune([]string{"--interactive"})
	})
	if err != nil {
		t.Fatalf("interactive prune: %v", err)
	}
	if _, err := os.Stat(removePath); !os.IsNotExist(err) {
		t.Fatalf("expected n response to remove first worktree, stat error: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("expected blank response to keep second worktree: %v", err)
	}
	if _, err := os.Stat(dirtyKeepPath); err != nil {
		t.Fatalf("expected blank response to keep dirty worktree: %v", err)
	}
	if _, err := os.Stat(dirtyRemovePath); !os.IsNotExist(err) {
		t.Fatalf("expected explicit n response to remove dirty worktree, stat error: %v", err)
	}
	if _, err := stderrFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind stderr capture: %v", err)
	}
	prompts, err := io.ReadAll(stderrFile)
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	if !strings.Contains(string(prompts), dirtyKeepPath) || !strings.Contains(string(prompts), "dirty; removal discards changes") {
		t.Fatalf("interactive prompt did not identify dirty worktree and removal impact:\n%s", prompts)
	}
	if !strings.Contains(output, "Kept ") {
		t.Fatalf("expected output to report default keep decision, got:\n%s", output)
	}
	for _, branch := range []string{"feature/remove", "feature/keep", "feature/dirty-keep", "feature/dirty-remove"} {
		if got := gitOutputString(t, repo, "show-ref", "--verify", "refs/heads/"+branch); got == "" {
			t.Errorf("expected interactive prune to retain local branch %s", branch)
		}
	}
}

func TestRunWorktreePruneMergedRequiresBase(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "topic")
	gitRun(t, repo, "config", "user.name", "Mono Test")
	gitRun(t, repo, "config", "user.email", "mono-test@example.com")
	writeFile(t, repo, "README.md", "topic\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "topic")

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
	if err := runWorktreePrune([]string{"--merged"}); err == nil || !strings.Contains(err.Error(), "could not determine default base branch") {
		t.Fatalf("expected base resolution error, got %v", err)
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

func TestRunWorktreeCreateSyncsDefaultBranchFromOrigin(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, ".", "init", "--bare", "--initial-branch=main", origin)

	repo := filepath.Join(t.TempDir(), "repo")
	gitRun(t, ".", "clone", origin, repo)
	gitRun(t, repo, "config", "user.name", "Mono Test")
	gitRun(t, repo, "config", "user.email", "mono-test@example.com")

	writeFile(t, repo, "README.md", "initial\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	gitRun(t, repo, "push", "-u", "origin", "main")
	initialMain := gitOutputString(t, repo, "rev-parse", "main")

	gitRun(t, repo, "checkout", "-b", "commitmessage")

	updater := filepath.Join(t.TempDir(), "updater")
	gitRun(t, ".", "clone", origin, updater)
	gitRun(t, updater, "config", "user.name", "Mono Test")
	gitRun(t, updater, "config", "user.email", "mono-test@example.com")
	writeFile(t, updater, "README.md", "updated\n")
	gitRun(t, updater, "add", "README.md")
	gitRun(t, updater, "commit", "-m", "update main")
	gitRun(t, updater, "push", "origin", "main")
	latestMain := gitOutputString(t, updater, "rev-parse", "HEAD")

	if initialMain == latestMain {
		t.Fatal("expected updater to advance main")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
		if setErr := os.Setenv("HOME", oldHome); setErr != nil {
			t.Fatalf("restore HOME: %v", setErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("os.Chdir repo: %v", err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	if err := runWorktreeCreate([]string{"feature/test", "--id", "feature-test", "--no-bootstrap"}); err != nil {
		t.Fatalf("runWorktreeCreate returned error: %v", err)
	}

	dest := filepath.Join(home, ".worktrees", filepath.Base(repo), "feature-test")
	if got := gitOutputString(t, dest, "branch", "--show-current"); got != "feature/test" {
		t.Fatalf("expected worktree branch %q, got %q", "feature/test", got)
	}
	if got := gitOutputString(t, dest, "rev-parse", "HEAD"); got != latestMain {
		t.Fatalf("expected worktree HEAD %q, got %q", latestMain, got)
	}
	if got := gitOutputString(t, repo, "rev-parse", "main"); got != latestMain {
		t.Fatalf("expected local main to fast-forward to %q, got %q", latestMain, got)
	}
}

func TestRunWorktreeCreateSkipSyncUsesCurrentHEAD(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, ".", "init", "--bare", "--initial-branch=main", origin)

	repo := filepath.Join(t.TempDir(), "repo")
	gitRun(t, ".", "clone", origin, repo)
	gitRun(t, repo, "config", "user.name", "Mono Test")
	gitRun(t, repo, "config", "user.email", "mono-test@example.com")

	writeFile(t, repo, "README.md", "initial\n")
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	gitRun(t, repo, "push", "-u", "origin", "main")
	gitRun(t, repo, "checkout", "-b", "commitmessage")

	writeFile(t, repo, "feature.txt", "local head\n")
	gitRun(t, repo, "add", "feature.txt")
	gitRun(t, repo, "commit", "-m", "local head")
	localHead := gitOutputString(t, repo, "rev-parse", "HEAD")
	localMainBefore := gitOutputString(t, repo, "rev-parse", "main")

	updater := filepath.Join(t.TempDir(), "updater")
	gitRun(t, ".", "clone", origin, updater)
	gitRun(t, updater, "config", "user.name", "Mono Test")
	gitRun(t, updater, "config", "user.email", "mono-test@example.com")
	writeFile(t, updater, "README.md", "updated\n")
	gitRun(t, updater, "add", "README.md")
	gitRun(t, updater, "commit", "-m", "update main")
	gitRun(t, updater, "push", "origin", "main")
	latestMain := gitOutputString(t, updater, "rev-parse", "HEAD")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
		if setErr := os.Setenv("HOME", oldHome); setErr != nil {
			t.Fatalf("restore HOME: %v", setErr)
		}
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("os.Chdir repo: %v", err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	if err := runWorktreeCreate([]string{"feature/test", "--id", "feature-test", "--no-bootstrap", "--skip-sync"}); err != nil {
		t.Fatalf("runWorktreeCreate returned error: %v", err)
	}

	dest := filepath.Join(home, ".worktrees", filepath.Base(repo), "feature-test")
	if got := gitOutputString(t, dest, "branch", "--show-current"); got != "feature/test" {
		t.Fatalf("expected worktree branch %q, got %q", "feature/test", got)
	}
	if got := gitOutputString(t, dest, "rev-parse", "HEAD"); got != localHead {
		t.Fatalf("expected worktree HEAD %q, got %q", localHead, got)
	}
	if got := gitOutputString(t, repo, "rev-parse", "main"); got != localMainBefore {
		t.Fatalf("expected local main to remain %q, got %q", localMainBefore, got)
	}
	if latestMain == localMainBefore {
		t.Fatal("expected remote main to advance beyond local main")
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

func gitOutputString(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s output failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
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

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	runErr := run()
	os.Stdout = oldStdout
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(output), runErr
}
