package server

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCoalescingRecordCreation(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Simulate coalescing: multiple requests merged into one record
	requests := []RebuildRequest{
		{
			ProjectPath: "/test/MyGame.uproject",
			Mode:        BuildModeHotReload,
			Label:       "Added inventory system",
			AgentID:     "agent-A",
		},
		{
			ProjectPath: "/test/MyGame.uproject",
			Mode:        BuildModeHotReload,
			Label:       "Added health component",
			AgentID:     "agent-B",
		},
	}

	record := b.createRecord(requests)

	// Verify labels from both agents are present
	if len(record.Labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d", len(record.Labels))
	}
	if record.Labels[0] != "Added inventory system" || record.Labels[1] != "Added health component" {
		t.Errorf("Unexpected labels: %v", record.Labels)
	}

	// Verify contributions
	if len(record.Contributions) != 2 {
		t.Fatalf("Expected 2 contributions, got %d", len(record.Contributions))
	}
	if record.Contributions[0].AgentID != "agent-A" {
		t.Errorf("Expected first contribution from agent-A, got '%s'", record.Contributions[0].AgentID)
	}
	if record.Contributions[1].AgentID != "agent-B" {
		t.Errorf("Expected second contribution from agent-B, got '%s'", record.Contributions[1].AgentID)
	}

	// Verify project path from first request
	if record.ProjectPath != "/test/MyGame.uproject" {
		t.Errorf("Expected project path '/test/MyGame.uproject', got '%s'", record.ProjectPath)
	}

	// Verify status
	if record.Status != BuildStatusBuilding {
		t.Errorf("Expected status 'building', got '%s'", record.Status)
	}
}

func TestModeEscalation(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// hot_reload + hot_reload = hot_reload
	record := b.createRecord([]RebuildRequest{
		{ProjectPath: "/test/P.uproject", Mode: BuildModeHotReload, Label: "A", AgentID: "a"},
		{ProjectPath: "/test/P.uproject", Mode: BuildModeHotReload, Label: "B", AgentID: "b"},
	})
	if record.Mode != BuildModeHotReload {
		t.Errorf("Expected hot_reload when all requests are hot_reload, got '%s'", record.Mode)
	}

	// hot_reload + full = full (escalation)
	record = b.createRecord([]RebuildRequest{
		{ProjectPath: "/test/P.uproject", Mode: BuildModeHotReload, Label: "A", AgentID: "a"},
		{ProjectPath: "/test/P.uproject", Mode: BuildModeFull, Label: "B", AgentID: "b"},
	})
	if record.Mode != BuildModeFull {
		t.Errorf("Expected full mode when any request is full, got '%s'", record.Mode)
	}

	// full + hot_reload = full (escalation regardless of order)
	record = b.createRecord([]RebuildRequest{
		{ProjectPath: "/test/P.uproject", Mode: BuildModeFull, Label: "A", AgentID: "a"},
		{ProjectPath: "/test/P.uproject", Mode: BuildModeHotReload, Label: "B", AgentID: "b"},
	})
	if record.Mode != BuildModeFull {
		t.Errorf("Expected full mode regardless of order, got '%s'", record.Mode)
	}
}

func TestDefaultTargetFromProject(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	record := b.createRecord([]RebuildRequest{
		{ProjectPath: "/Games/MyGame.uproject", Mode: BuildModeFull, Label: "test"},
	})

	if record.Target != "MyGameEditor" {
		t.Errorf("Expected target 'MyGameEditor', got '%s'", record.Target)
	}
	if record.Configuration != "Development" {
		t.Errorf("Expected configuration 'Development', got '%s'", record.Configuration)
	}
}

func TestCustomTargetPreserved(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	record := b.createRecord([]RebuildRequest{
		{
			ProjectPath:   "/Games/MyGame.uproject",
			Mode:          BuildModeFull,
			Label:         "test",
			Target:        "CustomTarget",
			Configuration: "Shipping",
		},
	})

	if record.Target != "CustomTarget" {
		t.Errorf("Expected custom target 'CustomTarget', got '%s'", record.Target)
	}
	if record.Configuration != "Shipping" {
		t.Errorf("Expected configuration 'Shipping', got '%s'", record.Configuration)
	}
}

func TestRequestRebuildEmptyLabel(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	_, err := b.RequestRebuild(context.TODO(), &RebuildRequest{
		ProjectPath: "/test/P.uproject",
		Mode:        BuildModeFull,
		Label:       "",
	})
	if err == nil {
		t.Error("Expected error for empty label")
	}
}

func TestFeatureAccumulationAcrossBuilds(t *testing.T) {
	store := NewStateStore()
	store.path = filepath.Join(t.TempDir(), "state.json")

	// Build 1: Feature A
	store.AddBuildRecord(BuildRecord{
		ID:     "build-1",
		Labels: []string{"Feature A"},
		Status: BuildStatusSucceeded,
	})

	// Build 2: Feature B (coalesced with Feature C)
	store.AddBuildRecord(BuildRecord{
		ID:     "build-2",
		Labels: []string{"Feature B", "Feature C"},
		Status: BuildStatusSucceeded,
	})

	// Build 3: Feature D
	store.AddBuildRecord(BuildRecord{
		ID:     "build-3",
		Labels: []string{"Feature D"},
		Status: BuildStatusSucceeded,
	})

	features := store.GetAccumulatedFeatures()
	expected := []string{"Feature A", "Feature B", "Feature C", "Feature D"}

	if len(features) != len(expected) {
		t.Fatalf("Expected %d features, got %d: %v", len(expected), len(features), features)
	}
	for i, f := range expected {
		if features[i] != f {
			t.Errorf("Feature[%d]: expected '%s', got '%s'", i, f, features[i])
		}
	}
}

func TestIsBuildingFlag(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	if b.IsBuilding() {
		t.Error("Should not be building initially")
	}
}
