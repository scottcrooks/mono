package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
	"gopkg.in/yaml.v3"
)

type worktreeCommand struct{}

type worktreeWorkflowStatus string

const (
	workflowStatusInProgress worktreeWorkflowStatus = "IN_PROGRESS"
	workflowStatusDone       worktreeWorkflowStatus = "DONE"
	workflowStatusNeedsInput worktreeWorkflowStatus = "NEEDS_INPUT"
	workflowStatusUnset      worktreeWorkflowStatus = "unset"
)

type worktreeListState string

const (
	worktreeListStateAll        worktreeListState = ""
	worktreeListStateActive     worktreeListState = "active"
	worktreeListStateNeedsInput worktreeListState = "needs-input"
	worktreeListStateDone       worktreeListState = "done"
)

type worktreeStatusStore struct {
	Worktrees map[string]worktreeWorkflowStatus `yaml:"worktrees"`
}

func init() {
	registerCommand("worktree", &worktreeCommand{})
}

func (c *worktreeCommand) Run(args []string) error {
	if len(args) < 3 {
		printWorktreeUsage()
		return fmt.Errorf("missing worktree subcommand")
	}

	switch args[2] {
	case "create":
		return runWorktreeCreate(args[3:])
	case "list":
		return runWorktreeList(args[3:])
	case "path":
		return runWorktreePath(args[3:])
	case "tag":
		return runWorktreeTag(args[3:])
	case "remove":
		return runWorktreeRemove(args[3:])
	case "prune":
		return runWorktreePrune()
	default:
		printWorktreeUsage()
		return fmt.Errorf("unknown worktree subcommand: %s", args[2])
	}
}

func runWorktreeCreate(args []string) error {
	p := output.DefaultPrinter()

	branch, fromRef, uniqueID, noBootstrap, skipSync, err := parseCreateArgs(args)
	if err != nil {
		return err
	}

	repoRoot, err := gitRepoRoot()
	if err != nil {
		return err
	}

	baseDir, err := worktreeBaseDir(repoRoot)
	if err != nil {
		return err
	}

	if uniqueID == "" {
		uniqueID = defaultWorktreeID(branch, time.Now())
	}

	dest := filepath.Join(baseDir, uniqueID)
	if _, statErr := os.Stat(dest); statErr == nil {
		return fmt.Errorf("destination already exists: %s", dest)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to inspect destination %s: %w", dest, statErr)
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("failed to create base worktree directory %s: %w", baseDir, err)
	}

	if skipSync {
		if fromRef == "" {
			fromRef = "HEAD"
		}
		p.StepWarn("worktree", "Sync skipped (--skip-sync)")
	} else {
		fromRef, err = resolveWorktreeCreateBase(repoRoot, fromRef, p)
		if err != nil {
			return err
		}
	}

	addCmd := exec.Command("git", "worktree", "add", "-b", branch, dest, fromRef)
	addCmd.Stdout = os.Stdout
	addCmd.Stderr = os.Stderr
	if runErr := addCmd.Run(); runErr != nil {
		return fmt.Errorf("git worktree add failed: %w", runErr)
	}

	p.StepOK("worktree", fmt.Sprintf("Created worktree for branch %q at %s", branch, dest))

	if noBootstrap {
		p.StepWarn("worktree", "Bootstrap skipped (--no-bootstrap)")
		return nil
	}

	copied, err := copyProjectEnvFiles(repoRoot, dest)
	if err != nil {
		return err
	}
	if copied > 0 {
		p.StepOK("worktree", fmt.Sprintf("Copied %d .env file(s) into worktree", copied))
	} else {
		p.StepWarn("worktree", "No .env files found to copy from apps/ or packages/")
	}

	if err := runBootstrap(dest); err != nil {
		return err
	}

	if err := runWorktreeRequirements(dest); err != nil {
		return err
	}

	return nil
}

func runWorktreeList(args []string) error {
	p := output.DefaultPrinter()

	listState, err := parseListArgs(args)
	if err != nil {
		return err
	}

	entries, err := gitWorktreeEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		p.Summary("No git worktrees found")
		return nil
	}

	mergeBase, mergeBaseErr := defaultMergeBaseBranch()
	mergeBaseLabel := mergeBase
	if mergeBaseLabel == "" {
		mergeBaseLabel = "default"
	}
	statusStore, statusLoadErr := loadWorktreeStatusStore()
	if statusLoadErr != nil {
		return statusLoadErr
	}

	printed := 0
	for _, entry := range entries {
		branch := entry.Branch
		if branch == "" && entry.Detached {
			branch = "(detached)"
		}
		dirtyState := "clean"
		dirty, dirtyErr := isWorktreeDirty(entry.Path)
		if dirtyErr != nil {
			dirtyState = "unknown"
		} else if dirty {
			dirtyState = "dirty"
		}
		mergedState := "unknown"
		switch {
		case entry.Branch == "" || entry.Detached:
			mergedState = "n/a"
		case mergeBaseErr != nil:
			mergedState = "unknown"
		default:
			merged, err := isBranchMergedIntoBase(entry.Branch, mergeBase)
			if err != nil {
				mergedState = "unknown"
			} else if merged {
				mergedState = "yes"
			} else {
				mergedState = "no"
			}
		}

		workflowStatus := workflowStatusUnset
		if tagged, ok := statusStore.Worktrees[normalizeWorktreePath(entry.Path)]; ok {
			workflowStatus = tagged
		}

		if !includeWorktreeInList(listState, workflowStatus, dirtyState, mergedState) {
			continue
		}

		printWorktreeHeader(p, entry.Path, workflowStatus)
		p.Summary("  branch: " + branch)
		p.Summary("  head:   " + shortSHA(entry.Head))
		p.Summary("  status: " + dirtyState)
		p.Summary(fmt.Sprintf("  merged(%s): %s", mergeBaseLabel, mergedState))
		p.Blank()
		printed++
	}

	if printed == 0 && listState != worktreeListStateAll {
		p.Summary(fmt.Sprintf("No worktrees matched --state %q", listState))
	}

	return nil
}

func runWorktreeTag(args []string) error {
	status, err := parseTagArgs(args)
	if err != nil {
		return err
	}

	currentPath, err := currentWorktreePath()
	if err != nil {
		return err
	}

	store, storePath, err := loadWorktreeStatusStoreWithPath()
	if err != nil {
		return err
	}
	if store.Worktrees == nil {
		store.Worktrees = make(map[string]worktreeWorkflowStatus)
	}
	store.Worktrees[normalizeWorktreePath(currentPath)] = status

	if err := saveWorktreeStatusStoreToPath(storePath, store); err != nil {
		return err
	}

	fmt.Printf("[ok] Tagged current worktree %s as %s\n", currentPath, status)
	return nil
}

func runWorktreePath(args []string) error {
	p := output.DefaultPrinter()
	if len(args) != 1 {
		return fmt.Errorf("usage: mono worktree path <branch-or-id>")
	}

	entry, err := resolveWorktree(args[0])
	if err != nil {
		return err
	}

	p.Summary(entry.Path)
	return nil
}

func runWorktreeRemove(args []string) error {
	p := output.DefaultPrinter()

	identifier, force, err := parseRemoveArgs(args)
	if err != nil {
		return err
	}

	entry, err := resolveWorktree(identifier)
	if err != nil {
		return err
	}

	if !force {
		dirty, dirtyErr := isWorktreeDirty(entry.Path)
		if dirtyErr != nil {
			return dirtyErr
		}
		if dirty {
			return fmt.Errorf("worktree %s has uncommitted changes; rerun with --force", entry.Path)
		}
	}

	removeArgs := []string{"worktree", "remove"}
	if force {
		removeArgs = append(removeArgs, "--force")
	}
	removeArgs = append(removeArgs, entry.Path)

	cmd := exec.Command("git", removeArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("git worktree remove failed: %w", runErr)
	}
	if err := deleteWorktreeStatus(entry.Path); err != nil {
		return err
	}

	p.StepOK("worktree", "Removed worktree "+entry.Path)
	return nil
}

func runWorktreePrune() error {
	p := output.DefaultPrinter()

	cmd := exec.Command("git", "worktree", "prune")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("git worktree prune failed: %w", runErr)
	}
	if err := pruneStaleWorktreeStatuses(); err != nil {
		return err
	}
	p.StepOK("worktree", "Pruned stale worktree metadata")
	return nil
}

func parseCreateArgs(args []string) (branch, fromRef, uniqueID string, noBootstrap, skipSync bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, false, fmt.Errorf("usage: mono worktree create <branch> [--from <ref>] [--id <unique-id>] [--no-bootstrap] [--skip-sync]")
	}

	fromRef = ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-bootstrap":
			noBootstrap = true
		case arg == "--skip-sync":
			skipSync = true
		case arg == "--from":
			if i+1 >= len(args) {
				return "", "", "", false, false, fmt.Errorf("--from requires a value")
			}
			fromRef = args[i+1]
			i++
		case strings.HasPrefix(arg, "--from="):
			fromRef = strings.TrimPrefix(arg, "--from=")
			if fromRef == "" {
				return "", "", "", false, false, fmt.Errorf("--from requires a value")
			}
		case arg == "--id":
			if i+1 >= len(args) {
				return "", "", "", false, false, fmt.Errorf("--id requires a value")
			}
			uniqueID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--id="):
			uniqueID = strings.TrimPrefix(arg, "--id=")
			if uniqueID == "" {
				return "", "", "", false, false, fmt.Errorf("--id requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return "", "", "", false, false, fmt.Errorf("unknown flag %q", arg)
		default:
			if branch != "" {
				return "", "", "", false, false, fmt.Errorf("unexpected extra argument %q", arg)
			}
			branch = arg
		}
	}

	if branch == "" {
		return "", "", "", false, false, fmt.Errorf("branch is required")
	}
	if uniqueID != "" && sanitizeSlug(uniqueID) != uniqueID {
		return "", "", "", false, false, fmt.Errorf("unique id %q must already be slug-safe (lowercase letters, numbers, and hyphens)", uniqueID)
	}

	return branch, fromRef, uniqueID, noBootstrap, skipSync, nil
}

func parseRemoveArgs(args []string) (identifier string, force bool, err error) {
	if len(args) == 0 {
		return "", false, fmt.Errorf("usage: mono worktree remove <branch-or-id> [--force]")
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--force":
			force = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown flag %q", arg)
			}
			if identifier != "" {
				return "", false, fmt.Errorf("unexpected extra argument %q", arg)
			}
			identifier = arg
		}
	}

	if identifier == "" {
		return "", false, fmt.Errorf("usage: mono worktree remove <branch-or-id> [--force]")
	}
	return identifier, force, nil
}

func parseTagArgs(args []string) (worktreeWorkflowStatus, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: mono worktree tag <IN_PROGRESS|DONE|NEEDS_INPUT>")
	}
	return parseWorkflowStatus(args[0])
}

func parseListArgs(args []string) (worktreeListState, error) {
	state := worktreeListStateAll
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--state":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--state requires a value")
			}
			parsed, err := parseListState(args[i+1])
			if err != nil {
				return "", err
			}
			state = parsed
			i++
		case strings.HasPrefix(arg, "--state="):
			value := strings.TrimPrefix(arg, "--state=")
			if value == "" {
				return "", fmt.Errorf("--state requires a value")
			}
			parsed, err := parseListState(value)
			if err != nil {
				return "", err
			}
			state = parsed
		default:
			return "", fmt.Errorf("unknown argument %q (usage: mono worktree list [--state <active|needs-input|done>])", arg)
		}
	}
	return state, nil
}

func parseListState(input string) (worktreeListState, error) {
	state := worktreeListState(strings.ToLower(strings.TrimSpace(input)))
	switch state {
	case worktreeListStateActive, worktreeListStateNeedsInput, worktreeListStateDone:
		return state, nil
	default:
		return "", fmt.Errorf("invalid list state %q (expected one of: active, needs-input, done)", input)
	}
}

func includeWorktreeInList(state worktreeListState, workflowStatus worktreeWorkflowStatus, dirtyState, mergedState string) bool {
	switch state {
	case worktreeListStateAll:
		return true
	case worktreeListStateActive:
		return workflowStatus == workflowStatusInProgress && mergedState == "no"
	case worktreeListStateNeedsInput:
		return workflowStatus == workflowStatusNeedsInput || mergedState == "unknown"
	case worktreeListStateDone:
		return workflowStatus == workflowStatusDone && mergedState == "yes" && dirtyState == "clean"
	default:
		return true
	}
}

func parseWorkflowStatus(input string) (worktreeWorkflowStatus, error) {
	status := worktreeWorkflowStatus(strings.ToUpper(strings.TrimSpace(input)))
	switch status {
	case workflowStatusInProgress, workflowStatusDone, workflowStatusNeedsInput:
		return status, nil
	default:
		return "", fmt.Errorf("invalid workflow status %q (expected one of: IN_PROGRESS, DONE, NEEDS_INPUT)", input)
	}
}

func formatWorktreeHeader(path string, status worktreeWorkflowStatus) string {
	label := "[" + string(status) + "]"
	return path + " " + label
}

func printWorktreeHeader(p output.Printer, path string, status worktreeWorkflowStatus) {
	header := formatWorktreeHeader(path, status)
	switch status {
	case workflowStatusDone:
		p.StepOK("", header)
	case workflowStatusInProgress:
		p.StepInfo("", header)
	case workflowStatusNeedsInput:
		p.StepErr("", header)
	default:
		p.Summary(header)
	}
}

func gitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect git repository root (run inside a git repo): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func worktreeBaseDir(repoRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	repoName := filepath.Base(repoRoot)
	return filepath.Join(home, ".worktrees", repoName), nil
}

func defaultWorktreeID(branch string, now time.Time) string {
	slug := sanitizeSlug(branch)
	if slug == "" {
		slug = "worktree"
	}
	return fmt.Sprintf("%s-%s", slug, now.Format("20060102-150405"))
}

func sanitizeSlug(input string) string {
	s := strings.ToLower(input)
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteRune('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

type gitWorktreeEntry struct {
	Path     string
	Head     string
	Branch   string
	Detached bool
}

func gitWorktreeEntries() ([]gitWorktreeEntry, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}
	return parseWorktreePorcelain(string(out))
}

func parseWorktreePorcelain(output string) ([]gitWorktreeEntry, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, nil
	}

	blocks := strings.Split(trimmed, "\n\n")
	entries := make([]gitWorktreeEntry, 0, len(blocks))
	for _, block := range blocks {
		var entry gitWorktreeEntry
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "worktree "):
				entry.Path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "HEAD "):
				entry.Head = strings.TrimPrefix(line, "HEAD ")
			case strings.HasPrefix(line, "branch "):
				raw := strings.TrimPrefix(line, "branch ")
				entry.Branch = strings.TrimPrefix(raw, "refs/heads/")
			case line == "detached":
				entry.Detached = true
			}
		}
		if entry.Path == "" {
			return nil, errors.New("failed to parse git worktree output: missing worktree path")
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func resolveWorktree(identifier string) (*gitWorktreeEntry, error) {
	entries, err := gitWorktreeEntries()
	if err != nil {
		return nil, err
	}
	return resolveWorktreeFromEntries(entries, identifier)
}

func resolveWorktreeFromEntries(entries []gitWorktreeEntry, identifier string) (*gitWorktreeEntry, error) {
	if len(entries) == 0 {
		return nil, errors.New("no worktrees found")
	}

	// Prefer exact branch match first.
	for i := range entries {
		if entries[i].Branch == identifier {
			return &entries[i], nil
		}
	}

	// Fall back to unique ID (directory basename) match.
	matches := make([]*gitWorktreeEntry, 0, 1)
	for i := range entries {
		if filepath.Base(entries[i].Path) == identifier {
			matches = append(matches, &entries[i])
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("identifier %q is ambiguous across multiple worktrees", identifier)
	}

	return nil, fmt.Errorf("worktree %q not found", identifier)
}

func isWorktreeDirty(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to inspect worktree status for %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func resolveWorktreeCreateBase(repoRoot, requestedRef string, p output.Printer) (string, error) {
	remotes, err := gitRemoteNames(repoRoot)
	if err != nil {
		return "", err
	}

	for _, remote := range preferredWorktreeRemotes(remotes) {
		fetchCmd := exec.Command("git", "fetch", "--prune", remote)
		fetchCmd.Dir = repoRoot
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
		if runErr := fetchCmd.Run(); runErr != nil {
			return "", fmt.Errorf("git fetch --prune %s failed: %w", remote, runErr)
		}
		p.StepOK("worktree", fmt.Sprintf("Fetched latest refs from %s", remote))
	}

	remoteName, branchName, remoteRef, err := defaultRemoteBaseBranch(repoRoot, remotes)
	if err != nil {
		if requestedRef != "" {
			return requestedRef, nil
		}
		return "", err
	}

	updated, err := fastForwardLocalBranch(repoRoot, branchName, remoteRef)
	if err != nil {
		return "", err
	}
	if updated {
		p.StepOK("worktree", fmt.Sprintf("Fast-forwarded local %s to %s", branchName, remoteRef))
	}

	if requestedRef == "" {
		p.StepInfo("worktree", fmt.Sprintf("Creating %q from %s", branchName, remoteRef))
		return remoteRef, nil
	}

	if requestedRef == branchName || requestedRef == remoteName+"/"+branchName {
		return remoteRef, nil
	}

	return requestedRef, nil
}

func defaultMergeBaseBranch() (string, error) {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return "", err
	}

	originHeadCmd := exec.Command("git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	originHeadCmd.Dir = repoRoot
	if out, err := originHeadCmd.Output(); err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch, nil
		}
	}

	for _, candidate := range []string{"main", "master", "origin/main", "origin/master"} {
		verifyCmd := exec.Command("git", "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
		verifyCmd.Dir = repoRoot
		if err := verifyCmd.Run(); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("could not determine default base branch (tried origin/HEAD, main, master)")
}

func gitRemoteNames(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote failed: %w", err)
	}

	var remotes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		remote := strings.TrimSpace(line)
		if remote != "" {
			remotes = append(remotes, remote)
		}
	}
	return remotes, nil
}

func preferredWorktreeRemotes(remotes []string) []string {
	seen := make(map[string]struct{}, len(remotes))
	for _, remote := range remotes {
		seen[remote] = struct{}{}
	}

	preferred := make([]string, 0, 2)
	for _, remote := range []string{"upstream", "origin"} {
		if _, ok := seen[remote]; ok {
			preferred = append(preferred, remote)
		}
	}
	return preferred
}

func defaultRemoteBaseBranch(repoRoot string, remotes []string) (remoteName, branchName, remoteRef string, err error) {
	preferredRemotes := preferredWorktreeRemotes(remotes)
	for _, remote := range preferredRemotes {
		headCmd := exec.Command("git", "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD")
		headCmd.Dir = repoRoot
		if out, headErr := headCmd.Output(); headErr == nil {
			ref := strings.TrimSpace(string(out))
			prefix := remote + "/"
			if strings.HasPrefix(ref, prefix) {
				branch := strings.TrimPrefix(ref, prefix)
				if branch != "" {
					return remote, branch, ref, nil
				}
			}
		}
	}

	for _, remote := range preferredRemotes {
		for _, branch := range []string{"main", "master"} {
			ref := remote + "/" + branch
			verifyCmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
			verifyCmd.Dir = repoRoot
			if verifyCmd.Run() == nil {
				return remote, branch, ref, nil
			}
		}
	}

	return "", "", "", errors.New("could not determine default remote base branch (tried upstream/origin HEAD, main, master)")
}

func fastForwardLocalBranch(repoRoot, branchName, remoteRef string) (bool, error) {
	verifyCmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branchName)
	verifyCmd.Dir = repoRoot
	if verifyCmd.Run() != nil {
		return false, nil
	}

	listCmd := exec.Command("git", "branch", "--format=%(refname:short)|%(worktreepath)")
	listCmd.Dir = repoRoot
	out, err := listCmd.Output()
	if err != nil {
		return false, fmt.Errorf("git branch --format failed: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == branchName && strings.TrimSpace(parts[1]) != "" {
			return false, nil
		}
	}

	updateCmd := exec.Command("git", "branch", "-f", branchName, remoteRef)
	updateCmd.Dir = repoRoot
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return false, fmt.Errorf("failed to fast-forward local %s to %s: %w", branchName, remoteRef, err)
	}
	return true, nil
}

func loadWorktreeStatusStore() (*worktreeStatusStore, error) {
	store, _, err := loadWorktreeStatusStoreWithPath()
	if err != nil {
		return nil, err
	}
	return store, nil
}

func loadWorktreeStatusStoreWithPath() (*worktreeStatusStore, string, error) {
	storePath, err := worktreeStatusStorePath()
	if err != nil {
		return nil, "", err
	}

	store, err := loadWorktreeStatusStoreFromPath(storePath)
	if err != nil {
		return nil, "", err
	}
	return store, storePath, nil
}

func loadWorktreeStatusStoreFromPath(storePath string) (*worktreeStatusStore, error) {
	data, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &worktreeStatusStore{Worktrees: make(map[string]worktreeWorkflowStatus)}, nil
		}
		return nil, fmt.Errorf("failed to read worktree status store %s: %w", storePath, err)
	}

	store := &worktreeStatusStore{}
	if len(strings.TrimSpace(string(data))) == 0 {
		store.Worktrees = make(map[string]worktreeWorkflowStatus)
		return store, nil
	}
	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("failed to parse worktree status store %s: %w", storePath, err)
	}
	if store.Worktrees == nil {
		store.Worktrees = make(map[string]worktreeWorkflowStatus)
	}
	return store, nil
}

func saveWorktreeStatusStoreToPath(storePath string, store *worktreeStatusStore) error {
	if store.Worktrees == nil {
		store.Worktrees = make(map[string]worktreeWorkflowStatus)
	}

	data, err := yaml.Marshal(store)
	if err != nil {
		return fmt.Errorf("failed to marshal worktree status store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return fmt.Errorf("failed to create worktree status store directory: %w", err)
	}
	if err := os.WriteFile(storePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write worktree status store %s: %w", storePath, err)
	}
	return nil
}

func deleteWorktreeStatus(worktreePath string) error {
	store, storePath, err := loadWorktreeStatusStoreWithPath()
	if err != nil {
		return err
	}
	key := normalizeWorktreePath(worktreePath)
	if _, ok := store.Worktrees[key]; !ok {
		return nil
	}
	delete(store.Worktrees, key)
	return saveWorktreeStatusStoreToPath(storePath, store)
}

func pruneStaleWorktreeStatuses() error {
	entries, err := gitWorktreeEntries()
	if err != nil {
		return err
	}

	store, storePath, err := loadWorktreeStatusStoreWithPath()
	if err != nil {
		return err
	}
	if len(store.Worktrees) == 0 {
		return nil
	}

	live := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		live[normalizeWorktreePath(entry.Path)] = struct{}{}
	}

	changed := false
	for path := range store.Worktrees {
		if _, ok := live[path]; !ok {
			delete(store.Worktrees, path)
			changed = true
		}
	}
	if !changed {
		return nil
	}

	return saveWorktreeStatusStoreToPath(storePath, store)
}

func currentWorktreePath() (string, error) {
	path, err := gitRepoRoot()
	if err != nil {
		return "", err
	}
	return normalizeWorktreePath(path), nil
}

func worktreeStatusStorePath() (string, error) {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = repoRoot
	if out, err := cmd.Output(); err == nil {
		commonDir := strings.TrimSpace(string(out))
		if commonDir != "" {
			return filepath.Join(commonDir, "mono-worktree-statuses.yaml"), nil
		}
	}

	fallbackCmd := exec.Command("git", "rev-parse", "--git-common-dir")
	fallbackCmd.Dir = repoRoot
	out, err := fallbackCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to locate git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return "", errors.New("failed to locate git common dir: empty path")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Clean(filepath.Join(repoRoot, commonDir))
	}
	return filepath.Join(commonDir, "mono-worktree-statuses.yaml"), nil
}

func normalizeWorktreePath(path string) string {
	cleaned := filepath.Clean(path)
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return cleaned
	}
	return absolute
}

func isBranchMergedIntoBase(branch, base string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", branch, base)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s failed: %w", branch, base, err)
	}
	return true, nil
}

func runBootstrap(worktreePath string) error {
	p := output.DefaultPrinter()

	bootstrapScript := filepath.Join(worktreePath, "scripts", "bootstrap")
	if info, err := os.Stat(bootstrapScript); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		p.StepStart("worktree", "Running bootstrap script: "+bootstrapScript)
		cmd := exec.Command(bootstrapScript)
		cmd.Dir = worktreePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			return fmt.Errorf("bootstrap script failed: %w", runErr)
		}
		p.StepOK("worktree", "Bootstrap completed")
		return nil
	}

	makefilePath := filepath.Join(worktreePath, "Makefile")
	content, err := os.ReadFile(makefilePath)
	if err == nil && hasDoctorTarget(string(content)) {
		p.StepStart("worktree", "Running bootstrap command: make doctor")
		cmd := exec.Command("make", "doctor")
		cmd.Dir = worktreePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			return fmt.Errorf("bootstrap command failed (make doctor): %w", runErr)
		}
		p.StepOK("worktree", "Bootstrap completed")
		return nil
	}

	p.StepWarn("worktree", "No bootstrap command detected (checked scripts/bootstrap, Makefile doctor target)")
	return nil
}

func hasDoctorTarget(makefileContent string) bool {
	lines := strings.Split(makefileContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "doctor:") {
			return true
		}
	}
	return false
}

func copyProjectEnvFiles(sourceRoot, destRoot string) (int, error) {
	relPaths, err := findProjectEnvFiles(sourceRoot)
	if err != nil {
		return 0, err
	}
	if len(relPaths) == 0 {
		return 0, nil
	}

	copied := 0
	for _, relPath := range relPaths {
		srcPath := filepath.Join(sourceRoot, relPath)
		dstPath := filepath.Join(destRoot, relPath)

		if _, statErr := os.Stat(dstPath); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return copied, fmt.Errorf("failed to inspect destination %s: %w", dstPath, statErr)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return copied, fmt.Errorf("failed to create destination directory for %s: %w", dstPath, err)
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			return copied, fmt.Errorf("failed to stat source env file %s: %w", srcPath, err)
		}

		data, err := os.ReadFile(srcPath)
		if err != nil {
			return copied, fmt.Errorf("failed to read source env file %s: %w", srcPath, err)
		}

		if err := os.WriteFile(dstPath, data, info.Mode().Perm()); err != nil {
			return copied, fmt.Errorf("failed to write destination env file %s: %w", dstPath, err)
		}
		copied++
	}

	return copied, nil
}

func findProjectEnvFiles(repoRoot string) ([]string, error) {
	roots := []string{
		filepath.Join(repoRoot, "apps"),
		filepath.Join(repoRoot, "packages"),
	}

	paths := make([]string, 0)
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to inspect %s: %w", root, err)
		}

		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != ".env" {
				return nil
			}
			relPath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			paths = append(paths, relPath)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("failed to search for .env files under %s: %w", root, err)
		}
	}

	sort.Strings(paths)
	return paths, nil
}

func runWorktreeRequirements(worktreePath string) error {
	p := output.DefaultPrinter()

	configPath := filepath.Join(worktreePath, "services.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			p.StepWarn("worktree", "services.yaml not found; skipping worktree requirements")
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	for _, svc := range config.Services {
		cmdString, exists := svc.Commands["reqs"]
		if !exists {
			p.StepWarn(svc.Name, "no 'reqs' command defined")
			continue
		}

		p.StepStart(svc.Name, "reqs")
		servicePath := filepath.Join(worktreePath, svc.Path)

		parts := strings.Fields(cmdString)
		cmd, err := commandFromParts(context.Background(), parts)
		if err != nil {
			return fmt.Errorf("[%s] invalid reqs command: %w", svc.Name, err)
		}
		cmd.Dir = servicePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("[%s] reqs failed: %w", svc.Name, err)
		}
		p.StepOK(svc.Name, "reqs completed")
		p.Blank()
	}

	return nil
}

func printWorktreeUsage() {
	p := output.DefaultPrinter()
	p.Summary("mono worktree - Manage git worktrees for this repository")
	p.Blank()
	p.Summary("Usage:")
	p.Summary("  mono worktree create <branch> [--from <ref>] [--id <unique-id>] [--no-bootstrap] [--skip-sync]")
	p.Summary("  mono worktree list [--state <active|needs-input|done>]")
	p.Summary("  mono worktree path <branch-or-id>")
	p.Summary("  mono worktree tag <IN_PROGRESS|DONE|NEEDS_INPUT>")
	p.Summary("  mono worktree remove <branch-or-id> [--force]")
	p.Summary("  mono worktree prune")
	p.Blank()
	p.Summary("Convention:")
	p.Summary("  Worktrees are created at ~/.worktrees/<repo>/<unique-id>/")
}
