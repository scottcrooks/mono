package runtimecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTelemetryArguments(t *testing.T) {
	for _, tc := range []struct {
		sub  string
		args []string
		want string
	}{
		{"doctor", []string{"--check", "--context", "kind-test", "--storage-size", "10Gi"}, ""},
		{"up", []string{"--storage-size=1Mi", "--storage-class=local"}, ""},
		{"doctor", []string{"--storage-size=0Gi"}, "positive integer"},
		{"up", []string{"--storage-size=-1Mi"}, "positive integer"},
		{"up", []string{"--storage-size=1.5Gi"}, "positive integer"},
		{"up", []string{"--storage-size=10G"}, "positive integer"},
		{"status", []string{"--storage-class=local"}, "flag provided but not defined"},
		{"up", []string{"--check"}, "flag provided but not defined"},
		{"env", []string{"--context=kind-test"}, "flag provided but not defined"},
		{"down", []string{"lgtm"}, "unexpected argument"},
		{"erase", nil, "unknown subcommand"},
		{"doctor", []string{"--context"}, "flag needs an argument"},
	} {
		t.Run(tc.sub+strings.Join(tc.args, ""), func(t *testing.T) {
			_, err := parseTelemetryOptions(tc.sub, tc.args)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
		})
	}
}

func TestTelemetryConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := telemetryConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if c, err := loadTelemetryConfig(); err != nil || c.Context != "" {
		t.Fatalf("missing config: %+v, %v", c, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("loading missing config created a directory")
	}
	want := telemetryConfig{1, "kind-test", "standard", "10Gi"}
	if err := saveTelemetryConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadTelemetryConfig()
	if err != nil || got != want {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	for name, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: info=%v err=%v; want %o", name, info, err, mode)
		}
	}
	want.Context = "other"
	if err := saveTelemetryConfig(want); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %v, %v", entries, err)
	}
	for _, invalid := range []string{`{`, `{"version":2}`, `null`, `{"version":1,"context":"x","storageClass":"s","storageSize":"0Gi"}`} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadTelemetryConfig(); err == nil || !strings.Contains(err.Error(), "repair or move") {
			t.Fatalf("invalid config accepted: %v", err)
		}
		if err := saveTelemetryConfig(want); err == nil {
			t.Fatal("overwrote invalid config")
		}
		data, _ := os.ReadFile(path)
		if string(data) != invalid {
			t.Fatal("invalid config changed")
		}
	}
}

func TestTelemetryConfigWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mono"), []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := saveTelemetryConfig(telemetryConfig{1, "kind-test", "standard", "10Gi"}); err == nil || !strings.Contains(err.Error(), "telemetry config") {
		t.Fatalf("expected actionable write error, got %v", err)
	}
}

type telemetryKubeFixture struct {
	t           *testing.T
	names       []string
	current     string
	unreachable map[string]bool
	resources   map[string]string
	classes     string
	calls       [][]string
	fail        string
}

func (f *telemetryKubeFixture) run(_ context.Context, args []string, _ []byte) ([]byte, error) {
	f.t.Helper()
	f.calls = append(f.calls, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	if f.fail != "" && strings.Contains(joined, f.fail) {
		return nil, fmt.Errorf("fixture denied %s", f.fail)
	}
	if reflect.DeepEqual(args, []string{"version", "--client"}) {
		return []byte("client"), nil
	}
	if reflect.DeepEqual(args, []string{"config", "view", "-o", "json"}) {
		contexts := make([]map[string]string, 0, len(f.names))
		for _, n := range f.names {
			contexts = append(contexts, map[string]string{"name": n})
		}
		data, _ := json.Marshal(map[string]any{"current-context": f.current, "contexts": contexts})
		return data, nil
	}
	if len(args) < 5 || args[0] != "--context" || args[1] == "" || args[2] != "--request-timeout=5s" {
		f.t.Fatalf("cluster call lacks explicit context/timeout: %v", args)
	}
	if f.unreachable[args[1]] {
		return nil, fmt.Errorf("offline")
	}
	args = args[3:]
	if reflect.DeepEqual(args, []string{"get", "--raw=/readyz"}) {
		return []byte("ok"), nil
	}
	if args[0] != "get" {
		f.t.Fatalf("unexpected mutation: %v", args)
	}
	if args[1] == "storageclasses" {
		return []byte(f.classes), nil
	}
	if !reflect.DeepEqual(args[3:], []string{"-n", telemetryNamespace, "--ignore-not-found", "-o", "json"}) {
		f.t.Fatalf("unexpected inspection: %v", args)
	}
	return []byte(f.resources[args[1]]), nil
}

func TestTelemetryContextSelection(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, saved, current string
		names                          []string
		offline                        map[string]bool
		want, failure                  string
	}{
		{"explicit remote", "production", "kind-old", "kind-old", []string{"production", "kind-old"}, nil, "production", ""},
		{"saved", "", "kind-old", "kind-current", []string{"kind-old", "kind-current"}, nil, "kind-old", ""},
		{"current", "", "", "docker-desktop", []string{"kind-other", "docker-desktop"}, nil, "docker-desktop", ""},
		{"single reachable", "", "", "remote", []string{"remote", "kind-a", "k3d-b"}, map[string]bool{"kind-a": true}, "k3d-b", ""},
		{"ambiguous", "", "", "remote", []string{"kind-a", "kind-b"}, nil, "", "found 2"},
		{"no local", "", "", "remote", []string{"remote"}, nil, "", "found 0"},
		{"missing explicit", "missing", "kind-a", "kind-a", []string{"kind-a"}, nil, "", "absent"},
		{"missing saved", "", "missing", "kind-a", []string{"kind-a"}, nil, "", "absent"},
		{"explicit offline", "kind-a", "", "kind-b", []string{"kind-a", "kind-b"}, map[string]bool{"kind-a": true}, "", "telemetry API"},
		{"saved offline", "", "kind-a", "kind-b", []string{"kind-a", "kind-b"}, map[string]bool{"kind-a": true}, "", "telemetry API"},
		{"current offline", "", "", "kind-a", []string{"kind-a", "kind-b"}, map[string]bool{"kind-a": true}, "", "telemetry API"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := telemetryKubeFixture{t: t, names: tc.names, current: tc.current, unreachable: tc.offline}
			got, err := (telemetryKube{run: f.run}).selectContext(context.Background(), tc.explicit, tc.saved)
			if got != tc.want || tc.failure == "" && err != nil || tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure)) {
				t.Fatalf("got %q, %v; want %q, %q", got, err, tc.want, tc.failure)
			}
		})
	}
	for _, failed := range []string{"version", "config view"} {
		f := telemetryKubeFixture{t: t, fail: failed}
		if _, err := (telemetryKube{run: f.run}).selectContext(context.Background(), "", ""); err == nil {
			t.Fatalf("accepted failed %s", failed)
		}
	}
}

const telemetryTestLabels = `"app.kubernetes.io/managed-by":"mono","app.kubernetes.io/part-of":"mono-telemetry"`

func healthyTelemetryResources() map[string]string {
	return map[string]string{
		"namespace":  `{"metadata":{"name":"mono-telemetry"}}`,
		"pvc":        `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}},"spec":{"storageClassName":"standard","resources":{"requests":{"storage":"10Gi"}}},"status":{"phase":"Bound"}}`,
		"deployment": `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}},"spec":{"replicas":1},"status":{"readyReplicas":1,"updatedReplicas":1}}`,
		"service":    `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}}}`,
	}
}

func TestTelemetryCheckInstallation(t *testing.T) {
	for _, tc := range []struct {
		name, kind, replacement, fail, want string
		opts                                telemetryOptions
	}{
		{name: "healthy bound no default"},
		{name: "missing namespace", kind: "namespace", want: "not installed"},
		{name: "missing claim", kind: "pvc", want: "not installed"},
		{name: "missing service", kind: "service", want: "missing service"},
		{name: "unmanaged", kind: "service", replacement: `{"metadata":{"name":"lgtm"}}`, want: "ownership"},
		{name: "stopped", kind: "deployment", replacement: `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}},"spec":{"replicas":0}}`, want: "stopped or unhealthy"},
		{name: "unhealthy", kind: "deployment", replacement: `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}},"spec":{"replicas":1},"status":{"readyReplicas":0}}`, want: "stopped or unhealthy"},
		{name: "unobserved update", kind: "deployment", replacement: `{"metadata":{"name":"lgtm","generation":2,"labels":{` + telemetryTestLabels + `}},"spec":{"replicas":1},"status":{"readyReplicas":1,"updatedReplicas":1,"observedGeneration":1}}`, want: "stopped or unhealthy"},
		{name: "pending storage", kind: "pvc", replacement: `{"metadata":{"name":"lgtm","labels":{` + telemetryTestLabels + `}},"status":{"phase":"Pending"}}`, want: "expected Bound"},
		{name: "class mismatch", opts: telemetryOptions{storageClass: "other"}, want: "settings differ"},
		{name: "size mismatch", opts: telemetryOptions{storageSize: "20Gi"}, want: "settings differ"},
		{name: "inspection denied", fail: "get pvc", want: "inspection"},
		{name: "bad resource JSON", kind: "pvc", replacement: `{`, want: "decode pvc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := telemetryKubeFixture{t: t, resources: healthyTelemetryResources(), classes: `{"items":[{"metadata":{"name":"standard","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}]}`, fail: tc.fail}
			if tc.kind != "" {
				f.resources[tc.kind] = tc.replacement
			}
			err := (telemetryKube{run: f.run}).checkInstallation(context.Background(), "kind-fixture", tc.opts)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
			if tc.name == "healthy bound no default" {
				for _, call := range f.calls {
					if strings.Contains(strings.Join(call, " "), "storageclasses") {
						t.Fatal("bound claim incorrectly requires a StorageClass")
					}
				}
			}
		})
	}
}

func TestTelemetryStorageClass(t *testing.T) {
	for _, tc := range []struct{ classes, request, want string }{
		{`{"items":[{"metadata":{"name":"local","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}]}`, "", "local"},
		{`{"items":[{"metadata":{"name":"named"}}]}`, "named", "named"},
		{`{"items":[{"metadata":{"name":"named"}}]}`, "absent", ""},
		{`{"items":[]}`, "", ""},
		{`{"items":[{"metadata":{"name":"one","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}},{"metadata":{"name":"two","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}]}`, "", ""},
		{`{`, "", ""},
	} {
		f := telemetryKubeFixture{t: t, classes: tc.classes}
		got, err := (telemetryKube{run: f.run}).storageClass(context.Background(), "kind-fixture", tc.request)
		if got != tc.want || tc.want == "" && err == nil || tc.want != "" && err != nil {
			t.Fatalf("class=%q err=%v; want %q", got, err, tc.want)
		}
	}
}

func TestTelemetryDoctorCheckDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	script := `#!/bin/sh
case "$*" in
  'version --client') exit 0 ;;
  'config view -o json') echo '{"current-context":"kind-fixture","contexts":[{"name":"kind-fixture"}]}' ;;
  '--context kind-fixture --request-timeout=5s get --raw=/readyz') echo ok ;;
  '--context kind-fixture --request-timeout=5s get namespace mono-telemetry -n mono-telemetry --ignore-not-found -o json') exit 0 ;;
  '--context kind-fixture --request-timeout=5s get pvc lgtm -n mono-telemetry --ignore-not-found -o json') exit 0 ;;
  '--context kind-fixture --request-timeout=5s get storageclasses -o json') echo '{"items":[{"metadata":{"name":"standard","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}]}' ;;
  *) echo "unexpected command $*" >&2; exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	err := (&telemetryCommand{}).Run([]string{"mono", "telemetry", "doctor", "--check"})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); !os.IsNotExist(err) {
		t.Fatal("check-only created configuration")
	}
}

func TestTelemetryDoctorHealthyCheckPreservesSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	if err := saveTelemetryConfig(telemetryConfig{1, "kind-saved", "standard", "10Gi"}); err != nil {
		t.Fatal(err)
	}
	path, _ := telemetryConfigPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"'version --client') exit 0 ;;\n" +
		"'config view -o json') echo '{\"contexts\":[{\"name\":\"kind-saved\"},{\"name\":\"kind-override\"}]}' ;;\n" +
		"'--context kind-override --request-timeout=5s get --raw=/readyz') echo ok ;;\n"
	for kind, data := range healthyTelemetryResources() {
		name := "lgtm"
		if kind == "namespace" {
			name = telemetryNamespace
		}
		script += fmt.Sprintf("'--context kind-override --request-timeout=5s get %s %s -n mono-telemetry --ignore-not-found -o json') echo '%s' ;;\n", kind, name, data)
	}
	script += "*) echo unexpected >&2; exit 99 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := (&telemetryCommand{}).Run([]string{"mono", "telemetry", "doctor", "--check", "--context=kind-override"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("check changed saved selection: %q, %v", after, err)
	}
}

func TestTelemetryKubectlBoundsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nwhile :; do echo diagnostic >&2; done\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runTelemetryKubectl(ctx, []string{"version", "--client"}, nil)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") || !strings.Contains(err.Error(), "diagnostic") || len(err.Error()) > 4500 {
		t.Fatalf("expected bounded deadline failure; got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("failed to cancel child promptly")
	}
}
