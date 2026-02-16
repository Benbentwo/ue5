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
