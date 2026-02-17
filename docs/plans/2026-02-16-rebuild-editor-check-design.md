# Pre-Restart Agent Approval Check

## Problem

When a full rebuild is requested, the server immediately stops and restarts the Unreal Editor. If an AI agent is actively working in the editor (e.g., inspecting properties, waiting for a Blueprint compile, or observing runtime behavior), this disrupts their work without warning.

## Solution

Before stopping the editor for a full rebuild, the server asks all connected AI agents whether they're okay with a restart. If any agent says no, the rebuild waits and retries every 5 seconds until all agents approve.

## Design Decisions

- **Conservative by default**: No response from an active session blocks the restart (treat silence as "not okay").
- **Only poll connected agents**: Disconnected agents (no active SSE session) are skipped since they can't be working in the editor.
- **Only for full rebuilds**: Hot reload doesn't stop the editor, so no approval check is needed.
- **MCP sampling for the check**: The server sends a `sampling/createMessage` request to each connected client. The AI agent responds with whether it's busy.

## Architecture

### Current flow

```
executeFullRebuild -> StopEditor -> Build -> StartEditor
```

### New flow

```
executeFullRebuild -> CheckAgentApproval (retry loop) -> StopEditor -> Build -> StartEditor
```

## Component Changes

### 1. `pkg/server/mcpserver.go` - `RequestRestartApproval`

New method on `mcpServerWrapper`:

```go
func (w *mcpServerWrapper) RequestRestartApproval(ctx context.Context) (approved bool, blockingAgents []string)
```

Behavior:
- Iterates over all active SSE sessions (`w.sessions`)
- Sends a sampling request to each session asking if the agent is currently working in the editor
- Sampling prompt: "A rebuild has been requested that requires restarting the Unreal Editor. Are you currently performing any work in the Unreal Editor that would be disrupted by a restart? Respond with ONLY 'yes' or 'no'."
- Per-agent timeout: 30 seconds for the sampling call
- If no active sessions: returns `approved = true`
- If any agent responds "yes" (busy) or fails to respond: returns `approved = false` with the blocking agent IDs
- Requires `mcpServer.EnableSampling()` during server creation

### 2. `pkg/server/builder.go` - Approval loop

In `executeFullRebuild`, before `manager.StopEditor()`:

```go
// Check with connected agents before stopping editor
if b.mcpServer != nil {
    for {
        approved, blockers := b.mcpServer.RequestRestartApproval(ctx)
        if approved {
            break
        }
        // Emit restart_blocked event
        b.agents.Emit(AgentEvent{
            Type: "restart_blocked",
            Data: map[string]interface{}{
                "build_id":        record.ID,
                "blocking_agents": blockers,
            },
        })
        // Wait and retry
        select {
        case <-ctx.Done():
            return fmt.Errorf("build cancelled while waiting for agent approval")
        case <-time.After(5 * time.Second):
            // retry
        }
    }
}
```

### 3. `pkg/server/builder.go` - MCP server reference

Add a `SetMCPServer` method (matches the `AgentRegistry.SetEventCallback` pattern):

```go
type BuildOrchestrator struct {
    // ... existing fields
    mcpServer *mcpServerWrapper
}

func (b *BuildOrchestrator) SetMCPServer(s *mcpServerWrapper) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.mcpServer = s
}
```

### 4. `pkg/server/daemon.go` - Wiring

After creating the MCP server, wire it to the builder:

```go
d.mcpServer = newMCPServer(d)
d.builder.SetMCPServer(d.mcpServer)  // NEW
```

### 5. `pkg/server/mcpserver.go` - Enable sampling

In `newMCPServer`, enable the sampling capability:

```go
s := mcpserver.NewMCPServer(
    "UE5 Editor Daemon",
    d.version,
    mcpserver.WithToolCapabilities(true),
    mcpserver.WithResourceCapabilities(true, true),
)
s.EnableSampling()  // NEW
```

### 6. Event: `restart_blocked`

Emitted when agents block a restart. Data:

```json
{
  "build_id": "build-123",
  "blocking_agents": ["session-1", "session-2"]
}
```

This lets the requesting agent know why the rebuild is delayed.

## What Does NOT Change

- Build coalescing logic (requests queue normally)
- Hot reload path (editor stays running, no check needed)
- Agent registration/unregistration
- CLI interface
- Existing MCP tools and resources

## Sampling Response Parsing

The agent's sampling response is parsed for approval:
- Contains "no" (case-insensitive) -> agent is NOT busy -> approved
- Contains "yes" (case-insensitive) -> agent IS busy -> blocked
- No response / error -> blocked (conservative default)

Note: The question asks "would you be disrupted?", so "yes" = busy = blocked, "no" = not busy = approved.
