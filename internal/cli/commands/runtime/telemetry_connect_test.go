package runtimecmd

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func telemetryAvailablePorts(t *testing.T) telemetryPorts {
	t.Helper()
	var ports telemetryPorts
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for i := range ports {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports[i] = listener.Addr().(*net.TCPAddr).Port
	}
	return ports
}

func TestTelemetryForwardHelper(t *testing.T) {
	if os.Getenv("MONO_TELEMETRY_FORWARD_HELPER") != "1" {
		return
	}
	mode := os.Getenv("MONO_TELEMETRY_FORWARD_MODE")
	switch mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "fixture forwarding failed")
		os.Exit(7)
	case "zero":
		os.Exit(0)
	case "blocked":
		for {
			time.Sleep(time.Hour)
		}
	case "ignore":
		signal.Ignore(syscall.SIGTERM)
	}
	for i, raw := range strings.Split(os.Getenv("MONO_TELEMETRY_FORWARD_PORTS"), ",") {
		port, _ := strconv.Atoi(raw)
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			fmt.Fprintln(os.Stderr, "port bind failed:", err)
			os.Exit(8)
		}
		server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if mode == "unhealthy" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		})}
		go func() { _ = server.Serve(listener) }()
		if mode != "partial" || i < 5 {
			fmt.Printf("Forwarding from 127.0.0.1:%d -> %d\n", port, defaultTelemetryPorts[i])
		}
	}
	if mode == "lost" {
		time.Sleep(600 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "lost connection to pod")
		os.Exit(1)
	}
	for {
		time.Sleep(time.Hour)
	}
}

type telemetryTestOutput struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (o *telemetryTestOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := o.buf.Write(p)
	if strings.Contains(o.buf.String(), "Ready on 127.0.0.1") {
		o.once.Do(func() { close(o.ready) })
	}
	return n, err
}

func (o *telemetryTestOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func TestTelemetryForwardLifecycle(t *testing.T) {
	for _, tc := range []struct {
		mode, failure string
		cancelReady   bool
		occupied      bool
	}{
		{mode: "ready", cancelReady: true},
		{mode: "ignore", cancelReady: true},
		{mode: "partial", failure: "startup timed out"},
		{mode: "unhealthy", failure: "startup timed out"},
		{mode: "blocked", failure: "startup timed out"},
		{mode: "fail", failure: "fixture forwarding failed"},
		{mode: "zero", failure: "exited unexpectedly"},
		{mode: "lost", failure: "lost connection to pod"},
		{mode: "ready", occupied: true, failure: "bind failed"},
	} {
		t.Run(tc.mode+tc.failure, func(t *testing.T) {
			ports := telemetryAvailablePorts(t)
			if tc.occupied {
				l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ports[0]))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestTelemetryForwardHelper$")
			var stringsPorts []string
			for _, p := range ports {
				stringsPorts = append(stringsPorts, strconv.Itoa(p))
			}
			cmd.Env = append(os.Environ(), "MONO_TELEMETRY_FORWARD_HELPER=1", "MONO_TELEMETRY_FORWARD_MODE="+tc.mode, "MONO_TELEMETRY_FORWARD_PORTS="+strings.Join(stringsPorts, ","))
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			out := &telemetryTestOutput{ready: make(chan struct{})}
			var diagnostics bytes.Buffer
			if tc.cancelReady {
				go func() {
					select {
					case <-out.ready:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			startup := 350 * time.Millisecond
			if tc.cancelReady || tc.mode == "lost" || tc.mode == "zero" {
				startup = 3 * time.Second
			}
			err := forwardTelemetry(ctx, cmd, ports, output.NewPrinter(out, &diagnostics), &diagnostics, startup, 100*time.Millisecond)
			if tc.failure == "" && err != nil || tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure)) {
				t.Fatalf("got %v; wanted %q; out=%s stderr=%s", err, tc.failure, out.String(), diagnostics.String())
			}
			if cmd.ProcessState == nil || !cmd.ProcessState.Exited() && cmd.ProcessState.Sys().(syscall.WaitStatus).Signaled() == false {
				t.Fatal("forward child was not reaped")
			}
			if tc.cancelReady && !strings.Contains(out.String(), "Ready on") {
				t.Fatal("did not reach ready state before cancellation")
			}
			if !tc.cancelReady && tc.mode != "lost" && strings.Contains(out.String(), "Ready on") {
				t.Fatal("printed ready on startup failure")
			}
			if tc.mode == "ignore" && cmd.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatal("did not force-stop child ignoring termination")
			}
			if !tc.occupied {
				for _, port := range ports {
					l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
					if err != nil {
						t.Fatalf("port %d leaked: %v", port, err)
					}
					_ = l.Close()
				}
			}
		})
	}
}

func TestTelemetryForwardStartFailureAndStartupCancellation(t *testing.T) {
	ports := telemetryAvailablePorts(t)
	out := &telemetryTestOutput{ready: make(chan struct{})}
	var stderr bytes.Buffer
	err := forwardTelemetry(context.Background(), exec.Command(filepath.Join(t.TempDir(), "missing")), ports, output.NewPrinter(out, &stderr), &stderr, time.Second, time.Second)
	if err == nil || !strings.Contains(err.Error(), "start port forwarding") {
		t.Fatalf("expected start error, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTelemetryForwardHelper$")
	cmd.Env = append(os.Environ(), "MONO_TELEMETRY_FORWARD_HELPER=1", "MONO_TELEMETRY_FORWARD_MODE=blocked")
	if err := forwardTelemetry(ctx, cmd, ports, output.NewPrinter(out, &stderr), &stderr, time.Second, 100*time.Millisecond); err != nil || cmd.ProcessState == nil {
		t.Fatalf("startup cancellation did not reap child: %v", err)
	}
}

func TestDetachedTelemetryForwardIsTrackedAndReused(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process ownership inspection is unsupported")
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	ports := telemetryAvailablePorts(t)
	binDir := t.TempDir()
	kubectl := filepath.Join(binDir, "kubectl")
	build := exec.Command("go", "build", "-o", kubectl, filepath.Join(oldDir, "testdata/telemetry-forward/main.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build kubectl fixture: %v: %s", err, output)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := connectTelemetry(ctx, "test", ports); err != nil {
		t.Fatal(err)
	}
	state := loadState()
	pid := state.PortForwards[telemetryForwardStateKey]
	if pid <= 0 || !isProcessRunning(pid) {
		t.Fatalf("forward PID not tracked: %#v", state.PortForwards)
	}
	if err := connectTelemetry(ctx, "test", ports); err != nil {
		t.Fatal(err)
	}
	endpoint, err := connectTelemetryEndpoint(ctx, "test", defaultTelemetryPorts, true)
	if err != nil || endpoint != ports.url(1) {
		t.Fatalf("custom endpoint: %q, %v", endpoint, err)
	}
	if got := loadState().PortForwards[telemetryForwardStateKey]; got != pid {
		t.Fatalf("healthy forward replaced: %d -> %d", pid, got)
	}
	_ = stopProcess(pid)
	deadline := time.Now().Add(time.Second)
	for isProcessRunning(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := connectTelemetry(ctx, "test", ports); err != nil {
		t.Fatal(err)
	}
	recovered := loadState().PortForwards[telemetryForwardStateKey]
	if recovered <= 0 || recovered == pid || !isProcessRunning(recovered) {
		t.Fatalf("dead forward was not replaced: old=%d new=%d", pid, recovered)
	}
	endpoint, err = connectTelemetryEndpoint(ctx, "other-context", ports, true)
	if err != nil || endpoint != ports.url(1) {
		t.Fatalf("wrong-context recovery: %q, %v", endpoint, err)
	}
	replacement := loadState().PortForwards[telemetryForwardStateKey]
	if replacement == recovered {
		t.Fatal("reused a forward for the wrong context")
	}
	_ = stopProcess(replacement)

	// A process with matching arguments is not trusted when other processes own
	// the listeners, and it must not be signaled when recovery fails.
	ports = telemetryAvailablePorts(t)
	var listeners []net.Listener
	for _, port := range ports {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, l)
	}
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	impostor := exec.Command(kubectl, telemetryForwardArgs("test", ports)...)
	impostor.Env = append(os.Environ(), "MONO_TEST_FORWARD_BLOCK=1")
	if err := impostor.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = impostor.Process.Kill(); _ = impostor.Wait() }()
	state = loadState()
	state.PortForwards[telemetryForwardStateKey] = impostor.Process.Pid
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := connectTelemetryEndpoint(ctx, "test", ports, true); err == nil {
		t.Fatal("accepted unrelated listeners")
	}
	if !isProcessRunning(impostor.Process.Pid) {
		t.Fatal("signaled an unverified process")
	}
	state = loadState()
	state.PortForwards[telemetryForwardStateKey] = os.Getpid()
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := connectTelemetryEndpoint(ctx, "test", ports, true); err == nil {
		t.Fatal("accepted malformed tracked process")
	}
	for _, l := range listeners {
		_ = l.Close()
	}
	t.Setenv("MONO_TEST_FORWARD_PARTIAL", "1")
	partialCtx, cancelPartial := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancelPartial()
	if _, err := connectTelemetryEndpoint(partialCtx, "test", telemetryAvailablePorts(t), true); err == nil {
		t.Fatal("accepted missing forwarding line")
	}
}

func TestTelemetryEnvAndPorts(t *testing.T) {
	o, err := parseTelemetryOptions("env", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeTelemetryEnv(&out, o); err != nil {
		t.Fatal(err)
	}
	want := "export OTEL_EXPORTER_OTLP_ENDPOINT='http://127.0.0.1:4318'\nexport OTEL_EXPORTER_OTLP_PROTOCOL='http/protobuf'\nexport GRAFANA_URL='http://127.0.0.1:3000'\nexport LOKI_URL='http://127.0.0.1:3100'\nexport TEMPO_URL='http://127.0.0.1:3200'\nexport PROMETHEUS_URL='http://127.0.0.1:9090'\n"
	if out.String() != want {
		t.Fatalf("env output: %q", out.String())
	}
	for _, args := range [][]string{{"--grafana-port=0"}, {"--tempo-port=65536"}, {"--grafana-port=3100"}, {"--otlp-http-port=not-a-port"}, {"--project=a,b"}, {"--project=a=b"}, {"--project=a\nb"}} {
		if _, err := parseTelemetryOptions("env", args); err == nil {
			t.Fatalf("accepted invalid settings %v", args)
		}
	}
	ports := telemetryAvailablePorts(t)
	var flags []string
	for i, port := range ports {
		flags = append(flags, "--"+telemetryPortNames[i]+"-port="+strconv.Itoa(port))
	}
	connect, err := parseTelemetryOptions("connect", flags)
	if err != nil {
		t.Fatal(err)
	}
	env, err := parseTelemetryOptions("env", flags)
	if err != nil || env.ports != connect.ports {
		t.Fatalf("port mismatch: %+v %+v %v", env.ports, connect.ports, err)
	}
	argv := strings.Join(telemetryForwardArgs("kind-test", connect.ports), " ")
	if !strings.Contains(argv, "--context kind-test -n mono-telemetry port-forward --address 127.0.0.1 service/lgtm") {
		t.Fatalf("unsafe forwarding arguments: %s", argv)
	}
	for i, port := range ports {
		if !strings.Contains(argv, fmt.Sprintf("%d:%d", port, defaultTelemetryPorts[i])) {
			t.Fatalf("missing mapping: %s", argv)
		}
	}
	out.Reset()
	if err := writeTelemetryEnv(&out, env); err != nil || !strings.Contains(out.String(), "OTEL_EXPORTER_OTLP_ENDPOINT='"+ports.url(4)+"'") || !strings.Contains(out.String(), "PROMETHEUS_URL='"+ports.url(5)+"'") {
		t.Fatalf("overridden env: %q, %v", out.String(), err)
	}
}

func TestTelemetryEnvShellEscaping(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "executed")
	service := "worker'; touch " + sentinel + "; echo '$(touch " + sentinel + ") `touch " + sentinel + "`"
	o, err := parseTelemetryOptions("env", []string{"--service-name=" + service, "--project=project one's"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeTelemetryEnv(&out, o); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", out.String()+"printf '%s\\n%s' \"$OTEL_SERVICE_NAME\" \"$OTEL_RESOURCE_ATTRIBUTES\"")
	data, err := cmd.CombinedOutput()
	want := service + "\nservice.namespace=project one's,deployment.environment.name=local"
	if err != nil || string(data) != want {
		t.Fatalf("shell roundtrip: %q, %v; want %q", data, err, want)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("env value executed shell commands")
	}
}

func TestTelemetryDevReusesVerifiedForwardAcrossProjects(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process ownership inspection is unsupported")
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	binDir := t.TempDir()
	kubectl := filepath.Join(binDir, "kubectl")
	build := exec.Command("go", "build", "-o", kubectl, filepath.Join(oldDir, "testdata/telemetry-forward/main.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build kubectl fixture: %v: %s", err, output)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	first, second, wrong := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Chdir(first); err != nil {
		t.Fatal(err)
	}
	ports := telemetryAvailablePorts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := connectTelemetry(ctx, "fixture-context", ports); err != nil {
		t.Fatal(err)
	}
	original := loadState().PortForwards[telemetryForwardStateKey]
	t.Cleanup(func() { _ = stopProcess(original) })
	if err := os.Chdir(second); err != nil {
		t.Fatal(err)
	}
	endpoint, err := connectTelemetryEndpoint(ctx, "fixture-context", defaultTelemetryPorts, true)
	if err != nil || endpoint != ports.url(1) {
		t.Fatalf("reuse custom forward from another project: endpoint=%q err=%v", endpoint, err)
	}
	if got := loadState().PortForwards[telemetryForwardStateKey]; got != original {
		t.Fatalf("tracked pid = %d, want existing %d", got, original)
	}
	if !isProcessRunning(original) {
		t.Fatal("replaced or stopped shared forward")
	}

	// A verified process for another context must not be adopted or signaled.
	if err := os.Chdir(wrong); err != nil {
		t.Fatal(err)
	}
	endpoint, err = connectTelemetryEndpoint(ctx, "different-context", ports, true)
	if err == nil || endpoint != "" {
		t.Fatalf("accepted wrong-context listener: endpoint=%q err=%v", endpoint, err)
	}
	if !isProcessRunning(original) {
		t.Fatal("stopped another context's forward")
	}
}
