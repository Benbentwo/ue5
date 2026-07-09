package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// maxBuildHistory caps how many full BuildRecords state.json retains.
	// Older records are rolled to the JSONL archive next to the state file.
	maxBuildHistory = 100

	// maxAccumulatedFeatures caps the deduplicated feature-label list carried
	// on the current build.
	maxAccumulatedFeatures = 200
)

// PersistentState is the top-level schema for ~/.ue5/state.json.
type PersistentState struct {
	CurrentBuild *BuildRecord  `json:"current_build,omitempty"`
	BuildHistory []BuildRecord `json:"build_history"`
	ActiveAgents []AgentInfo   `json:"active_agents"`
	TotalBuilds  int           `json:"total_builds"`
	LastModified time.Time     `json:"last_modified"`
}

// StateStore manages persistent state with file-backed storage.
type StateStore struct {
	mu    sync.RWMutex
	state PersistentState
	path  string
}

// NewStateStore creates a new state store backed by ~/.ue5/state.json.
func NewStateStore() *StateStore {
	return &StateStore{
		path: StateFile(),
		state: PersistentState{
			BuildHistory: []BuildRecord{},
			ActiveAgents: []AgentInfo{},
		},
	}
}

// Load reads state from disk. No-op if the file doesn't exist.
func (s *StateStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// Ensure slices are non-nil
	if state.BuildHistory == nil {
		state.BuildHistory = []BuildRecord{}
	}
	if state.ActiveAgents == nil {
		state.ActiveAgents = []AgentInfo{}
	}

	// Migrate files written before retention caps existed: derive the
	// lifetime counter, strip per-record accumulated features (derived data
	// that grew state.json quadratically with build count), and prune the
	// current build's list.
	if state.TotalBuilds == 0 {
		state.TotalBuilds = len(state.BuildHistory)
	}
	for i := range state.BuildHistory {
		state.BuildHistory[i].Features = nil
	}
	if state.CurrentBuild != nil {
		state.CurrentBuild.Features = accumulateFeatures(state.CurrentBuild.Features, nil)
	}

	s.state = state
	s.trimHistoryLocked()
	return nil
}

// Save writes state to disk atomically (write to temp file, then rename).
func (s *StateStore) Save() error {
	// Marshal while holding the lock: the snapshot shares slice backing
	// arrays with s.state, so encoding after unlock races with in-place
	// mutations like UpdateBuildStatus.
	s.mu.RLock()
	state := s.state
	state.LastModified = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// GetCurrentBuild returns the current (most recent) build record.
func (s *StateStore) GetCurrentBuild() *BuildRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.CurrentBuild == nil {
		return nil
	}
	copy := *s.state.CurrentBuild
	return &copy
}

// AddBuildRecord appends a build to history and sets it as current.
// Features are accumulated from the previous build.
func (s *StateStore) AddBuildRecord(record BuildRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Accumulate features from previous build
	var prior []string
	if s.state.CurrentBuild != nil {
		prior = s.state.CurrentBuild.Features
	}
	record.Features = accumulateFeatures(prior, record.Labels)

	s.state.CurrentBuild = &record

	// History entries don't carry the accumulated feature list: it's derived
	// data, and persisting a copy per record made state.json grow
	// quadratically with build count.
	entry := record
	entry.Features = nil
	s.state.BuildHistory = append(s.state.BuildHistory, entry)
	s.state.TotalBuilds++
	s.trimHistoryLocked()
}

// accumulateFeatures merges new labels into the existing feature list,
// dropping duplicates and keeping only the most recent maxAccumulatedFeatures.
func accumulateFeatures(existing, labels []string) []string {
	merged := make([]string, 0, len(existing)+len(labels))
	seen := make(map[string]struct{}, len(existing)+len(labels))
	for _, group := range [][]string{existing, labels} {
		for _, f := range group {
			if _, dup := seen[f]; dup {
				continue
			}
			seen[f] = struct{}{}
			merged = append(merged, f)
		}
	}
	if len(merged) > maxAccumulatedFeatures {
		merged = append([]string{}, merged[len(merged)-maxAccumulatedFeatures:]...)
	}
	return merged
}

// trimHistoryLocked rolls history beyond maxBuildHistory to the archive file.
// Caller must hold s.mu.
func (s *StateStore) trimHistoryLocked() {
	overflow := len(s.state.BuildHistory) - maxBuildHistory
	if overflow <= 0 {
		return
	}
	s.appendToArchive(s.state.BuildHistory[:overflow])
	s.state.BuildHistory = append([]BuildRecord{}, s.state.BuildHistory[overflow:]...)
}

// appendToArchive appends records to the JSONL history archive. Best-effort:
// an unwritable archive must never block builds or state persistence.
func (s *StateStore) appendToArchive(records []BuildRecord) {
	f, err := os.OpenFile(s.archivePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	enc := json.NewEncoder(f)
	for _, r := range records {
		r.Features = nil // derived from prior labels; omit to keep the archive compact
		_ = enc.Encode(r)
	}
}

// UpdateBuildStatus updates the status of a build record by ID.
func (s *StateStore) UpdateBuildStatus(id string, status BuildStatus, completedAt *time.Time, buildErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update in history
	for i := range s.state.BuildHistory {
		if s.state.BuildHistory[i].ID == id {
			s.state.BuildHistory[i].Status = status
			s.state.BuildHistory[i].CompletedAt = completedAt
			s.state.BuildHistory[i].Error = buildErr
			break
		}
	}

	// Update current if it matches
	if s.state.CurrentBuild != nil && s.state.CurrentBuild.ID == id {
		s.state.CurrentBuild.Status = status
		s.state.CurrentBuild.CompletedAt = completedAt
		s.state.CurrentBuild.Error = buildErr
	}
}

// GetBuildHistory returns the last N build records (most recent first).
func (s *StateStore) GetBuildHistory(limit int) []BuildRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.state.BuildHistory
	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}

	// Return most recent first
	result := make([]BuildRecord, limit)
	for i := 0; i < limit; i++ {
		result[i] = history[len(history)-1-i]
	}
	return result
}

// GetAccumulatedFeatures returns all features from the current build.
func (s *StateStore) GetAccumulatedFeatures() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state.CurrentBuild == nil {
		return []string{}
	}

	features := make([]string, len(s.state.CurrentBuild.Features))
	copy(features, s.state.CurrentBuild.Features)
	return features
}

// SetActiveAgents replaces the active agents list.
func (s *StateStore) SetActiveAgents(agents []AgentInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ActiveAgents = agents
}

// TotalBuilds returns the lifetime build count, including records trimmed
// from history.
func (s *StateStore) TotalBuilds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.TotalBuilds
}

// archivePath returns the JSONL file that trimmed history records roll to.
func (s *StateStore) archivePath() string {
	return filepath.Join(filepath.Dir(s.path), "history_archive.jsonl")
}

// GetState returns a snapshot of the full persistent state.
func (s *StateStore) GetState() PersistentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}
