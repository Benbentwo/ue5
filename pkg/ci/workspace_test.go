package ci

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=ci@test", "-c", "user.name=ci", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// origin makes a local repo with two commits and returns its URL and SHAs.
func origin(t *testing.T, name string) (string, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	_ = os.MkdirAll(dir, 0o755)
	gitT(t, dir, "init", "-q")
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one"), 0o644)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "one")
	first := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "tag", "v0.1.0")
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two"), 0o644)
	gitT(t, dir, "commit", "-qam", "two")
	return "file://" + dir, first, gitT(t, dir, "rev-parse", "HEAD")
}

func TestWorkspaceSyncChecksOutPinnedCommits(t *testing.T) {
	if exec.Command("git", "lfs", "version").Run() != nil {
		t.Skip("git-lfs not installed")
	}
	gameURL, gameOld, gameNew := origin(t, "Game")
	plugURL, plugOld, _ := origin(t, "SharedPlugins")
	w := Workspace{Root: t.TempDir()}
	j := &Job{Request: Request{Game: "Game", Repo: gameURL, SHA: gameNew,
		Plugins: map[string]Pinned{"SharedPlugins": {Repo: plugURL, SHA: plugOld}}}}
	var log bytes.Buffer
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(w.GameDir("Game"), "f.txt")); string(got) != "two" {
		t.Fatalf("game file %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(w.PluginDir("SharedPlugins"), "f.txt")); string(got) != "one" {
		t.Fatalf("plugin must sit at its pinned (older) commit, got %q", got)
	}
	// The game gets tags (version scripts need them).
	if tags := gitT(t, w.GameDir("Game"), "tag"); !strings.Contains(tags, "v0.1.0") {
		t.Fatalf("tags %q", tags)
	}
	// Moving back to an older commit, with an untracked file and ignored output.
	_ = os.WriteFile(filepath.Join(w.GameDir("Game"), "stray.txt"), []byte("x"), 0o644)
	j.SHA = gameOld
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(w.GameDir("Game"), "f.txt")); string(got) != "one" {
		t.Fatalf("after resync %q", got)
	}
	if _, err := os.Stat(filepath.Join(w.GameDir("Game"), "stray.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked files must be cleaned")
	}
}

func TestWorkspaceSyncFillsLFSFiles(t *testing.T) {
	if exec.Command("git", "lfs", "version").Run() != nil {
		t.Skip("git-lfs not installed")
	}
	dir := filepath.Join(t.TempDir(), "LfsGame")
	_ = os.MkdirAll(dir, 0o755)
	gitT(t, dir, "init", "-q")
	gitT(t, dir, "lfs", "install", "--local")
	gitT(t, dir, "lfs", "track", "*.bin")
	payload := bytes.Repeat([]byte("lfs-payload-"), 4096)
	_ = os.WriteFile(filepath.Join(dir, "big.bin"), payload, 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "sub dir"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sub dir", "two.bin"), append(payload, 'x'), 0o644)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "lfs")
	sha := gitT(t, dir, "rev-parse", "HEAD")

	w := Workspace{Root: t.TempDir()}
	j := &Job{Request: Request{Game: "LfsGame", Repo: "file://" + dir, SHA: sha}}
	var log bytes.Buffer
	if err := w.Sync(context.Background(), j, &log); err != nil {
		if strings.Contains(log.String(), "file://") && strings.Contains(strings.ToLower(log.String()), "not supported") {
			t.Skip("this git-lfs cannot fetch from a file:// remote")
		}
		t.Fatalf("%v\n%s", err, log.String())
	}
	got, _ := os.ReadFile(filepath.Join(w.GameDir("LfsGame"), "big.bin"))
	if !bytes.Equal(got, payload) {
		t.Fatalf("big.bin holds %d bytes, want the %d-byte object (a pointer was left?)\n%s", len(got), len(payload), log.String())
	}
	got2, _ := os.ReadFile(filepath.Join(w.GameDir("LfsGame"), "sub dir", "two.bin"))
	if len(got2) != len(payload)+1 {
		t.Fatalf("a path with a space was not filled in: %d bytes", len(got2))
	}
	if !strings.Contains(log.String(), "lfs: 2 file(s) filled") {
		t.Fatalf("no lfs summary line in the log:\n%s", log.String())
	}
	if strings.Contains(log.String(), "dedup failed") {
		t.Fatalf("dedup should not run after a clone fill-in:\n%s", log.String())
	}
	// A second sync of the same commit keeps the files intact.
	log.Reset()
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("resync: %v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(w.GameDir("LfsGame"), "big.bin")); !bytes.Equal(got, payload) {
		t.Fatalf("resync broke big.bin (%d bytes)\n%s", len(got), log.String())
	}
	t.Log(strings.TrimSpace(log.String()[strings.Index(log.String(), "lfs: "):]))
}
