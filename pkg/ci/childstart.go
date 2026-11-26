package ci

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// statusDLLInitFailed is the Windows exit status of a process that could not
// initialise (STATUS_DLL_INIT_FAILED). From a service it almost always means the
// session's desktop heap is exhausted or the service lost its console, not that
// the program itself is broken.
const statusDLLInitFailed = 0xC0000142

// childStartHint explains a 0xC0000142 in words an operator can act on.
const childStartHint = "this service cannot start child processes (0xC0000142: the session's desktop heap is exhausted or the service lost its console); restart the service from an interactive session"

// isChildStartCode reports whether a process exit code is STATUS_DLL_INIT_FAILED.
func isChildStartCode(code int) bool {
	return uint32(code) == statusDLLInitFailed
}

// childStartFailure reports whether err is a child that died at start with
// STATUS_DLL_INIT_FAILED.
func childStartFailure(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && isChildStartCode(exit.ExitCode())
}

// checkChildStart starts a trivial console program the way every job step is
// started. A job that cannot even do that fails at once with a clear reason,
// instead of a bare exit status from its first git call.
func checkChildStart(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := probeCommand(ctx).Run()
	if err == nil {
		return nil
	}
	if childStartFailure(err) {
		return errors.New(childStartHint)
	}
	return fmt.Errorf("could not start a trivial child process: %w", err)
}
