package ci

import (
	"bytes"
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
	LFSPrune   bool   // after a sync, drop local LFS objects the checked-out commit does not use
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
		shallow := filepath.Join(dir, ".git", "shallow")
		fetchAll := func() error {
			// Submodules are fetched by the submodule update below, which can heal them;
			// a recursive fetch here fails the whole sync on one stale submodule.
			args := []string{"fetch", "--quiet", "--recurse-submodules=no", "--tags", "--force", "--prune"}
			if _, err := os.Stat(shallow); err == nil {
				args = append(args, "--unshallow")
			}
			args = append(args, "origin", "+refs/heads/*:refs/remotes/origin/*")
			return git(args...)
		}
		if err := fetchAll(); err != nil {
			if _, serr := os.Stat(shallow); serr != nil {
				return err
			}
			// A shallow checkout made by another tool can record a boundary commit the
			// server no longer has (a deleted merge commit, say), and every deepening
			// fetch then fails. Forget the local history and fetch it whole: objects
			// already on disk stay, and the working tree is reset by the checkout below.
			_, _ = fmt.Fprintln(log, "  fetch failed in a shallow checkout; dropping its local history and fetching the history whole")
			if rerr := w.forgetHistory(ctx, dir, log); rerr != nil {
				return fmt.Errorf("%w (and resetting the shallow history failed: %v)", err, rerr)
			}
			if err := fetchAll(); err != nil {
				return err
			}
		}
		if w.git(ctx, dir, io.Discard, "cat-file", "-e", sha+"^{commit}") != nil {
			if err := git("fetch", "--quiet", "--recurse-submodules=no", "origin", sha); err != nil {
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
		update := func() error {
			return w.gitEnv(ctx, dir, log, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "submodule", "update", "--init", "--force", "--recursive")
		}
		if err := update(); err != nil {
			// The same stale shallow record the game fetch heals above can sit in a
			// submodule's own history. Forget it in every shallow submodule and retry once.
			healed, herr := w.forgetSubmoduleHistory(ctx, dir, log)
			if herr != nil || !healed {
				return err
			}
			if err := update(); err != nil {
				return err
			}
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
	if w.LFSPrune {
		w.pruneLFS(ctx, dir, log)
	}
	return nil
}

// pruneLFS deletes local LFS objects that the checked-out commit does not
// reference. The recent-refs and recent-commits windows are set to zero so
// only HEAD counts, and the prune offset to zero so nothing is kept "just in
// case". Objects not yet pushed are still kept (git lfs prune's own rule). It
// frees space only, so a failure is a warning and never fails the sync.
func (w Workspace) pruneLFS(ctx context.Context, dir string, log io.Writer) {
	err := w.git(ctx, dir, log,
		"-c", "lfs.fetchrecentrefsdays=0",
		"-c", "lfs.fetchrecentcommitsdays=0",
		"-c", "lfs.fetchrecentremoterefs=false",
		"-c", "lfs.pruneoffsetdays=0",
		"lfs", "prune")
	if err != nil {
		_, _ = fmt.Fprintf(log, "  warning: git lfs prune failed in %s (the checkout is still correct): %v\n", dir, err)
	}
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

// forgetHistory removes a checkout's shallow record and every ref, so the next
// fetch negotiates from nothing and receives complete history. Objects stay on
// disk and the working tree is not touched.
func (w *Workspace) forgetHistory(ctx context.Context, dir string, log io.Writer) error {
	shallow, err := w.shallowFile(ctx, dir)
	if err != nil {
		return err
	}
	if err := os.Remove(shallow); err != nil && !os.IsNotExist(err) {
		return err
	}
	var refs bytes.Buffer
	if err := w.git(ctx, dir, &refs, "for-each-ref", "--format=%(refname)"); err != nil {
		return err
	}
	for _, ref := range strings.Fields(refs.String()) {
		if err := w.git(ctx, dir, log, "update-ref", "-d", ref); err != nil {
			return err
		}
	}
	return w.git(ctx, dir, log, "symbolic-ref", "HEAD", "refs/heads/ue5-ci-reset")
}

// shallowFile is the path of a checkout's shallow record, which for a submodule
// lives in the parent's .git/modules rather than beside the working tree.
func (w *Workspace) shallowFile(ctx context.Context, dir string) (string, error) {
	p, err := w.output(ctx, dir, "rev-parse", "--git-path", "shallow")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p, nil
}

// forgetSubmoduleHistory runs forgetHistory in every initialised submodule that
// has a shallow record. It reports whether it changed anything.
func (w *Workspace) forgetSubmoduleHistory(ctx context.Context, dir string, log io.Writer) (bool, error) {
	status, err := w.output(ctx, dir, "submodule", "status", "--recursive")
	if err != nil {
		return false, err
	}
	healed := false
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(strings.TrimLeft(line, " +-U"))
		if len(fields) < 2 {
			continue
		}
		sub := filepath.Join(dir, filepath.FromSlash(fields[1]))
		shallow, err := w.shallowFile(ctx, sub)
		if err != nil {
			continue // not initialised yet; the update creates it
		}
		if _, err := os.Stat(shallow); err != nil {
			continue
		}
		_, _ = fmt.Fprintf(log, "  submodule %s is shallow; dropping its local history and retrying\n", fields[1])
		// submodule update needs a resolvable HEAD, so keep it detached where it was.
		head, herr := w.output(ctx, sub, "rev-parse", "HEAD")
		if err := w.forgetHistory(ctx, sub, log); err != nil {
			return healed, err
		}
		if herr == nil {
			if err := w.git(ctx, sub, log, "update-ref", "--no-deref", "HEAD", head); err != nil {
				return healed, err
			}
		}
		healed = true
	}
	return healed, nil
}
