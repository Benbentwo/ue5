package ci

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// toolDirs are where git, git-lfs and friends usually live on a macOS or Linux
// box. A daemon started by launchd or systemd gets a minimal PATH without them,
// and then every sync fails ("git-lfs was not found on your path").
var toolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"}

// withToolPath returns path with any existing toolDirs appended that it lacks.
// exists is a seam for tests.
func withToolPath(path string, exists func(string) bool) string {
	have := map[string]bool{}
	for _, p := range filepath.SplitList(path) {
		have[filepath.Clean(p)] = true
	}
	parts := []string{}
	if path != "" {
		parts = append(parts, path)
	}
	for _, d := range toolDirs {
		if !have[d] && exists(d) {
			parts = append(parts, d)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// ensureToolPath extends this process's PATH so git and recipes find their tools
// however the daemon was started. Windows installers put tools on the system PATH,
// so it only acts on other systems.
func ensureToolPath() {
	if runtime.GOOS == "windows" {
		return
	}
	isDir := func(d string) bool { fi, err := os.Stat(d); return err == nil && fi.IsDir() }
	_ = os.Setenv("PATH", withToolPath(os.Getenv("PATH"), isDir))
}
