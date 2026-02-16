package server

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// PersistentState is the top-level schema for ~/.ue5/state.json.
type PersistentState struct {
	CurrentBuild *BuildRecord  `json:"current_build,omitempty"`
	BuildHistory []BuildRecord `json:"build_history"`
	ActiveAgents []AgentInfo   `json:"active_agents"`
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

	s.state = state
	return nil
}

// Save writes state to disk atomically (write to temp file, then rename).
func (s *StateStore) Save() error {
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()

	state.LastModified = time.Now()

	data, err := json.MarshalIndent(state, "", "  ")
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
	if s.state.CurrentBuild != nil {
		existing := make([]string, len(s.state.CurrentBuild.Features))
		copy(existing, s.state.CurrentBuild.Features)
		record.Features = append(existing, record.Labels...)
	} else {
		record.Features = make([]string, len(record.Labels))
		copy(record.Features, record.Labels)
	}

	s.state.CurrentBuild = &record
	s.state.BuildHistory = append(s.state.BuildHistory, record)
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

// GetState returns a snapshot of the full persistent state.
func (s *StateStore) GetState() PersistentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}
