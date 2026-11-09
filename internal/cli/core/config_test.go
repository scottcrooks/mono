package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigTelemetryDefaultsAndBoolean(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, tc := range []struct {
		yaml string
		want bool
	}{
		{"services: []\n", false},
		{"telemetry: false\nservices: []\n", false},
		{"telemetry: true\nservices: []\n", true},
	} {
		if err := os.WriteFile(filepath.Join(dir, "services.yaml"), []byte(tc.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Telemetry != tc.want {
			t.Fatalf("LoadConfig(%q).Telemetry = %v, want %v", tc.yaml, cfg.Telemetry, tc.want)
		}
	}
}

func TestLoadConfigIntegrationFocusPolicyDefaultsAndParses(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, tc := range []struct {
		yaml    string
		want    bool
		present bool
	}{{"services: []\n", false, false}, {"integration:\n  requireFocus: false\nservices: []\n", false, true}, {"integration:\n  requireFocus: true\nservices: []\n", true, true}} {
		if err := os.WriteFile(filepath.Join(dir, "services.yaml"), []byte(tc.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.Integration != nil; got != tc.present {
			t.Fatalf("policy present=%v, want %v", got, tc.present)
		}
		if cfg.Integration != nil && cfg.Integration.RequireFocus != tc.want {
			t.Fatalf("RequireFocus=%v, want %v", cfg.Integration.RequireFocus, tc.want)
		}
	}
}

func TestLoadConfigIntegrationPreflight(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	data := []byte("services:\n  - name: api\n    path: api\n    archetype: go\n    integrationPreflight: node scripts/check-integration-env.js\n")
	if err := os.WriteFile(filepath.Join(dir, "services.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].IntegrationPreflight != "node scripts/check-integration-env.js" {
		t.Fatalf("unexpected integration preflight config: %+v", cfg.Services)
	}
}
