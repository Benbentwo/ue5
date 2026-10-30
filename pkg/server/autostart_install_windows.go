//go:build windows

package server

import (
	"fmt"
	"os/exec"
)

const scheduledTaskName = "ue5-daemon"

// InstallAutostart registers a Scheduled Task that starts the daemon when the
// current user logs on, and starts it now. It runs in that user's session, so
// builds get a desktop session rather than a service's Session 0.
func InstallAutostart(bin string) (string, error) {
	out, err := exec.Command("schtasks", "/Create", "/F", "/TN", scheduledTaskName,
		"/SC", "ONLOGON", "/RL", "LIMITED", "/TR", fmt.Sprintf(`"%s" server daemon`, bin)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("schtasks /Create: %v: %s", err, out)
	}
	_ = exec.Command("schtasks", "/Run", "/TN", scheduledTaskName).Run()
	return "Scheduled Task " + scheduledTaskName + " (at logon)", nil
}

// UninstallAutostart deletes the Scheduled Task; a running daemon keeps running.
func UninstallAutostart() error {
	if out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", scheduledTaskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Delete: %v: %s", err, out)
	}
	return nil
}

// AutostartStatus describes what is installed.
func AutostartStatus() string {
	if exec.Command("schtasks", "/Query", "/TN", scheduledTaskName).Run() != nil {
		return "not installed"
	}
	return "installed: Scheduled Task " + scheduledTaskName
}
