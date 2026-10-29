package ci

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// maxHistory caps how many finished jobs the store keeps.
const maxHistory = 500

// ErrNotFound is returned for an unknown job id.
var ErrNotFound = errors.New("job not found")

// Queue is the box's job list, persisted to one JSON file so a daemon restart
// loses nothing. All methods are safe for concurrent use.
type Queue struct {
	mu   sync.Mutex
	path string
	box  string
	jobs []*Job // oldest first
	now  func() time.Time
	// changed is signalled (non-blocking) whenever a job is added or freed.
	changed chan struct{}
}

// NewQueue loads path (missing is fine). A job left "running" by a daemon that
// died is marked interrupted and queued again as a retry, so a crash mid-build
// shows on the dashboard instead of silently vanishing.
func NewQueue(path, box string) (*Queue, error) {
	q := &Queue{path: path, box: box, now: time.Now, changed: make(chan struct{}, 1)}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &q.jobs); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	var retries []*Job
	for _, j := range q.jobs {
		if j.State == StateRunning {
			t := q.now()
			j.State = StateInterrupted
			j.FinishedAt = &t
			j.Message = "the daemon stopped while this job ran; queued again"
			retries = append(retries, j)
		}
	}
	for _, j := range retries {
		q.enqueueLocked(j.Request, j.ID)
	}
	return q, q.saveLocked()
}

// Changed fires when there may be new work.
func (q *Queue) Changed() <-chan struct{} { return q.changed }

func (q *Queue) poke() {
	select {
	case q.changed <- struct{}{}:
	default:
	}
}

// Enqueue adds a job. Any job still queued with the same key is superseded:
// only the newest commit is worth building.
func (q *Queue) Enqueue(r Request) (*Job, []*Job, error) {
	if err := r.Validate(); err != nil {
		return nil, nil, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	j, superseded := q.enqueueLocked(r, "")
	if err := q.saveLocked(); err != nil {
		return nil, nil, err
	}
	q.poke()
	return clone(j), cloneAll(superseded), nil
}

func (q *Queue) enqueueLocked(r Request, retryOf string) (*Job, []*Job) {
	j := &Job{ID: newID(), Request: r, State: StateQueued, QueuedAt: q.now(), Box: q.box, RetryOf: retryOf}
	var superseded []*Job
	for _, old := range q.jobs {
		if old.State == StateQueued && old.Key() == r.Key() {
			t := q.now()
			old.State = StateSuperseded
			old.FinishedAt = &t
			old.SupersededBy = j.ID
			old.Message = "superseded by " + short(r.SHA)
			superseded = append(superseded, old)
		}
	}
	q.jobs = append(q.jobs, j)
	q.trimLocked()
	return j, superseded
}

// Next claims the highest-priority queued job (compile before package before
// steam; oldest first within a kind) and marks it running. ok is false when
// nothing is queued. A non-empty hold (e.g. disk below the floor) vetoes
// starting; the job stays queued with the reason as its message.
func (q *Queue) Next(hold string) (*Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var best *Job
	for _, j := range q.jobs {
		if j.State != StateQueued {
			continue
		}
		if best == nil || j.Kind.Priority() < best.Kind.Priority() ||
			(j.Kind.Priority() == best.Kind.Priority() && j.QueuedAt.Before(best.QueuedAt)) {
			best = j
		}
	}
	if best == nil {
		return nil, false
	}
	if hold != "" {
		if best.Message != hold {
			best.Message = hold
			_ = q.saveLocked()
		}
		return nil, false
	}
	t := q.now()
	best.State = StateRunning
	best.StartedAt = &t
	best.Message = ""
	_ = q.saveLocked()
	return clone(best), true
}

// Update applies fn to the stored job and persists it.
func (q *Queue) Update(id string, fn func(*Job)) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.findLocked(id)
	if j == nil {
		return nil, ErrNotFound
	}
	fn(j)
	if err := q.saveLocked(); err != nil {
		return nil, err
	}
	if j.State.Terminal() {
		q.poke()
	}
	return clone(j), nil
}

// Finish records a terminal state for a running job.
func (q *Queue) Finish(id string, state State, exit *int, msg string, errs []string) (*Job, error) {
	return q.Update(id, func(j *Job) {
		t := q.now()
		j.State = state
		j.FinishedAt = &t
		j.ExitCode = exit
		if msg != "" {
			j.Message = msg
		}
		j.Errors = errs
	})
}

// Cancel stops a queued job at once. A running job is only marked here; the
// runner owns its process and finishes it as canceled.
func (q *Queue) Cancel(id string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.findLocked(id)
	if j == nil {
		return nil, ErrNotFound
	}
	if j.State.Terminal() {
		return nil, fmt.Errorf("job %s is already %s", id, j.State)
	}
	if j.State == StateQueued {
		t := q.now()
		j.State = StateCanceled
		j.FinishedAt = &t
		j.Message = "canceled before it started"
	} else {
		j.CancelRequested = true
		j.Message = "cancel requested"
	}
	return clone(j), q.saveLocked()
}

// Retry queues a fresh copy of any job (it supersedes a queued twin as usual).
func (q *Queue) Retry(id string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	old := q.findLocked(id)
	if old == nil {
		return nil, ErrNotFound
	}
	j, _ := q.enqueueLocked(old.Request, old.ID)
	if err := q.saveLocked(); err != nil {
		return nil, err
	}
	q.poke()
	return clone(j), nil
}

// Get returns one job.
func (q *Queue) Get(id string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j := q.findLocked(id); j != nil {
		return clone(j), nil
	}
	return nil, ErrNotFound
}

// List returns jobs newest first, at most limit (0 = all).
func (q *Queue) List(limit int) []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*Job, 0, len(q.jobs))
	for i := len(q.jobs) - 1; i >= 0; i-- {
		out = append(out, clone(q.jobs[i]))
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// Queued returns waiting jobs in the order they will run.
func (q *Queue) Queued() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []*Job
	for _, j := range q.jobs {
		if j.State == StateQueued {
			out = append(out, clone(j))
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Kind.Priority() != out[b].Kind.Priority() {
			return out[a].Kind.Priority() < out[b].Kind.Priority()
		}
		return out[a].QueuedAt.Before(out[b].QueuedAt)
	})
	return out
}

// Position is a queued job's 1-based place in line (0 if not queued).
func (q *Queue) Position(id string) int {
	for i, j := range q.Queued() {
		if j.ID == id {
			return i + 1
		}
	}
	return 0
}

func (q *Queue) findLocked(id string) *Job {
	for _, j := range q.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// trimLocked drops the oldest finished jobs past maxHistory.
func (q *Queue) trimLocked() {
	finished := 0
	for _, j := range q.jobs {
		if j.State.Terminal() {
			finished++
		}
	}
	if finished <= maxHistory {
		return
	}
	drop := finished - maxHistory
	kept := q.jobs[:0]
	for _, j := range q.jobs {
		if drop > 0 && j.State.Terminal() {
			drop--
			continue
		}
		kept = append(kept, j)
	}
	q.jobs = kept
}

// saveLocked writes atomically: a crash mid-write must not empty the queue.
func (q *Queue) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(q.jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func clone(j *Job) *Job {
	if j == nil {
		return nil
	}
	c := *j
	c.Errors = append([]string(nil), j.Errors...)
	if j.Plugins != nil {
		c.Plugins = make(map[string]Pinned, len(j.Plugins))
		maps.Copy(c.Plugins, j.Plugins)
	}
	return &c
}

func cloneAll(js []*Job) []*Job {
	out := make([]*Job, len(js))
	for i, j := range js {
		out[i] = clone(j)
	}
	return out
}
