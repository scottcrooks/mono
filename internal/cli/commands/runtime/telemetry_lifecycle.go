package runtimecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func telemetryOwned(r telemetryResource) bool {
	return r.Metadata.Labels["app.kubernetes.io/managed-by"] == "mono" && r.Metadata.Labels["app.kubernetes.io/part-of"] == telemetryNamespace
}

func sameTelemetrySize(a, b string) bool {
	units := func(s string) *big.Int {
		if !validTelemetryStorageSize(s) {
			return nil
		}
		n, _ := new(big.Int).SetString(s[:len(s)-2], 10)
		if strings.HasSuffix(s, "Gi") {
			n.Mul(n, big.NewInt(1024))
		}
		return n
	}
	x, y := units(a), units(b)
	return x != nil && y != nil && x.Cmp(y) == 0
}

func (k telemetryKube) inspectOwned(ctx context.Context, selected string) (map[string]telemetryResource, error) {
	objects := make(map[string]telemetryResource)
	for _, kind := range []string{"namespace", "pvc", "service", "deployment"} {
		name := "lgtm"
		if kind == "namespace" {
			name = telemetryNamespace
		}
		r, err := k.resource(ctx, selected, kind, name)
		if err != nil {
			return nil, err
		}
		if kind != "namespace" && r.Metadata.Name != "" && !telemetryOwned(r) {
			return nil, fmt.Errorf("telemetry ownership: %s/lgtm exists without mono ownership labels; refusing adoption", kind)
		}
		objects[kind] = r
	}
	return objects, nil
}

func (k telemetryKube) install(ctx context.Context, selected string, o telemetryOptions, saved telemetryConfig) error {
	objects, err := k.inspectOwned(ctx, selected)
	if err != nil {
		return err
	}
	pvc := objects["pvc"]
	c := telemetryConfig{Version: 1, Context: selected, StorageClass: o.storageClass, StorageSize: o.storageSize}
	if pvc.Metadata.Name != "" {
		actualSize := pvc.Spec.Resources.Requests["storage"]
		if c.StorageClass != "" && c.StorageClass != pvc.Spec.StorageClassName || c.StorageSize != "" && !sameTelemetrySize(c.StorageSize, actualSize) {
			return fmt.Errorf("telemetry storage: existing claim uses class %q and size %s; mono cannot change a claim's size/class", pvc.Spec.StorageClassName, actualSize)
		}
		c.StorageClass, c.StorageSize = pvc.Spec.StorageClassName, actualSize
	} else if saved.Context == selected {
		if c.StorageClass == "" {
			c.StorageClass = saved.StorageClass
		}
		if c.StorageSize == "" {
			c.StorageSize = saved.StorageSize
		}
	}
	if c.StorageSize == "" {
		c.StorageSize = "10Gi"
	}
	if pvc.Status.Phase != "Bound" {
		c.StorageClass, err = k.storageClass(ctx, selected, c.StorageClass)
		if err != nil {
			return err
		}
	}
	manifests, err := telemetryManifests(c)
	if err != nil {
		return err
	}
	if err := saveTelemetryConfig(c); err != nil {
		return err
	}
	for _, manifest := range manifests {
		if manifest.kind == "Namespace" && objects["namespace"].Metadata.Name != "" && !telemetryOwned(objects["namespace"]) {
			continue
		}
		argv := []string{"--context", selected, "--request-timeout=5s", "apply", "-f", "-"}
		if _, err := k.run(ctx, argv, manifest.data); err != nil {
			return k.failureDiagnostics(selected, fmt.Errorf("telemetry apply %s: %w", manifest.kind, err))
		}
	}
	rolloutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := pollTelemetry(rolloutCtx, func() error { return k.checkInstallation(rolloutCtx, selected, telemetryOptions{}) }); err != nil {
		return k.failureDiagnostics(selected, fmt.Errorf("telemetry readiness: %w", err))
	}
	return nil
}

func pollTelemetry(ctx context.Context, check func() error) error {
	for {
		err := check()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w (last observation: %v)", ctx.Err(), err)
		case <-timer.C:
		}
	}
}

func (k telemetryKube) failureDiagnostics(selected string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var details []string
	for _, args := range [][]string{
		{"get", "pvc", "lgtm", "-o", "wide"},
		{"get", "deployment,pods", "-o", "wide"},
		{"get", "events", "--sort-by=.lastTimestamp"},
		{"logs", "deployment/lgtm", "--tail=50", "--limit-bytes=4096"},
	} {
		args = append([]string{"-n", telemetryNamespace}, args...)
		data, err := k.cluster(ctx, selected, args...)
		if err != nil {
			details = append(details, err.Error())
		} else {
			if len(data) > 4096 {
				data = data[:4096]
			}
			details = append(details, strings.Join(args, " ")+":\n"+string(data))
		}
	}
	return fmt.Errorf("%w\nStorage retained. Diagnostics:\n%s", cause, strings.Join(details, "\n"))
}

func (k telemetryKube) down(ctx context.Context, selected string) error {
	state := loadState()
	if pid, ok := state.PortForwards[telemetryForwardStateKey]; ok {
		if isProcessRunning(pid) {
			_ = stopProcess(pid)
		}
		delete(state.PortForwards, telemetryForwardStateKey)
		if err := saveState(state); err != nil {
			return fmt.Errorf("telemetry stop: clear forwarding state: %w", err)
		}
	}
	objects, err := k.inspectOwned(ctx, selected)
	if err != nil {
		return err
	}
	if objects["deployment"].Metadata.Name == "" {
		return fmt.Errorf("telemetry installation: deployment/lgtm not installed in %q; run mono telemetry doctor", selected)
	}
	if _, err := k.cluster(ctx, selected, "-n", telemetryNamespace, "scale", "deployment/lgtm", "--replicas=0"); err != nil {
		return fmt.Errorf("telemetry stop: %w", err)
	}
	err = pollTelemetry(ctx, func() error {
		data, err := k.cluster(ctx, selected, "-n", telemetryNamespace, "get", "pods", "-l", "app=mono-telemetry-lgtm", "-o", "json")
		if err != nil {
			return err
		}
		var pods struct{ Items []json.RawMessage }
		if err := json.Unmarshal(data, &pods); err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return fmt.Errorf("waiting for %d pod(s) to terminate", len(pods.Items))
		}
		return nil
	})
	if err != nil {
		return k.failureDiagnostics(selected, fmt.Errorf("telemetry stop: %w", err))
	}
	output.DefaultPrinter().StepOK("telemetry", "Stopped; service, configuration and PVC lgtm retained")
	return nil
}

func (k telemetryKube) status(ctx context.Context, selected string) error {
	objects, err := k.inspectOwned(ctx, selected)
	if err != nil {
		return err
	}
	d, pvc := objects["deployment"], objects["pvc"]
	p := output.DefaultPrinter()
	state := loadState()
	forward := "stopped"
	if pid, ok := state.PortForwards[telemetryForwardStateKey]; ok && isProcessRunning(pid) {
		forward = fmt.Sprintf("active (PID %d)", pid)
	}
	p.Summary(fmt.Sprintf("Context: %s\nNamespace: %s\nDeployment: desired=%d ready=%d\nPVC: phase=%s capacity=%s class=%s", selected, telemetryNamespace, d.Spec.Replicas, d.Status.ReadyReplicas, pvc.Status.Phase, pvc.Status.Capacity["storage"], pvc.Spec.StorageClassName))
	p.Summary("Host forwarding: " + forward)
	p.Summary("Host endpoints via mono telemetry connect: Grafana :3000, Loki :3100, Tempo :3200, OTLP gRPC :4317, OTLP HTTP :4318, Prometheus :9090 (127.0.0.1; invocation overrides supported)")
	return k.checkInstallation(ctx, selected, telemetryOptions{})
}
