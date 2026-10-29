package ci

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := git("lfs", "checkout"); err != nil {
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
	// Copy-on-write clones of the LFS objects; space only, so never fatal.
	if w.git(ctx, dir, io.Discard, "lfs", "dedup", "--test") == nil {
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
