package pkg

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// StreamDrainTimeout bounds how long a caller waits for stream-reader
// goroutines to finish after a child process exits. Draining is normally
// instantaneous — the pipes hit EOF the moment the last writer closes them.
// The bound exists because a grandchild process that inherited the pipe (UBA
// workers, ShaderCompileWorker, CrashReportClient) can hold it open
// indefinitely, and wedging a build or the daemon is worse than losing a few
// trailing log lines.
const StreamDrainTimeout = 5 * time.Second

func GetPlatform() string {
	// Convert the OS to a platform string
	platform := OsToPlatform(runtime.GOOS)
	if platform == "" {
		log.Error("Unsupported operating system", "os", runtime.GOOS)
		return ""
	}
	return platform
}

func OsToPlatform(os string) string {
	switch os {
	case "darwin":
		return "Mac"
	case "linux":
		return "Linux"
	case "windows":
		return "Win64"
	default:
		return ""
	}
}

// WaitForStreams blocks until wg reaches zero or timeout elapses, reporting
// true if the readers drained and false if it gave up. Use it to join
// stream-reader goroutines before cmd.Wait(): Wait closes the pipes returned
// by StdoutPipe/StderrPipe as soon as the child exits, so calling it first
// races the readers — it truncates trailing output and makes the reader's next
// Read fail with "file already closed".
func WaitForStreams(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// IsStreamClosedErr reports whether err is the benign "the pipe was closed
// underneath us" error, as opposed to a real read failure worth surfacing.
// It shows up whenever a reader is still blocked in Read while the pipe's
// owner closes it during teardown.
func IsStreamClosedErr(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}

func RunCmd(cmd *exec.Cmd) error {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Stream stdout and stderr, tracking both readers so we can drain them
	// before Wait() closes the pipes out from under them.
	var streams sync.WaitGroup
	streams.Add(2)
	go func() {
		defer streams.Done()
		streamOutput(stdoutPipe, "stdout")
	}()
	go func() {
		defer streams.Done()
		streamOutput(stderrPipe, "stderr")
	}()

	if !WaitForStreams(&streams, StreamDrainTimeout) {
		log.Warn("Timed out draining command output; trailing lines may be missing", "timeout", StreamDrainTimeout)
	}

	// Wait for command to finish
	return cmd.Wait()
}

func streamOutput(r io.Reader, label string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parsed := ParseLine(line)

		switch parsed.Level {
		case LogLevelError:
			log.WithPrefix("|").Error(line)
		case LogLevelWarning:
			log.WithPrefix("|").Warn(line)
		default:
			log.WithPrefix("|").Info(line)
		}
	}
	if err := scanner.Err(); err != nil && !IsStreamClosedErr(err) {
		log.Error("Error reading output", "label", label, "error", err)
	}
}

func OsStringSliceSwitcher(Windows []string, Mac []string, Linux []string) []string {
	// Switch based on the current operating system
	switch runtime.GOOS {
	case "windows":
		return Windows
	case "darwin":
		return Mac
	case "linux":
		return Linux // Default to Mac for Linux as well
	default:
		log.Error("Unsupported operating system", "os", runtime.GOOS)
		return nil
	}
}
