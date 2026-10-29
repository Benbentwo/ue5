package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// readArchive parses the JSONL history archive written when records are
// trimmed from build history.
func readArchive(t *testing.T, path string) []BuildRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	var records []BuildRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r BuildRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("parse archive line %q: %v", line, err)
		}
		records = append(records, r)
	}
	return records
}

func TestBuildHistoryCappedAndArchived(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	total := maxBuildHistory + 5
	for i := 0; i < total; i++ {
		store.AddBuildRecord(BuildRecord{
			ID:     fmt.Sprintf("build-%03d", i),
			Labels: []string{fmt.Sprintf("feature %03d", i)},
			Status: BuildStatusSucceeded,
		})
	}

	history := store.GetBuildHistory(0)
	if len(history) != maxBuildHistory {
		t.Errorf("Expected history capped at %d, got %d", maxBuildHistory, len(history))
	}
	if history[0].ID != fmt.Sprintf("build-%03d", total-1) {
		t.Errorf("Expected most recent build retained, got %s", history[0].ID)
	}
	if got := store.TotalBuilds(); got != total {
		t.Errorf("Expected TotalBuilds %d after trim, got %d", total, got)
	}

	archived := readArchive(t, store.archivePath())
	if len(archived) != 5 {
		t.Fatalf("Expected 5 archived records, got %d", len(archived))
	}
	if archived[0].ID != "build-000" {
		t.Errorf("Expected oldest record archived first, got %s", archived[0].ID)
	}
}

func TestHistoryEntriesOmitAccumulatedFeatures(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	for _, id := range []string{"a", "b", "c"} {
		store.AddBuildRecord(BuildRecord{
			ID:     id,
			Labels: []string{"feature " + id},
			Status: BuildStatusSucceeded,
		})
	}

	if got := store.GetAccumulatedFeatures(); len(got) != 3 {
		t.Errorf("Expected 3 accumulated features, got %v", got)
	}
	for _, rec := range store.GetBuildHistory(0) {
		if len(rec.Features) != 0 {
			t.Errorf("History record %s should not carry accumulated features, got %d", rec.ID, len(rec.Features))
		}
	}
}

func TestAccumulatedFeaturesDeduped(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	store.AddBuildRecord(BuildRecord{ID: "1", Labels: []string{"Feature A"}})
	store.AddBuildRecord(BuildRecord{ID: "2", Labels: []string{"Feature A", "Feature B"}})

	features := store.GetAccumulatedFeatures()
	if len(features) != 2 || features[0] != "Feature A" || features[1] != "Feature B" {
		t.Errorf("Expected deduped [Feature A, Feature B], got %v", features)
	}
}

func TestAccumulatedFeaturesCapped(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	total := maxAccumulatedFeatures + 10
	for i := 0; i < total; i++ {
		store.AddBuildRecord(BuildRecord{
			ID:     fmt.Sprintf("b%d", i),
			Labels: []string{fmt.Sprintf("feature %04d", i)},
		})
	}

	features := store.GetAccumulatedFeatures()
	if len(features) != maxAccumulatedFeatures {
		t.Errorf("Expected features capped at %d, got %d", maxAccumulatedFeatures, len(features))
	}
	if got := features[len(features)-1]; got != fmt.Sprintf("feature %04d", total-1) {
		t.Errorf("Expected most recent feature retained last, got %s", got)
	}
}

func TestLoadMigratesLegacyBloatedState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Legacy shape: every history record carries the full accumulated
	// feature list (with duplicates from repeated labels) and there is
	// no total_builds field.
	total := maxBuildHistory + 20
	legacy := PersistentState{ActiveAgents: []AgentInfo{}}
	features := []string{}
	for i := 0; i < total; i++ {
		label := fmt.Sprintf("feature %03d", i)
		features = append(features, label, label)
		legacy.BuildHistory = append(legacy.BuildHistory, BuildRecord{
			ID:       fmt.Sprintf("build-%03d", i),
			Labels:   []string{label},
			Features: append([]string{}, features...),
			Status:   BuildStatusSucceeded,
		})
	}
	last := legacy.BuildHistory[len(legacy.BuildHistory)-1]
	legacy.CurrentBuild = &last

	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy state: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store := &StateStore{path: path}
	if err := store.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := store.TotalBuilds(); got != total {
		t.Errorf("Expected TotalBuilds %d from legacy history length, got %d", total, got)
	}
	if got := len(store.GetBuildHistory(0)); got != maxBuildHistory {
		t.Errorf("Expected legacy history trimmed to %d, got %d", maxBuildHistory, got)
	}
	for _, rec := range store.GetBuildHistory(0) {
		if len(rec.Features) != 0 {
			t.Errorf("Legacy history record %s should have features stripped, got %d", rec.ID, len(rec.Features))
			break
		}
	}

	// Duplicates pruned from the accumulated list (120 unique labels).
	accumulated := store.GetAccumulatedFeatures()
	if len(accumulated) != total {
		t.Errorf("Expected %d deduped accumulated features, got %d", total, len(accumulated))
	}

	archived := readArchive(t, store.archivePath())
	if len(archived) != total-maxBuildHistory {
		t.Errorf("Expected %d archived records, got %d", total-maxBuildHistory, len(archived))
	}

	// TotalBuilds survives a save/reload cycle.
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	store2 := &StateStore{path: path}
	if err := store2.Load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if got := store2.TotalBuilds(); got != total {
		t.Errorf("Expected TotalBuilds %d after reload, got %d", total, got)
	}
}

func TestConcurrentSaveAndUpdate(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	store.AddBuildRecord(BuildRecord{ID: "b", Labels: []string{"x"}, Status: BuildStatusBuilding})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = store.Save()
		}()
		go func() {
			defer wg.Done()
			now := time.Now()
			store.UpdateBuildStatus("b", BuildStatusSucceeded, &now, "")
		}()
	}
	wg.Wait()
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
