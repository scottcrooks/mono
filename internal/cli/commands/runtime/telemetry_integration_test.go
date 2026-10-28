package runtimecmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type telemetryE2E struct {
	t                 *testing.T
	ctx               context.Context
	env               []string
	binary, name, dir string
	ports             telemetryPorts
	portFlags         []string
	forward           *telemetryE2EForward
}

type telemetryE2EForward struct {
	pid int
}

func TestTelemetryE2E(t *testing.T) {
	if os.Getenv("MONO_TELEMETRY_E2E") != "1" {
		t.Skip("run make test-telemetry-integration for disposable-cluster evidence")
	}
	for _, tool := range []string{"kubectl", "kind", "docker"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("telemetry E2E prerequisite: install %s and add it to PATH; use Rancher Desktop's Docker engine or another running Docker daemon, then rerun make test-telemetry-integration: %v", tool, err)
		}
	}
	binaryPath := os.Getenv("MONO_TELEMETRY_BINARY")
	if binaryPath == "" {
		t.Fatal("MONO_TELEMETRY_BINARY is missing; run make test-telemetry-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(nonce[:])
	f := &telemetryE2E{t: t, ctx: ctx, binary: binaryPath, name: "mono-telemetry-" + id, dir: t.TempDir(), ports: telemetryAvailablePorts(t)}
	f.env = append(os.Environ(), "KUBECONFIG="+filepath.Join(f.dir, "kubeconfig"), "XDG_CONFIG_HOME="+filepath.Join(f.dir, "config"), "KIND_EXPERIMENTAL_PROVIDER=docker", "NO_COLOR=1")
	for i, port := range f.ports {
		f.portFlags = append(f.portFlags, "--"+telemetryPortNames[i]+"-port="+strconv.Itoa(port))
	}
	f.mustRun(f.dir, "docker", "info", "--format", "{{.ServerVersion}}")
	// Cleanup is registered before creation so partially created fixture nodes are removed.
	// The random name and private kubeconfig are the only cluster scope we ever delete.
	t.Cleanup(func() {
		if t.Failed() {
			diagnosticCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			for _, args := range [][]string{{"get", "pods,pvc,deployments", "-o", "wide"}, {"get", "events", "--sort-by=.lastTimestamp"}, {"logs", "deployment/lgtm", "--tail=100", "--limit-bytes=8192"}} {
				argv := append([]string{"--context", "kind-" + f.name, "--request-timeout=5s", "-n", telemetryNamespace}, args...)
				data, err := f.run(diagnosticCtx, f.dir, "kubectl", argv...)
				t.Logf("failure diagnostics: %v: %v\n%s", args, err, data)
			}
		}
		f.stopForward()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stop()
		data, err := f.run(cleanupCtx, f.dir, "kind", "delete", "cluster", "--name", f.name, "--kubeconfig", filepath.Join(f.dir, "kubeconfig"))
		if err != nil {
			t.Errorf("fixture cleanup failed for %s: %v\n%s", f.name, err, data)
		}
	})
	t.Logf("creating isolated cluster %s", f.name)
	f.mustRun(f.dir, "kind", "create", "cluster", "--name", f.name, "--kubeconfig", filepath.Join(f.dir, "kubeconfig"), "--wait", "120s")
	projectA, projectB := filepath.Join(f.dir, "project-a"), filepath.Join(f.dir, "project-b")
	for _, project := range []string{projectA, projectB} {
		if err := os.Mkdir(project, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.cli(projectA, "doctor", "--context", "kind-"+f.name)
	claimUID := strings.TrimSpace(string(f.kubectl("get", "pvc", "lgtm", "-o", "jsonpath={.metadata.uid}")))
	if claimUID == "" {
		t.Fatal("claim UID missing")
	}
	configPath := filepath.Join(f.dir, "config", "mono", "telemetry.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	f.cli(projectA, "doctor", "--check")
	f.cli(projectB, "status")
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("observational commands changed saved configuration")
	}
	f.cli(projectB, "doctor")
	f.startForward(projectA)
	envOut := f.cli(projectB, append([]string{"env", "--service-name=mono-e2e-" + id, "--project=fixture"}, f.portFlags...)...)
	if !strings.Contains(string(envOut), "export OTEL_EXPORTER_OTLP_ENDPOINT='"+f.ports.url(4)+"'") {
		t.Fatalf("env/connect port mismatch: %s", envOut)
	}

	sample := time.Now().Truncate(time.Second)
	nanos := strconv.FormatInt(sample.UnixNano(), 10)
	service, marker := "mono-e2e-"+id, "mono-otlp-log-"+id
	traceID := id + id
	spanMarker := "mono-e2e-span-" + id
	metric, nativeMetric := "mono_telemetry_otlp_"+id, "mono_telemetry_native_"+id
	resource := map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": service}}}}
	f.postJSON(f.ports.url(4)+"/v1/logs", map[string]any{"resourceLogs": []any{map[string]any{"resource": resource, "scopeLogs": []any{map[string]any{"logRecords": []any{map[string]any{"timeUnixNano": nanos, "severityNumber": 9, "body": map[string]any{"stringValue": marker}}}}}}}}, false)
	f.postJSON(f.ports.url(4)+"/v1/metrics", map[string]any{"resourceMetrics": []any{map[string]any{"resource": resource, "scopeMetrics": []any{map[string]any{"metrics": []any{map[string]any{"name": metric, "gauge": map[string]any{"dataPoints": []any{map[string]any{"timeUnixNano": nanos, "asDouble": 42}}}}}}}}}}, false)
	f.postJSON(f.ports.url(4)+"/v1/traces", map[string]any{"resourceSpans": []any{map[string]any{"resource": resource, "scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"traceId": traceID, "spanId": id, "name": spanMarker, "kind": 1, "startTimeUnixNano": nanos, "endTimeUnixNano": strconv.FormatInt(sample.Add(time.Second).UnixNano(), 10), "status": map[string]any{"code": 1}}}}}}}}, false)
	nativeMarker := "mono-native-log-" + id
	f.postJSON(f.ports.url(1)+"/loki/api/v1/push", map[string]any{"streams": []any{map[string]any{"stream": map[string]string{"source": "mono-native-e2e", "run_id": id}, "values": [][]string{{nanos, nativeMarker}}}}}, false)
	remoteWrite := telemetryRemoteWriteFixture(nativeMetric, sample.UnixMilli(), 43)
	if _, err := f.request(http.MethodPost, f.ports.url(5)+"/api/v1/write", remoteWrite, map[string]string{"Content-Type": "application/x-protobuf", "Content-Encoding": "snappy", "X-Prometheus-Remote-Write-Version": "0.1.0"}, false); err != nil {
		t.Fatal("native remote write:", err)
	}
	dashboardUID := "mono-e2e-" + id
	f.postJSON(f.ports.url(0)+"/api/dashboards/db", map[string]any{"dashboard": map[string]any{"uid": dashboardUID, "title": dashboardUID, "schemaVersion": 39, "panels": []any{}}, "overwrite": true}, true)
	datasources, err := f.request(http.MethodGet, f.ports.url(0)+"/api/datasources", nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var sources []struct{ Type string }
	if err := json.Unmarshal(datasources, &sources); err != nil {
		t.Fatal(err)
	}
	types := make(map[string]bool)
	for _, source := range sources {
		types[source.Type] = true
	}
	if !types["prometheus"] || !types["loki"] || !types["tempo"] {
		t.Fatalf("missing preprovisioned data sources: %s", datasources)
	}
	verifyData := func(stage string) {
		t.Helper()
		start, end := strconv.FormatInt(sample.Add(-time.Minute).UnixNano(), 10), strconv.FormatInt(sample.Add(time.Hour).UnixNano(), 10)
		f.eventually(stage+" OTLP log", func() error {
			return f.contains(f.ports.url(1)+"/loki/api/v1/query_range?"+url.Values{"query": {`{service_name="` + service + `"} |= "` + marker + `"`}, "start": {start}, "end": {end}}.Encode(), marker, false)
		})
		f.eventually(stage+" native log", func() error {
			return f.contains(f.ports.url(1)+"/loki/api/v1/query_range?"+url.Values{"query": {`{source="mono-native-e2e",run_id="` + id + `"}`}, "start": {start}, "end": {end}}.Encode(), nativeMarker, false)
		})
		for name, expected := range map[string]string{metric: "42", nativeMetric: "43"} {
			f.eventually(stage+" historical metric "+name, func() error {
				endpoint := f.ports.url(5) + "/api/v1/query_range?" + url.Values{"query": {name}, "start": {strconv.FormatInt(sample.Unix()-5, 10)}, "end": {strconv.FormatInt(sample.Unix()+60, 10)}, "step": {"1"}}.Encode()
				data, err := f.request(http.MethodGet, endpoint, nil, nil, false)
				if err != nil {
					return err
				}
				var result struct {
					Data struct {
						Result []struct{ Values [][]json.RawMessage }
					}
				}
				if err := json.Unmarshal(data, &result); err != nil {
					return err
				}
				for _, series := range result.Data.Result {
					for _, value := range series.Values {
						if len(value) == 2 && string(value[1]) == `"`+expected+`"` {
							return nil
						}
					}
				}
				return fmt.Errorf("expected historical sample %s: %s", expected, data)
			})
		}
		f.eventually(stage+" trace", func() error { return f.contains(f.ports.url(2)+"/api/traces/"+traceID, spanMarker, false) })
		f.eventually(stage+" dashboard", func() error {
			return f.contains(f.ports.url(0)+"/api/dashboards/uid/"+dashboardUID, dashboardUID, true)
		})
		uid := strings.TrimSpace(string(f.kubectl("get", "pvc", "lgtm", "-o", "jsonpath={.metadata.uid}")))
		if uid != claimUID {
			t.Fatalf("%s replaced claim: %s -> %s", stage, claimUID, uid)
		}
		t.Logf("%s: OTLP logs/metrics/traces, native APIs and dashboard queryable; PVC UID %s unchanged", stage, uid)
	}
	verifyData("initial ingestion and shared use")
	f.cli(projectB, "doctor", "--check")
	podUID := string(f.kubectl("get", "pods", "-l", "app=mono-telemetry-lgtm", "-o", "jsonpath={.items[0].metadata.uid}"))
	f.kubectl("delete", "pod", "-l", "app=mono-telemetry-lgtm", "--wait=true", "--timeout=120s")
	f.stopForward()
	f.kubectl("rollout", "status", "deployment/lgtm", "--timeout=300s")
	if next := string(f.kubectl("get", "pods", "-l", "app=mono-telemetry-lgtm", "-o", "jsonpath={.items[0].metadata.uid}")); next == podUID {
		t.Fatal("pod replacement did not occur")
	}
	f.startForward(projectB)
	verifyData("normal pod replacement")
	f.cli(projectA, "down")
	f.stopForward()
	if data, err := f.run(f.ctx, projectB, f.binary, "telemetry", "status"); err == nil {
		t.Fatalf("stopped status should fail: %s", data)
	}
	f.cli(projectB, "up")
	f.startForward(projectB)
	verifyData("down/up")
	f.stopForward()
	for _, project := range []string{projectA, projectB} {
		entries, err := os.ReadDir(project)
		if err != nil || len(entries) != 0 {
			t.Fatalf("project directory modified: %s: %v %v", project, entries, err)
		}
	}
	t.Log("two project directories share the installation; stopping the tracked forward releases all six ports")
}

func (f *telemetryE2E) run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env, cmd.Dir, cmd.WaitDelay = f.env, dir, time.Second
	return cmd.CombinedOutput()
}

func (f *telemetryE2E) mustRun(dir, name string, args ...string) []byte {
	f.t.Helper()
	data, err := f.run(f.ctx, dir, name, args...)
	if err != nil {
		f.t.Fatalf("%s %v: %v\n%s", name, args, err, data)
	}
	return data
}

func (f *telemetryE2E) cli(dir string, args ...string) []byte {
	f.t.Helper()
	return f.mustRun(dir, f.binary, append([]string{"telemetry"}, args...)...)
}

func (f *telemetryE2E) kubectl(args ...string) []byte {
	f.t.Helper()
	return f.mustRun(f.dir, "kubectl", append([]string{"--context", "kind-" + f.name, "--request-timeout=5s", "-n", telemetryNamespace}, args...)...)
}

func (f *telemetryE2E) startForward(dir string) {
	f.t.Helper()
	f.cli(dir, append([]string{"connect"}, f.portFlags...)...)
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		f.t.Fatalf("read forwarding state: %v", err)
	}
	var state InfraState
	if err := json.Unmarshal(data, &state); err != nil {
		f.t.Fatal(err)
	}
	pid := state.PortForwards[telemetryForwardStateKey]
	if pid <= 0 {
		f.t.Fatalf("connect did not record forwarding PID: %s", data)
	}
	f.forward = &telemetryE2EForward{pid: pid}
}

func (f *telemetryE2E) stopForward() {
	f.t.Helper()
	process := f.forward
	if process == nil {
		return
	}
	f.forward = nil
	_ = stopProcess(process.pid)
	data, err := os.ReadFile(filepath.Join(f.dir, stateFile))
	if err != nil {
		f.t.Fatalf("read forwarding state: %v", err)
	}
	var state InfraState
	if err := json.Unmarshal(data, &state); err != nil {
		f.t.Fatal(err)
	}
	delete(state.PortForwards, telemetryForwardStateKey)
	if len(state.PortForwards) == 0 {
		_ = os.Remove(filepath.Join(f.dir, stateFile))
	} else if data, err := json.Marshal(state); err == nil {
		_ = os.WriteFile(filepath.Join(f.dir, stateFile), data, 0600)
	}
	deadline := time.Now().Add(8 * time.Second)
	for _, port := range f.ports {
		for {
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				_ = listener.Close()
				break
			}
			if time.Now().After(deadline) {
				f.t.Errorf("forward port %d leaked: %v", port, err)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (f *telemetryE2E) request(method, endpoint string, body []byte, headers map[string]string, grafana bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	if grafana {
		req.SetBasicAuth("admin", "admin")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", endpoint, resp.StatusCode, data)
	}
	return data, nil
}

func (f *telemetryE2E) postJSON(endpoint string, body any, grafana bool) {
	f.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	response, err := f.request(http.MethodPost, endpoint, data, map[string]string{"Content-Type": "application/json"}, grafana)
	if err != nil || strings.Contains(string(response), "rejectedLogRecords") || strings.Contains(string(response), "rejectedDataPoints") || strings.Contains(string(response), "rejectedSpans") {
		f.t.Fatalf("ingest %s: %v: %s", endpoint, err, response)
	}
}

func (f *telemetryE2E) contains(endpoint, marker string, grafana bool) error {
	data, err := f.request(http.MethodGet, endpoint, nil, nil, grafana)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(marker)) {
		return fmt.Errorf("%s missing %q: %s", endpoint, marker, data)
	}
	return nil
}

func (f *telemetryE2E) eventually(stage string, check func() error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer cancel()
	if err := pollTelemetry(ctx, check); err != nil {
		f.t.Fatalf("%s: %v", stage, err)
	}
}

// Minimal fixture-only protobuf and Snappy literal block. No production dependencies.
func telemetryRemoteWriteFixture(metric string, timestamp int64, value float64) []byte {
	field := func(tag byte, data []byte) []byte {
		result := binary.AppendUvarint([]byte{tag}, uint64(len(data)))
		return append(result, data...)
	}
	label := append(field(10, []byte("__name__")), field(18, []byte(metric))...)
	sample := binary.LittleEndian.AppendUint64([]byte{9}, math.Float64bits(value))
	sample = binary.AppendUvarint(append(sample, 16), uint64(timestamp))
	series := append(field(10, label), field(18, sample)...)
	write := field(10, series)
	compressed := binary.AppendUvarint(nil, uint64(len(write)))
	length := len(write) - 1
	if length < 60 {
		compressed = append(compressed, byte(length<<2))
	} else {
		compressed = append(compressed, 60<<2, byte(length))
	}
	return append(compressed, write...)
}
