//go:build !windows

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A pid file this user cannot write (left by a daemon once run with sudo) is
// replaced rather than stopping the daemon from starting.
func TestWritePIDFileReplacesAnUnwritableStaleFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write any file")
	}
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DaemonPIDFile(), []byte("1"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := (&Daemon{}).writePIDFile(); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}
	got, _ := os.ReadFile(filepath.Clean(DaemonPIDFile()))
	if string(got) != fmt.Sprint(os.Getpid()) {
		t.Fatalf("pid file holds %q", got)
	}
}
