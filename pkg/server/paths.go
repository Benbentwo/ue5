package server

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ue5Dir       = ".ue5"
	socketName   = "server.sock"
	pidFileName  = "daemon.pid"
	stateFileName = "state.json"
	logsDirName  = "logs"
)

// HomeDir returns the base directory for ue5 server state (~/.ue5/).
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ue5Dir)
}

// SocketPath returns the path to the Unix domain socket.
func SocketPath() string {
	return filepath.Join(HomeDir(), socketName)
}

// DaemonPIDFile returns the path to the daemon PID file.
func DaemonPIDFile() string {
	return filepath.Join(HomeDir(), pidFileName)
}

// StateFile returns the path to the state persistence file.
func StateFile() string {
	return filepath.Join(HomeDir(), stateFileName)
}

// LogDir returns the log directory for a given project path.
// The project path is hashed to create a filesystem-safe directory name.
func LogDir(projectPath string) string {
	return filepath.Join(HomeDir(), logsDirName, projectHash(projectPath))
}

// LogFile returns the log file path for a given project.
func LogFile(projectPath string) string {
	return filepath.Join(LogDir(projectPath), "editor.log")
}

// EnsureHomeDir creates the ~/.ue5/ directory if it doesn't exist.
func EnsureHomeDir() error {
	return os.MkdirAll(HomeDir(), 0755)
}

// EnsureLogDir creates the log directory for a project if it doesn't exist.
func EnsureLogDir(projectPath string) error {
	return os.MkdirAll(LogDir(projectPath), 0755)
}

// projectHash returns the first 12 characters of the SHA-256 hash of the project path.
func projectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("%x", h)[:12]
}
