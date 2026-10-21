//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package runtimecmd

import (
	"errors"
	"os/exec"
	"syscall"
)

func processGroupsSupported() bool { return false }

func configureBackgroundProcess(*exec.Cmd) error {
	return errors.New("process groups are unavailable")
}

func signalDevChild(*devChild, syscall.Signal) error {
	return errors.New("process groups are unavailable")
}

func devProcessGroupExists(int) bool { return false }

func expectedSignalExit(error) bool { return false }
