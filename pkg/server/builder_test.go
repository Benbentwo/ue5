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

func TestCrossProjectQueueRefused(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Simulate an in-progress build for project A without spawning goroutines.
	b.mu.Lock()
	b.building = true
	b.activeProject = "/test/A.uproject"
	b.mu.Unlock()

	// A request for a different project must be refused, not silently coalesced.
	_, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/B.uproject", Mode: BuildModeHotReload, Label: "B change", AgentID: "b",
	})
	if err == nil || !strings.Contains(err.Error(), "cross-project") {
		t.Fatalf("expected cross-project refusal, got err=%v", err)
	}

	// A same-project request still queues normally.
	rec, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/A.uproject", Mode: BuildModeHotReload, Label: "A change", AgentID: "a",
	})
	if err != nil {
		t.Fatalf("same-project queue failed: %v", err)
	}
	if rec == nil || rec.Status != BuildStatusPending {
		t.Errorf("expected pending queued record, got %+v", rec)
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

	// Stub the build step so we can test the approval flow
	b.buildRunner = func(record *BuildRecord) error { return nil }

	callCount := 0
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		callCount++
		if callCount == 1 {
			return false, []string{"session-1"}
		}
		return true, nil
	}
	b.approvalRetryInterval = 10 * time.Millisecond

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

func TestFullRebuildProceedsAfterApprovalDeadline(t *testing.T) {
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

	buildRan := false
	b.buildRunner = func(record *BuildRecord) error {
		buildRan = true
		return nil
	}

	// An approver that never approves — e.g. a wedged client that times out
	// every round. Without a deadline this loops forever.
	approverCalls := 0
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		approverCalls++
		return false, []string{"session-stuck"}
	}
	b.approvalRetryInterval = 10 * time.Millisecond
	b.approvalTimeout = 50 * time.Millisecond

	record := &BuildRecord{
		ID:          "test-deadline",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeFull,
	}

	done := make(chan error, 1)
	go func() { done <- b.executeFullRebuild(context.Background(), record) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Expected build to proceed after approval deadline, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("executeFullRebuild did not return: approval deadline was not enforced")
	}

	if !buildRan {
		t.Error("Expected build to run once the approval deadline expired")
	}
	if approverCalls < 2 {
		t.Errorf("Expected approver to be retried before the deadline, got %d call(s)", approverCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	foundForced := false
	for _, e := range events {
		if e.Type == "restart_forced" {
			foundForced = true
			break
		}
	}
	if !foundForced {
		t.Error("Expected 'restart_forced' event when proceeding past blockers")
	}
}

func TestFullRebuildCancelledDuringApprovalWait(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Stub the build step so we can test the approval flow
	b.buildRunner = func(record *BuildRecord) error { return nil }

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

func TestBuildCaptureSubscribe(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// No active build — subscribe should return nil, error
	_, err := b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err == nil {
		t.Error("Expected error when no build is active")
	}

	// No active build — recent lines should return empty
	lines := b.RecentBuildLines(100)
	if len(lines) != 0 {
		t.Errorf("Expected 0 recent lines, got %d", len(lines))
	}
}

func TestBuildCaptureLifecycle(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Simulate setting a build capture
	logPath := filepath.Join(t.TempDir(), "build.log")
	capture, err := NewLogCapture(logPath)
	if err != nil {
		t.Fatalf("Failed to create LogCapture: %v", err)
	}

	b.setBuildCapture(capture)

	// Should now be able to subscribe
	ch, err := b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if ch == nil {
		t.Fatal("Expected non-nil channel")
	}

	// Clear the capture
	b.clearBuildCapture()

	// Should error again
	_, err = b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err == nil {
		t.Error("Expected error after clearing build capture")
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

// waitForOrchestratorIdle blocks until no build is running or queued. The
// orchestrator clears its building flag only after the final state Save, so
// idleness also means background goroutines are done touching the state file
// — required before t.TempDir cleanup runs.
func waitForOrchestratorIdle(t *testing.T, b *BuildOrchestrator) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for b.IsBuilding() || b.PendingBuild() != nil {
		if time.Now().After(deadline) {
			t.Fatal("orchestrator did not quiesce within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestQueuedRequestsShareStableBuildID covers the ID-continuity contract for
// queued rebuilds: every caller that queues while a build is in flight gets
// the SAME pending id, and the coalesced build keeps that id when it starts,
// so the ids handed out are pollable across their whole lifecycle. The old
// behavior minted a fresh id per queued caller and a different one again for
// the coalesced build — ids that never appeared in state or events.
func TestQueuedRequestsShareStableBuildID(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	b.buildRunner = func(record *BuildRecord) error {
		once.Do(func() { close(started) })
		<-release // first build blocks; coalesced build sails through (closed)
		return nil
	}
	b.restartApprover = func(ctx context.Context) (bool, []string) { return true, nil }

	first, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "first", AgentID: "agent-A",
	})
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	<-started // first build is now in flight (and recorded as current)

	second, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeHotReload, Label: "second", AgentID: "agent-B",
	})
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	third, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "third", AgentID: "agent-C",
	})
	if err != nil {
		t.Fatalf("third request failed: %v", err)
	}

	if second.Status != BuildStatusPending || third.Status != BuildStatusPending {
		t.Fatalf("queued requests should be pending, got %q / %q", second.Status, third.Status)
	}
	if second.ID != third.ID {
		t.Fatalf("queued callers must share one pending id, got %q vs %q", second.ID, third.ID)
	}
	if second.ID == first.ID {
		t.Fatalf("pending id must differ from the in-flight build id %q", first.ID)
	}
	if len(third.Labels) != 2 || third.Labels[0] != "second" || third.Labels[1] != "third" {
		t.Errorf("pending record should accumulate queued labels, got %v", third.Labels)
	}
	if third.Mode != BuildModeFull {
		t.Errorf("full mode must win escalation, got %q", third.Mode)
	}

	// The pending id resolves while queued.
	if p := b.PendingBuild(); p == nil || p.ID != second.ID {
		t.Fatalf("PendingBuild should expose the queued record, got %+v", p)
	}

	close(release)

	// The coalesced build must reach state under the SAME id the callers hold.
	deadline := time.Now().Add(5 * time.Second)
	for {
		current := state.GetCurrentBuild()
		if current != nil && current.ID == second.ID && current.Status == BuildStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("coalesced build never became current under the pending id %q; current=%+v", second.ID, current)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if p := b.PendingBuild(); p != nil {
		t.Errorf("pending record should clear once the coalesced build starts, got %+v", p)
	}
	waitForOrchestratorIdle(t, b)
}

// TestPendingBuildCopyIsIsolated guards against later queue merges mutating a
// record already returned to a caller (shared slice backing arrays).
func TestPendingBuildCopyIsIsolated(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	b := NewBuildOrchestrator(NewInstanceManager(), state, NewAgentRegistry())

	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	b.buildRunner = func(record *BuildRecord) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	b.restartApprover = func(ctx context.Context) (bool, []string) { return true, nil }

	if _, err := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "first", AgentID: "agent-A",
	}); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	<-started

	second, _ := b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "second", AgentID: "agent-B",
	})
	labelsBefore := append([]string(nil), second.Labels...)

	_, _ = b.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "third", AgentID: "agent-C",
	})

	if len(second.Labels) != len(labelsBefore) {
		t.Errorf("returned record mutated by later queue merge: %v -> %v", labelsBefore, second.Labels)
	}

	close(release)
	waitForOrchestratorIdle(t, b)
}
