//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr configures the command to run as a detached process on Windows.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// processAlive reports whether a process with the given PID exists.
// Windows has no cheap signal-0 equivalent; the socket liveness check in
// StopDaemonAndWait is the effective wait there.
func processAlive(pid int) bool {
	return false
}
