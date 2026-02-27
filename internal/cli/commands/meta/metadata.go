package meta

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type metadataCommand struct{}

func init() {
	registerCommand("metadata", &metadataCommand{})
}

var (
	metadataNow      = time.Now
	metadataLookPath = exec.LookPath
	metadataRunGit   = runMetadataGit
)

type metadataInfo struct {
	DateTimeTZ string
	FilenameTS string
	RepoName   string
	GitBranch  string
	GitCommit  string
}

func (c *metadataCommand) Run(_ []string) error {
	info := collectMetadata()
	p := output.DefaultPrinter()
	for _, line := range strings.Split(strings.TrimSuffix(formatMetadataOutput(info), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p.Summary(line)
	}
	return nil
}

func collectMetadata() metadataInfo {
	now := metadataNow()
	info := metadataInfo{
		DateTimeTZ: now.Format("2006-01-02 15:04:05 MST"),
		FilenameTS: now.Format("2006-01-02_15-04-05"),
	}

	if _, err := metadataLookPath("git"); err != nil {
		return info
	}

	inGitWorktree, err := metadataRunGit("rev-parse", "--is-inside-work-tree")
	if err != nil || inGitWorktree != "true" {
		return info
	}

	if repoRoot, err := metadataRunGit("rev-parse", "--show-toplevel"); err == nil {
		info.RepoName = filepath.Base(repoRoot)
	}

	if branch, err := metadataRunGit("branch", "--show-current"); err == nil && branch != "" {
		info.GitBranch = branch
	} else if branch, err := metadataRunGit("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.GitBranch = branch
	}

	if commit, err := metadataRunGit("rev-parse", "HEAD"); err == nil {
		info.GitCommit = commit
	}

	return info
}

func runMetadataGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // G204: binary is static and args are fixed in code paths above.
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func formatMetadataOutput(info metadataInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Current Date/Time (TZ): %s\n", info.DateTimeTZ))
	if info.GitCommit != "" {
		b.WriteString(fmt.Sprintf("Current Git Commit Hash: %s\n", info.GitCommit))
	}
	if info.GitBranch != "" {
		b.WriteString(fmt.Sprintf("Current Branch Name: %s\n", info.GitBranch))
	}
	if info.RepoName != "" {
		b.WriteString(fmt.Sprintf("Repository Name: %s\n", info.RepoName))
	}
	b.WriteString(fmt.Sprintf("Timestamp For Filename: %s\n", info.FilenameTS))
	return b.String()
}
