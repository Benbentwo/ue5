//go:build windows

package ci

import (
	"errors"

	"golang.org/x/sys/windows"
)

// pidAlive reports whether a process exists. A process owned by another account
// (a CI agent running as NetworkService, say) can refuse even a limited query;
// access-denied still proves it exists, so it counts as alive and its lock holds.
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == 259 // STILL_ACTIVE
}
