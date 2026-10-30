package runtimecmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCollectInfraDeps(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "polaris"},
			{Name: "pythia"},
			{Name: "mallos"},
		},
		Local: &InfraSpec{
			Resources: []InfraResource{
				{Name: "postgres"},
				{Name: "redis"},
			},
		},
	}

	deps, err := collectInfraDeps(cfg, []Service{
		{Name: "pythia", Depends: []string{"polaris", "postgres"}, DevDepends: []string{"redis"}},
		{Name: "mallos", Depends: []string{"redis", "polaris", "postgres"}, DevDepends: []string{"postgres"}},
	})
	if err != nil {
		t.Fatalf("collectInfraDeps returned error: %v", err)
	}

	want := []string{"postgres", "redis"}
	if !reflect.DeepEqual(deps, want) {
		t.Fatalf("unexpected infra deps: got %v, want %v", deps, want)
	}
}

func TestCollectInfraDepsUnknownDependency(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{{Name: "api"}},
		Local:    &InfraSpec{Resources: []InfraResource{{Name: "postgres"}}},
	}

	_, err := collectInfraDeps(cfg, []Service{
		{Name: "api", Depends: []string{"missing"}},
	})
	if err == nil {
		t.Fatal("expected error for unknown dependency")
	}
}

func TestResolveDevCommandPriority(t *testing.T) {
	t.Parallel()

	svc := Service{
		Name:      "pythia",
		Archetype: "go",
		Dev:       "go run ./cmd/api",
		Commands: map[string]string{
			"dev": "go test ./...",
		},
	}

	got, ok := resolveDevCommand(svc)
	if !ok {
		t.Fatal("expected resolveDevCommand to return command")
	}
	if got != "go test ./..." {
		t.Fatalf("unexpected command: got %q", got)
	}
}

func TestResolveDevCommandArchetypeDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		svc  Service
		want string
		ok   bool
	}{
		{
			name: "go",
			svc:  Service{Name: "pythia", Archetype: "go"},
			want: "go run github.com/air-verse/air@latest -c .air.toml",
			ok:   true,
		},
		{
			name: "go-tui",
			svc:  Service{Name: "sonatad", Archetype: "go-tui"},
			want: "go run github.com/air-verse/air@latest -c .air.toml",
			ok:   true,
		},
		{
			name: "react",
			svc:  Service{Name: "mallos", Archetype: "react"},
			want: "pnpm dev",
			ok:   true,
		},
		{
			name: "ts-ink",
			svc:  Service{Name: "sonata", Archetype: "ts-ink"},
			want: "pnpm dev",
			ok:   true,
		},
		{
			name: "ts-node",
			svc:  Service{Name: "sonatad", Archetype: "ts-node"},
			want: "pnpm dev",
			ok:   true,
		},
		{
			name: "ts-lib",
			svc:  Service{Name: "core", Archetype: "ts-lib"},
			want: "",
			ok:   false,
		},
		{
			name: "unknown",
			svc:  Service{Name: "custom", Archetype: "unknown"},
			want: "",
			ok:   false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolveDevCommand(tc.svc)
			if ok != tc.ok {
				t.Fatalf("unexpected ok: got %v want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("unexpected command: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestResolveDevCommandNoDefaultForPackageKind(t *testing.T) {
	t.Parallel()

	for _, archetype := range []string{"go", "go-tui"} {
		svc := Service{Name: "polaris", Kind: "package", Archetype: archetype}
		if _, ok := resolveDevCommand(svc); ok {
			t.Fatalf("expected %s package kind to not receive inferred dev default", archetype)
		}
	}
}

func TestResolveGoTUIDevCommandOverridesDefault(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		svc  Service
		want string
	}{
		{svc: Service{Name: "sonatad", Archetype: "go-tui", Dev: "go run ./cmd/custom"}, want: "go run ./cmd/custom"},
		{svc: Service{Name: "sonatad", Archetype: "go-tui", Commands: map[string]string{"dev": "go run ./cmd/command"}}, want: "go run ./cmd/command"},
	} {
		got, ok := resolveDevCommand(tc.svc)
		if !ok || got != tc.want {
			t.Fatalf("unexpected command: got %q supported=%v want %q", got, ok, tc.want)
		}
	}
}

func TestSelectServicesForDevIncludesServiceDependencies(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "frontend", Kind: "service", Archetype: "react", Depends: []string{"backend"}},
			{Name: "backend", Kind: "service", Archetype: "go"},
		},
	}

	got, err := selectServicesForDev(cfg, []string{"frontend"})
	if err != nil {
		t.Fatalf("selectServicesForDev returned error: %v", err)
	}

	want := []Service{
		{Name: "frontend", Kind: "service", Archetype: "react", Depends: []string{"backend"}},
		{Name: "backend", Kind: "service", Archetype: "go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected selected services: got %#v, want %#v", got, want)
	}
}

func TestSelectServicesForDevSkipsNonRunnableDependencies(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "frontend", Kind: "service", Archetype: "react", Depends: []string{"shared-lib"}},
			{Name: "shared-lib", Kind: "package", Archetype: "go"},
		},
	}

	got, err := selectServicesForDev(cfg, []string{"frontend"})
	if err != nil {
		t.Fatalf("selectServicesForDev returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "frontend" {
		t.Fatalf("unexpected selected services: got %#v", got)
	}
}

func TestSelectServicesForDevErrorsForExplicitNonRunnableService(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "shared-lib", Kind: "package", Archetype: "go"},
		},
	}

	_, err := selectServicesForDev(cfg, []string{"shared-lib"})
	if err == nil {
		t.Fatal("expected error for explicit service without dev command")
	}
}

func TestSelectServicesForDevIncludesDevDepends(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Services: []Service{
			{Name: "frontend", Kind: "service", Archetype: "react", DevDepends: []string{"backend"}},
			{Name: "backend", Kind: "service", Archetype: "go"},
		},
	}

	got, err := selectServicesForDev(cfg, []string{"frontend"})
	if err != nil {
		t.Fatalf("selectServicesForDev returned error: %v", err)
	}

	want := []Service{
		{Name: "frontend", Kind: "service", Archetype: "react", DevDepends: []string{"backend"}},
		{Name: "backend", Kind: "service", Archetype: "go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected selected services: got %#v, want %#v", got, want)
	}
}

func TestEffectiveDevDependenciesMergesAndDeduplicates(t *testing.T) {
	t.Parallel()

	svc := Service{
		Depends:    []string{"api", "postgres", "api"},
		DevDepends: []string{"mallos", "postgres", "redis", " "},
	}

	got := effectiveDevDependencies(svc)
	want := []string{"api", "postgres", "mallos", "redis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected dependencies: got %v, want %v", got, want)
	}
}

// The report includes only synthetic fixture variables, never the full environment.
const devEnvReportSource = `package main
import ("encoding/json"; "os"; "strings")
func main() {
 values := map[string]string{}
 for _, entry := range os.Environ() {
  key, value, _ := strings.Cut(entry, "=")
  if strings.HasPrefix(key, "MONO_DEV_FIXTURE_") { values[key] = value }
 }
 values["cwd"], _ = os.Getwd()
 data, _ := json.Marshal(values)
 if err := os.WriteFile(os.Args[1], data, 0600); err != nil { panic(err) }
}
`

func devEnvironmentFixture(t *testing.T) (string, []Service) {
	t.Helper()
	t.Setenv("MONO_DEV_FIXTURE_SHELL", "inherited")
	t.Setenv("MONO_DEV_FIXTURE_EMPTY", "")
	root := t.TempDir()
	writeDevEnv(t, root, "MONO_DEV_FIXTURE_ROOT=default\nMONO_DEV_FIXTURE_OWN=root\nMONO_DEV_FIXTURE_SHELL=root\nMONO_DEV_FIXTURE_EMPTY=root")
	var services []Service
	for _, name := range []string{"sample", "sample2"} {
		dir := filepath.Join(root, "apps", name)
		writeDevEnv(t, dir, "MONO_DEV_FIXTURE_OWN="+name+"\nMONO_DEV_FIXTURE_ONLY_"+name+"=yes\nMONO_DEV_FIXTURE_SHELL=service\nMONO_DEV_FIXTURE_EMPTY=service")
		services = append(services, Service{Name: name, Path: filepath.Join("apps", name), Dev: "go run main.go report.json"})
	}
	return root, services
}

func assertDevEnvironmentReport(t *testing.T, path, dir, name string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"cwd": dir, "MONO_DEV_FIXTURE_ROOT": "default", "MONO_DEV_FIXTURE_OWN": name,
		"MONO_DEV_FIXTURE_ONLY_" + name: "yes", "MONO_DEV_FIXTURE_SHELL": "inherited", "MONO_DEV_FIXTURE_EMPTY": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("child report got %v, want %v", got, want)
	}
}

func TestRunServiceDevEnvironment(t *testing.T) {
	root, services := devEnvironmentFixture(t)
	t.Chdir(root)
	before := os.Environ()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, svc := range services {
		dir := filepath.Join(root, svc.Path)
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(devEnvReportSource), 0o600); err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			if err := runServiceDev(ctx, svc, nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, svc := range services {
		dir := filepath.Join(root, svc.Path)
		assertDevEnvironmentReport(t, filepath.Join(dir, "report.json"), dir, svc.Name)
	}
	if !reflect.DeepEqual(before, os.Environ()) {
		t.Fatal("parent environment changed")
	}
}

func TestStreamingDevDotenvFailurePreventsLaunch(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDevEnv(t, filepath.Join(root, "service"), "BAD!KEY=SECRET_SENTINEL")
	// This command would create a marker if launched.
	source := `package main; import "os"; func main(){os.WriteFile("started",nil,0600)}`
	if err := os.WriteFile(filepath.Join(root, "service", "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := openTestStream(t, "errors")
	original := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = original }()
	if err := runStreamingDev([]Service{{Name: "broken", Path: "service", Dev: "go run main.go"}}, ""); err == nil {
		t.Fatal("expected startup failure")
	}
	message := readTestFile(t, stderr.Name())
	for _, part := range []string{"[broken]", filepath.Join(root, "service", ".env"), "invalid dotenv"} {
		if !strings.Contains(message, part) {
			t.Fatalf("missing %q in %s", part, message)
		}
	}
	if strings.Contains(message, "SECRET_SENTINEL") {
		t.Fatal("secret leaked")
	}
	if _, err := os.Stat(filepath.Join(root, "service", "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("service launched or unexpected error: %v", err)
	}
}

func TestDevTelemetryEnsuresBeforeEmptyReturnAndServiceLaunch(t *testing.T) {
	t.Run("disabled setting skips ensure", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("services.yaml", []byte("telemetry: false\nservices: []\n"), 0600); err != nil {
			t.Fatal(err)
		}
		called := 0
		old := ensureTelemetryForDevHook
		ensureTelemetryForDevHook = func() (string, error) { called++; return "", nil }
		defer func() { ensureTelemetryForDevHook = old }()
		if err := (&devCommand{}).Run([]string{"mono", "dev"}); err != nil {
			t.Fatal(err)
		}
		if called != 0 {
			t.Fatalf("ensure called %d times while disabled", called)
		}
	})
	t.Run("empty selection still ensures", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("services.yaml", []byte("telemetry: true\nservices: []\n"), 0600); err != nil {
			t.Fatal(err)
		}
		called := 0
		old := ensureTelemetryForDevHook
		ensureTelemetryForDevHook = func() (string, error) { called++; return "", nil }
		defer func() { ensureTelemetryForDevHook = old }()
		if err := (&devCommand{}).Run([]string{"mono", "dev"}); err != nil {
			t.Fatal(err)
		}
		if called != 1 {
			t.Fatalf("ensure called %d times, want 1", called)
		}
	})
	t.Run("failure prevents service launch", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		manifest := "telemetry: true\nservices:\n  - name: svc\n    path: svc\n    kind: service\n    archetype: go\n    dev: go run main.go\n"
		if err := os.WriteFile("services.yaml", []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir("svc", 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("svc", "main.go"), []byte(`package main; import "os"; func main(){os.WriteFile("started",nil,0600)}`), 0600); err != nil {
			t.Fatal(err)
		}
		old := ensureTelemetryForDevHook
		ensureTelemetryForDevHook = func() (string, error) { return "", errors.New("forward unavailable") }
		defer func() { ensureTelemetryForDevHook = old }()
		err := (&devCommand{}).Run([]string{"mono", "dev"})
		if err == nil || !strings.Contains(err.Error(), "forward unavailable") {
			t.Fatalf("expected telemetry error, got %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "svc", "started")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("service launched before telemetry was ready: %v", err)
		}
	})
}
