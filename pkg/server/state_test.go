package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStoreRoundTrip(t *testing.T) {
	// Use a temp directory for state
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := &StateStore{
		path: path,
		state: PersistentState{
			BuildHistory: []BuildRecord{},
			ActiveAgents: []AgentInfo{},
		},
	}

	// Add a build record
	store.AddBuildRecord(BuildRecord{
		ID:          "build-1",
		ProjectPath: "/test/project.uproject",
		Labels:      []string{"Added inventory system"},
		Mode:        BuildModeFull,
		Status:      BuildStatusSucceeded,
		StartedAt:   time.Now(),
	})

	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("State file not created: %v", err)
	}

	// Load into a fresh store
	store2 := &StateStore{
		path: path,
		state: PersistentState{
			BuildHistory: []BuildRecord{},
			ActiveAgents: []AgentInfo{},
		},
	}

	if err := store2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if store2.state.CurrentBuild == nil {
		t.Fatal("CurrentBuild is nil after load")
	}

	if store2.state.CurrentBuild.ID != "build-1" {
		t.Errorf("Expected build ID 'build-1', got '%s'", store2.state.CurrentBuild.ID)
	}

	if len(store2.state.BuildHistory) != 1 {
		t.Errorf("Expected 1 build in history, got %d", len(store2.state.BuildHistory))
	}
}

func TestFeatureAccumulation(t *testing.T) {
	store := NewStateStore()
	// Override path to temp
	store.path = filepath.Join(t.TempDir(), "state.json")

	// First build: adds feature A
	store.AddBuildRecord(BuildRecord{
		ID:     "build-1",
		Labels: []string{"Feature A"},
		Status: BuildStatusSucceeded,
	})

	features := store.GetAccumulatedFeatures()
	if len(features) != 1 || features[0] != "Feature A" {
		t.Errorf("Expected [Feature A], got %v", features)
	}

	// Second build: adds feature B (should accumulate)
	store.AddBuildRecord(BuildRecord{
		ID:     "build-2",
		Labels: []string{"Feature B"},
		Status: BuildStatusSucceeded,
	})

	features = store.GetAccumulatedFeatures()
	if len(features) != 2 {
		t.Errorf("Expected 2 features, got %d: %v", len(features), features)
	}
	if features[0] != "Feature A" || features[1] != "Feature B" {
		t.Errorf("Expected [Feature A, Feature B], got %v", features)
	}

	// Third build with multiple labels (coalesced): adds C and D
	store.AddBuildRecord(BuildRecord{
		ID:     "build-3",
		Labels: []string{"Feature C", "Feature D"},
		Status: BuildStatusSucceeded,
	})

	features = store.GetAccumulatedFeatures()
	if len(features) != 4 {
		t.Errorf("Expected 4 features, got %d: %v", len(features), features)
	}
}

func TestBuildStatusUpdate(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	store.AddBuildRecord(BuildRecord{
		ID:     "build-1",
		Labels: []string{"test"},
		Status: BuildStatusBuilding,
	})

	now := time.Now()
	store.UpdateBuildStatus("build-1", BuildStatusSucceeded, &now, "")

	current := store.GetCurrentBuild()
	if current.Status != BuildStatusSucceeded {
		t.Errorf("Expected status 'succeeded', got '%s'", current.Status)
	}
	if current.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}

	// Test failed status with error message
	store.AddBuildRecord(BuildRecord{
		ID:     "build-2",
		Labels: []string{"test2"},
		Status: BuildStatusBuilding,
	})

	store.UpdateBuildStatus("build-2", BuildStatusFailed, &now, "compilation error")

	current = store.GetCurrentBuild()
	if current.Status != BuildStatusFailed {
		t.Errorf("Expected status 'failed', got '%s'", current.Status)
	}
	if current.Error != "compilation error" {
		t.Errorf("Expected error 'compilation error', got '%s'", current.Error)
	}
}

func TestBuildHistory(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	for i := 0; i < 5; i++ {
		store.AddBuildRecord(BuildRecord{
			ID:     "build-" + string(rune('A'+i)),
			Labels: []string{"feature"},
			Status: BuildStatusSucceeded,
		})
	}

	// Get last 3 (most recent first)
	history := store.GetBuildHistory(3)
	if len(history) != 3 {
		t.Errorf("Expected 3 builds, got %d", len(history))
	}
	if history[0].ID != "build-E" {
		t.Errorf("Expected most recent build 'build-E', got '%s'", history[0].ID)
	}
}

func TestLoadMissingFile(t *testing.T) {
	store := &StateStore{
		path: filepath.Join(t.TempDir(), "nonexistent.json"),
		state: PersistentState{
			BuildHistory: []BuildRecord{},
			ActiveAgents: []AgentInfo{},
		},
	}

	// Should not error on missing file
	if err := store.Load(); err != nil {
		t.Errorf("Load should not error on missing file: %v", err)
	}
}
