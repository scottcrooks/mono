package runtimecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const telemetryNamespace = "mono-telemetry"

// The seam records argv in normal tests; no test needs a real cluster.
type telemetryKube struct {
	run func(context.Context, []string, []byte) ([]byte, error)
}

type telemetryOutput struct {
	buf   bytes.Buffer
	limit int
}

func (b *telemetryOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.buf.Len() {
		p = p[:b.limit-b.buf.Len()]
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

func runTelemetryKubectl(ctx context.Context, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(input)
	stdout := &telemetryOutput{limit: 1024 * 1024}
	stderr := &telemetryOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.buf.String()))
	}
	return stdout.buf.Bytes(), nil
}

func (k telemetryKube) cluster(ctx context.Context, selected string, args ...string) ([]byte, error) {
	argv := []string{"--context", selected, "--request-timeout=5s"}
	return k.run(ctx, append(argv, args...), nil)
}

func recognizedTelemetryContext(name string) bool {
	switch name {
	case "docker-desktop", "rancher-desktop", "minikube", "microk8s", "colima":
		return true
	}
	return strings.HasPrefix(name, "kind-") || strings.HasPrefix(name, "k3d-")
}

func (k telemetryKube) selectContext(ctx context.Context, explicit, saved string) (string, error) {
	if _, err := k.run(ctx, []string{"version", "--client"}, nil); err != nil {
		return "", fmt.Errorf("telemetry tools: install kubectl and ensure it is in PATH: %w", err)
	}
	data, err := k.run(ctx, []string{"config", "view", "-o", "json"}, nil)
	if err != nil {
		return "", fmt.Errorf("telemetry context: read kubeconfig: %w", err)
	}
	var config struct {
		Current  string                  `json:"current-context"`
		Contexts []struct{ Name string } `json:"contexts"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("telemetry context: decode kubeconfig: %w", err)
	}
	var names []string
	for _, c := range config.Contexts {
		names = append(names, c.Name)
	}
	selected := explicit
	if selected == "" {
		selected = saved
	}
	if selected == "" && recognizedTelemetryContext(config.Current) {
		selected = config.Current
	}
	if selected != "" {
		found := false
		for _, name := range names {
			found = found || selected == name
		}
		if !found {
			return "", fmt.Errorf("telemetry context: %q is absent; available: %s; use --context NAME", selected, strings.Join(names, ", "))
		}
		if _, err := k.cluster(ctx, selected, "get", "--raw=/readyz"); err != nil {
			return "", fmt.Errorf("telemetry API: context %q unavailable (start its cluster or select --context NAME): %w", selected, err)
		}
		return selected, nil
	}
	var reachable []string
	for _, name := range names {
		if recognizedTelemetryContext(name) {
			if _, err := k.cluster(ctx, name, "get", "--raw=/readyz"); err == nil {
				reachable = append(reachable, name)
			}
		}
	}
	if len(reachable) == 1 {
		return reachable[0], nil
	}
	return "", fmt.Errorf("telemetry context: expected one reachable local context, found %d (%s); available: %s; use --context NAME", len(reachable), strings.Join(reachable, ", "), strings.Join(names, ", "))
}

type telemetryResource struct {
	Metadata struct {
		Name        string
		Generation  int64
		Labels      map[string]string
		Annotations map[string]string
	}
	Spec struct {
		Replicas         int
		StorageClassName string
		Resources        struct{ Requests map[string]string }
	}
	Status struct {
		Phase              string
		ReadyReplicas      int
		UpdatedReplicas    int
		ObservedGeneration int64
		Capacity           map[string]string
	}
}

func (k telemetryKube) resource(ctx context.Context, selected, kind, name string) (telemetryResource, error) {
	var resource telemetryResource
	data, err := k.cluster(ctx, selected, "get", kind, name, "-n", telemetryNamespace, "--ignore-not-found", "-o", "json")
	if err != nil {
		return resource, fmt.Errorf("telemetry inspection: %s/%s: %w", kind, name, err)
	}
	if len(bytes.TrimSpace(data)) != 0 {
		if err := json.Unmarshal(data, &resource); err != nil {
			return resource, fmt.Errorf("telemetry inspection: decode %s/%s: %w", kind, name, err)
		}
	}
	return resource, nil
}

func (k telemetryKube) storageClass(ctx context.Context, selected, requested string) (string, error) {
	data, err := k.cluster(ctx, selected, "get", "storageclasses", "-o", "json")
	if err != nil {
		return "", fmt.Errorf("telemetry storage: inspect StorageClasses: %w", err)
	}
	var classes struct{ Items []telemetryResource }
	if err := json.Unmarshal(data, &classes); err != nil {
		return "", fmt.Errorf("telemetry storage: decode StorageClasses: %w", err)
	}
	var defaults []string
	for _, c := range classes.Items {
		if requested != "" && c.Metadata.Name == requested {
			return requested, nil
		}
		if c.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || c.Metadata.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			defaults = append(defaults, c.Metadata.Name)
		}
	}
	if requested == "" && len(defaults) == 1 {
		return defaults[0], nil
	}
	return "", fmt.Errorf("telemetry storage: requested class %q unavailable or no sole default (%s); install a provisioner or use --storage-class NAME", requested, strings.Join(defaults, ", "))
}

func (k telemetryKube) checkInstallation(ctx context.Context, selected string, o telemetryOptions) error {
	ns, err := k.resource(ctx, selected, "namespace", telemetryNamespace)
	if err != nil {
		return err
	}
	pvc, err := k.resource(ctx, selected, "pvc", "lgtm")
	if err != nil {
		return err
	}
	if pvc.Metadata.Name == "" || pvc.Status.Phase != "Bound" {
		class := o.storageClass
		if class == "" {
			class = pvc.Spec.StorageClassName
		}
		if _, err := k.storageClass(ctx, selected, class); err != nil {
			return err
		}
	}
	if ns.Metadata.Name == "" || pvc.Metadata.Name == "" {
		return fmt.Errorf("telemetry installation: not installed in context %q; run mono telemetry doctor --context %q", selected, selected)
	}
	if o.storageClass != "" && o.storageClass != pvc.Spec.StorageClassName || o.storageSize != "" && !sameTelemetrySize(o.storageSize, pvc.Spec.Resources.Requests["storage"]) {
		return fmt.Errorf("telemetry storage: existing claim settings differ; mono cannot change a claim's size/class")
	}
	for _, kind := range []string{"deployment", "service", "pvc"} {
		r := pvc
		if kind != "pvc" {
			r, err = k.resource(ctx, selected, kind, "lgtm")
			if err != nil {
				return err
			}
		}
		if r.Metadata.Name == "" {
			return fmt.Errorf("telemetry installation: missing %s/lgtm; run mono telemetry doctor", kind)
		}
		if r.Metadata.Labels["app.kubernetes.io/managed-by"] != "mono" || r.Metadata.Labels["app.kubernetes.io/part-of"] != "mono-telemetry" {
			return fmt.Errorf("telemetry ownership: %s/lgtm is not mono-owned", kind)
		}
		if kind == "deployment" && (r.Spec.Replicas < 1 || r.Status.ReadyReplicas < r.Spec.Replicas || r.Status.UpdatedReplicas < r.Spec.Replicas || r.Status.ObservedGeneration < r.Metadata.Generation) {
			return fmt.Errorf("telemetry health: deployment/lgtm stopped or unhealthy (desired %d, ready %d, updated %d)", r.Spec.Replicas, r.Status.ReadyReplicas, r.Status.UpdatedReplicas)
		}
	}
	if pvc.Status.Phase != "Bound" {
		return fmt.Errorf("telemetry storage: pvc/lgtm is %s, expected Bound; check provisioner and PVC events", pvc.Status.Phase)
	}
	return nil
}
