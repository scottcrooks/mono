package cli

import (
	"os"
	"path/filepath"
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
