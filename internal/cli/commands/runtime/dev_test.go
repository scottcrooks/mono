package runtimecmd

import (
	"reflect"
	"testing"
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
			name: "react",
			svc:  Service{Name: "mallos", Archetype: "react"},
			want: "pnpm dev",
			ok:   true,
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

	svc := Service{
		Name:      "polaris",
		Kind:      "package",
		Archetype: "go",
	}

	_, ok := resolveDevCommand(svc)
	if ok {
		t.Fatal("expected package kind to not receive inferred dev default")
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
