//go:build !darwin && !windows

package server

import "errors"

var errAutostartUnsupported = errors.New("autostart is implemented for macOS (LaunchAgent) and Windows (Scheduled Task); use your init system here, running `ue5 server daemon`")

func InstallAutostart(string) (string, error) { return "", errAutostartUnsupported }
func UninstallAutostart() error               { return errAutostartUnsupported }
func AutostartStatus() string                 { return "not supported on this platform" }
