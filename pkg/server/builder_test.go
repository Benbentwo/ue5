package server

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestBareEditorTargetOverridden(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	record := b.createRecord([]RebuildRequest{
		{
			ProjectPath: "/Games/IslandSurvival.uproject",
			Mode:        BuildModeFull,
			Label:       "test",
			Target:      "Editor",
		},
	})

	if record.Target != "IslandSurvivalEditor" {
		t.Errorf("Expected target 'IslandSurvivalEditor', got '%s'", record.Target)
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

func TestSetMCPServer(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	if b.mcpServer != nil {
		t.Error("mcpServer should be nil initially")
	}

	d := &Daemon{version: "test"}
	w := newMCPServer(d)
	b.SetMCPServer(w)

	if b.mcpServer == nil {
		t.Error("mcpServer should be set after SetMCPServer")
	}
}

func TestFullRebuildEmitsRestartBlocked(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	var events []AgentEvent
	var mu sync.Mutex
	agents.SetEventCallback(func(event AgentEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	callCount := 0
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		callCount++
		if callCount == 1 {
			return false, []string{"session-1"}
		}
		return true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	record := &BuildRecord{
		ID:          "test-build",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeFull,
	}

	_ = b.executeFullRebuild(ctx, record)

	mu.Lock()
	defer mu.Unlock()

	foundBlocked := false
	for _, e := range events {
		if e.Type == "restart_blocked" {
			foundBlocked = true
			break
		}
	}
	if !foundBlocked {
		t.Error("Expected 'restart_blocked' event to be emitted")
	}

	if callCount < 2 {
		t.Errorf("Expected approver to be called at least twice, got %d", callCount)
	}
}

func TestFullRebuildCancelledDuringApprovalWait(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Always block
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		return false, []string{"session-1"}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	record := &BuildRecord{
		ID:          "test-cancel",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeFull,
	}

	err := b.executeFullRebuild(ctx, record)
	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}
}

func TestHotReloadSkipsApprovalCheck(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	approverCalled := false
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		approverCalled = true
		return false, []string{"session-1"}
	}

	record := &BuildRecord{
		ID:          "test-hotreload",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeHotReload,
	}

	_ = b.executeHotReload(context.Background(), record)

	if approverCalled {
		t.Error("Approval check should NOT be called for hot reload")
	}
}
