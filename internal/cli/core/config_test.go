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
