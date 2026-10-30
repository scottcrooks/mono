package runtimecmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func telemetryForwardArgs(selected string, ports telemetryPorts) []string {
	args := []string{"--context", selected, "-n", telemetryNamespace, "port-forward", "--address", "127.0.0.1", "service/lgtm"}
	for i, port := range ports {
		args = append(args, fmt.Sprintf("%d:%d", port, defaultTelemetryPorts[i]))
	}
	return args
}

type telemetryForwardOutput struct {
	mu      sync.Mutex
	writer  io.Writer
	pending string
	ports   telemetryPorts
	ready   map[int]bool
	detail  telemetryOutput
}

func (o *telemetryForwardOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.detail.Write(data)
	o.pending += string(data)
	for {
		line, rest, found := strings.Cut(o.pending, "\n")
		if !found {
			break
		}
		o.pending = rest
		var local, target int
		if _, err := fmt.Sscanf(line, "Forwarding from 127.0.0.1:%d -> %d", &local, &target); err == nil {
			for i, port := range o.ports {
				if local == port && target == defaultTelemetryPorts[i] {
					o.ready[local] = true
				}
			}
		}
	}
	if len(o.pending) > 8192 {
		o.pending = o.pending[len(o.pending)-8192:]
	}
	return o.writer.Write(data)
}

func (o *telemetryForwardOutput) allReady() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ready) == len(o.ports)
}

func (o *telemetryForwardOutput) diagnostics() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.TrimSpace(o.detail.buf.String())
}

func telemetryHostReady(ctx context.Context, ports telemetryPorts) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for _, endpoint := range []struct {
		index int
		path  string
	}{{0, "/api/health"}, {1, "/ready"}, {2, "/ready"}, {5, "/-/ready"}} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ports.url(endpoint.index)+endpoint.path, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

func connectTelemetry(ctx context.Context, selected string, ports telemetryPorts) error {
	_, err := connectTelemetryEndpoint(ctx, selected, ports, false)
	return err
}

var inspectTelemetryForwardHook = inspectTelemetryForward

func connectTelemetryEndpoint(ctx context.Context, selected string, ports telemetryPorts, reuseAny bool) (string, error) {
	state := loadState()
	if pid, ok := state.PortForwards[telemetryForwardStateKey]; ok {
		contextName, actual, verified := inspectTelemetryForwardHook(pid)
		if verified && contextName == selected && (reuseAny || actual == ports) && telemetryForwardReady(ctx, actual) {
			printTelemetryForwardReady(actual)
			return actual.url(1), nil
		}
		if verified && isProcessRunning(pid) {
			if err := stopProcess(pid); err != nil {
				return "", fmt.Errorf("telemetry connect: stop stale forwarding: %w", err)
			}
			// Give the kernel time to release the verified sockets before rebinding.
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				_, _, stillOwned := inspectTelemetryForwardHook(pid)
				if !stillOwned {
					break
				}
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		delete(state.PortForwards, telemetryForwardStateKey)
		if err := saveState(state); err != nil {
			return "", fmt.Errorf("telemetry connect: clear stale forwarding state: %w", err)
		}
	}
	// Dev can share a verified forward started in a different project. Only
	// adopt processes whose full mapping, listener ownership and health match.
	if reuseAny {
		for _, pid := range telemetryForwardPIDs() {
			contextName, actual, verified := inspectTelemetryForwardHook(pid)
			if !verified || contextName != selected || !telemetryForwardReady(ctx, actual) {
				continue
			}
			// Recheck identity after the health probes in case the process exited
			// or its PID/listeners changed while requests were in flight.
			confirmedContext, confirmedPorts, confirmed := inspectTelemetryForwardHook(pid)
			if !confirmed || confirmedContext != selected || confirmedPorts != actual {
				continue
			}
			state.PortForwards[telemetryForwardStateKey] = pid
			if err := saveState(state); err != nil {
				return "", fmt.Errorf("telemetry connect: save reused forwarding PID: %w", err)
			}
			printTelemetryForwardReady(actual)
			return actual.url(1), nil
		}
	}
	cmd := exec.Command("kubectl", telemetryForwardArgs(selected, ports)...)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer devNull.Close()
	logFile, err := os.CreateTemp("", "mono-telemetry-forward-*.log")
	if err != nil {
		return "", fmt.Errorf("telemetry connect: create startup log: %w", err)
	}
	defer os.Remove(logFile.Name())
	defer logFile.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("telemetry connect: start port forwarding: %w", err)
	}
	pid := cmd.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	state.PortForwards[telemetryForwardStateKey] = pid
	if err := saveState(state); err != nil {
		stopTelemetryForward(cmd.Process, exited)
		return "", fmt.Errorf("telemetry connect: save forwarding PID: %w: %s", err, readForwardLog(logFile.Name()))
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-exited:
			delete(state.PortForwards, telemetryForwardStateKey)
			_ = saveState(state)
			return "", fmt.Errorf("telemetry connect: port-forward exited during startup (%v); check kubectl context %q and local port availability: %s", waitErr, selected, readForwardLog(logFile.Name()))
		default:
		}
		if !isProcessRunning(pid) {
			delete(state.PortForwards, telemetryForwardStateKey)
			_ = saveState(state)
			return "", fmt.Errorf("telemetry connect: port-forward exited during startup; check kubectl context %q and local port availability: %s", selected, readForwardLog(logFile.Name()))
		}
		if forwardLogReady(logFile.Name(), ports) && telemetryForwardReady(ctx, ports) {
			select {
			case waitErr := <-exited:
				delete(state.PortForwards, telemetryForwardStateKey)
				_ = saveState(state)
				return "", fmt.Errorf("telemetry connect: port-forward exited during startup (%v): %s", waitErr, readForwardLog(logFile.Name()))
			default:
			}
			if !isProcessRunning(pid) {
				continue
			}
			_ = os.Remove(logFile.Name())
			printTelemetryForwardReady(ports)
			return ports.url(1), nil
		}
		select {
		case <-ctx.Done():
			stopTelemetryForward(cmd.Process, exited)
			delete(state.PortForwards, telemetryForwardStateKey)
			_ = saveState(state)
			return "", ctx.Err()
		case <-deadline.C:
			stopTelemetryForward(cmd.Process, exited)
			delete(state.PortForwards, telemetryForwardStateKey)
			_ = saveState(state)
			return "", fmt.Errorf("telemetry connect: startup timed out waiting for all six loopback forwards and backend health; check for occupied ports and rerun connect: %s", readForwardLog(logFile.Name()))
		case <-ticker.C:
		}
	}
}

func forwardLogReady(path string, ports telemetryPorts) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	output := &telemetryForwardOutput{writer: io.Discard, ports: ports, ready: make(map[int]bool)}
	_, _ = output.Write(data)
	return output.allReady()
}

func readForwardLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "no kubectl diagnostics"
	}
	if len(data) > 4096 {
		data = data[len(data)-4096:]
	}
	return strings.TrimSpace(string(data))
}

func stopTelemetryForward(process *os.Process, exited <-chan error) {
	_ = process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-exited:
	case <-timer.C:
		_ = process.Kill()
		<-exited
	}
}

const telemetryForwardStateKey = "telemetry"

func telemetryForwardReady(ctx context.Context, ports telemetryPorts) bool {
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 250*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	return telemetryHostReady(ctx, ports)
}

func printTelemetryForwardReady(ports telemetryPorts) {
	p := output.DefaultPrinter()
	p.StepOK("telemetry", "Ready on 127.0.0.1; forwarding continues in the background")
	p.Summary(fmt.Sprintf("Grafana %s | Loki %s | Tempo %s | Prometheus %s\nOTLP HTTP %s | OTLP gRPC 127.0.0.1:%d", ports.url(0), ports.url(1), ports.url(2), ports.url(5), ports.url(4), ports[3]))
}

// The caller owns signal cancellation; this function owns and always reaps its child.
func forwardTelemetry(ctx context.Context, cmd *exec.Cmd, ports telemetryPorts, printer output.Printer, stderr io.Writer, startup, stopGrace time.Duration) error {
	logs := &telemetryForwardOutput{writer: stderr, ports: ports, ready: make(map[int]bool), detail: telemetryOutput{limit: 4096}}
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("telemetry connect: start port forwarding: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	reaped := false
	defer func() {
		if reaped {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(stopGrace)
		defer timer.Stop()
		select {
		case <-exited:
		case <-timer.C:
			_ = cmd.Process.Kill()
			<-exited
		}
	}()
	startupCtx, cancel := context.WithTimeout(ctx, startup)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-exited:
			reaped = true
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("telemetry connect: forwarding exited unexpectedly (%v); rerun connect: %s", err, logs.diagnostics())
		case <-startupCtx.Done():
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("telemetry connect: startup timed out waiting for all six loopback forwards and backend health: %s", logs.diagnostics())
		case <-ticker.C:
			if !ready && logs.allReady() && telemetryHostReady(startupCtx, ports) {
				// Check for an already exited child before announcing availability.
				select {
				case err := <-exited:
					reaped = true
					return fmt.Errorf("telemetry connect: forwarding exited during startup (%v): %s", err, logs.diagnostics())
				default:
				}
				ready = true
				// Disable the startup deadline while retaining parent cancellation.
				startupCtx = ctx
				printer.StepOK("telemetry", "Ready on 127.0.0.1; keep connect running (Ctrl-C to stop)")
				printer.Summary(fmt.Sprintf("Grafana %s | Loki %s | Tempo %s | Prometheus %s\nOTLP HTTP %s | OTLP gRPC 127.0.0.1:%d", ports.url(0), ports.url(1), ports.url(2), ports.url(5), ports.url(4), ports[3]))
			}
		}
	}
}
