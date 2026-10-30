//go:build windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Benbentwo/ue5/pkg/ci"
	"github.com/Benbentwo/ue5/pkg/server"
)

// warnInstallLocation flags a binary installed inside the user's profile on a
// build box: a CI agent service runs as another account and cannot run it.
func warnInstallLocation(installed string) {
	cfg, err := ci.LoadConfig(server.HomeDir())
	if err != nil || !cfg.Enabled {
		return
	}
	profile, err := os.UserHomeDir()
	if err != nil || profile == "" {
		return
	}
	rel, err := filepath.Rel(profile, installed)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	fmt.Printf("\nWarning: %s is inside your user profile. A CI agent service running as\n", installed)
	fmt.Println("another account cannot run it. Install ue5.exe in a shared folder on the")
	fmt.Println(`system PATH (for example C:\ProgramData\ue5) and remove this copy.`)
}
