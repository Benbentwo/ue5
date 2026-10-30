//go:build windows

package ci

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCopyFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("CopyFileW")

// cloneFile copies src to dst with CopyFileW. On a ReFS volume (a Windows 11
// Dev Drive, for one) Windows 11 24H2+ performs that copy as a block clone,
// sharing src's clusters at no extra disk cost; on NTFS it is an ordinary full
// copy, which costs what `git lfs checkout` would have.
func cloneFile(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	s, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	d, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	r, _, callErr := procCopyFileW.Call(uintptrOf(s), uintptrOf(d), 1) // bFailIfExists
	if r == 0 {
		return callErr
	}
	return nil
}

func uintptrOf(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }
