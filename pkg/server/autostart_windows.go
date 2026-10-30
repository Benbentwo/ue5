//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the daemon gets its own hidden console,
// so it outlives the terminal that started it (closing a console ends every
// process attached to it), and the console tools it runs (git, PowerShell
// recipes) share that hidden console instead of each opening a window.
const createNoWindow = 0x08000000

// setSysProcAttr configures the command to run as a detached process on Windows.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
	}
}

// processAlive reports whether a process with the given PID exists.
// Windows has no cheap signal-0 equivalent; the socket liveness check in
// StopDaemonAndWait is the effective wait there.
func processAlive(pid int) bool {
	return false
}
