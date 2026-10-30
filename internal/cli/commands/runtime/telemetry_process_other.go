//go:build !linux

package runtimecmd

// Unsupported platforms cannot safely establish listener ownership.
func inspectTelemetryForward(int) (string, telemetryPorts, bool) {
	return "", telemetryPorts{}, false
}

func telemetryForwardPIDs() []int { return nil }
