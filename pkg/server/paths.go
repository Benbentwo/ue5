package server

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ue5Dir               = ".ue5"
	socketName           = "server.sock"
	pidFileName          = "daemon.pid"
	stateFileName        = "state.json"
	instancesFileName    = "instances.json"
	logsDirName          = "logs"
	buildLogName         = "build.log"
	mcpDefaultPort       = "9515"
	dashboardDefaultPort = "9516"
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

// InstancesFile returns the path to the instance discovery registry.
func InstancesFile() string {
	return filepath.Join(HomeDir(), instancesFileName)
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

// BuildLogFile returns the build log path for a given project.
func BuildLogFile(projectPath string) string {
	return filepath.Join(LogDir(projectPath), buildLogName)
}

// MCPAddr returns the MCP server listen address.
func MCPAddr() string {
	if port := os.Getenv("UE5_MCP_PORT"); port != "" {
		return ":" + port
	}
	return ":" + mcpDefaultPort
}

// DashboardAddr returns the dashboard server listen address.
func DashboardAddr() string {
	if port := os.Getenv("UE5_DASHBOARD_PORT"); port != "" {
		return ":" + port
	}
	return ":" + dashboardDefaultPort
}

// projectHash returns the first 12 characters of the SHA-256 hash of the project path.
func projectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("%x", h)[:12]
}
