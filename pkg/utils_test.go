package pkg

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

// captureLogOutput redirects the global logger to a buffer for the duration of
// fn and returns everything written to it.
func captureLogOutput(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	prev := log.Default()
	logger := log.New(&buf)
	logger.SetLevel(log.DebugLevel)
	log.SetDefault(logger)
	t.Cleanup(func() { log.SetDefault(prev) })

	fn()
	return buf.String()
}

// TestRunCmdCapturesTrailingOutput is the regression test for the
// "read |0: file already closed" error: cmd.Wait() closes the stdout/stderr
// pipes as soon as the child exits, so calling it without joining the reader
// goroutines both drops the tail of the output and surfaces a spurious error.
func TestRunCmdCapturesTrailingOutput(t *testing.T) {
	const lines = 500

	output := captureLogOutput(t, func() {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("for i in $(seq 1 %d); do echo ue5-line-$i; done", lines))
		if err := RunCmd(cmd); err != nil {
			t.Errorf("RunCmd returned error: %v", err)
		}
	})

	// The final line is the first casualty of the Wait/read race.
	if !strings.Contains(output, fmt.Sprintf("ue5-line-%d", lines)) {
		t.Errorf("trailing output was dropped: last line %q missing from captured output", fmt.Sprintf("ue5-line-%d", lines))
	}

	for _, i := range []int{1, lines / 2, lines - 1} {
		if !strings.Contains(output, fmt.Sprintf("ue5-line-%d", i)) {
			t.Errorf("output line %d missing from captured output", i)
		}
	}

	if strings.Contains(output, "Error reading output") {
		t.Errorf("spurious stream error logged:\n%s", output)
	}
}

// TestRunCmdCapturesStderr confirms stderr is drained on the same terms as
// stdout — UBT reports failures there.
func TestRunCmdCapturesStderr(t *testing.T) {
	output := captureLogOutput(t, func() {
		cmd := exec.Command("sh", "-c", "echo boom-on-stderr 1>&2")
		if err := RunCmd(cmd); err != nil {
			t.Errorf("RunCmd returned error: %v", err)
		}
	})

	if !strings.Contains(output, "boom-on-stderr") {
		t.Errorf("stderr output missing from captured output:\n%s", output)
	}
	if strings.Contains(output, "Error reading output") {
		t.Errorf("spurious stream error logged:\n%s", output)
	}
}

// TestRunCmdPropagatesExitCode confirms draining first does not swallow the
// child's failure status.
func TestRunCmdPropagatesExitCode(t *testing.T) {
	captureLogOutput(t, func() {
		cmd := exec.Command("sh", "-c", "echo before-failure; exit 3")
		err := RunCmd(cmd)
		if err == nil {
			t.Fatal("expected non-nil error for exit code 3")
		}
		var exitErr *exec.ExitError
		if !asExitError(err, &exitErr) {
			t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
		}
		if got := exitErr.ExitCode(); got != 3 {
			t.Errorf("exit code = %d, want 3", got)
		}
	})
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

func TestWaitForStreamsReturnsTrueWhenDrained(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		wg.Done()
	}()

	if !WaitForStreams(&wg, time.Second) {
		t.Error("WaitForStreams = false, want true when readers finish before the timeout")
	}
}

// TestWaitForStreamsTimesOut covers the reason the wait is bounded: a
// grandchild process holding an inherited pipe open must not wedge the caller.
func TestWaitForStreamsTimesOut(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	t.Cleanup(wg.Done) // release the never-finishing "reader" at test end

	start := time.Now()
	if WaitForStreams(&wg, 50*time.Millisecond) {
		t.Error("WaitForStreams = true, want false when readers never finish")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("WaitForStreams blocked for %v, want it to give up near the 50ms timeout", elapsed)
	}
}

func TestIsStreamClosedErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "os.ErrClosed", err: os.ErrClosed, want: true},
		{name: "io.ErrClosedPipe", err: io.ErrClosedPipe, want: true},
		{name: "wrapped os.ErrClosed", err: fmt.Errorf("read |0: %w", os.ErrClosed), want: true},
		{name: "PathError wrapping os.ErrClosed", err: &os.PathError{Op: "read", Path: "|0", Err: os.ErrClosed}, want: true},
		{name: "unrelated error", err: io.ErrUnexpectedEOF, want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStreamClosedErr(tt.err); got != tt.want {
				t.Errorf("IsStreamClosedErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
