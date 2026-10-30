//go:build !linux

package runtimecmd

import (
	"fmt"
	"os/exec"
)

type telemetryPTYResult struct {
	output string
	err    error
}

func startTelemetryPTY(*exec.Cmd) (<-chan telemetryPTYResult, error) {
	return nil, fmt.Errorf("telemetry integration pseudo-terminal fixture requires Linux")
}

func stopTelemetryPTY(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
