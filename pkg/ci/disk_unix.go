//go:build !windows

package ci

import "golang.org/x/sys/unix"

// FreeGB is the space available to this user on the volume holding path.
func FreeGB(path string) (int, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int(st.Bavail * uint64(st.Bsize) / (1 << 30)), nil
}
