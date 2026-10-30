package ci

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Workspace syncs the repos a job needs into WorkRoot: one checkout per repo,
// shared by every game on the box, LFS objects deduplicated into copy-on-write
// clones where the volume supports it (APFS, Btrfs, ReFS).
// Only one job runs per box, so nothing else writes these checkouts meanwhile.
type Workspace struct {
	Root       string
	PluginsDir string // relative to Root; default Shared/Plugins
	PAT        string // Azure PAT for fetches; empty uses whatever git credentials the user has
}

// GameDir is where a game is checked out.
func (w Workspace) GameDir(game string) string { return filepath.Join(w.Root, game) }

// PluginDir is where a shared plugin repo is checked out.
func (w Workspace) PluginDir(name string) string {
	dir := w.PluginsDir
	if dir == "" {
		dir = filepath.Join("Shared", "Plugins")
	}
	return filepath.Join(w.Root, dir, name)
}

// Sync brings the game and every pinned plugin repo to the requested commits.
// The game gets full history and tags (version scripts derive from tags);
// plugin repos are fetched shallow at their pinned commit.
func (w Workspace) Sync(ctx context.Context, j *Job, log io.Writer) error {
	for _, name := range sortedKeys(j.Plugins) {
		p := j.Plugins[name]
		if err := w.syncRepo(ctx, w.PluginDir(name), p.Repo, p.SHA, false, false, log); err != nil {
			return fmt.Errorf("sync %s: %w", name, err)
		}
	}
	if err := w.syncRepo(ctx, w.GameDir(j.Game), j.Repo, j.SHA, true, true, log); err != nil {
		return fmt.Errorf("sync %s: %w", j.Game, err)
	}
	return nil
}

func (w Workspace) syncRepo(ctx context.Context, dir, repo, sha string, full, submodules bool, log io.Writer) error {
	_, _ = fmt.Fprintf(log, "=== sync %s -> %s\n", dir, sha)
	git := func(args ...string) error { return w.git(ctx, dir, log, args...) }
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		_, _ = fmt.Fprintln(log, "  no checkout yet; initialising (the first sync fetches the whole LFS set and is slow)")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := git("init", "--quiet"); err != nil {
			return err
		}
		if err := git("remote", "add", "origin", cleanRepoURL(repo)); err != nil {
			return err
		}
	} else if err := git("remote", "set-url", "origin", cleanRepoURL(repo)); err != nil {
		return err
	}
	if full {
		args := []string{"fetch", "--quiet", "--tags", "--force", "--prune"}
		if _, err := os.Stat(filepath.Join(dir, ".git", "shallow")); err == nil {
			args = append(args, "--unshallow")
		}
		args = append(args, "origin", "+refs/heads/*:refs/remotes/origin/*")
		if err := git(args...); err != nil {
			return err
		}
		if w.git(ctx, dir, io.Discard, "cat-file", "-e", sha+"^{commit}") != nil {
			if err := git("fetch", "--quiet", "origin", sha); err != nil {
				return err
			}
		}
	} else if err := git("fetch", "--depth", "1", "--no-tags", "--quiet", "origin", sha); err != nil {
		return err
	}
	// LFS smudge is skipped on checkout and done in one batch after.
	if err := w.gitEnv(ctx, dir, log, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "checkout", "--force", "--detach", "--quiet", sha); err != nil {
		return err
	}
	// -ffd, not -ffdx: untracked source goes, ignored build output stays warm.
	if err := git("clean", "-ffd", "--quiet"); err != nil {
		return err
	}
	if err := git("lfs", "fetch", "origin", sha); err != nil {
		return err
	}
	copied, err := w.linkLFS(ctx, dir, log)
	if err != nil {
		return err
	}
	if submodules {
		if err := git("submodule", "sync", "--recursive", "--quiet"); err != nil {
			return err
		}
		if err := w.gitEnv(ctx, dir, log, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "submodule", "update", "--init", "--force", "--recursive"); err != nil {
			return err
		}
	}
	head, err := w.output(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != sha {
		return fmt.Errorf("%s is at %s after the sync, not %s", dir, head, sha)
	}
	// Files git lfs checkout wrote as full copies become clones; space only, so
	// never fatal. Not needed when linkLFS cloned everything itself (and dedup
	// would refuse anyway: a freshly cloned file differs from the index by stat).
	if copied > 0 && w.git(ctx, dir, io.Discard, "lfs", "dedup", "--test") == nil {
		if err := git("lfs", "dedup"); err != nil {
			_, _ = fmt.Fprintf(log, "  warning: git lfs dedup failed in %s (the checkout is still correct): %v\n", dir, err)
		}
	}
	return nil
}

func (w Workspace) git(ctx context.Context, dir string, log io.Writer, args ...string) error {
	return w.gitEnv(ctx, dir, log, nil, args...)
}

func (w Workspace) gitEnv(ctx context.Context, dir string, log io.Writer, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", w.prefix(args)...)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Env = append(append(os.Environ(), noPrompt...), env...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}

// noPrompt makes every git call fail fast instead of asking for a username: the
// daemon has no terminal ("could not read Username ...: Device not configured").
var noPrompt = []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_ASKPASS=", "SSH_ASKPASS="}

func (w Workspace) output(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", w.prefix(args)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), noPrompt...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// prefix adds safe.directory and, with a PAT, an auth header passed per call
// (never written into a repo's config). git-lfs and submodule commands inherit
// -c settings through GIT_CONFIG_PARAMETERS.
func (w Workspace) prefix(args []string) []string {
	out := []string{"-c", "safe.directory=*"}
	if runtime.GOOS == "windows" {
		// Deep Unreal content paths pass 260 characters under a long work root.
		out = append(out, "-c", "core.longpaths=true")
	}
	if w.PAT != "" {
		// The header is the only credential: no helper may answer (or prompt) instead.
		out = append(out, "-c", "credential.helper=")
		tok := base64.StdEncoding.EncodeToString([]byte(":" + w.PAT))
		out = append(out, "-c", "http.extraheader=AUTHORIZATION: Basic "+tok)
	}
	return append(out, args...)
}

// cleanRepoURL drops user info, so a URL like https://Org@dev.azure.com/...
// does not make git prompt for that user's password.
func cleanRepoURL(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && (!strings.Contains(rest, "/") || at < strings.Index(rest, "/")) {
			return raw[:i+3] + rest[at+1:]
		}
	}
	return raw
}

// authFailure reports whether a git log tail looks like a rejected credential.
func authFailure(log string) bool {
	l := strings.ToLower(log)
	for _, s := range []string{"could not read username", "authentication failed", "terminal prompts disabled", "http basic: access denied", "returned error: 401", "returned error: 203"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// LsRemote reads a repository's HEAD the way Sync authenticates, without a
// checkout: `ue5 ci doctor` uses it to prove the credentials.
func (w Workspace) LsRemote(ctx context.Context, repo string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", w.prefix([]string{"ls-remote", cleanRepoURL(repo), "HEAD"})...)
	cmd.Env = append(os.Environ(), noPrompt...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// linkLFS fills in every LFS file the checkout left as a pointer. Where it can
// (macOS/APFS) it makes the file a clone of the object in .git/lfs/objects, so
// filling in even a very large tree costs no extra disk. `git lfs checkout`
// writes a full copy of every file and needs the tree's whole LFS size free
// until a later `git lfs dedup` clones them back, so a large tree can run a
// disk out mid-sync. Files it cannot clone go to `git lfs checkout` as before.
// It returns how many files went through `git lfs checkout` (full copies).
func (w Workspace) linkLFS(ctx context.Context, dir string, log io.Writer) (int, error) {
	out, err := w.output(ctx, dir, "lfs", "ls-files", "--long")
	if err != nil {
		return 0, fmt.Errorf("git lfs ls-files: %w", err)
	}
	var fallback []string
	cloned, already := 0, 0
	for _, line := range strings.Split(out, "\n") {
		// "<oid> <*|-> <path>": '-' means the working file is still a pointer.
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 || len(parts[0]) < 5 {
			continue
		}
		oid, mark, path := parts[0], parts[1], parts[2]
		if mark != "-" {
			already++
			continue
		}
		src := filepath.Join(dir, ".git", "lfs", "objects", oid[0:2], oid[2:4], oid)
		dst := filepath.Join(dir, path)
		if _, err := os.Stat(src); err == nil {
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil && cloneFile(src, dst) == nil {
				cloned++
				continue
			}
		}
		fallback = append(fallback, path)
	}
	_, _ = fmt.Fprintf(log, "  lfs: %d file(s) filled from the object store (cloned where the volume supports it), %d already present, %d via git lfs checkout\n",
		cloned, already, len(fallback))
	copied := len(fallback)
	for len(fallback) > 0 {
		n := min(len(fallback), 200)
		args := append([]string{"lfs", "checkout", "--"}, fallback[:n]...)
		if err := w.git(ctx, dir, log, args...); err != nil {
			return copied, err
		}
		fallback = fallback[n:]
	}
	return copied, nil
}
