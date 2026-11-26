//go:build !windows

package ci

import (
	"context"
	"os/exec"
	"syscall"
)

// detach is a no-op off Windows: children never share a console there.
func detach(*exec.Cmd) {}

// recipeCommand runs a game's ci/<kind>.sh under bash, in its own process
// group so a cancel or timeout stops RunUAT and everything it started.
func recipeCommand(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/bash", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}

// probeCommand is a trivial program that must start and exit 0.
func probeCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
}

// recipeExt is the recipe file extension on this OS.
const recipeExt = ".sh"
