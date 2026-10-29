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
	cmd := exec.Command("git", append([]string{"-c", "user.email=ci@test", "-c", "user.name=ci", "-c", "init.defaultBranch=main"}, args...)...)
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
