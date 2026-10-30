//go:build !darwin && !windows

package ci

import "errors"

// cloneFile is implemented on macOS (APFS) and Windows. Elsewhere the caller falls
// back to `git lfs checkout`, which writes full copies.
func cloneFile(src, dst string) error { return errors.ErrUnsupported }
