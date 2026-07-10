//go:build darwin || linux

package pkg

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestReplaceBinarySwapsInode is the contract that keeps a running daemon
// alive through upgrades: the installed path must get a NEW inode via
// rename(2), never a truncate-and-rewrite of the existing one. Overwriting
// the inode a running process executes from invalidates its code signature
// on macOS and the kernel SIGKILLs it on the next page fault — the
// 2026-07-09 v0.3.25 upgrade killed the daemon exactly this way.
func TestReplaceBinarySwapsInode(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "ue5")
	if err := os.WriteFile(target, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	inodeBefore := inodeOf(t, target)

	// Source lives elsewhere (the real flow stages from a temp download dir).
	srcDir := filepath.Join(dir, "download")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "ue5")
	if err := os.WriteFile(src, []byte("new-binary"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(src, target); err != nil {
		t.Fatalf("replaceBinary failed: %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-binary" {
		t.Errorf("target content = %q, want new binary", content)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("installed binary mode = %v, want 0755", info.Mode().Perm())
	}

	if inodeAfter := inodeOf(t, target); inodeAfter == inodeBefore {
		t.Errorf("target kept inode %d: replacement rewrote the running binary in place instead of renaming a new inode over it", inodeBefore)
	}

	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Errorf("staging file %s.new left behind (err=%v)", target, err)
	}
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("stat does not expose inode on this platform")
	}
	return st.Ino
}
