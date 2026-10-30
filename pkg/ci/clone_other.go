//go:build !darwin

package ci

import "errors"

// cloneFile is only implemented on macOS (APFS). Elsewhere the caller falls
// back to `git lfs checkout`, which writes full copies.
func cloneFile(src, dst string) error { return errors.ErrUnsupported }
