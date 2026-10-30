//go:build linux

package runtimecmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// startTelemetryPTY gives the child a real controlling terminal and drains it.
func startTelemetryPTY(cmd *exec.Cmd) (<-chan telemetryPTYResult, error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	master := os.NewFile(uintptr(fd), "ptmx")
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, err
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		_ = master.Close()
		return nil, err
	}
	_ = slave.Close()
	done := make(chan telemetryPTYResult, 1)
	go func() {
		var output bytes.Buffer
		copied := make(chan struct{})
		go func() { _, _ = io.Copy(&output, master); close(copied) }()
		err := cmd.Wait()
		<-copied
		_ = master.Close()
		done <- telemetryPTYResult{output: output.String(), err: err}
	}()
	return done, nil
}

type telemetryPTYResult struct {
	output string
	err    error
}

func TestTelemetryPTYFixture(t *testing.T) {
	cmd := exec.Command("sh", "-c", "test -t 0 && test -t 1 && test -t 2 && printf pty-ready")
	done, err := startTelemetryPTY(cmd)
	if err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || result.output != "pty-ready" {
		t.Fatalf("pseudo-terminal: %q: %v", result.output, result.err)
	}
}

func stopTelemetryPTY(cmd *exec.Cmd) { _ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL) }
