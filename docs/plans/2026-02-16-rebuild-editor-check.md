# Pre-Restart Agent Approval Check — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Before stopping the Unreal Editor for a full rebuild, ask all connected AI agents if they're okay with a restart, and wait until they all approve.

**Architecture:** The `BuildOrchestrator.executeFullRebuild` gains a pre-stop approval loop that calls into `mcpServerWrapper.RequestRestartApproval`. This fans out MCP sampling requests to all connected SSE sessions, parses yes/no responses, and retries every 5 seconds if any agent is busy. The `BuildOrchestrator` gets a reference to the MCP server via `SetMCPServer`.

**Tech Stack:** Go, mcp-go v0.44.0 (sampling via `MCPServer.RequestSampling`), standard library `testing`

---

### Task 1: Enable Sampling on MCP Server

**Files:**
- Modify: `pkg/server/mcpserver.go:27-44` (newMCPServer function)

**Step 1: Write the failing test**

Add to `pkg/server/mcpserver_test.go` (new file):

```go
package server

import (
	"testing"
)

func TestMCPServerEnablesSampling(t *testing.T) {
	d := &Daemon{
		version: "test",
	}
	w := newMCPServer(d)
	if w.mcp == nil {
		t.Fatal("MCP server should be created")
	}
	// Verify the server was created (sampling is enabled internally)
	// We can't directly assert on capabilities, but we ensure
	// newMCPServer doesn't panic with sampling enabled.
}
```

**Step 2: Run test to verify it passes (baseline)**

Run: `go test ./pkg/server/ -run TestMCPServerEnablesSampling -v`
Expected: PASS (we're just confirming the setup works before adding EnableSampling)

**Step 3: Add `EnableSampling()` call**

In `pkg/server/mcpserver.go`, in `newMCPServer`, add after the `NewMCPServer` call:

```go
s.EnableSampling()
```

**Step 4: Run test to verify it still passes**

Run: `go test ./pkg/server/ -run TestMCPServerEnablesSampling -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/mcpserver.go pkg/server/mcpserver_test.go
git commit -m "feat: enable MCP sampling capability on daemon server"
```

---

### Task 2: Add `RequestRestartApproval` Method to MCP Server

**Files:**
- Modify: `pkg/server/mcpserver.go` (add method)
- Modify: `pkg/server/mcpserver_test.go` (add tests)

**Step 1: Write the failing test — no sessions returns approved**

```go
func TestRequestRestartApproval_NoSessions(t *testing.T) {
	d := &Daemon{version: "test"}
	w := newMCPServer(d)

	approved, blockers := w.RequestRestartApproval(context.Background())
	if !approved {
		t.Error("Expected approved=true when no sessions are connected")
	}
	if len(blockers) != 0 {
		t.Errorf("Expected no blockers, got %v", blockers)
	}
}
```

Add `"context"` to the test file imports.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestRequestRestartApproval_NoSessions -v`
Expected: FAIL — method does not exist

**Step 3: Implement `RequestRestartApproval`**

Add to `pkg/server/mcpserver.go`:

```go
// RequestRestartApproval asks all connected MCP clients whether they are okay
// with an editor restart. Returns true if all agents approve (or if no agents
// are connected). Returns false with a list of blocking session IDs if any
// agent is busy or fails to respond.
func (w *mcpServerWrapper) RequestRestartApproval(ctx context.Context) (approved bool, blockingAgents []string) {
	w.sessionsMu.RLock()
	if len(w.sessions) == 0 {
		w.sessionsMu.RUnlock()
		return true, nil
	}

	// Snapshot sessions
	type sessionEntry struct {
		id  string
		ctx context.Context
	}
	entries := make([]sessionEntry, 0, len(w.sessions))
	for id, sCtx := range w.sessions {
		entries = append(entries, sessionEntry{id: id, ctx: sCtx})
	}
	w.sessionsMu.RUnlock()

	samplingReq := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Type: "text",
						Text: "A rebuild has been requested that requires restarting the Unreal Editor. " +
							"Are you currently performing any work in the Unreal Editor that would be " +
							"disrupted by a restart? Respond with ONLY 'yes' or 'no'.",
					},
				},
			},
			MaxTokens: 10,
		},
	}

	type result struct {
		sessionID string
		busy      bool
	}

	results := make(chan result, len(entries))

	for _, entry := range entries {
		go func(e sessionEntry) {
			samplingCtx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
			defer cancel()

			mcpSrv := mcpserver.ServerFromContext(e.ctx)
			if mcpSrv == nil {
				// Can't reach this session — treat as blocking (conservative)
				results <- result{sessionID: e.id, busy: true}
				return
			}

			resp, err := mcpSrv.RequestSampling(samplingCtx, samplingReq)
			if err != nil {
				log.Debug("Sampling request failed, treating as busy", "session", e.id, "error", err)
				results <- result{sessionID: e.id, busy: true}
				return
			}

			results <- result{sessionID: e.id, busy: parseRestartResponse(resp)}
		}(entry)
	}

	// Collect results
	for range entries {
		r := <-results
		if r.busy {
			blockingAgents = append(blockingAgents, r.sessionID)
		}
	}

	return len(blockingAgents) == 0, blockingAgents
}

// parseRestartResponse checks whether the agent indicated it is busy.
// The question asks "would you be disrupted?", so:
//   - "yes" → agent IS busy → return true (busy)
//   - "no"  → agent is NOT busy → return false (not busy)
//   - anything else → conservative: return true (busy)
func parseRestartResponse(resp *mcp.CreateMessageResult) bool {
	if resp == nil {
		return true // no response = busy (conservative)
	}
	text := ""
	if tc, ok := resp.Content.(mcp.TextContent); ok {
		text = tc.Text
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(lower, "no") {
		return false // "no" = not busy = approved
	}
	// "yes" or anything else = busy
	return true
}
```

Add `"strings"` to the imports in `mcpserver.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/server/ -run TestRequestRestartApproval_NoSessions -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/mcpserver.go pkg/server/mcpserver_test.go
git commit -m "feat: add RequestRestartApproval with MCP sampling fan-out"
```

---

### Task 3: Write `parseRestartResponse` Unit Tests

**Files:**
- Modify: `pkg/server/mcpserver_test.go`

**Step 1: Write tests**

```go
func TestParseRestartResponse_No(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "no"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("'no' should mean not busy")
	}
}

func TestParseRestartResponse_Yes(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "yes"},
		},
	}
	if !parseRestartResponse(resp) {
		t.Error("'yes' should mean busy")
	}
}

func TestParseRestartResponse_CaseInsensitive(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "No"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("'No' (capitalized) should mean not busy")
	}
}

func TestParseRestartResponse_NilResponse(t *testing.T) {
	if !parseRestartResponse(nil) {
		t.Error("nil response should be treated as busy (conservative)")
	}
}

func TestParseRestartResponse_GarbageText(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "I'm not sure what you mean"},
		},
	}
	if !parseRestartResponse(resp) {
		t.Error("ambiguous response should be treated as busy (conservative)")
	}
}

func TestParseRestartResponse_NoInSentence(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "No, I am not working on anything"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("Response containing 'no' should mean not busy")
	}
}
```

Add `"github.com/mark3labs/mcp-go/mcp"` to the test file imports.

**Step 2: Run tests**

Run: `go test ./pkg/server/ -run TestParseRestartResponse -v`
Expected: PASS (implementation already done in Task 2)

**Step 3: Commit**

```bash
git add pkg/server/mcpserver_test.go
git commit -m "test: add parseRestartResponse unit tests"
```

---

### Task 4: Add `SetMCPServer` to `BuildOrchestrator`

**Files:**
- Modify: `pkg/server/builder.go:17-24` (struct) and add method
- Modify: `pkg/server/builder_test.go`

**Step 1: Write the failing test**

Add to `pkg/server/builder_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestSetMCPServer -v`
Expected: FAIL — `b.mcpServer` and `SetMCPServer` do not exist

**Step 3: Implement**

In `pkg/server/builder.go`, add `mcpServer` field to the struct:

```go
type BuildOrchestrator struct {
	manager   *InstanceManager
	state     *StateStore
	agents    *AgentRegistry
	mcpServer *mcpServerWrapper
	mu        sync.Mutex
	building  bool
	queue     []RebuildRequest
}
```

Add the method:

```go
// SetMCPServer sets the MCP server reference for restart approval checks.
func (b *BuildOrchestrator) SetMCPServer(s *mcpServerWrapper) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mcpServer = s
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/server/ -run TestSetMCPServer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/builder.go pkg/server/builder_test.go
git commit -m "feat: add SetMCPServer to BuildOrchestrator"
```

---

### Task 5: Add Approval Loop to `executeFullRebuild`

**Files:**
- Modify: `pkg/server/builder.go:209-256` (executeFullRebuild method)

**Step 1: Write the failing test**

Add to `pkg/server/builder_test.go`:

```go
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

	// Use a mock approver that blocks once then approves
	callCount := 0
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		callCount++
		if callCount == 1 {
			return false, []string{"session-1"}
		}
		return true, nil
	}

	// We can't run a real full rebuild (no engine), but we can verify
	// the approval loop emits the event. The build will fail at the
	// actual build step, which is fine — we're testing the approval loop.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	record := &BuildRecord{
		ID:          "test-build",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeFull,
	}

	// This will fail at StopEditor or runBuild, but the approval loop
	// should have run first
	_ = b.executeFullRebuild(ctx, record)

	mu.Lock()
	defer mu.Unlock()

	// Check that restart_blocked was emitted
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
```

Add `"sync"` and `"time"` to the test file imports if not already present.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestFullRebuildEmitsRestartBlocked -v`
Expected: FAIL — `restartApprover` field doesn't exist

**Step 3: Implement the approval loop**

In `pkg/server/builder.go`, add a `restartApprover` field to the struct (this allows testing without a real MCP server):

```go
type BuildOrchestrator struct {
	manager         *InstanceManager
	state           *StateStore
	agents          *AgentRegistry
	mcpServer       *mcpServerWrapper
	restartApprover func(ctx context.Context) (bool, []string) // for testing
	mu              sync.Mutex
	building        bool
	queue           []RebuildRequest
}
```

Update `SetMCPServer` to also wire the approver:

```go
func (b *BuildOrchestrator) SetMCPServer(s *mcpServerWrapper) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mcpServer = s
	b.restartApprover = s.RequestRestartApproval
}
```

Modify `executeFullRebuild` to add the approval loop before stopping the editor. Replace the beginning of the method (before `// Step 1: Stop the editor`) with:

```go
func (b *BuildOrchestrator) executeFullRebuild(ctx context.Context, record *BuildRecord) error {
	// Step 0: Check with connected agents before stopping editor
	if b.restartApprover != nil {
		for {
			approved, blockers := b.restartApprover(ctx)
			if approved {
				break
			}
			log.Info("Restart blocked by agents, retrying in 5s",
				"build_id", record.ID, "blockers", blockers)
			b.agents.Emit(AgentEvent{
				Type:      "restart_blocked",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"build_id":        record.ID,
					"blocking_agents": blockers,
				},
			})
			select {
			case <-ctx.Done():
				return fmt.Errorf("build cancelled while waiting for agent approval")
			case <-time.After(5 * time.Second):
				// retry
			}
		}
	}

	// Step 1: Stop the editor
	// ... rest of existing code unchanged
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/server/ -run TestFullRebuildEmitsRestartBlocked -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./pkg/server/ -v`
Expected: All existing tests still pass

**Step 6: Commit**

```bash
git add pkg/server/builder.go pkg/server/builder_test.go
git commit -m "feat: add pre-restart agent approval loop to full rebuild"
```

---

### Task 6: Wire MCP Server to Builder in Daemon

**Files:**
- Modify: `pkg/server/daemon.go:101-108`

**Step 1: Add the wiring**

In `pkg/server/daemon.go`, after `d.mcpServer = newMCPServer(d)` (line 102), add:

```go
d.builder.SetMCPServer(d.mcpServer)
```

This goes right after the `d.mcpServer = newMCPServer(d)` line and before the `go func()` that starts the MCP server.

**Step 2: Verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 3: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

**Step 4: Commit**

```bash
git add pkg/server/daemon.go
git commit -m "feat: wire MCP server to builder for restart approval"
```

---

### Task 7: Test Cancellation During Approval Wait

**Files:**
- Modify: `pkg/server/builder_test.go`

**Step 1: Write the test**

```go
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
```

Add `"strings"` to the test file imports.

**Step 2: Run test**

Run: `go test ./pkg/server/ -run TestFullRebuildCancelledDuringApprovalWait -v`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/server/builder_test.go
git commit -m "test: verify build cancellation during approval wait"
```

---

### Task 8: Test No Approval Check for Hot Reload

**Files:**
- Modify: `pkg/server/builder_test.go`

**Step 1: Write the test**

```go
func TestHotReloadSkipsApprovalCheck(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	approverCalled := false
	b.restartApprover = func(ctx context.Context) (bool, []string) {
		approverCalled = true
		return false, []string{"session-1"} // would block if called
	}

	record := &BuildRecord{
		ID:          "test-hotreload",
		ProjectPath: "/fake/project.uproject",
		Mode:        BuildModeHotReload,
	}

	// Hot reload will fail at the build step (no engine), but the
	// approval check should NOT have been called
	_ = b.executeHotReload(context.Background(), record)

	if approverCalled {
		t.Error("Approval check should NOT be called for hot reload")
	}
}
```

**Step 2: Run test**

Run: `go test ./pkg/server/ -run TestHotReloadSkipsApprovalCheck -v`
Expected: PASS (hot reload path doesn't touch the approval check)

**Step 3: Commit**

```bash
git add pkg/server/builder_test.go
git commit -m "test: verify hot reload skips approval check"
```

---

### Task 9: Final Verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests pass

**Step 2: Verify build**

Run: `go build ./...`
Expected: Clean build, no warnings

**Step 3: Review diff**

Run: `git diff main --stat`
Expected: Changes only in:
- `pkg/server/builder.go`
- `pkg/server/builder_test.go`
- `pkg/server/daemon.go`
- `pkg/server/mcpserver.go`
- `pkg/server/mcpserver_test.go` (new)
- `docs/plans/` (design + plan docs)
