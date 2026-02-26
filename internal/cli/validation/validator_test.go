package validation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateServicesManifestValid(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "packages", "polaris"))
	mustMkdirAll(t, filepath.Join(repo, "apps", "pythia"))
	mustMkdirAll(t, filepath.Join(repo, "apps", "mallos"))
	mustMkdirAll(t, filepath.Join(repo, "apps", "daedalus"))
	mustWrite(t, filepath.Join(repo, "packages", "polaris", "go.mod"), "module polaris\n")
	mustWrite(t, filepath.Join(repo, "apps", "pythia", "go.mod"), "module pythia\n")
	mustWrite(t, filepath.Join(repo, "apps", "mallos", "package.json"), "{}\n")
	mustWrite(t, filepath.Join(repo, "apps", "daedalus", "package.json"), "{}\n")

	manifest := `services:
  - name: polaris
    path: packages/polaris
    kind: package
    archetype: go
    owner: platform
  - name: pythia
    path: apps/pythia
    depends: [postgres, polaris]
    kind: service
    archetype: go
    owner: backend
    deploy:
      containerPort: 8080
      probes:
        readiness: {path: /ready, port: 8080}
        liveness: {path: /health, port: 8080}
      resources:
        requests: {cpu: 100m, memory: 128Mi}
        limits: {cpu: 500m, memory: 512Mi}
  - name: mallos
    path: apps/mallos
    kind: service
    archetype: react
    owner: frontend
    deploy:
      containerPort: 3000
      probes:
        readiness: {path: /, port: 3000}
        liveness: {path: /, port: 3000}
      resources:
        requests: {cpu: 100m, memory: 128Mi}
        limits: {cpu: 500m, memory: 512Mi}
  - name: daedalus
    path: apps/daedalus
    kind: service
    archetype: react
    owner: frontend
    deploy:
      containerPort: 3001
      probes:
        readiness: {path: /, port: 3001}
        liveness: {path: /, port: 3001}
      resources:
        requests: {cpu: 100m, memory: 128Mi}
        limits: {cpu: 500m, memory: 512Mi}
local:
  resources:
    - name: postgres
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if report.HasErrors() {
		t.Fatalf("expected no diagnostics, got %+v", report.Diagnostics)
	}
	if report.WarningCount() != 0 {
		t.Fatalf("expected no warnings, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestSchemaAndRequiredFields(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "svc"))
	manifest := `services:
  - name: svc
    path: apps/svc
    kind: service
    archetype: go
    unknownField: true
anotherTop: bad
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if !containsCode(report, "schema.unknown_key") {
		t.Fatalf("expected schema.unknown_key diagnostic, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "schema.unknown_service_key") {
		t.Fatalf("expected schema.unknown_service_key diagnostic, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "schema.required") {
		t.Fatalf("expected schema.required diagnostic, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestNestedUnknownKeys(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "svc"))
	mustWrite(t, filepath.Join(repo, "apps", "svc", "go.mod"), "module svc\n")
	manifest := `services:
  - name: svc
    path: apps/svc
    kind: service
    archetype: go
    owner: team
    deploy:
      containerPort: 8080
      unknownDeploy: true
      probes:
        readiness:
          path: /ready
          port: 8080
          extraProbe: true
        liveness: {path: /health, port: 8080}
      resources:
        requests: {cpu: 100m}
        limits: {cpu: 200m}
        extraResources: true
local:
  namespace: default
  unknownLocal: true
  resources:
    - name: postgres
      unknownResource: true
      readyCheck:
        selector: app=postgres
        extraReady: true
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	for _, code := range []string{
		"schema.unknown_local_key",
		"schema.unknown_local_resource_key",
		"schema.unknown_readycheck_key",
		"schema.unknown_deploy_key",
		"schema.unknown_probe_key",
		"schema.unknown_deploy_resources_key",
	} {
		if !containsCode(report, code) {
			t.Fatalf("expected %s diagnostic, got %+v", code, report.Diagnostics)
		}
	}
}

func TestValidateServicesManifestGraphRules(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "a"))
	mustMkdirAll(t, filepath.Join(repo, "apps", "b"))
	manifest := `services:
  - name: a
    path: apps/a
    kind: service
    archetype: go
    owner: team-a
    depends: [b]
    deploy:
      containerPort: 8081
      probes:
        readiness: {path: /ready, port: 8081}
        liveness: {path: /health, port: 8081}
      resources:
        requests: {cpu: 100m}
        limits: {cpu: 200m}
  - name: b
    path: apps/b
    kind: service
    archetype: go
    owner: team-b
    depends: [a, missing]
    deploy:
      containerPort: 8082
      probes:
        readiness: {path: /ready, port: 8082}
        liveness: {path: /health, port: 8082}
      resources:
        requests: {cpu: 100m}
        limits: {cpu: 200m}
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if !containsCode(report, "graph.unknown_dependency") {
		t.Fatalf("expected graph.unknown_dependency diagnostic, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "graph.cycle") {
		t.Fatalf("expected graph.cycle diagnostic, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestPathAndDeployRules(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "ok"))
	manifest := `services:
  - name: ok
    path: apps/ok
    kind: service
    archetype: go
    owner: team
    deploy:
      probes:
        readiness: {path: /ready, port: 8080}
        liveness: {path: /health, port: 8080}
      resources:
        requests: {cpu: 100m}
        limits: {cpu: 200m}
  - name: bad
    path: apps/missing
    kind: service
    archetype: go
    owner: team
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if !containsCode(report, "path.missing") {
		t.Fatalf("expected path.missing diagnostic, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "deploy.container_port") {
		t.Fatalf("expected deploy.container_port diagnostic, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "deploy.required") {
		t.Fatalf("expected deploy.required diagnostic, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestWarnsOnMissingProjectMarker(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "svc"))
	manifest := `services:
  - name: svc
    path: apps/svc
    kind: package
    archetype: go
    owner: team
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if report.HasErrors() {
		t.Fatalf("expected warnings only, got %+v", report.Diagnostics)
	}
	if !containsCode(report, "path.project_marker") {
		t.Fatalf("expected path.project_marker warning, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestDoesNotWarnOnMissingProjectMarkerForGoWithRootModule(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "svc"))
	mustWrite(t, filepath.Join(repo, "go.mod"), "module example.com/mono\n")
	manifest := `services:
  - name: svc
    path: apps/svc
    kind: package
    archetype: go
    owner: team
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	report, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if containsCode(report, "path.project_marker") {
		t.Fatalf("expected no path.project_marker warning for go service with repo go.mod, got %+v", report.Diagnostics)
	}
}

func TestValidateServicesManifestDiagnosticsDeterministic(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, "apps", "a"))
	mustMkdirAll(t, filepath.Join(repo, "apps", "b"))
	mustWrite(t, filepath.Join(repo, "apps", "a", "go.mod"), "module a\n")
	mustWrite(t, filepath.Join(repo, "apps", "b", "go.mod"), "module b\n")
	manifest := `unknownTop: true
services:
  - name: b
    path: apps/b
    kind: service
    archetype: go
    owner: team
    depends: [a]
  - name: a
    path: apps/a
    kind: service
    archetype: go
    owner: team
    depends: [missing, b]
`
	manifestPath := filepath.Join(repo, "services.yaml")
	mustWrite(t, manifestPath, manifest)

	first, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	second, err := ValidateServicesManifest(manifestPath)
	if err != nil {
		t.Fatalf("ValidateServicesManifest returned error: %v", err)
	}
	if !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
		t.Fatalf("expected deterministic diagnostics ordering:\nfirst=%+v\nsecond=%+v", first.Diagnostics, second.Diagnostics)
	}
}

func containsCode(report Report, code string) bool {
	for _, diag := range report.Diagnostics {
		if diag.Code == code {
			return true
		}
	}
	return false
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
