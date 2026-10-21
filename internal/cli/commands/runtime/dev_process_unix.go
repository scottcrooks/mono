//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runtimecmd

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

func processGroupsSupported() bool { return true }

func configureBackgroundProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func signalDevChild(child *devChild, signal syscall.Signal) error {
	if child.cmd.Process == nil {
		return nil
	}
	if child.isTUI {
		return child.cmd.Process.Signal(signal)
	}
	if err := syscall.Kill(-child.cmd.Process.Pid, signal); err != nil {
		return fmt.Errorf("signal process group: %w", err)
	}
	return nil
}

func devProcessGroupExists(pid int) bool {
	err := syscall.Kill(-pid, 0)
	return err == nil || err == syscall.EPERM
}

func expectedSignalExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM
	}
	return exitErr.ExitCode() == 130 || exitErr.ExitCode() == 143
}
