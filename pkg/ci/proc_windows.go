//go:build windows

package ci

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow gives a console program its own hidden console instead of the
// service's. A service started from a terminal that later closed, or whose
// session lost its console, otherwise makes every console child (git, cmd,
// powershell) die at start with 0xC0000142 or fail to set its window title.
const createNoWindow = 0x08000000

// detach makes cmd independent of the service's own console.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// recipeCommand runs a game's ci/<kind>.ps1 under Windows PowerShell. A cancel
// or timeout kills the whole process tree (RunUAT, UBT, the cooker).
func recipeCommand(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	detach(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		detach(kill)
		return kill.Run()
	}
	return cmd
}

// probeCommand is a trivial console program that must start and exit 0.
func probeCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe", "/c", "exit", "0")
	detach(cmd)
	return cmd
}

// recipeExt is the recipe file extension on this OS.
const recipeExt = ".ps1"
