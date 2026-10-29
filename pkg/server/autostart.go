package server

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// EnsureDaemon checks for a running daemon and starts one if needed.
// It checks if the socket file exists, attempts a Ping, and if either fails,
// spawns the daemon as a detached process and polls until it responds.
func EnsureDaemon() error {
	// Find our own executable to spawn the daemon
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}
	return EnsureDaemonAt(self)
}

// EnsureDaemonAt is EnsureDaemon spawning an explicit binary instead of
// os.Executable(). The upgrade flow needs this: after the binary swap,
// os.Executable() still describes the process's replaced (unlinked) image,
// while the install path holds the new version to start the daemon from.
func EnsureDaemonAt(binPath string) error {
	client := NewClient()

	// Check if already running
	if client.IsRunning() {
		return nil
	}

	log.Info("Daemon not running, starting it")

	// Spawn daemon as detached process
	cmd := exec.Command(binPath, "server", "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	// Detach from parent process group
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Release the child process so it doesn't become a zombie
	_ = cmd.Process.Release()

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

// StopDaemonAndWait asks a running daemon to shut down and waits until it has
// fully exited: the socket stops answering AND the recorded daemon process is
// gone. Waiting matters when a new daemon starts right after — the old
// process removes its socket and PID file during teardown, and starting the
// replacement too early lets that teardown delete the new daemon's files.
// Returns nil if no daemon was running.
func StopDaemonAndWait(timeout time.Duration) error {
	client := NewClient()
	if !client.IsRunning() {
		return nil
	}

	pid := readDaemonPID()

	if _, err := client.Send(Request{ID: "shutdown", Type: ReqShutdown}); err != nil {
		return fmt.Errorf("failed to send shutdown request: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !client.IsRunning() && (pid == 0 || !processAlive(pid)) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not exit within %s", timeout)
}

// readDaemonPID returns the PID from the daemon's PID file, or 0 if unknown.
func readDaemonPID() int {
	data, err := os.ReadFile(DaemonPIDFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
