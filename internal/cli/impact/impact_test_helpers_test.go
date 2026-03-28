package impact

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir %s: %v", dir, err)
	}
}

func writeServicesConfig(t *testing.T, repo string) {
	t.Helper()
	content := `services:
  - name: lib
    path: libs/lib
    description: Shared library
    kind: package
    archetype: go
  - name: api
    path: apps/api
    description: API service
    kind: service
    archetype: go
    depends: [lib]
  - name: web
    path: apps/web
    description: Web service
    kind: service
    archetype: go
    depends: [api]
`
	writeFile(t, repo, "services.yaml", content)
}

func initImpactRepoWithFeatureChange(t *testing.T) string {
	t.Helper()

	repo := initTestGitRepo(t)
	writeServicesConfig(t, repo)
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n")
	writeFile(t, repo, filepath.Join("apps", "api", "api.go"), "package api\n")
	writeFile(t, repo, filepath.Join("apps", "web", "web.go"), "package web\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial")

	gitRun(t, repo, "checkout", "-b", "feature/impact")
	writeFile(t, repo, filepath.Join("libs", "lib", "lib.go"), "package lib\n\n// changed\n")
	gitRun(t, repo, "add", "libs/lib/lib.go")
	gitRun(t, repo, "commit", "-m", "change lib")
	return repo
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test User")
	return repo
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func gitOutputString(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v output failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func detachHeadWithoutBaseRefs(t *testing.T, repo string) {
	t.Helper()

	head := gitOutputString(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", "--detach", head)
	gitRun(t, repo, "branch", "-D", "feature/impact")
	gitRun(t, repo, "branch", "-D", "main")
}

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
