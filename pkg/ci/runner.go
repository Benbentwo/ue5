package ci

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Service is one box's build service: a queue and the single runner lane that
// drains it. One heavy build at a time per box, because one UE compile or cook
// already uses every core, and because every game on the box shares one
// plugins checkout that two jobs would sync to different commits.
type Service struct {
	Config    Config
	Queue     *Queue
	Workspace Workspace
	Reporter  *Reporter
	Home      string // ~/.ue5
	Logf      func(format string, args ...any)

	// syncFn and freeFn are seams for tests.
	syncFn func(ctx context.Context, j *Job, log io.Writer) error
	freeFn func(path string) (int, error)

	mu        sync.Mutex
	running   *Job
	cancelRun context.CancelFunc
	reclaimed time.Time
}

// NewService wires a service from config and the secret store under home.
func NewService(home string, cfg Config) (*Service, error) {
	q, err := NewQueue(filepath.Join(Dir(home), "jobs.json"), cfg.Box)
	if err != nil {
		return nil, err
	}
	pat := Secret(home, "azure-pat")
	s := &Service{
		Config:    cfg,
		Queue:     q,
		Workspace: Workspace{Root: cfg.WorkRoot, PluginsDir: cfg.PluginsDir, PAT: pat},
		Home:      home,
		Logf:      func(string, ...any) {},
	}
	s.Reporter = &Reporter{PAT: pat, PublicURL: cfg.PublicURL, Errorf: func(f string, a ...any) { s.Logf(f, a...) }}
	return s, nil
}

// Running returns the job the lane is working on, if any.
func (s *Service) Running() *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		return nil
	}
	j, err := s.Queue.Get(s.running.ID)
	if err != nil {
		return clone(s.running)
	}
	return j
}

// Dispatch queues a request and reports it (and anything it superseded).
func (s *Service) Dispatch(ctx context.Context, r Request) (*Job, error) {
	j, superseded, err := s.Queue.Enqueue(r)
	if err != nil {
		return nil, err
	}
	for _, old := range superseded {
		s.Reporter.Report(ctx, old)
	}
	s.Reporter.Report(ctx, j)
	return j, nil
}

// Cancel stops a queued or running job.
func (s *Service) Cancel(ctx context.Context, id string) (*Job, error) {
	j, err := s.Queue.Cancel(id)
	if err != nil {
		return nil, err
	}
	if j.State == StateCanceled {
		s.Reporter.Report(ctx, j)
		return j, nil
	}
	s.mu.Lock()
	if s.running != nil && s.running.ID == id && s.cancelRun != nil {
		s.cancelRun()
	}
	s.mu.Unlock()
	return j, nil
}

// Run drains the queue until ctx ends. It wakes on every dispatch and at
// least once a minute, which is also how a disk hold gets re-checked.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		for {
			hold := ""
			if len(s.Queue.Queued()) > 0 {
				hold = s.diskHold()
			}
			j, ok := s.Queue.Next(hold)
			if !ok {
				break
			}
			s.run(ctx, j)
			if ctx.Err() != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.Queue.Changed():
		case <-tick.C:
		}
	}
}

// diskHold keeps the queue waiting while the disk is below the floor, running
// the reclaim command at most once an hour, instead of failing every queued job.
func (s *Service) diskHold() string {
	free := s.freeFn
	if free == nil {
		free = FreeGB
	}
	root := s.Config.WorkRoot
	_ = os.MkdirAll(root, 0o755)
	gb, err := free(root)
	if err != nil || gb >= s.Config.DiskFloorGB {
		return ""
	}
	if len(s.Config.ReclaimCommand) > 0 && time.Since(s.reclaimed) > time.Hour {
		s.reclaimed = time.Now()
		s.Logf("ci: %d GB free on %s, below the %d GB floor; running reclaim", gb, root, s.Config.DiskFloorGB)
		cmd := exec.Command(s.Config.ReclaimCommand[0], s.Config.ReclaimCommand[1:]...)
		_ = cmd.Run()
		if gb2, err := free(root); err == nil && gb2 >= s.Config.DiskFloorGB {
			return ""
		}
	}
	return fmt.Sprintf("waiting for disk: %d GB free on %s, floor is %d GB", gb, root, s.Config.DiskFloorGB)
}

func (s *Service) run(parent context.Context, j *Job) {
	jobDir := filepath.Join(Dir(s.Home), "jobs", j.ID)
	_ = os.MkdirAll(jobDir, 0o755)
	logPath := filepath.Join(jobDir, "log.txt")
	artifacts := filepath.Join(s.Config.ArtifactRoot, j.Game, j.ID)
	j, _ = s.Queue.Update(j.ID, func(x *Job) { x.LogPath = logPath; x.ArtifactDir = artifacts })

	ctx, cancel := context.WithTimeout(parent, s.Config.Timeout(j.Kind))
	s.mu.Lock()
	s.running, s.cancelRun = j, cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.running, s.cancelRun = nil, nil
		s.mu.Unlock()
	}()

	s.Reporter.Report(parent, j)
	logFile, err := os.Create(logPath)
	if err != nil {
		s.finish(parent, j, StateFailed, nil, "cannot write the log: "+err.Error(), nil)
		return
	}
	defer func() { _ = logFile.Close() }()
	_, _ = fmt.Fprintf(logFile, "ue5 ci job %s on %s: %s %s %s at %s (from %s)\n", j.ID, j.Box, j.Game, j.Kind, j.Platform, j.SHA, j.Source)

	state, exit, msg := s.execute(ctx, j, logFile, artifacts)
	if parent.Err() != nil {
		// The daemon is shutting down; NewQueue turns "running" into a retry.
		return
	}
	if latest, err := s.Queue.Get(j.ID); err == nil && latest.CancelRequested {
		state, msg = StateCanceled, "canceled while running"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		state, msg = StateFailed, fmt.Sprintf("timed out after %s", s.Config.Timeout(j.Kind))
	}
	_ = logFile.Sync()
	var errs []string
	if state == StateFailed {
		errs = ExtractErrors(logPath, 12)
	}
	s.finish(parent, j, state, exit, msg, errs)
	if state == StateSucceeded {
		s.pruneArtifacts(j.Game)
	}
}

// execute syncs the workspace and runs the game's recipe.
func (s *Service) execute(ctx context.Context, j *Job, log *os.File, artifacts string) (State, *int, string) {
	syncFn := s.syncFn
	if syncFn == nil {
		syncFn = s.Workspace.Sync
	}
	if err := syncFn(ctx, j, log); err != nil {
		_, _ = fmt.Fprintf(log, "ERROR: %v\n", err)
		return StateFailed, nil, err.Error()
	}
	gameDir := s.Workspace.GameDir(j.Game)
	script := filepath.Join(gameDir, "ci", string(j.Kind)+recipeExt)
	if _, err := os.Stat(script); err != nil {
		msg := fmt.Sprintf("%s has no recipe ci/%s%s at %s", j.Game, j.Kind, recipeExt, short(j.SHA))
		_, _ = fmt.Fprintln(log, "ERROR: "+msg)
		return StateFailed, nil, msg
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return StateFailed, nil, err.Error()
	}
	_, _ = fmt.Fprintf(log, "=== recipe %s\n", script)
	cmd := recipeCommand(ctx, script)
	cmd.Dir = gameDir
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(), s.recipeEnv(j, gameDir, artifacts)...)
	cmd.WaitDelay = 30 * time.Second
	err := cmd.Run()
	if cmd.ProcessState == nil {
		return StateFailed, nil, err.Error()
	}
	code := cmd.ProcessState.ExitCode()
	if err != nil || code != 0 {
		return StateFailed, &code, fmt.Sprintf("ci/%s%s exited %d", j.Kind, recipeExt, code)
	}
	return StateSucceeded, &code, ""
}

// recipeEnv is the contract a game's recipe can rely on.
func (s *Service) recipeEnv(j *Job, gameDir, artifacts string) []string {
	env := []string{
		"UE5_CI=1",
		"UE5_CI_JOB_ID=" + j.ID,
		"UE5_CI_BOX=" + j.Box,
		"UE5_CI_GAME=" + j.Game,
		"UE5_CI_KIND=" + string(j.Kind),
		"UE5_CI_PLATFORM=" + j.Platform,
		"UE5_CI_SHA=" + j.SHA,
		"UE5_CI_BRANCH=" + j.Branch,
		"UE5_CI_SOURCE=" + j.Source,
		"GAME_ROOT=" + gameDir,
		"SHARED_ROOT=" + filepath.Dir(s.Workspace.PluginDir("x")),
		"ARTIFACT_DIR=" + artifacts,
	}
	if ue, err := os.ReadFile(filepath.Join(gameDir, j.Game+".uproject")); err == nil {
		var u struct{ EngineAssociation string }
		if json.Unmarshal(ue, &u) == nil && u.EngineAssociation != "" {
			env = append(env, "UE5_CI_ENGINE_ASSOCIATION="+u.EngineAssociation)
		}
	}
	return env
}

func (s *Service) finish(ctx context.Context, j *Job, state State, exit *int, msg string, errs []string) {
	done, err := s.Queue.Finish(j.ID, state, exit, msg, errs)
	if err != nil {
		s.Logf("ci: cannot record %s: %v", j.ID, err)
		return
	}
	s.writeBuildJSON(done)
	s.Reporter.Report(ctx, done)
	s.Logf("ci: %s %s %s %s -> %s %s", done.ID, done.Game, done.Kind, done.Platform, done.State, done.Message)
}

// writeBuildJSON leaves the job's metadata beside its log and its package.
func (s *Service) writeBuildJSON(j *Job) {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(j.LogPath), "build.json"), data, 0o644)
	if fi, err := os.Stat(j.ArtifactDir); err == nil && fi.IsDir() {
		entries, _ := os.ReadDir(j.ArtifactDir)
		if len(entries) == 0 {
			_ = os.Remove(j.ArtifactDir)
			return
		}
		_ = os.WriteFile(filepath.Join(j.ArtifactDir, "build.json"), data, 0o644)
	}
}

// pruneArtifacts keeps the newest KeepArtifacts packaged builds of a game.
func (s *Service) pruneArtifacts(game string) {
	dir := filepath.Join(s.Config.ArtifactRoot, game)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // job ids start with a UTC timestamp
	for len(names) > s.Config.KeepArtifacts {
		_ = os.RemoveAll(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// errorLine matches the lines worth showing for a failed build: compiler and
// UBT errors, UAT/script ERRORs, Python and test failures, git fatals.
var errorLine = regexp.MustCompile(`(?i)(\berror\b[: ]|\berror [A-Z]{1,3}\d{3,5}\b|^\s*FAIL\b|FAILED \(|\bfatal:|Result: Failed|Traceback|[A-Za-z]+Error: |No space left on device|ERROR!)`)

// noise is matched lines that are not failures.
var noise = regexp.MustCompile(`(?i)(warning|0 error\(s\)|-error|errors? ?= ?0|LogInit|Display: .*error)`)

// ExtractErrors returns up to max distinct error lines from a log, earliest
// first, since the first error is usually the cause and the rest fallout.
func ExtractErrors(path string, max int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !errorLine.MatchString(line) || noise.MatchString(line) {
			continue
		}
		if len(line) > 300 {
			line = line[:299] + "…"
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) == max {
			break
		}
	}
	return out
}

func sortedKeys(m map[string]Pinned) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
