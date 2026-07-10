package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// buildStatusDaemon assembles the minimal daemon wiring handleGetBuildStatus
// touches: a state store and a build orchestrator.
func buildStatusDaemon(t *testing.T) (*Daemon, *mcpServerWrapper) {
	t.Helper()
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	d := &Daemon{version: "test", state: state}
	d.builder = NewBuildOrchestrator(NewInstanceManager(), state, NewAgentRegistry())
	return d, newMCPServer(d)
}

func callGetBuildStatus(t *testing.T, w *mcpServerWrapper, args map[string]any) (*mcp.CallToolResult, BuildStatusResponse) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := w.handleGetBuildStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetBuildStatus returned transport error: %v", err)
	}
	var status BuildStatusResponse
	if !res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &status); err != nil {
			t.Fatalf("response is not a BuildStatusResponse: %v (%s)", err, text)
		}
	}
	return res, status
}

// TestGetBuildStatusByIDResolvesSupersededBuild reproduces the 2026-07-09
// incident shape: an agent's build succeeds, a "manual" build immediately
// replaces it as current, and the agent polls its own id. The lookup must
// resolve the superseded id from history instead of leaving the agent
// staring at someone else's build forever.
func TestGetBuildStatusByIDResolvesSupersededBuild(t *testing.T) {
	d, w := buildStatusDaemon(t)

	agentBuild := BuildRecord{
		ID:     "build-agent",
		Labels: []string{"delete_asset round 4"},
		Status: BuildStatusBuilding,
	}
	d.state.AddBuildRecord(agentBuild)
	now := time.Now()
	d.state.UpdateBuildStatus("build-agent", BuildStatusSucceeded, &now, "")

	manualBuild := BuildRecord{
		ID:     "build-manual",
		Labels: []string{"manual"},
		Status: BuildStatusBuilding,
	}
	d.state.AddBuildRecord(manualBuild)

	res, status := callGetBuildStatus(t, w, map[string]any{"build_id": "build-agent"})
	if res.IsError {
		t.Fatalf("expected success for superseded id, got error: %+v", res.Content)
	}
	if status.ID != "build-agent" || status.Status != BuildStatusSucceeded {
		t.Errorf("expected superseded build to resolve as succeeded, got %+v", status)
	}
	if status.Prompt != "delete_asset round 4" {
		t.Errorf("expected the agent's own prompt, got %q", status.Prompt)
	}
}

func TestGetBuildStatusDefaultsToCurrent(t *testing.T) {
	d, w := buildStatusDaemon(t)
	d.state.AddBuildRecord(BuildRecord{ID: "build-current", Labels: []string{"now"}, Status: BuildStatusBuilding})

	res, status := callGetBuildStatus(t, w, nil)
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}
	if status.ID != "build-current" || status.Status != BuildStatusBuilding {
		t.Errorf("expected current build, got %+v", status)
	}
}

func TestGetBuildStatusNoBuildsYet(t *testing.T) {
	_, w := buildStatusDaemon(t)

	res, status := callGetBuildStatus(t, w, nil)
	if res.IsError {
		t.Fatalf("no-builds-yet must not be an error: %+v", res.Content)
	}
	if status.ID != "" {
		t.Errorf("expected empty id sentinel, got %+v", status)
	}
}

// TestGetBuildStatusUnknownIDIsExplicitError: an id the daemon has never seen
// (e.g. issued before a restart) must fail loudly, not silently return some
// other build — a polling agent needs the signal to re-submit.
func TestGetBuildStatusUnknownIDIsExplicitError(t *testing.T) {
	d, w := buildStatusDaemon(t)
	d.state.AddBuildRecord(BuildRecord{ID: "build-current", Labels: []string{"now"}, Status: BuildStatusBuilding})

	res, _ := callGetBuildStatus(t, w, map[string]any{"build_id": "build-from-before-restart"})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown build id")
	}
	text := res.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "unknown build id") || !strings.Contains(text, "build-from-before-restart") {
		t.Errorf("error should name the unknown id, got %q", text)
	}
}

// TestGetBuildStatusResolvesPendingQueuedBuild: ids handed out while queued
// must resolve (as pending) before the coalesced build starts.
func TestGetBuildStatusResolvesPendingQueuedBuild(t *testing.T) {
	d, w := buildStatusDaemon(t)

	release := make(chan struct{})
	started := make(chan struct{})
	d.builder.buildRunner = func(record *BuildRecord) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	}
	d.builder.restartApprover = func(ctx context.Context) (bool, []string) { return true, nil }

	if _, err := d.builder.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "first", AgentID: "agent-A",
	}); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	<-started

	queued, err := d.builder.RequestRebuild(context.Background(), &RebuildRequest{
		ProjectPath: "/test/MyGame.uproject", Mode: BuildModeFull, Label: "queued-work", AgentID: "agent-B",
	})
	if err != nil {
		t.Fatalf("queued request failed: %v", err)
	}

	res, status := callGetBuildStatus(t, w, map[string]any{"build_id": queued.ID})
	if res.IsError {
		t.Fatalf("queued id must resolve, got error: %+v", res.Content)
	}
	if status.ID != queued.ID || status.Status != BuildStatusPending {
		t.Errorf("expected pending status for queued id, got %+v", status)
	}

	close(release)
	waitForOrchestratorIdle(t, d.builder)
}
