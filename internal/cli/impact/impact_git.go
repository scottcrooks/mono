package impact

import (
	"fmt"
	"os/exec"
	"strings"
)

var gitOutput = gitOutputCmd

const localDiffBaseRef = "local"

func gitOutputCmd(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func defaultMergeBaseBranch() (string, error) {
	if out, err := gitOutput("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		branch := strings.TrimSpace(out)
		if branch != "" {
			return branch, nil
		}
	}

	for _, candidate := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := gitOutput("rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not determine default base branch (tried origin/HEAD, main, master)")
}
