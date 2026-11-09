package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/core"
	"github.com/scottcrooks/mono/internal/cli/output"
)

func TestBuildTaskGraphDetectsCycle(t *testing.T) {
	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go", Depends: []string{"b"}},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}

	_, err := buildTaskGraph(cfg, resolved)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestFocusedSelectionListsQualifiedTopLevelTestsWithoutRunningThem(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	service := filepath.Join(repo, "svc")
	mustWrite(t, filepath.Join(service, "go.mod"), "module example.test/svc\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(service, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(service, "a", "a_test.go"), "package a\nimport (\"os\";\"testing\")\nfunc TestShared(t *testing.T){_ = os.WriteFile(\"ran.marker\", []byte(\"yes\"), 0600)}\nfunc TestAlpha(t *testing.T){}\nfunc TestShared_Subtest(t *testing.T){}\n")
	mustWrite(t, filepath.Join(service, "b", "b.go"), "package b\n")
	mustWrite(t, filepath.Join(service, "b", "b_test.go"), "package b\nimport \"testing\"\nfunc TestShared(t *testing.T){}\nfunc TestBeta(t *testing.T){}\n")
	cfg := &Config{Services: []Service{{Name: "svc", Path: "svc", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = resolveFocusedGoSelection(prepared.graph, TaskRunOptions{Integration: true, List: true})
	_ = w.Close()
	os.Stdout = old
	_, _ = output.ReadFrom(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"example.test/svc/a TestAlpha", "example.test/svc/a TestShared", "example.test/svc/b TestBeta", "example.test/svc/b TestShared"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q in %s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "TestAlpha/dynamic") {
		t.Fatalf("dynamic subtest should not be listed: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(service, "a", "ran.marker")); !os.IsNotExist(err) {
		t.Fatalf("list-only discovery ran a test: %v", err)
	}
}

func TestFocusedSelectionRejectsEmptyAndInvalidPackagesBeforeTests(t *testing.T) {
	for _, pattern := range []string{"./missing/...", "[", "./..."} {
		t.Run(pattern, func(t *testing.T) {
			repo := t.TempDir()
			withWorkingDir(t, repo)
			service := filepath.Join(repo, "svc")
			mustWrite(t, filepath.Join(service, "go.mod"), "module example.test/svc\n\ngo 1.24\n")
			mustWrite(t, filepath.Join(service, "svc.go"), "package svc\n")
			mustWrite(t, filepath.Join(service, "svc_test.go"), "package svc\nimport (\"os\";\"testing\")\nfunc TestMarker(t *testing.T){_ = os.WriteFile(\"marker\",[]byte(\"ran\"),0600)}\n")
			if pattern == "./..." {
				mustWrite(t, filepath.Join(service, "broken_test.go"), "package svc\nfunc TestBroken( {\n")
			}
			cfg := &Config{Services: []Service{{Name: "svc", Path: "svc", Kind: "service", Archetype: "go"}}}
			prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
			if err != nil {
				t.Fatal(err)
			}
			err = resolveFocusedGoSelection(prepared.graph, TaskRunOptions{Integration: true, Packages: []string{pattern}})
			if err == nil {
				t.Fatal("expected selection error")
			}
			if _, statErr := os.Stat(filepath.Join(service, "marker")); !os.IsNotExist(statErr) {
				t.Fatalf("selection failure ran tests: %v", statErr)
			}
		})
	}
}

func TestFocusedDiscoveryHonorsCommandContextCancellation(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	dir := filepath.Join(repo, "svc")
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test/svc\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(dir, "svc_test.go"), "package svc\nimport (\"os\";\"testing\")\nfunc TestMarker(t *testing.T){_ = os.WriteFile(\"marker\",[]byte(\"ran\"),0600)}\n")
	prepared, err := PrepareTaskRun(&Config{Services: []Service{{Name: "svc", Path: "svc", Kind: "service", Archetype: "go"}}}, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = resolveFocusedGoSelection(prepared.graph, TaskRunOptions{Integration: true, Packages: []string{"./..."}, Context: ctx})
	if err == nil {
		t.Fatal("expected discovery cancellation")
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
		t.Fatalf("test ran after discovery cancellation: %v", err)
	}
}

func TestFocusedSelectionAppliesToDependencyTestNodes(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	for name, testName := range map[string]string{"dep": "TestDifferent", "app": "TestSelected"} {
		dir := filepath.Join(repo, name)
		module := "example.test/" + name
		mustWrite(t, filepath.Join(dir, "go.mod"), "module "+module+"\n\ngo 1.24\n")
		pkgDir, pkgName := dir, name
		if name == "app" {
			pkgDir = filepath.Join(dir, "specific")
			pkgName = "specific"
		}
		mustWrite(t, filepath.Join(pkgDir, "test_test.go"), "package "+pkgName+"\nimport \"testing\"\nfunc "+testName+"(t *testing.T){}\n")
	}
	cfg := &Config{Services: []Service{{Name: "dep", Path: "dep", Kind: "package", Archetype: "go"}, {Name: "app", Path: "app", Kind: "service", Archetype: "go", Depends: []string{"dep"}}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"app"}, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	opts := TaskRunOptions{Integration: true, Packages: []string{"./specific"}, Run: "^TestSelected$", Concurrency: 1}
	err = resolveFocusedGoSelection(prepared.graph, opts)
	if err != nil {
		t.Fatalf("dependency with no match should be skipped: %v", err)
	}
	if got := prepared.graph.nodes[TaskNode{Service: "dep", Task: TaskTest}].SkipReason; !strings.HasPrefix(got, "no matching focused") {
		t.Fatalf("dependency skip reason=%q", got)
	}
	if got := prepared.graph.nodes[TaskNode{Service: "app", Task: TaskTest}].SkipReason; got != "" {
		t.Fatalf("requested service was skipped: %q", got)
	}
	results, err := RunPreparedTaskRun(prepared, opts)
	if err != nil {
		t.Fatalf("run focused service: %v", err)
	}
	statuses := map[string]TaskRunStatus{}
	for _, result := range results {
		statuses[result.Node.Service] = result.Status
	}
	if statuses["dep"] != TaskStatusSkipped || statuses["app"] != TaskStatusSucceeded {
		t.Fatalf("unexpected dependency/app statuses: %v", statuses)
	}
}

func TestFocusedSelectionRejectsWhenEveryNodeIsSkipped(t *testing.T) {
	node := TaskNode{Service: "svc", Task: TaskTest}
	graph := &taskGraph{nodes: map[TaskNode]ResolvedTaskNode{node: {Node: node, Service: Service{Name: "svc", Archetype: "go"}, SkipReason: "unsupported"}}}
	err := resolveFocusedGoSelection(graph, TaskRunOptions{Integration: true, Packages: []string{"./..."}})
	if err == nil || !strings.Contains(err.Error(), "no runnable Go nodes") {
		t.Fatalf("expected no-runnable-node error, got %v", err)
	}
}

func TestFocusedSelectionExecutesOnlyResolvedPackageAndRun(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	service := filepath.Join(repo, "svc")
	mustWrite(t, filepath.Join(service, "go.mod"), "module example.test/svc\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(service, "a", "a_test.go"), "package a\nimport (\"os\";\"testing\")\nfunc TestChosen(t *testing.T){}\nfunc TestOther(t *testing.T){_ = os.WriteFile(\"unexpected.marker\",[]byte(\"ran\"),0600)}\n")
	mustWrite(t, filepath.Join(service, "b", "b_test.go"), "package b\nimport (\"os\";\"testing\")\nfunc TestChosen(t *testing.T){_ = os.WriteFile(\"wrong-package.marker\",[]byte(\"ran\"),0600)}\n")
	cfg := &Config{Services: []Service{{Name: "svc", Path: "svc", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	focusOpts := TaskRunOptions{Integration: true, Packages: []string{"./a"}, Run: "^TestChosen$", GoConcurrency: 2}
	if err := resolveFocusedGoSelection(prepared.graph, focusOpts); err != nil {
		t.Fatal(err)
	}
	focusOpts.Concurrency = 1
	if _, err := RunPreparedTaskRun(prepared, focusOpts); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unexpected.marker", "wrong-package.marker"} {
		if _, err := os.Stat(filepath.Join(service, name)); !os.IsNotExist(err) {
			t.Fatalf("unselected test ran (%s): %v", name, err)
		}
	}
}

func TestIntegrationFocusPolicyPrecedesServiceResolution(t *testing.T) {
	cfg := &Config{Integration: &core.IntegrationSpec{RequireFocus: true}}
	req := TaskRequest{Task: TaskTest, Services: []string{"unknown"}, ExactServices: true, Integration: true}
	_, err := RunOrchestratedTaskRequestWithConfig(cfg, req, TaskRunOptions{Integration: true})
	if err == nil || !strings.Contains(err.Error(), "requires --package, --run, or explicit --full-suite") {
		t.Fatalf("expected policy rejection before service resolution, got %v", err)
	}
	for _, opts := range []TaskRunOptions{{Integration: true, Packages: []string{"./..."}}, {Integration: true, FullSuite: true}} {
		_, err = RunOrchestratedTaskRequestWithConfig(cfg, req, opts)
		if err == nil || strings.Contains(err.Error(), "integration policy") {
			t.Fatalf("explicit intent should pass policy, got %v", err)
		}
	}
	legacy := &Config{}
	_, err = RunOrchestratedTaskRequestWithConfig(legacy, req, TaskRunOptions{Integration: true})
	if err == nil || strings.Contains(err.Error(), "integration policy") {
		t.Fatalf("legacy config should bypass policy, got %v", err)
	}
}

func TestTaskExecutorHonorsDependencyOrdering(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "lib", "go.mod"), "module lib\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n")
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
		{Node: TaskNode{Service: "lib", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	byNode := map[string]TaskRunStatus{}
	for _, r := range results {
		byNode[r.Node.String()] = r.Status
	}
	if byNode["lib:build"] != TaskStatusSucceeded {
		t.Fatalf("expected lib success, got %s", byNode["lib:build"])
	}
	if byNode["api:build"] != TaskStatusSucceeded {
		t.Fatalf("expected api success, got %s", byNode["api:build"])
	}
}

func TestIntegrationTestReceivesTestEnvCascade(t *testing.T) {
	priorCI, hadCI := os.LookupEnv("CI")
	_ = os.Unsetenv("CI")
	t.Cleanup(func() {
		if hadCI {
			_ = os.Setenv("CI", priorCI)
		} else {
			_ = os.Unsetenv("CI")
		}
	})
	repo := t.TempDir()
	withWorkingDir(t, repo)
	serviceDir := filepath.Join(repo, "api")
	mustWrite(t, filepath.Join(serviceDir, "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "test.env"), "CASCADE=root\nROOT_ONLY=shared\n")
	mustWrite(t, filepath.Join(serviceDir, "test.env"), "CASCADE=service\nSERVICE_ONLY=yes\nCI=false\n")
	writeExecutableFile(t, repo, filepath.Join("api", "observe.js"), "const fs=require('fs');fs.writeFileSync('observed.json',JSON.stringify({cascade:process.env.CASCADE,root:process.env.ROOT_ONLY,service:process.env.SERVICE_ONLY,ci:process.env.CI,dev:process.env.DEV_ONLY||null}));\n")
	writeExecutableFile(t, repo, filepath.Join("api", "preflight.js"), "const fs=require('fs');fs.writeFileSync('preflight.json',JSON.stringify({cascade:process.env.CASCADE,root:process.env.ROOT_ONLY,service:process.env.SERVICE_ONLY,ci:process.env.CI,dev:process.env.DEV_ONLY||null}));\n")
	t.Setenv("CI_OVERRIDE", "")
	t.Setenv("DEV_ONLY", "inherited-only")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go", IntegrationPreflight: "node preflight.js"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node observe.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
	if err != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
		t.Fatalf("integration task failed: %+v, %v", results, err)
	}
	observed, err := os.ReadFile(filepath.Join(serviceDir, "observed.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cascade":"service","root":"shared","service":"yes","ci":"false","dev":"inherited-only"}`
	if string(observed) != want {
		t.Fatalf("child environment = %s, want %s", observed, want)
	}
	preflightObserved, err := os.ReadFile(filepath.Join(serviceDir, "preflight.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(preflightObserved) != want {
		t.Fatalf("preflight environment=%s, want same test snapshot %s", preflightObserved, want)
	}
	if got := os.Getenv("CASCADE"); got != "" {
		t.Fatalf("mono process environment was mutated: CASCADE=%q", got)
	}
}

func TestIntegrationPreflightRunsOnCacheHitWithSameEnvironment(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	dir := filepath.Join(repo, "svc")
	mustWrite(t, filepath.Join(dir, "test.env"), "PREFLIGHT_VALUE=service-value\n")
	writeExecutableFile(t, repo, filepath.Join("svc", "preflight.js"), "const fs=require('fs');fs.writeFileSync('preflight.marker',process.env.PREFLIGHT_VALUE||'missing')\n")
	writeExecutableFile(t, repo, filepath.Join("svc", "test.js"), "require('fs').writeFileSync('test.marker','ran')\n")
	svc := Service{Name: "svc", Path: "svc", Kind: "service", Archetype: "go", IntegrationPreflight: "node preflight.js"}
	cfg := &Config{Services: []Service{svc}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	env, err := core.BuildEnvironment(repo, dir, "test.env", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	env = core.WithDefaultCI(env)
	key, err := buildIntegrationTaskCacheKey(svc, TaskTest, "node test.js\nintegration-preflight="+svc.IntegrationPreflight, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := newTaskCache().store(taskCacheEntry{Key: key, Service: "svc", Task: TaskTest}); err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "svc", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node test.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
	if err != nil || len(results) != 1 || !results[0].Cached {
		t.Fatalf("expected cache hit after preflight: results=%+v err=%v", results, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "preflight.marker"))
	if err != nil || string(got) != "service-value" {
		t.Fatalf("preflight env=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.marker")); !os.IsNotExist(err) {
		t.Fatalf("cached test unexpectedly ran: %v", err)
	}
}

func TestIntegrationPreflightFailurePreventsCachedTest(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	dir := filepath.Join(repo, "svc")
	writeExecutableFile(t, repo, filepath.Join("svc", "preflight.js"), "process.exit(7)\n")
	writeExecutableFile(t, repo, filepath.Join("svc", "test.js"), "require('fs').writeFileSync('test.marker','ran')\n")
	svc := Service{Name: "svc", Path: "svc", Kind: "service", Archetype: "go", IntegrationPreflight: "node preflight.js"}
	cfg := &Config{Services: []Service{svc}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	env, err := core.BuildEnvironment(repo, dir, "test.env", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	env = core.WithDefaultCI(env)
	key, err := buildIntegrationTaskCacheKey(svc, TaskTest, "node test.js\nintegration-preflight="+svc.IntegrationPreflight, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := newTaskCache().store(taskCacheEntry{Key: key, Service: "svc", Task: TaskTest}); err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "svc", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node test.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
	if err == nil || len(results) != 1 || results[0].Status != TaskStatusFailed {
		t.Fatalf("expected preflight failure: results=%+v err=%v", results, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.marker")); !os.IsNotExist(err) {
		t.Fatalf("test ran after preflight failure: %v", err)
	}
}

func TestIntegrationPreflightHonorsCommandContextCancellation(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	dir := filepath.Join(repo, "svc")
	writeExecutableFile(t, repo, filepath.Join("svc", "preflight.js"), "require('fs').writeFileSync('preflight.marker','ran')\n")
	writeExecutableFile(t, repo, filepath.Join("svc", "test.js"), "require('fs').writeFileSync('test.marker','ran')\n")
	svc := Service{Name: "svc", Path: "svc", Kind: "service", Archetype: "go", IntegrationPreflight: "node preflight.js"}
	prepared, err := PrepareTaskRun(&Config{Services: []Service{svc}}, TaskRequest{Task: TaskTest, Services: []string{"svc"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "svc", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node test.js"
	prepared.graph.nodes[node] = resolved
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = newTaskExecutor().execute(ctx, prepared.graph, TaskRunOptions{Integration: true, Concurrency: 1})
	if err == nil {
		t.Fatal("expected cancelled preflight")
	}
	if _, err := os.Stat(filepath.Join(dir, "test.marker")); !os.IsNotExist(err) {
		t.Fatalf("test ran after cancellation: %v", err)
	}
}

func TestIntegrationTestCommandHonorsCommandContextCancellation(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	dir := filepath.Join(repo, "svc")
	writeExecutableFile(t, repo, filepath.Join("svc", "slow.js"), "setTimeout(()=>require('fs').writeFileSync('test.marker','ran'),1500)\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := runTaskCommand(ctx, output.DefaultPrinter(), Service{Name: "svc", Path: "svc", Archetype: "go"}, TaskNode{Service: "svc", Task: TaskTest}, "node slow.js", nil, nil, TaskRunOptions{Integration: true}, nil)
	if err == nil {
		t.Fatal("expected test command cancellation")
	}
	if _, err := os.Stat(filepath.Join(dir, "test.marker")); !os.IsNotExist(err) {
		t.Fatalf("test continued after cancellation: %v", err)
	}
}

func TestIntegrationEnvironmentErrorPrecedesCacheAndLaunch(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	serviceDir := filepath.Join(repo, "api")
	mustWrite(t, filepath.Join(serviceDir, "go.mod"), "module api\n\ngo 1.24\n")
	secret := "do-not-print-this-value"
	mustWrite(t, filepath.Join(serviceDir, "test.env"), "BAD!KEY="+secret+"\n")
	writeExecutableFile(t, repo, filepath.Join("api", "marker.js"), "require('fs').writeFileSync('launched', 'yes')\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node marker.js"
	prepared.graph.nodes[node] = resolved
	childEnv, err := core.BuildEnvironment(repo, serviceDir, "test.env", os.Environ())
	if err == nil {
		t.Fatal("expected malformed environment file")
	}
	cacheKey, err := buildIntegrationTaskCacheKey(resolved.Service, node.Task, resolved.Command, childEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := newTaskCache().store(taskCacheEntry{Key: cacheKey, Service: "api", Task: TaskTest}); err != nil {
		t.Fatal(err)
	}
	_, runErr := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
	if runErr == nil || strings.Contains(runErr.Error(), secret) {
		t.Fatalf("expected secret-free env construction failure, got %v", runErr)
	}
	if _, err := os.Stat(filepath.Join(serviceDir, "launched")); !os.IsNotExist(err) {
		t.Fatalf("test child launched despite environment error: %v", err)
	}
}

func TestIntegrationExecutorCacheTracksEffectiveEnvironment(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	serviceDir := filepath.Join(repo, "api")
	mustWrite(t, filepath.Join(serviceDir, "go.mod"), "module api\n\ngo 1.24\n")
	countPath := filepath.Join(repo, "runs")
	t.Setenv("INTEGRATION_SWITCH", "first")
	t.Setenv("INTEGRATION_RUNS", countPath)
	writeExecutableFile(t, repo, filepath.Join("api", "count.js"), "const fs=require('fs');fs.appendFileSync(process.env.INTEGRATION_RUNS,process.env.INTEGRATION_SWITCH+'\\n');\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true, Integration: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node count.js"
	prepared.graph.nodes[node] = resolved
	run := func(wantStatus TaskRunStatus) {
		t.Helper()
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
		if err != nil || len(results) != 1 || results[0].Status != wantStatus {
			t.Fatalf("integration run = %+v, %v; want %s", results, err, wantStatus)
		}
	}
	run(TaskStatusSucceeded)
	run(TaskStatusSkipped)
	t.Setenv("INTEGRATION_SWITCH", "second")
	run(TaskStatusSucceeded)
	got, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Fatalf("cache executions = %q, want first and second effective environments", got)
	}
}

func TestRegularTestDoesNotReadTestEnv(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	serviceDir := filepath.Join(repo, "api")
	mustWrite(t, filepath.Join(serviceDir, "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(serviceDir, "test.env"), "BAD!KEY=malformed\n")
	writeExecutableFile(t, repo, filepath.Join("api", "regular.js"), "require('fs').writeFileSync('ran', 'yes')\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskTest}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node regular.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
	if err != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
		t.Fatalf("regular test read integration dotenv: %+v, %v", results, err)
	}
}

func TestTaskExecutorBufferedSuccessStaysQuiet(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-success.js"), "process.stdout.write('success-stdout\\n')\nprocess.stderr.write('success-stderr\\n')\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-success.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestTaskExecutorBufferedCacheMissStaysQuiet(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 1, BufferOutput: true})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestTaskExecutorBufferedFailureReplaysTranscript(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-fail.js"), "process.stdout.write('fail-stdout\\n')\nprocess.stderr.write('fail-stderr\\n')\nprocess.exit(1)\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-fail.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
		if err == nil {
			t.Fatalf("expected execute error")
		}
		if len(results) != 1 || results[0].Status != TaskStatusFailed {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if !strings.Contains(stdout, "[api:build] start") {
		t.Fatalf("expected buffered step start in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "[api:build] fail-stdout") {
		t.Fatalf("expected buffered stdout replay, got %q", stdout)
	}
	if !strings.Contains(stderr, "[api:build] fail-stderr") {
		t.Fatalf("expected buffered stderr replay, got %q", stderr)
	}
	if !strings.Contains(stderr, "[api:build] failed") {
		t.Fatalf("expected failure line in stderr, got %q", stderr)
	}
}

func TestTaskExecutorLivePathStreamsOutput(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")
	writeExecutableFile(t, repo, filepath.Join("api", "emit-live.js"), "process.stdout.write('live-stdout\\n')\nprocess.stderr.write('live-stderr\\n')\n")

	cfg := &Config{Services: []Service{
		{Name: "api", Path: "api", Kind: "service", Archetype: "go"},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[0], Command: "node emit-live.js"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	stdout, stderr := captureStreams(t, func() {
		results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	if !strings.Contains(stdout, "[api:build] live-stdout") {
		t.Fatalf("expected live stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "[api:build] live-stderr") {
		t.Fatalf("expected live stderr, got %q", stderr)
	}
}

func TestTaskExecutorFailureSkipsRemaining(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:build"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:build"])
	}
	if byNode["b:build"].Status != TaskStatusSkipped {
		t.Fatalf("expected b to be skipped, got %+v", byNode["b:build"])
	}
}

func TestTaskExecutorContinueOnFailureRunsDependents(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskBuild}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{NoCache: true, Concurrency: 2, ContinueOnFailure: true})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:build"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:build"])
	}
	if byNode["b:build"].Status != TaskStatusSucceeded {
		t.Fatalf("expected b to run despite failure, got %+v", byNode["b:build"])
	}
}

func TestTaskExecutorDependencyChangeInvalidatesDependentCache(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)

	mustWrite(t, filepath.Join(repo, "lib", "go.mod"), "module lib\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n")
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api.go"), "package api\n")

	cfg := &Config{Services: []Service{
		{Name: "lib", Path: "lib", Kind: "package", Archetype: "go"},
		{Name: "api", Path: "api", Kind: "service", Archetype: "go", Depends: []string{"lib"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "api", Task: TaskBuild}, Service: cfg.Services[1], Command: "go version"},
		{Node: TaskNode{Service: "lib", Task: TaskBuild}, Service: cfg.Services[0], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	first, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	for _, r := range first {
		if r.Status == TaskStatusFailed {
			t.Fatalf("unexpected failure in first run: %+v", r)
		}
	}

	second, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	byNodeSecond := map[string]TaskRunStatus{}
	for _, r := range second {
		byNodeSecond[r.Node.String()] = r.Status
	}
	if byNodeSecond["lib:build"] != TaskStatusSkipped || byNodeSecond["api:build"] != TaskStatusSkipped {
		t.Fatalf("expected both nodes cached on second run, got %+v", byNodeSecond)
	}

	mustWrite(t, filepath.Join(repo, "lib", "lib.go"), "package lib\n// changed\n")
	third, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err != nil {
		t.Fatalf("third execute: %v", err)
	}
	byNodeThird := map[string]TaskRunStatus{}
	for _, r := range third {
		byNodeThird[r.Node.String()] = r.Status
	}
	if byNodeThird["lib:build"] != TaskStatusSucceeded {
		t.Fatalf("expected lib to rerun after lib change, got %+v", byNodeThird)
	}
	if byNodeThird["api:build"] != TaskStatusSucceeded {
		t.Fatalf("expected api to rerun after dependency change, got %+v", byNodeThird)
	}
}

func TestTaskExecutorAuditContinuesOnFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "a", "a.go"), "package a\n")
	mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "b", "b.go"), "package b\n")

	cfg := &Config{Services: []Service{
		{Name: "a", Path: "a", Kind: "service", Archetype: "go"},
		{Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}},
	}}
	resolved := []ResolvedTaskNode{
		{Node: TaskNode{Service: "a", Task: TaskAudit}, Service: cfg.Services[0], Command: "go definitely-not-a-command"},
		{Node: TaskNode{Service: "b", Task: TaskAudit}, Service: cfg.Services[1], Command: "go version"},
	}
	graph, err := buildTaskGraph(cfg, resolved)
	if err != nil {
		t.Fatalf("buildTaskGraph: %v", err)
	}

	exec := newTaskExecutor()
	results, err := exec.execute(context.Background(), graph, TaskRunOptions{Concurrency: 2})
	if err == nil {
		t.Fatalf("expected execution error")
	}

	byNode := map[string]TaskRunResult{}
	for _, r := range results {
		byNode[r.Node.String()] = r
	}
	if byNode["a:audit"].Status != TaskStatusFailed {
		t.Fatalf("expected a to fail, got %+v", byNode["a:audit"])
	}
	if byNode["b:audit"].Status != TaskStatusSucceeded {
		t.Fatalf("expected b to succeed despite dependency failure, got %+v", byNode["b:audit"])
	}
}

func TestPreparedTaskRunPersistsOnlyStartedCommandsAndSeparatesVariants(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	mustWrite(t, filepath.Join(repo, "api", "api_test.go"), "package api\nimport \"testing\"\nfunc TestOK(t *testing.T) {}\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	keyRegular := taskTimingKey{Service: "api", Task: string(TaskTest)}
	keyIntegration := taskTimingKey{Service: "api", Task: string(TaskTest), Integration: true}
	for _, variant := range []bool{false, true} {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskTest, Services: []string{"api"}, ExactServices: true, Integration: variant})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskTest}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
			t.Fatalf("variant %v run: %+v, %v", variant, results, err)
		}
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[keyRegular]) != 1 || len(loaded[keyIntegration]) != 1 {
		t.Fatalf("variant observations missing: %#v", loaded)
	}

	failed, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	buildNode := TaskNode{Service: "api", Task: TaskBuild}
	resolved := failed.graph.nodes[buildNode]
	resolved.Command = "definitely-not-a-real-command"
	failed.graph.nodes[buildNode] = resolved
	_, runErr := RunPreparedTaskRun(failed, TaskRunOptions{NoCache: true, Concurrency: 1})
	if runErr == nil {
		t.Fatal("expected command construction failure")
	}
	loaded, err = newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
		t.Fatal("command not started contributed a timing sample")
	}
}

func TestPreparedTaskRunLearnsStreamedSuccessAndFailure(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	for _, tc := range []struct {
		task       TaskName
		command    string
		wantStatus TaskRunStatus
	}{
		{TaskBuild, "go version", TaskStatusSucceeded},
		{TaskAudit, "go definitely-not-a-command", TaskStatusFailed},
	} {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: tc.task, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: tc.task}
		resolved := prepared.graph.nodes[node]
		resolved.Command = tc.command
		prepared.graph.nodes[node] = resolved
		_, runErr := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: false})
		if tc.wantStatus == TaskStatusSucceeded && runErr != nil {
			t.Fatal(runErr)
		}
		if tc.wantStatus == TaskStatusFailed && runErr == nil {
			t.Fatal("expected command failure")
		}
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 1 || len(loaded[taskTimingKey{Service: "api", Task: string(TaskAudit)}]) != 1 {
		t.Fatalf("streamed observations missing: %#v", loaded)
	}
}

func TestPreparedTaskRunCacheHitDoesNotAddTiming(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepare := func() *PreparedTaskRun {
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskBuild}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		return prepared
	}
	first, err := RunPreparedTaskRun(prepare(), TaskRunOptions{Concurrency: 1})
	if err != nil || first[0].Status != TaskStatusSucceeded {
		t.Fatalf("first run: %+v, %v", first, err)
	}
	second, err := RunPreparedTaskRun(prepare(), TaskRunOptions{Concurrency: 1})
	if err != nil || second[0].Status != TaskStatusSkipped || !second[0].Cached {
		t.Fatalf("cache run: %+v, %v", second, err)
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]; len(got) != 1 {
		t.Fatalf("cache hit added timing sample: %v", got)
	}
}

func TestCompatibilityRunnerUsesRegisteredPreparedPhases(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	var runs []*PreparedTaskRun
	for _, task := range []TaskName{TaskFix, TaskTest} {
		run, err := PrepareTaskRun(cfg, TaskRequest{Task: task, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		for node, resolved := range run.graph.nodes {
			resolved.Command = "go version"
			run.graph.nodes[node] = resolved
		}
		runs = append(runs, run)
	}
	opts := TaskRunOptions{NoCache: true, BufferOutput: true, Concurrency: 1}
	session := NewTaskProgressSession(runs, opts)
	defer session.Close()
	for i, run := range runs {
		opts.PreparedRun, opts.Progress = run, session
		if _, err := RunOrchestratedTaskRequestWithConfig(cfg, TaskRequest{Task: []TaskName{TaskFix, TaskTest}[i], Services: []string{"api"}, ExactServices: true}, opts); err != nil {
			t.Fatal(err)
		}
		got := session.model.Snapshot()
		if got.Total != 2 || got.Settled != i+1 || got.Percent != (i+1)*50 {
			t.Fatalf("phase %d did not advance shared progress: %+v", i, got)
		}
	}
}

func TestPreparedTaskRunHistoryFailuresRemainAdvisory(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	if err := os.Mkdir(filepath.Join(repo, taskTimingFile), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskBuild}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "go version"
	prepared.graph.nodes[node] = resolved
	var results []TaskRunResult
	var runErr error
	stdout, stderr := captureStreams(t, func() {
		results, runErr = RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1, BufferOutput: true})
	})
	if runErr != nil || len(results) != 1 || results[0].Status != TaskStatusSucceeded {
		t.Fatalf("history failure changed task outcome: %+v, %v", results, runErr)
	}
	if !strings.Contains(stderr, "timing history load failed") || !strings.Contains(stderr, "timing history save failed") {
		t.Fatalf("missing advisory history warnings: %q", stderr)
	}
	if strings.Contains(stdout+stderr, "\x1b[2K") {
		t.Fatalf("buffered pipe wrote progress controls: stdout=%q stderr=%q", stdout, stderr)
	}

	failing, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskAudit, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	failedNode := TaskNode{Service: "api", Task: TaskAudit}
	resolved = failing.graph.nodes[failedNode]
	resolved.Command = "go definitely-not-a-command"
	failing.graph.nodes[failedNode] = resolved
	failedResults, runErr := RunPreparedTaskRun(failing, TaskRunOptions{NoCache: true, Concurrency: 1})
	if runErr == nil || len(failedResults) != 1 || failedResults[0].Err == nil || !strings.Contains(failedResults[0].Err.Error(), "exit status") {
		t.Fatalf("history failure replaced task error: results=%+v error=%v", failedResults, runErr)
	}
}

func TestPreparedTaskRunDoesNotLearnFormatSkipsBlockedNodesOrCacheErrors(t *testing.T) {
	t.Run("format skip", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskFormat, Services: []string{"api"}, ExactServices: true, FileTargets: map[string][]string{}})
		if err != nil {
			t.Fatal(err)
		}
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
		if err != nil || len(results) != 1 || results[0].Status != TaskStatusSkipped {
			t.Fatalf("skip result: %+v, %v", results, err)
		}
		if _, err := os.Stat(filepath.Join(repo, taskTimingFile)); !os.IsNotExist(err) {
			t.Fatalf("planned skip created timing history: %v", err)
		}
	})
	t.Run("blocked dependency", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		mustWrite(t, filepath.Join(repo, "a", "go.mod"), "module a\n\ngo 1.24\n")
		mustWrite(t, filepath.Join(repo, "b", "go.mod"), "module b\n\ngo 1.24\n")
		cfg := &Config{Services: []Service{{Name: "a", Path: "a", Kind: "service", Archetype: "go"}, {Name: "b", Path: "b", Kind: "service", Archetype: "go", Depends: []string{"a"}}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"b"}})
		if err != nil {
			t.Fatal(err)
		}
		for node, resolved := range prepared.graph.nodes {
			resolved.Command = "go version"
			if node.Service == "a" {
				resolved.Command = "go definitely-not-a-command"
			}
			prepared.graph.nodes[node] = resolved
		}
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
		if err == nil {
			t.Fatal("expected failed dependency")
		}
		statuses := map[string]TaskRunStatus{}
		for _, result := range results {
			statuses[result.Node.Service] = result.Status
		}
		if statuses["a"] != TaskStatusFailed || statuses["b"] != TaskStatusSkipped {
			t.Fatalf("unexpected dependency statuses: %v", statuses)
		}
		loaded, err := newTaskTimingStore(repo).load()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded[taskTimingKey{Service: "a", Task: string(TaskBuild)}]) != 1 || len(loaded[taskTimingKey{Service: "b", Task: string(TaskBuild)}]) != 0 {
			t.Fatalf("blocked node affected history: %#v", loaded)
		}
	})
	t.Run("cache load error", func(t *testing.T) {
		repo := t.TempDir()
		withWorkingDir(t, repo)
		mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
		cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
		prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
		if err != nil {
			t.Fatal(err)
		}
		node := TaskNode{Service: "api", Task: TaskBuild}
		resolved := prepared.graph.nodes[node]
		resolved.Command = "go version"
		prepared.graph.nodes[node] = resolved
		key, err := buildTaskCacheKey(resolved.Service, node.Task, resolved.Command)
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(repo, ".cache", "mono", "tasks", key+".json"), "invalid json")
		results, err := RunPreparedTaskRun(prepared, TaskRunOptions{Concurrency: 1})
		if err == nil || results[0].Status != TaskStatusFailed {
			t.Fatalf("expected cache failure: %+v, %v", results, err)
		}
		loaded, err := newTaskTimingStore(repo).load()
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
			t.Fatalf("cache failure produced timing sample: %#v", loaded)
		}
	})
}

func TestPreparedTaskRunStartupFailureDoesNotLearn(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	mustWrite(t, filepath.Join(repo, "api", "go.mod"), "module api\n\ngo 1.24\n")
	tools := t.TempDir()
	mustWrite(t, filepath.Join(tools, "node"), "not executable")
	if err := os.Chmod(filepath.Join(tools, "node"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskBuild, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	node := TaskNode{Service: "api", Task: TaskBuild}
	resolved := prepared.graph.nodes[node]
	resolved.Command = "node command.js"
	prepared.graph.nodes[node] = resolved
	results, err := RunPreparedTaskRun(prepared, TaskRunOptions{NoCache: true, Concurrency: 1})
	if err == nil || results[0].Status != TaskStatusFailed {
		t.Fatalf("expected subprocess startup failure: %+v, %v", results, err)
	}
	loaded, err := newTaskTimingStore(repo).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded[taskTimingKey{Service: "api", Task: string(TaskBuild)}]) != 0 {
		t.Fatalf("failed startup produced timing sample: %#v", loaded)
	}
}

func TestTaskExecutorSerializesConcurrentFailureReplayWithProgress(t *testing.T) {
	repo := t.TempDir()
	withWorkingDir(t, repo)
	cfg := &Config{Services: []Service{{Name: "a", Path: "a", Kind: "service", Archetype: "go"}, {Name: "b", Path: "b", Kind: "service", Archetype: "go"}}}
	for _, name := range []string{"a", "b"} {
		mustWrite(t, filepath.Join(repo, name, "go.mod"), "module "+name+"\n\ngo 1.24\n")
		mustWrite(t, filepath.Join(repo, name, "fail.js"), "const name = "+strconv.Quote(strings.ToUpper(name))+"; process.stdout.write(name+'-START\\n'); setTimeout(() => { process.stdout.write(name+'-END\\n'); process.stderr.write(name+'-ERR\\n'); process.exit(1) }, 20);\n")
	}
	prepared, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskAudit, Services: []string{"a", "b"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	for node, resolved := range prepared.graph.nodes {
		resolved.Command = "node fail.js"
		prepared.graph.nodes[node] = resolved
	}
	var events bytes.Buffer
	line := output.NewProgressLine(output.ProgressOptions{Writer: &events, Mode: output.ModeInteractive, IsTTY: func() bool { return true }, Term: func() string { return "xterm" }, Width: func() (int, error) { return 100, nil }, TickInterval: time.Hour})
	session := newTaskProgressSession([]*PreparedTaskRun{prepared}, TaskRunOptions{BufferOutput: true, Concurrency: 2}, line)
	options := TaskRunOptions{NoCache: true, BufferOutput: true, Concurrency: 2, Progress: session, progressIDs: session.ids[prepared]}
	executor := taskExecutor{cache: newTaskCache(), printer: output.NewPrinterWithMode(&events, &events, output.ModePlain), stdout: &events, stderr: &events}
	_, runErr := executor.execute(context.Background(), prepared.graph, options)
	session.Close()
	if runErr == nil {
		t.Fatal("expected concurrent command failures")
	}
	text := events.String()
	clear := strings.Index(text, "\x1b[2K")
	if clear < 0 {
		t.Fatalf("progress line was not cleared before transcript replay: %q", text)
	}
	for _, name := range []string{"A", "B"} {
		start := strings.Index(text, name+"-START")
		end := strings.Index(text, name+"-END")
		errAt := strings.Index(text, name+"-ERR")
		if start < clear || end < start || errAt < end {
			t.Fatalf("missing or reordered %s transcript: %q", name, text)
		}
		other := "A"
		if name == "A" {
			other = "B"
		}
		otherStart := strings.Index(text, other+"-START")
		if otherStart > start && otherStart < errAt {
			t.Fatalf("concurrent transcripts interleaved: %q", text)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
