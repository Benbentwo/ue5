//go:build darwin || linux

package server

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// processArgsAndAge returns a process's command line and how long it has
// been running, or zero values when ps cannot say.
func processArgsAndAge(pid int) (string, time.Duration) {
	out, err := exec.Command("ps", "-ww", "-o", "etime=,args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", 0
	}
	fields := strings.SplitN(strings.TrimSpace(string(out)), " ", 2)
	if len(fields) != 2 {
		return "", 0
	}
	return strings.TrimSpace(fields[1]), parseEtime(fields[0])
}
