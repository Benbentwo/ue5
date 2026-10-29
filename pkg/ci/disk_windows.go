//go:build windows

package ci

import "golang.org/x/sys/windows"

// FreeGB is the space available to this user on the volume holding path.
func FreeGB(path string) (int, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}
	return int(avail / (1 << 30)), nil
}
