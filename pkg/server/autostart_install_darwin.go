//go:build darwin

package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const launchAgentLabel = "dev.ue5.daemon"

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// InstallAutostart registers a per-user LaunchAgent that starts the daemon at
// login, and again if it crashes (a clean exit, as `ue5 server stop` makes, is
// left alone).
func InstallAutostart(bin string) (string, error) {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>server</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, launchAgentLabel, bin, filepath.Join(HomeDir(), "autostart.log"), filepath.Join(HomeDir(), "autostart.log"))
	path := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := EnsureHomeDir(); err != nil {
		return "", err
	}
	_ = exec.Command("launchctl", "bootout", guiDomain()+"/"+launchAgentLabel).Run()
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("launchctl", "bootstrap", guiDomain(), path).CombinedOutput(); err != nil {
		return "", fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return "LaunchAgent " + path, nil
}

// UninstallAutostart removes the LaunchAgent; a running daemon keeps running.
func UninstallAutostart() error {
	_ = exec.Command("launchctl", "bootout", guiDomain()+"/"+launchAgentLabel).Run()
	if err := os.Remove(launchAgentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AutostartStatus describes what is installed.
func AutostartStatus() string {
	if _, err := os.Stat(launchAgentPath()); err != nil {
		return "not installed"
	}
	return "installed: LaunchAgent " + launchAgentPath()
}

func guiDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
