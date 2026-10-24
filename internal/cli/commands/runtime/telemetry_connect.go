package runtimecmd

import (
	"context"
	"fmt"
	"io"
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
	cmd := exec.Command("kubectl", telemetryForwardArgs(selected, ports)...)
	return forwardTelemetry(ctx, cmd, ports, output.DefaultPrinter(), os.Stderr, 30*time.Second, 5*time.Second)
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
