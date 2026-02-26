package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMapFilesToChangedServices(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "app-a", Path: "apps/a"},
			{Name: "app-ab", Path: "apps/ab"},
			{Name: "shared", Path: "libs/shared"},
		},
	}
	files := []string{
		"README.md",
		"apps/a/main.go",
		"apps/ab/main.go",
		"libs/shared/util.go",
	}

	got := mapFilesToChangedServices(cfg, files)
	want := []string{"app-a", "app-ab", "shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapFilesToChangedServices mismatch: got %v, want %v", got, want)
	}
}

func TestComputeImpactedClosure(t *testing.T) {
	t.Parallel()

	reverse := map[string][]string{
		"lib": []string{"api"},
		"api": []string{"web"},
		"web": nil,
	}

	got := computeImpactedClosure([]string{"lib"}, reverse)
	want := []string{"api", "lib", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("computeImpactedClosure mismatch: got %v, want %v", got, want)
	}
}

func TestComputeExplainChains(t *testing.T) {
	t.Parallel()

	reverse := map[string][]string{
		"lib":  []string{"api"},
		"util": []string{"api"},
		"api":  []string{"web"},
		"web":  nil,
	}

	got := computeExplainChains([]string{"lib", "util"}, reverse)
	if !reflect.DeepEqual(got["api"], []string{"lib -> api", "util -> api"}) {
		t.Fatalf("unexpected api chains: %v", got["api"])
	}
	if !reflect.DeepEqual(got["web"], []string{"lib -> api -> web", "util -> api -> web"}) {
		t.Fatalf("unexpected web chains: %v", got["web"])
	}
}

func TestChangedFilesUsesMergeBaseDiff(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	got, err := changedFiles("main")
	if err != nil {
		t.Fatalf("changedFiles returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "libs/lib/lib.go" {
		t.Fatalf("unexpected changed files: %v", got)
	}
}

func TestChangedFilesIncludesWorkingTreeTrackedDiffs(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "api", "api.go"), "package api\n\n// local change\n")

	got, err := changedFiles("main")
	if err != nil {
		t.Fatalf("changedFiles returned error: %v", err)
	}
	want := []string{"apps/api/api.go", "libs/lib/lib.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected changed files: got %v, want %v", got, want)
	}
}

func TestChangedFilesIncludesUntrackedFiles(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	writeFile(t, repo, filepath.Join("apps", "web", "new_local_file.txt"), "new file\n")

	got, err := changedFiles("main")
	if err != nil {
		t.Fatalf("changedFiles returned error: %v", err)
	}
	want := []string{"apps/web/new_local_file.txt", "libs/lib/lib.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected changed files: got %v, want %v", got, want)
	}
}

func TestBuildImpactReport(t *testing.T) {
	repo := initImpactRepoWithFeatureChange(t)
	withWorkingDir(t, repo)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	report, err := buildImpactReport(cfg, "main", true)
	if err != nil {
		t.Fatalf("buildImpactReport returned error: %v", err)
	}

	if !reflect.DeepEqual(report.Changed, []string{"lib"}) {
		t.Fatalf("unexpected changed services: %v", report.Changed)
	}
	if !reflect.DeepEqual(report.Impacted, []string{"api", "lib", "web"}) {
		t.Fatalf("unexpected impacted services: %v", report.Impacted)
	}
	if chains := report.Explain["web"]; !reflect.DeepEqual(chains, []string{"lib -> api -> web"}) {
		t.Fatalf("unexpected explain chains for web: %v", chains)
	}
}

func TestBuildCheckTaskPreview(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "lib", Kind: "package", Archetype: "go"},
			{Name: "api", Kind: "service", Archetype: "go"},
		},
	}

	rows := buildCheckTaskPreview(cfg, []string{"lib", "api"})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0].Service != "api" || strings.Join(rows[0].Present, ",") != "lint,typecheck,test" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].Service != "lib" || strings.Join(rows[1].Missing, ",") != "typecheck" {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

func TestBuildPendingCheckPlan(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "lib", Kind: "package", Archetype: "go"},
			{Name: "api", Kind: "service", Archetype: "go"},
		},
	}

	plan := buildPendingCheckPlan(cfg, []string{"lib", "api"})
	if !reflect.DeepEqual(plan.ImpactedServices, []string{"lib", "api"}) {
		t.Fatalf("unexpected impacted services: %v", plan.ImpactedServices)
	}

	if len(plan.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Task != TaskLint || !reflect.DeepEqual(plan.Phases[0].Services, []string{"api", "lib"}) {
		t.Fatalf("unexpected lint phase: %+v", plan.Phases[0])
	}
	if plan.Phases[1].Task != TaskTypecheck || !reflect.DeepEqual(plan.Phases[1].Services, []string{"api"}) {
		t.Fatalf("unexpected typecheck phase: %+v", plan.Phases[1])
	}
	if plan.Phases[2].Task != TaskTest || !reflect.DeepEqual(plan.Phases[2].Services, []string{"api", "lib"}) {
		t.Fatalf("unexpected test phase: %+v", plan.Phases[2])
	}
}
