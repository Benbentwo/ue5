//go:build windows

package ci

import (
	"context"
	"os/exec"
	"strconv"
)

// recipeCommand runs a game's ci/<kind>.ps1 under Windows PowerShell. A cancel
// or timeout kills the whole process tree (RunUAT, UBT, the cooker).
func recipeCommand(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
	return cmd
}

// recipeExt is the recipe file extension on this OS.
const recipeExt = ".ps1"
