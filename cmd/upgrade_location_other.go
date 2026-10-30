//go:build !windows

package cmd

// warnInstallLocation only matters on Windows, where CI agents run as a service account.
func warnInstallLocation(string) {}
