package runtimecmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// InfraState tracks running infrastructure state
type InfraState struct {
	PortForwards map[string]int `json:"portForwards"` // resource name -> PID
}

const stateFile = ".infra-state.json"
const kubeAPICheckTimeout = "5s"

type infraCommand struct{}

func init() {
	registerCommand("infra", &infraCommand{})
}

// Run dispatches to infra subcommands
func (c *infraCommand) Run(args []string) error {
	if len(args) < 3 {
		printInfraUsage()
		return fmt.Errorf("missing subcommand")
	}

	subcommand := args[2]

	switch subcommand {
	case "up":
		return infraUp(args)
	case "down":
		return infraDown(args)
	case "status":
		return infraStatus()
	case "logs":
		return infraLogs(args)
	default:
		printInfraUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

// loadLocalConfig returns the local infrastructure spec from services.yaml.
func loadLocalConfig() (*InfraSpec, error) {
	config, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if config.Local == nil {
		return nil, fmt.Errorf("no 'local' block found in services.yaml")
	}
	return config.Local, nil
}

// kubeNameRe matches valid Kubernetes names: lowercase alphanumeric and hyphens,
// 1-253 characters, starting and ending with alphanumeric.
var kubeNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,251}[a-z0-9])?$`)

// validateKubeName returns an error if s is not a safe Kubernetes resource name.
// This prevents argument injection when values from services.yaml are passed to kubectl.
func validateKubeName(s string) error {
	if !kubeNameRe.MatchString(s) {
		return fmt.Errorf("invalid Kubernetes name %q: must match %s", s, kubeNameRe.String())
	}
	return nil
}

// checkKubectl verifies kubectl is available and shows current context
func checkKubectl() error {
	// Check if kubectl is installed
	if err := exec.Command("kubectl", "version", "--client").Run(); err != nil {
		return fmt.Errorf("kubectl is not installed or not in PATH")
	}

	// Get current context
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current kubectl context: %w", err)
	}

	context := strings.TrimSpace(string(output))
	fmt.Printf("==> [infra] Using kubectl context: %s\n", context)

	// Fail fast if the current context's API server is unreachable.
	readyCmd := exec.Command("kubectl", "--request-timeout="+kubeAPICheckTimeout, "get", "--raw=/readyz")
	readyOutput, err := readyCmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(readyOutput))
		if detail == "" {
			return fmt.Errorf("kubernetes API is unreachable for context %q (start Rancher Desktop or switch context): %w", context, err)
		}
		return fmt.Errorf("kubernetes API is unreachable for context %q (start Rancher Desktop or switch context): %w: %s", context, err, detail)
	}

	fmt.Printf("==> [infra] Kubernetes API reachable (timeout %s)\n", kubeAPICheckTimeout)
	fmt.Println()

	return nil
}

// infraUp deploys infrastructure resources
func infraUp(args []string) error {
	local, err := loadLocalConfig()
	if err != nil {
		return err
	}

	if err := checkKubectl(); err != nil {
		return err
	}

	if err := validateKubeName(local.Namespace); err != nil {
		return fmt.Errorf("infra config: namespace: %w", err)
	}

	// Filter resources if specific ones requested
	var resourcesToDeploy []InfraResource
	if len(args) > 3 {
		requestedResources := args[3:]
		for _, name := range requestedResources {
			found := false
			for _, res := range local.Resources {
				if res.Name == name {
					resourcesToDeploy = append(resourcesToDeploy, res)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unknown resource: %s", name)
			}
		}
	} else {
		resourcesToDeploy = local.Resources
	}

	// Create namespace (idempotent)
	fmt.Printf("==> [infra] Creating namespace: %s\n", local.Namespace)
	createNsCmd := exec.Command("kubectl", "create", "namespace", local.Namespace, "--dry-run=client", "-o", "yaml") //nolint:gosec // G204: namespace validated by validateKubeName above; no shell involved
	applyNsCmd := exec.Command("kubectl", "apply", "-f", "-")
	pipe, err := createNsCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create namespace pipe: %w", err)
	}
	applyNsCmd.Stdin = pipe

	if err := createNsCmd.Start(); err != nil {
		return fmt.Errorf("failed to start namespace creation: %w", err)
	}
	if err := applyNsCmd.Run(); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	if err := createNsCmd.Wait(); err != nil {
		return fmt.Errorf("failed to complete namespace creation: %w", err)
	}

	state := loadState()

	// Clean up any existing port-forwards for resources we're about to deploy
	// This makes infra-up corrective - you don't need to run infra-down first
	for _, res := range resourcesToDeploy {
		if pid, exists := state.PortForwards[res.Name]; exists {
			fmt.Printf("==> [infra] Stopping existing port-forward for %s (PID %d)...\n", res.Name, pid)
			if err := stopProcess(pid); err != nil {
				// Log but don't fail - process might already be dead
				fmt.Printf("    (Process may have already stopped)\n")
			}
			delete(state.PortForwards, res.Name)
		}
	}

	// Deploy each resource
	for _, res := range resourcesToDeploy {
		fmt.Printf("==> [infra] Deploying %s...\n", res.Name)

		// Apply manifest
		manifestPath, err := filepath.Abs(res.Manifest)
		if err != nil {
			return fmt.Errorf("failed to resolve manifest path for %s: %w", res.Name, err)
		}

		applyCmd := exec.Command("kubectl", "apply", "-n", local.Namespace, "-f", manifestPath) //nolint:gosec // G204: namespace validated by validateKubeName above; manifestPath resolved via filepath.Abs; no shell involved
		applyCmd.Stdout = os.Stdout
		applyCmd.Stderr = os.Stderr
		if err := applyCmd.Run(); err != nil {
			return fmt.Errorf("failed to apply manifest for %s: %w", res.Name, err)
		}

		// Wait for resource to be ready
		fmt.Printf("    Waiting for %s to be ready...\n", res.Name)
		if err := waitForReady(local.Namespace, res.ReadyCheck.Selector, 120*time.Second); err != nil {
			return fmt.Errorf("resource %s failed to become ready: %w", res.Name, err)
		}

		// Start port-forward if configured
		if res.PortForward != nil {
			fmt.Printf("    Starting port-forward for %s (%d:%d)...\n", res.Name, res.PortForward.LocalPort, res.PortForward.TargetPort)
			pid, err := startPortForward(local.Namespace, res.PortForward)
			if err != nil {
				return fmt.Errorf("failed to start port-forward for %s: %w", res.Name, err)
			}
			state.PortForwards[res.Name] = pid
		}

		fmt.Printf("✓ %s ready\n", res.Name)
		if res.PortForward != nil {
			fmt.Printf("  Port-forward: localhost:%d\n", res.PortForward.LocalPort)
		}
		fmt.Println()
	}

	// Save state
	if err := saveState(state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// infraDown tears down infrastructure resources
func infraDown(args []string) error {
	local, err := loadLocalConfig()
	if err != nil {
		return err
	}

	if err := checkKubectl(); err != nil {
		return err
	}

	if err := validateKubeName(local.Namespace); err != nil {
		return fmt.Errorf("infra config: namespace: %w", err)
	}

	state := loadState()

	// Filter resources if specific ones requested
	var resourcesToRemove []InfraResource
	if len(args) > 3 {
		requestedResources := args[3:]
		for _, name := range requestedResources {
			found := false
			for _, res := range local.Resources {
				if res.Name == name {
					resourcesToRemove = append(resourcesToRemove, res)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unknown resource: %s", name)
			}
		}
	} else {
		resourcesToRemove = local.Resources
	}

	// Stop port-forwards
	for _, res := range resourcesToRemove {
		if pid, exists := state.PortForwards[res.Name]; exists {
			fmt.Printf("==> [infra] Stopping port-forward for %s (PID %d)...\n", res.Name, pid)
			if err := stopProcess(pid); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to stop port-forward: %v\n", err)
			}
			delete(state.PortForwards, res.Name)
		}
	}

	// Delete manifests
	for _, res := range resourcesToRemove {
		fmt.Printf("==> [infra] Removing %s...\n", res.Name)

		manifestPath, err := filepath.Abs(res.Manifest)
		if err != nil {
			return fmt.Errorf("failed to resolve manifest path for %s: %w", res.Name, err)
		}

		deleteCmd := exec.Command("kubectl", "delete", "-n", local.Namespace, "-f", manifestPath, "--ignore-not-found=true") //nolint:gosec // G204: namespace validated by validateKubeName above; manifestPath resolved via filepath.Abs; no shell involved
		deleteCmd.Stdout = os.Stdout
		deleteCmd.Stderr = os.Stderr
		if err := deleteCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete manifest for %s: %v\n", res.Name, err)
		}

		fmt.Printf("✓ %s removed\n\n", res.Name)
	}

	// Save state
	if err := saveState(state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Println("==> [infra] Infrastructure removed")
	return nil
}

// infraStatus shows the status of infrastructure resources
func infraStatus() error {
	local, err := loadLocalConfig()
	if err != nil {
		return err
	}

	if err := checkKubectl(); err != nil {
		return err
	}

	if err := validateKubeName(local.Namespace); err != nil {
		return fmt.Errorf("infra config: namespace: %w", err)
	}

	state := loadState()

	fmt.Println("Infrastructure Status:")
	fmt.Println()
	fmt.Printf("%-15s %-15s %-20s %-15s\n", "RESOURCE", "STATUS", "POD", "PORT-FORWARD")
	fmt.Println(strings.Repeat("-", 70))

	for _, res := range local.Resources {
		// Check pod status
		podStatus := checkPodStatus(local.Namespace, res.ReadyCheck.Selector)

		// Check port-forward status
		pfStatus := "N/A"
		if res.PortForward != nil {
			if pid, exists := state.PortForwards[res.Name]; exists {
				if isProcessRunning(pid) {
					pfStatus = fmt.Sprintf(":%d (PID %d)", res.PortForward.LocalPort, pid)
				} else {
					pfStatus = "Stopped"
				}
			} else {
				pfStatus = "Not running"
			}
		}

		fmt.Printf("%-15s %-15s %-20s %-15s\n", res.Name, podStatus, res.ReadyCheck.Selector, pfStatus)
	}

	fmt.Println()
	return nil
}

// infraLogs tails logs from a specific resource
func infraLogs(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: mono infra logs <resource>")
	}

	resourceName := args[3]

	local, err := loadLocalConfig()
	if err != nil {
		return err
	}

	if err := validateKubeName(local.Namespace); err != nil {
		return fmt.Errorf("infra config: namespace: %w", err)
	}

	// Find resource
	var resource *InfraResource
	for _, res := range local.Resources {
		if res.Name == resourceName {
			resource = &res
			break
		}
	}

	if resource == nil {
		return fmt.Errorf("unknown resource: %s", resourceName)
	}

	// Tail logs
	fmt.Printf("==> [infra] Tailing logs for %s...\n", resourceName)
	logsCmd := exec.Command("kubectl", "logs", "-n", local.Namespace, "-l", resource.ReadyCheck.Selector, "-f", "--tail=50") //nolint:gosec // G204: namespace validated by validateKubeName above; selector is a label string from services.yaml; no shell involved
	logsCmd.Stdout = os.Stdout
	logsCmd.Stderr = os.Stderr
	logsCmd.Stdin = os.Stdin

	return logsCmd.Run()
}

// waitForReady polls until pods matching the selector are ready
func waitForReady(namespace, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "jsonpath={.items[*].status.conditions[?(@.type=='Ready')].status}")
		output, err := cmd.Output()
		if err == nil {
			statuses := strings.TrimSpace(string(output))
			if statuses != "" && !strings.Contains(statuses, "False") {
				return nil // All pods are ready
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for pods with selector %s", selector)
}

// startPortForward starts a kubectl port-forward in the background
func startPortForward(namespace string, pf *PortForwardSpec) (int, error) {
	portMapping := fmt.Sprintf("%d:%d", pf.LocalPort, pf.TargetPort)
	cmd := exec.Command("kubectl", "port-forward", "-n", namespace, pf.Target, portMapping) //nolint:gosec // G204: namespace validated by validateKubeName at call sites; portMapping is fmt.Sprintf of integer ports; no shell involved

	// Redirect outputs to /dev/null to prevent the process from blocking
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open /dev/null: %w", err)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		_ = devNull.Close()
		return 0, err
	}

	// Give port-forward a moment to establish
	time.Sleep(2 * time.Second)

	return cmd.Process.Pid, nil
}

// stopProcess kills a process by PID
func stopProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// isProcessRunning checks if a process is still running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	// Signal 0 doesn't actually send a signal but checks if the process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// checkPodStatus returns the status of pods matching the selector
func checkPodStatus(namespace, selector string) string {
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "jsonpath={.items[*].status.phase}")
	output, err := cmd.Output()
	if err != nil {
		return "Error"
	}

	status := strings.TrimSpace(string(output))
	if status == "" {
		return "Not Found"
	}

	// Check if all pods are running
	phases := strings.Split(status, " ")
	allRunning := true
	for _, phase := range phases {
		if phase != "Running" {
			allRunning = false
			break
		}
	}

	if allRunning {
		return "Running"
	}

	return status
}

// isInfraResourceRunning returns true if the resource has a running pod and an
// active port-forward process recorded in state.
func isInfraResourceRunning(local *InfraSpec, res InfraResource, state *InfraState) bool {
	podStatus := checkPodStatus(local.Namespace, res.ReadyCheck.Selector)
	if podStatus != "Running" {
		return false
	}
	if res.PortForward == nil {
		return true
	}
	pid, exists := state.PortForwards[res.Name]
	return exists && isProcessRunning(pid)
}

// loadState loads the infrastructure state from disk
func loadState() *InfraState {
	state := &InfraState{
		PortForwards: make(map[string]int),
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return state // File doesn't exist yet, return empty state
	}

	_ = json.Unmarshal(data, state) // Ignore errors, use empty state
	return state
}

// saveState saves the infrastructure state to disk
func saveState(state *InfraState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return os.WriteFile(stateFile, data, 0600)
}

// printInfraUsage prints usage information for infra commands
func printInfraUsage() {
	fmt.Println("mono infra - Infrastructure management")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono infra up [resource...]      Deploy local infrastructure")
	fmt.Println("  mono infra down [resource...]    Tear down local infrastructure")
	fmt.Println("  mono infra status                Show status of infrastructure")
	fmt.Println("  mono infra logs <resource>       Tail logs from a resource")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mono infra up                    Deploy all infrastructure")
	fmt.Println("  mono infra up postgres           Deploy only postgres")
	fmt.Println("  mono infra status                Check infrastructure status")
	fmt.Println("  mono infra logs postgres         Tail postgres logs")
	fmt.Println("  mono infra down                  Remove all infrastructure")
}
