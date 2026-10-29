// Package ci is the build service a build box runs inside the ue5 daemon.
//
// Azure DevOps (or a person) dispatches a job with one HTTP call to the box's
// own daemon; the job ends in seconds and frees the org's single parallel slot.
// The daemon queues the job, syncs the checkout, runs the game repo's own
// recipe script (ci/<kind>.sh or .ps1), and reports the result back as a
// commit status on Azure Repos and on the dashboard.
//
// The service is generic on purpose: it knows how to check out a commit and
// run a script, and nothing about any one game. Build steps live in each game
// repo so they change in the same commit as the game, and an old commit
// rebuilds with its own steps.
package ci

import (
	"fmt"
	"time"
)

// Kind is the type of work a job does. Lower Priority runs first.
type Kind string

const (
	KindCompile Kind = "compile"
	KindPackage Kind = "package"
	KindSteam   Kind = "steam"
)

// Priority orders the queue: a push compile (~10-15 min) must never sit behind
// a full nightly cook, which can take hours.
func (k Kind) Priority() int {
	switch k {
	case KindCompile:
		return 0
	case KindPackage:
		return 1
	case KindSteam:
		return 2
	}
	return 9
}

func (k Kind) Valid() bool { return k.Priority() != 9 }

// State is a job's lifecycle position.
type State string

const (
	StateQueued      State = "queued"
	StateRunning     State = "running"
	StateSucceeded   State = "succeeded"
	StateFailed      State = "failed"
	StateSuperseded  State = "superseded"
	StateCanceled    State = "canceled"
	StateInterrupted State = "interrupted" // the daemon died while it ran; a retry is queued
)

// Terminal reports whether the job will never run again.
func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateSuperseded, StateCanceled, StateInterrupted:
		return true
	}
	return false
}

// Request is what a dispatcher sends. Repo/SHA identify the game commit;
// Plugins pins the shared plugin repos to the commits the dispatcher resolved,
// so the build matches what the pipeline would have built.
type Request struct {
	Game     string            `json:"game"`
	Kind     Kind              `json:"kind"`
	Platform string            `json:"platform"`
	Repo     string            `json:"repo"`
	SHA      string            `json:"sha"`
	Branch   string            `json:"branch,omitempty"`
	Plugins  map[string]Pinned `json:"plugins,omitempty"`
	// Source says who asked: "azure:<buildId>", "manual", ...
	Source string `json:"source,omitempty"`
	// SourceURL links back to the dispatching run, when there is one.
	SourceURL string `json:"source_url,omitempty"`
}

// Pinned is one extra repository to sync beside the game.
type Pinned struct {
	Repo string `json:"repo"`
	SHA  string `json:"sha"`
}

// Validate rejects a request the runner could not act on.
func (r Request) Validate() error {
	if r.Game == "" {
		return fmt.Errorf("game is required")
	}
	if !safeName(r.Game) {
		return fmt.Errorf("game %q must be letters, digits, '-' or '_'", r.Game)
	}
	if !r.Kind.Valid() {
		return fmt.Errorf("kind %q must be compile, package or steam", r.Kind)
	}
	if r.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	if !safeName(r.Platform) {
		return fmt.Errorf("platform %q must be letters, digits, '-' or '_'", r.Platform)
	}
	if r.Repo == "" {
		return fmt.Errorf("repo is required")
	}
	if !isSHA(r.SHA) {
		return fmt.Errorf("sha %q is not a 40-character commit id", r.SHA)
	}
	for name, p := range r.Plugins {
		if !safeName(name) {
			return fmt.Errorf("plugin name %q must be letters, digits, '-' or '_'", name)
		}
		if p.Repo == "" || !isSHA(p.SHA) {
			return fmt.Errorf("plugin %s needs a repo and a 40-character sha", name)
		}
	}
	return nil
}

// Key is the coalescing identity: one queued job per key, newest wins.
func (r Request) Key() string {
	return r.Game + "/" + string(r.Kind) + "/" + r.Platform
}

// Job is a Request plus everything the service learns while running it.
type Job struct {
	ID string `json:"id"`
	Request
	State        State      `json:"state"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	Message      string     `json:"message,omitempty"` // one-line reason, e.g. "superseded by ab12" or "disk below floor"
	Errors       []string   `json:"errors,omitempty"`  // key error lines pulled from the log
	SupersededBy string     `json:"superseded_by,omitempty"`
	RetryOf      string     `json:"retry_of,omitempty"`
	// CancelRequested asks the runner to stop a running job.
	CancelRequested bool   `json:"cancel_requested,omitempty"`
	Box             string `json:"box"`
	LogPath         string `json:"log_path,omitempty"`
	ArtifactDir     string `json:"artifact_dir,omitempty"`
}

// Duration is how long the job ran, or has been running.
func (j *Job) Duration(now time.Time) time.Duration {
	if j.StartedAt == nil {
		return 0
	}
	end := now
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	return end.Sub(*j.StartedAt).Round(time.Second)
}

func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func safeName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}
