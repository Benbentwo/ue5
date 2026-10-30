//go:build darwin

package ci

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile makes dst a copy-on-write clone of src (APFS clonefile): it shares
// src's blocks, so it costs no disk until one side is modified.
func cloneFile(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
