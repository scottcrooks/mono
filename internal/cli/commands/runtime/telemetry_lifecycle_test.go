package runtimecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTelemetryManifestStorageAndExposure(t *testing.T) {
	manifests, err := telemetryManifests(telemetryConfig{1, "kind-test", "custom-class", "42Gi"})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, m := range manifests {
		kinds = append(kinds, m.kind)
		var doc struct {
			Metadata struct {
				Name, Namespace string
				Labels          map[string]string
				OwnerReferences []any `yaml:"ownerReferences"`
			}
			Spec struct {
				Type             string
				Replicas         int
				AccessModes      []string `yaml:"accessModes"`
				StorageClassName string   `yaml:"storageClassName"`
				Resources        struct{ Requests map[string]string }
				Ports            []struct{ Port, TargetPort, NodePort int }
				Strategy         struct{ Type string }
				Template         struct {
					Spec struct {
						TerminationGracePeriodSeconds int `yaml:"terminationGracePeriodSeconds"`
						Containers                    []struct {
							Image        string
							Liveness     any `yaml:"livenessProbe"`
							Resources    struct{ Requests, Limits map[string]string }
							VolumeMounts []struct {
								Name, MountPath string
							} `yaml:"volumeMounts"`
							Startup struct {
								Period, Failures int `yaml:"-"`
								PeriodSeconds    int `yaml:"periodSeconds"`
								FailureThreshold int `yaml:"failureThreshold"`
								Exec             struct{ Command []string }
							} `yaml:"startupProbe"`
							Readiness struct{ Exec struct{ Command []string } } `yaml:"readinessProbe"`
						}
						Volumes []struct {
							Claim struct {
								Name string `yaml:"claimName"`
							} `yaml:"persistentVolumeClaim"`
							EmptyDir any `yaml:"emptyDir"`
						}
					}
				}
			}
		}
		// YAML uses camelCase for fields that Go's decoder would otherwise lowercase.
		var raw map[string]any
		if err := yaml.Unmarshal(m.data, &doc); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(m.data, &raw); err != nil {
			t.Fatal(err)
		}
		if doc.Metadata.Labels["app.kubernetes.io/managed-by"] != "mono" || doc.Metadata.Labels["app.kubernetes.io/part-of"] != telemetryNamespace {
			t.Fatalf("missing ownership on %s", m.kind)
		}
		if len(doc.Metadata.OwnerReferences) != 0 {
			t.Fatal("managed resources must be independent")
		}
		if m.kind != "Namespace" && (doc.Metadata.Name != "lgtm" || doc.Metadata.Namespace != telemetryNamespace) {
			t.Fatalf("unexpected identity: %+v", doc.Metadata)
		}
		switch m.kind {
		case "PersistentVolumeClaim":
			if doc.Spec.StorageClassName != "custom-class" || doc.Spec.Resources.Requests["storage"] != "42Gi" || !reflect.DeepEqual(doc.Spec.AccessModes, []string{"ReadWriteOnce"}) {
				t.Fatalf("wrong claim: %+v", doc.Spec)
			}
		case "Service":
			if doc.Spec.Type != "ClusterIP" {
				t.Fatal("service exposed beyond ClusterIP")
			}
			ports := raw["spec"].(map[string]any)["ports"].([]any)
			var actual []int
			for _, p := range ports {
				port := p.(map[string]any)
				if port["targetPort"] != port["port"] || port["nodePort"] != nil {
					t.Fatal("unexpected service exposure")
				}
				actual = append(actual, port["port"].(int))
			}
			if !reflect.DeepEqual(actual, []int{3000, 3100, 3200, 4317, 4318, 9090}) {
				t.Fatalf("ports %v", actual)
			}
		case "Deployment":
			spec := doc.Spec.Template.Spec
			if doc.Spec.Replicas != 1 || doc.Spec.Strategy.Type != "Recreate" || spec.TerminationGracePeriodSeconds != 60 || len(spec.Containers) != 1 || len(spec.Volumes) != 1 || spec.Volumes[0].Claim.Name != "lgtm" || spec.Volumes[0].EmptyDir != nil {
				t.Fatalf("unsafe deployment: %+v", doc.Spec)
			}
			c := spec.Containers[0]
			mounts := raw["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["volumeMounts"].([]any)
			if len(mounts) != 1 || mounts[0].(map[string]any)["mountPath"] != "/data" {
				t.Fatal("backend data masked by another mount")
			}
			if c.Image != "grafana/otel-lgtm:0.29.0" || c.Liveness != nil || c.Resources.Requests["cpu"] != "500m" || c.Resources.Requests["memory"] != "1Gi" || c.Resources.Limits["cpu"] != "2" || c.Resources.Limits["memory"] != "4Gi" {
				t.Fatalf("wrong runtime settings: %+v", c)
			}
			if c.Startup.PeriodSeconds*c.Startup.FailureThreshold != 300 || !strings.Contains(strings.Join(c.Startup.Exec.Command, " "), "/tmp/ready") {
				t.Fatal("wrong startup budget")
			}
			readiness := strings.Join(c.Readiness.Exec.Command, " ")
			for _, endpoint := range []string{":3000/api/health", ":3100/ready", ":3200/ready", ":9090/-/ready", ":13133/ready"} {
				if !strings.Contains(readiness, endpoint) {
					t.Fatalf("missing health check %s", endpoint)
				}
			}
		}
		if strings.Contains(string(m.data), "hostPort:") || strings.Contains(string(m.data), "hostNetwork:") {
			t.Fatal("host exposure in manifest")
		}
	}
	if !reflect.DeepEqual(kinds, []string{"Namespace", "PersistentVolumeClaim", "Service", "Deployment"}) {
		t.Fatalf("unexpected apply order/resources: %v", kinds)
	}
}

type telemetryLifecycleFixture struct {
	inspection telemetryKubeFixture
	mutations  []string
	inputs     [][]byte
	failKind   string
	unhealthy  bool
}

func (f *telemetryLifecycleFixture) run(ctx context.Context, argv []string, input []byte) ([]byte, error) {
	f.inspection.t.Helper()
	if len(argv) > 3 && argv[3] == "apply" {
		if argv[0] != "--context" || argv[1] != "kind-fixture" || argv[2] != "--request-timeout=5s" {
			f.inspection.t.Fatalf("unsafe apply argv: %v", argv)
		}
		if _, err := loadTelemetryConfig(); err != nil {
			f.inspection.t.Fatal("selection not saved before apply", err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(input, &doc); err != nil {
			f.inspection.t.Fatal(err)
		}
		kind := doc["kind"].(string)
		f.mutations = append(f.mutations, kind)
		f.inputs = append(f.inputs, append([]byte(nil), input...))
		if kind == f.failKind {
			return nil, fmt.Errorf("apply failure")
		}
		key := map[string]string{"Namespace": "namespace", "PersistentVolumeClaim": "pvc", "Service": "service", "Deployment": "deployment"}[kind]
		if key == "pvc" {
			doc["status"] = map[string]any{"phase": "Bound", "capacity": map[string]any{"storage": "10Gi"}}
		}
		if key == "deployment" && !f.unhealthy {
			doc["status"] = map[string]any{"readyReplicas": 1, "updatedReplicas": 1}
		}
		data, _ := json.Marshal(doc)
		f.inspection.resources[key] = string(data)
		return nil, nil
	}
	if len(argv) > 5 && argv[3] == "-n" {
		args := strings.Join(argv[5:], " ")
		if args == "scale deployment/lgtm --replicas=0" {
			f.mutations = append(f.mutations, "scale-zero")
			var doc map[string]any
			_ = json.Unmarshal([]byte(f.inspection.resources["deployment"]), &doc)
			doc["spec"].(map[string]any)["replicas"] = 0
			doc["status"] = map[string]any{}
			data, _ := json.Marshal(doc)
			f.inspection.resources["deployment"] = string(data)
			return nil, nil
		}
		if args == "get pods -l app=mono-telemetry-lgtm -o json" {
			return []byte(`{"items":[]}`), nil
		}
		return []byte("diagnostic fixture"), nil
	}
	return f.inspection.run(ctx, argv, input)
}

func newTelemetryLifecycleFixture(t *testing.T, resources map[string]string) *telemetryLifecycleFixture {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return &telemetryLifecycleFixture{inspection: telemetryKubeFixture{t: t, resources: resources, classes: `{"items":[{"metadata":{"name":"standard","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}]}`}}
}

func TestTelemetryLifecycleInstallDownUp(t *testing.T) {
	f := newTelemetryLifecycleFixture(t, map[string]string{})
	k := telemetryKube{run: f.run}
	ctx := context.Background()
	if err := k.install(ctx, "kind-fixture", telemetryOptions{}, telemetryConfig{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.mutations, []string{"Namespace", "PersistentVolumeClaim", "Service", "Deployment"}) {
		t.Fatalf("bad first-consumer ordering: %v", f.mutations)
	}
	c, err := loadTelemetryConfig()
	if err != nil || c.Context != "kind-fixture" || c.StorageClass != "standard" || c.StorageSize != "10Gi" {
		t.Fatalf("bad saved selection: %+v, %v", c, err)
	}
	for i := 0; i < 2; i++ {
		if err := k.down(ctx, "kind-fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.status(ctx, "kind-fixture"); err == nil {
		t.Fatal("stopped deployment reported healthy")
	}
	if err := k.install(ctx, "kind-fixture", telemetryOptions{}, c); err != nil {
		t.Fatal(err)
	}
	if err := k.install(ctx, "kind-fixture", telemetryOptions{}, c); err != nil {
		t.Fatal(err)
	}
	if err := k.status(ctx, "kind-fixture"); err != nil {
		t.Fatal(err)
	}
	for _, action := range f.mutations {
		if action != "Namespace" && action != "PersistentVolumeClaim" && action != "Service" && action != "Deployment" && action != "scale-zero" {
			t.Fatalf("unexpected mutation %q", action)
		}
	}
}

func TestTelemetryLifecyclePreflightRejectsWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name, conflict string
		o              telemetryOptions
		classes        string
	}{
		{name: "class mismatch", o: telemetryOptions{storageClass: "other"}},
		{name: "size mismatch", o: telemetryOptions{storageSize: "20Gi"}},
		{name: "PVC conflict", conflict: "pvc"},
		{name: "service conflict", conflict: "service"},
		{name: "deployment conflict", conflict: "deployment"},
		{name: "no storage class", classes: `{"items":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resources := healthyTelemetryResources()
			if tc.conflict != "" {
				resources[tc.conflict] = `{"metadata":{"name":"lgtm"}}`
			}
			f := newTelemetryLifecycleFixture(t, resources)
			if tc.classes != "" {
				f.inspection.classes = tc.classes
				delete(resources, "pvc")
			}
			if err := (telemetryKube{run: f.run}).install(context.Background(), "kind-fixture", tc.o, telemetryConfig{}); err == nil {
				t.Fatal("expected preflight rejection")
			}
			if len(f.mutations) > 0 {
				t.Fatalf("mutated on preflight failure: %v", f.mutations)
			}
			path, _ := telemetryConfigPath()
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("saved settings before preflight passed")
			}
		})
	}
}

func TestTelemetryLifecycleAdoptionAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fail      string
		unhealthy bool
	}{
		{name: "bound claim adopts actual settings without default"},
		{name: "partial apply", fail: "Deployment"},
		{name: "rollout timeout", unhealthy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTelemetryLifecycleFixture(t, healthyTelemetryResources())
			f.failKind, f.unhealthy = tc.fail, tc.unhealthy
			f.inspection.classes = `{"items":[]}`
			// Namespace exists but belongs to the operator; do not apply its labels.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			err := (telemetryKube{run: f.run}).install(ctx, "kind-fixture", telemetryOptions{storageSize: "10240Mi"}, telemetryConfig{1, "other-cluster", "old-class", "1Gi"})
			if (tc.fail != "" || tc.unhealthy) && (err == nil || !strings.Contains(err.Error(), "Storage retained")) || tc.fail == "" && !tc.unhealthy && err != nil {
				t.Fatalf("unexpected install result: %v", err)
			}
			c, err := loadTelemetryConfig()
			if err != nil || c.StorageClass != "standard" || c.StorageSize != "10Gi" || c.Context != "kind-fixture" {
				t.Fatalf("did not adopt existing claim: %+v %v", c, err)
			}
			for _, kind := range f.mutations {
				if kind == "Namespace" {
					t.Fatal("modified external namespace")
				}
			}
			f.failKind, f.unhealthy = "", false
			if err := (telemetryKube{run: f.run}).install(context.Background(), "kind-fixture", telemetryOptions{}, c); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
		})
	}
}

func TestTelemetryDownMissingDoesNotInstall(t *testing.T) {
	f := newTelemetryLifecycleFixture(t, map[string]string{})
	if err := (telemetryKube{run: f.run}).down(context.Background(), "kind-fixture"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected missing installation: %v", err)
	}
	if len(f.mutations) != 0 {
		t.Fatal("down mutated missing installation")
	}
}
