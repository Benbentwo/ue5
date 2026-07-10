//go:build darwin || linux

package server

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr configures the command to run as a detached process on Unix systems.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// processAlive reports whether a process with the given PID exists.
// Signal 0 performs the existence check without delivering anything;
// EPERM still means the process exists.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
