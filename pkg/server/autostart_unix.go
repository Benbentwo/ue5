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
