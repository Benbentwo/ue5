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
	if !strings.Contains(log.String(), "  lfs: ") {
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

// A shallow checkout whose shallow record names a commit the server never had
// (as when another tool's checkout fetched a merge commit that was later
// deleted) must not wedge the sync: it forgets the history and fetches it whole.
func TestWorkspaceSyncRecoversFromAStaleShallowRecord(t *testing.T) {
	if exec.Command("git", "lfs", "version").Run() != nil {
		t.Skip("git-lfs not installed")
	}
	gameURL, _, gameNew := origin(t, "Game")
	w := Workspace{Root: t.TempDir()}
	j := &Job{Request: Request{Game: "Game", Repo: gameURL, SHA: gameNew}}
	var log bytes.Buffer
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	dir := w.GameDir("Game")
	// A commit that exists only here, recorded as a shallow boundary.
	_ = os.WriteFile(filepath.Join(dir, "local.txt"), []byte("x"), 0o644)
	gitT(t, dir, "add", "local.txt")
	gitT(t, dir, "commit", "-qm", "local only")
	stray := gitT(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, ".git", "shallow"), []byte(stray+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Some servers reject a fetch that names an unknown shallow boundary (the sync
	// then calls forgetHistory); git's own file:// server tolerates it, so drive
	// the recovery step directly and prove a sync from that state is whole.
	log.Reset()
	if err := w.forgetHistory(context.Background(), dir, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if refs := gitT(t, dir, "for-each-ref"); refs != "" {
		t.Fatalf("refs left behind: %s", refs)
	}
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("sync after reset: %v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "two" {
		t.Fatalf("game file %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "shallow")); !os.IsNotExist(err) {
		t.Fatalf("the checkout must no longer be shallow\n%s", log.String())
	}
	if tags := gitT(t, dir, "tag"); !strings.Contains(tags, "v0.1.0") {
		t.Fatalf("tags %q", tags)
	}
}

// A stale shallow record inside a submodule (not the game repo) is healed too.
func TestForgetSubmoduleHistoryClearsAStaleShallowSubmodule(t *testing.T) {
	if exec.Command("git", "lfs", "version").Run() != nil {
		t.Skip("git-lfs not installed")
	}
	// Local file:// submodules are refused by default since git 2.38.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	subURL, _, _ := origin(t, "Tools")
	gameURL, _, _ := origin(t, "Game")
	gameDir := gameURL[len("file://"):]
	gitT(t, gameDir, "submodule", "add", "-q", subURL, "tools")
	gitT(t, gameDir, "commit", "-qm", "add tools")
	sha := gitT(t, gameDir, "rev-parse", "HEAD")

	w := Workspace{Root: t.TempDir()}
	j := &Job{Request: Request{Game: "Game", Repo: gameURL, SHA: sha}}
	var log bytes.Buffer
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	sub := filepath.Join(w.GameDir("Game"), "tools")
	if _, err := os.Stat(filepath.Join(sub, "f.txt")); err != nil {
		t.Skipf("this sync does not check submodules out here: %v", err)
	}
	_ = os.WriteFile(filepath.Join(sub, "local.txt"), []byte("x"), 0o644)
	gitT(t, sub, "add", "local.txt")
	gitT(t, sub, "commit", "-qm", "local only")
	stray := gitT(t, sub, "rev-parse", "HEAD")
	shallow, err := w.shallowFile(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shallow, []byte(stray+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log.Reset()
	healed, err := w.forgetSubmoduleHistory(context.Background(), w.GameDir("Game"), &log)
	if err != nil || !healed {
		t.Fatalf("healed=%v err=%v\n%s", healed, err, log.String())
	}
	if _, err := os.Stat(shallow); !os.IsNotExist(err) {
		t.Fatal("the submodule's shallow record must be gone")
	}
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("sync after heal: %v\n%s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(sub, "f.txt")); string(got) != "two" {
		t.Fatalf("submodule file %q", got)
	}
}

// With LFSPrune on, a sync keeps only the LFS objects its commit uses.
func TestWorkspaceSyncPrunesLFSObjectsTheCommitDoesNotUse(t *testing.T) {
	if exec.Command("git", "lfs", "version").Run() != nil {
		t.Skip("git-lfs not installed")
	}
	dir := filepath.Join(t.TempDir(), "LfsGame")
	_ = os.MkdirAll(dir, 0o755)
	gitT(t, dir, "init", "-q")
	gitT(t, dir, "lfs", "install", "--local")
	gitT(t, dir, "lfs", "track", "*.bin")
	_ = os.WriteFile(filepath.Join(dir, "big.bin"), bytes.Repeat([]byte("old-"), 4096), 0o644)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "v1")
	v1 := gitT(t, dir, "rev-parse", "HEAD")
	oldOid := strings.Fields(gitT(t, dir, "lfs", "ls-files", "-l"))[0]
	_ = os.WriteFile(filepath.Join(dir, "big.bin"), bytes.Repeat([]byte("new-"), 4096), 0o644)
	gitT(t, dir, "commit", "-qam", "v2")
	v2 := gitT(t, dir, "rev-parse", "HEAD")

	w := Workspace{Root: t.TempDir(), LFSPrune: true}
	j := &Job{Request: Request{Game: "LfsGame", Repo: "file://" + dir, SHA: v1}}
	var log bytes.Buffer
	if err := w.Sync(context.Background(), j, &log); err != nil {
		if strings.Contains(strings.ToLower(log.String()), "not supported") {
			t.Skip("this git-lfs cannot fetch from a file:// remote")
		}
		t.Fatalf("%v\n%s", err, log.String())
	}
	object := filepath.Join(w.GameDir("LfsGame"), ".git", "lfs", "objects", oldOid[0:2], oldOid[2:4], oldOid)
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("v1's object must be present after syncing v1: %v\n%s", err, log.String())
	}
	j.SHA = v2
	if err := w.Sync(context.Background(), j, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if _, err := os.Stat(object); !os.IsNotExist(err) {
		t.Fatalf("v1's object should be pruned after syncing v2\n%s", log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(w.GameDir("LfsGame"), "big.bin")); !bytes.HasPrefix(got, []byte("new-")) {
		t.Fatalf("big.bin is not the v2 content (%d bytes)", len(got))
	}
}
