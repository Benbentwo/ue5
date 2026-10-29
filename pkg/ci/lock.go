package ci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LockDirName is the workspace lock under WorkRoot. Anything else that writes
// the same checkouts (a CI agent job still building the old way, a person
// syncing by hand) can take part with a few lines of shell:
//
//	lock=<work_root>/.workspace-lock
//	until mkdir "$lock" 2>/dev/null; do                 # held: wait, or take it if stale
//	  pid=$(sed -n 's/^pid=//p' "$lock/owner" 2>/dev/null)
//	  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null || rm -rf "$lock"; sleep 30
//	done
//	printf 'pid=%s\nwho=%s\n' <a pid that lives as long as your work> "<who>" > "$lock/owner"
//
// The holder is identified by a process id; a lock whose process is gone is
// stale and may be taken. A lock with no readable owner after a minute is
// treated as stale too (its creator died between mkdir and writing owner).
const LockDirName = ".workspace-lock"

// LockOwner is what a lock's owner file says.
type LockOwner struct {
	PID   int
	Who   string
	Since time.Time
}

// processAlive is a seam for tests.
var processAlive = pidAlive

// ReadLock returns the current holder, or nil when the lock is free or stale.
func ReadLock(root string) *LockOwner {
	dir := filepath.Join(root, LockDirName)
	fi, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "owner"))
	if err != nil {
		if time.Since(fi.ModTime()) < time.Minute {
			return &LockOwner{Who: "(starting)", Since: fi.ModTime()}
		}
		return nil
	}
	o := LockOwner{Since: fi.ModTime()}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "pid":
			o.PID, _ = strconv.Atoi(v)
		case "who":
			o.Who = v
		}
	}
	if o.PID <= 0 || !processAlive(o.PID) {
		return nil
	}
	return &o
}

// ErrLockHeld means someone else is using the workspace.
var ErrLockHeld = errors.New("workspace in use")

// AcquireLock takes the lock for this process, clearing a stale one. The
// returned func releases it (only if it is still ours).
func AcquireLock(root, who string) (func(), error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, LockDirName)
	for attempt := 0; attempt < 2; attempt++ {
		if err := os.Mkdir(dir, 0o755); err == nil {
			pid := os.Getpid()
			owner := fmt.Sprintf("pid=%d\nwho=%s\n", pid, who)
			if err := os.WriteFile(filepath.Join(dir, "owner"), []byte(owner), 0o644); err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
			return func() {
				if b, err := os.ReadFile(filepath.Join(dir, "owner")); err == nil && strings.Contains(string(b), fmt.Sprintf("pid=%d\n", pid)) {
					_ = os.RemoveAll(dir)
				}
			}, nil
		} else if !os.IsExist(err) {
			return nil, err
		}
		if holder := ReadLock(root); holder != nil {
			return nil, fmt.Errorf("%w by %s", ErrLockHeld, holder.Who)
		}
		_ = os.RemoveAll(dir) // stale
	}
	return nil, fmt.Errorf("%w: could not take %s", ErrLockHeld, dir)
}
