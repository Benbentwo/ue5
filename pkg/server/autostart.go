package server

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/log"
)

// EnsureDaemon checks for a running daemon and starts one if needed.
// It checks if the socket file exists, attempts a Ping, and if either fails,
// spawns the daemon as a detached process and polls until it responds.
func EnsureDaemon() error {
	client := NewClient()

	// Check if already running
	if client.IsRunning() {
		return nil
	}

	log.Info("Daemon not running, starting it")

	// Find our own executable to spawn the daemon
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}

	// Spawn daemon as detached process
	cmd := exec.Command(self, "server", "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	// Detach from parent process group
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Release the child process so it doesn't become a zombie
	cmd.Process.Release()

	// Poll until the daemon is responsive (up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if client.IsRunning() {
			log.Info("Daemon started successfully")
			return nil
		}
	}

	return fmt.Errorf("daemon did not become responsive within 5 seconds")
}
