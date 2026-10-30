//go:build windows

package server

import (
	"errors"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "ue5-daemon"
	// legacyTaskName is the Scheduled Task earlier versions created. Creating
	// a logon task needs an elevated prompt, so it is no longer used.
	legacyTaskName = "ue5-daemon"
)

// InstallAutostart starts the daemon when the current user logs on, using the
// per-user Run key (no administrator rights needed), and starts it now. It runs
// in that user's desktop session as that user, never elevated.
func InstallAutostart(bin string) (string, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return "", fmt.Errorf("open HKCU\\%s: %w", runKeyPath, err)
	}
	defer func() { _ = k.Close() }()
	// "server start" spawns the daemon without a window and exits, so the
	// console Windows opens for it at logon closes straight away.
	if err := k.SetStringValue(runValueName, fmt.Sprintf(`"%s" server start`, bin)); err != nil {
		return "", fmt.Errorf("write HKCU\\%s\\%s: %w", runKeyPath, runValueName, err)
	}
	if err := EnsureDaemonAt(bin); err != nil {
		return "", fmt.Errorf("autostart installed, but the daemon did not start now: %w", err)
	}
	return `HKCU\` + runKeyPath + `\` + runValueName + " (at logon)", nil
}

// UninstallAutostart removes the Run entry (and a Scheduled Task left by an
// earlier version, where this account may delete it); a running daemon keeps running.
func UninstallAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err == nil {
		err = k.DeleteValue(runValueName)
		_ = k.Close()
	}
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove HKCU\\%s\\%s: %w", runKeyPath, runValueName, err)
	}
	if legacyTaskInstalled() {
		if out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", legacyTaskName).CombinedOutput(); err != nil {
			return fmt.Errorf("the Run entry is removed, but Scheduled Task %s remains (delete it from an administrator prompt): %v: %s", legacyTaskName, err, out)
		}
	}
	return nil
}

// AutostartStatus describes what is installed.
func AutostartStatus() string {
	status := "not installed"
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue(runValueName); err == nil {
			status = "installed: " + v
		}
		_ = k.Close()
	}
	if legacyTaskInstalled() {
		status += "; also Scheduled Task " + legacyTaskName + " from an earlier version (remove it with 'ue5 server autostart uninstall')"
	}
	return status
}

func legacyTaskInstalled() bool {
	return exec.Command("schtasks", "/Query", "/TN", legacyTaskName).Run() == nil
}
