package main

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

	"gopkg.in/yaml.v3"
)

type worktreeCommand struct{}

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
		return runWorktreeList()
	case "path":
		return runWorktreePath(args[3:])
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
	branch, fromRef, uniqueID, noBootstrap, err := parseCreateArgs(args)
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

	addCmd := exec.Command("git", "worktree", "add", "-b", branch, dest, fromRef)
	addCmd.Stdout = os.Stdout
	addCmd.Stderr = os.Stderr
	if runErr := addCmd.Run(); runErr != nil {
		return fmt.Errorf("git worktree add failed: %w", runErr)
	}

	fmt.Printf("[ok] Created worktree for branch %q at %s\n", branch, dest)

	if noBootstrap {
		fmt.Println("[skip] Bootstrap skipped (--no-bootstrap)")
		return nil
	}

	copied, err := copyProjectEnvFiles(repoRoot, dest)
	if err != nil {
		return err
	}
	if copied > 0 {
		fmt.Printf("[ok] Copied %d .env file(s) into worktree\n", copied)
	} else {
		fmt.Println("[skip] No .env files found to copy from apps/ or packages/")
	}

	if err := runBootstrap(dest); err != nil {
		return err
	}

	if err := runWorktreeRequirements(dest); err != nil {
		return err
	}

	return nil
}

func runWorktreeList() error {
	entries, err := gitWorktreeEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No git worktrees found")
		return nil
	}

	mergeBase, mergeBaseErr := defaultMergeBaseBranch()
	mergeBaseLabel := mergeBase
	if mergeBaseLabel == "" {
		mergeBaseLabel = "default"
	}

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

		fmt.Printf("%s\n", entry.Path)
		fmt.Printf("  branch: %s\n", branch)
		fmt.Printf("  head:   %s\n", shortSHA(entry.Head))
		fmt.Printf("  status: %s\n", dirtyState)
		fmt.Printf("  merged(%s): %s\n", mergeBaseLabel, mergedState)
		fmt.Println()
	}

	return nil
}

func runWorktreePath(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: mono worktree path <branch-or-id>")
	}

	entry, err := resolveWorktree(args[0])
	if err != nil {
		return err
	}

	fmt.Println(entry.Path)
	return nil
}

func runWorktreeRemove(args []string) error {
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

	fmt.Printf("[ok] Removed worktree %s\n", entry.Path)
	return nil
}

func runWorktreePrune() error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("git worktree prune failed: %w", runErr)
	}
	fmt.Println("[ok] Pruned stale worktree metadata")
	return nil
}

func parseCreateArgs(args []string) (branch, fromRef, uniqueID string, noBootstrap bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, fmt.Errorf("usage: mono worktree create <branch> [--from <ref>] [--id <unique-id>] [--no-bootstrap]")
	}

	fromRef = "HEAD"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-bootstrap":
			noBootstrap = true
		case arg == "--from":
			if i+1 >= len(args) {
				return "", "", "", false, fmt.Errorf("--from requires a value")
			}
			fromRef = args[i+1]
			i++
		case strings.HasPrefix(arg, "--from="):
			fromRef = strings.TrimPrefix(arg, "--from=")
			if fromRef == "" {
				return "", "", "", false, fmt.Errorf("--from requires a value")
			}
		case arg == "--id":
			if i+1 >= len(args) {
				return "", "", "", false, fmt.Errorf("--id requires a value")
			}
			uniqueID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--id="):
			uniqueID = strings.TrimPrefix(arg, "--id=")
			if uniqueID == "" {
				return "", "", "", false, fmt.Errorf("--id requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return "", "", "", false, fmt.Errorf("unknown flag %q", arg)
		default:
			if branch != "" {
				return "", "", "", false, fmt.Errorf("unexpected extra argument %q", arg)
			}
			branch = arg
		}
	}

	if branch == "" {
		return "", "", "", false, fmt.Errorf("branch is required")
	}
	if uniqueID != "" && sanitizeSlug(uniqueID) != uniqueID {
		return "", "", "", false, fmt.Errorf("unique id %q must already be slug-safe (lowercase letters, numbers, and hyphens)", uniqueID)
	}

	return branch, fromRef, uniqueID, noBootstrap, nil
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
	bootstrapScript := filepath.Join(worktreePath, "scripts", "bootstrap")
	if info, err := os.Stat(bootstrapScript); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		fmt.Printf("==> [worktree] Running bootstrap script: %s\n", bootstrapScript)
		cmd := exec.Command(bootstrapScript)
		cmd.Dir = worktreePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			return fmt.Errorf("bootstrap script failed: %w", runErr)
		}
		fmt.Println("[ok] Bootstrap completed")
		return nil
	}

	makefilePath := filepath.Join(worktreePath, "Makefile")
	content, err := os.ReadFile(makefilePath)
	if err == nil && hasDoctorTarget(string(content)) {
		fmt.Println("==> [worktree] Running bootstrap command: make doctor")
		cmd := exec.Command("make", "doctor")
		cmd.Dir = worktreePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			return fmt.Errorf("bootstrap command failed (make doctor): %w", runErr)
		}
		fmt.Println("[ok] Bootstrap completed")
		return nil
	}

	fmt.Println("[skip] No bootstrap command detected (checked scripts/bootstrap, Makefile doctor target)")
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
	configPath := filepath.Join(worktreePath, "services.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	for _, svc := range config.Services {
		cmdString, exists := svc.Commands["reqs"]
		if !exists {
			fmt.Printf("[skip] [%s] no 'reqs' command defined\n", svc.Name)
			continue
		}

		fmt.Printf("==> [%s] reqs\n", svc.Name)
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
		fmt.Printf("[ok] [%s] reqs completed\n\n", svc.Name)
	}

	return nil
}

func printWorktreeUsage() {
	fmt.Println("mono worktree - Manage git worktrees for this repository")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono worktree create <branch> [--from <ref>] [--id <unique-id>] [--no-bootstrap]")
	fmt.Println("  mono worktree list")
	fmt.Println("  mono worktree path <branch-or-id>")
	fmt.Println("  mono worktree remove <branch-or-id> [--force]")
	fmt.Println("  mono worktree prune")
	fmt.Println()
	fmt.Println("Convention:")
	fmt.Println("  Worktrees are created at ~/.worktrees/<repo>/<unique-id>/")
}
