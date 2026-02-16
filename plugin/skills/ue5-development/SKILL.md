---
name: ue5-server-mode
description: This skill should be used when working with the UE5 server daemon for managing Unreal Editor instances, triggering AI-driven rebuilds, tracking build metadata, registering as an agent consumer, or when the user mentions "ue5 server", "editor server", "server mode", "rebuild", "build metadata", "MCP", "agent registration", "hot reload", or needs to manage editor lifecycle programmatically. Provides knowledge about the server daemon architecture, AI rebuild system, MCP integration, agent sessions, and multi-agent coordination.
---

# UE5 Server Mode

The UE5 CLI includes a Server Mode that runs a background daemon to manage Unreal Editor instances. The daemon captures all editor output (stdout/stderr), tracks process lifecycle, coordinates AI-driven rebuilds with coalescing, and provides both a JSON API and MCP (Model Context Protocol) server for AI agent integration.

## Architecture

```
AI Agent ──► MCP SSE ──► Daemon Process
              :9515       │
AI Agent ──► ue5 CLI ──► Unix Socket
                          ~/.ue5/         ├─ Instance Manager
                          server.sock     │    ├─ Project A (PID, state, logs)
                                          │    └─ Project B (PID, state, logs)
                                          ├─ State Store
                                          │    └─ ~/.ue5/state.json
                                          ├─ Agent Registry
                                          │    └─ Tracks registered AI sessions
                                          ├─ Build Orchestrator
                                          │    └─ Coalescing queue
                                          └─ MCP SSE Server (:9515)
                                               └─ Push notifications
```

- **Dual protocol**: Unix socket (CLI) + MCP SSE (AI agents)
- **Multi-project**: Tracks multiple editor instances by project path
- **Multi-agent**: Multiple AI agents work concurrently with coalesced rebuilds
- **Auto-start**: Daemon starts automatically when needed

## Core Commands

### Daemon Management
```bash
ue5 server start          # Start daemon (or auto-starts with other commands)
ue5 server start -f       # Run in foreground (for debugging)
ue5 server stop           # Stop daemon and all editors
ue5 server status         # Human-readable status
ue5 server status --json  # Machine-readable JSON status
```

### Editor Lifecycle
```bash
ue5 server run            # Start editor for current project (auto-starts daemon)
ue5 server run -p /path/to/Project.uproject  # Specify project explicitly
ue5 server kill           # Stop editor for current project (SIGTERM)
ue5 server kill --force   # Force stop (SIGKILL)
ue5 server kill --all     # Stop all managed editors
```

### AI-Driven Rebuilds
```bash
ue5 server rebuild --label "Added inventory system" --mode full
ue5 server rebuild --label "Fixed health component" --mode hot_reload
ue5 server rebuild --label "Updated AI behavior" --mode full --agent my-agent-id
```

Build modes:
- **`full`**: Stop editor → build → restart editor. Use when C++ headers changed or major refactoring.
- **`hot_reload`**: Build in background while editor stays running. Use for .cpp-only changes.

### Build Metadata
```bash
ue5 server build-info         # Current build metadata + accumulated features
ue5 server build-info --json  # Machine-readable
```

### Agent Management
```bash
ue5 server agents         # List registered AI agents
ue5 server agents --json  # Machine-readable
```

### Log Querying
```bash
ue5 server logs                             # Last 100 lines
ue5 server logs -n 50                       # Last 50 lines
ue5 server logs --level error               # Only errors
ue5 server logs --level warning             # Only warnings
ue5 server logs --category LogCompile       # Filter by UE log category
ue5 server logs --pattern "MyActor"         # Regex filter
ue5 server logs --since 5m                  # Last 5 minutes
ue5 server logs --json                      # JSON output
ue5 server logs -f                          # Stream (tail -f style)
ue5 server logs -f --level error            # Stream errors only
```

## MCP Integration

The daemon exposes an MCP SSE server on port `9515` (configurable via `UE5_MCP_PORT` env var). AI agents connect to receive push notifications about editor state changes and build events.

### MCP Tools

| Tool | Description |
|------|-------------|
| `rebuild` | Trigger rebuild (full or hot_reload) with label |
| `register_agent` | Register AI agent as consumer |
| `unregister_agent` | Unregister agent |
| `get_build_info` | Query build metadata and feature history |

### MCP Resources

| URI | Description |
|-----|-------------|
| `ue5://build/current` | Current build metadata + accumulated features |
| `ue5://agents` | Registered agents list |
| `ue5://instances` | Editor instance states |

### MCP Notifications (push to ALL connected agents)

| Event | When |
|-------|------|
| `editor_state_changed` | Instance transitions (running→stopped, etc.) |
| `rebuild_started` | Build orchestrator begins build |
| `rebuild_complete` | Build finishes (success or failure) |
| `agent_registered` | New agent registers |
| `agent_unregistered` | Agent leaves |

## Multi-Agent Coordination

Multiple AI agents can work on the same codebase simultaneously. Key concepts:

### Build Coalescing
When multiple agents request rebuilds concurrently, requests are **coalesced** into a single UBT build:
- Agent A requests rebuild with label "inventory system"
- Agent B requests rebuild with label "health component" (while A's build runs)
- Result: one build that includes both changes, both agents notified

### Mode Escalation
If any agent requests `full` mode, the coalesced build uses `full` mode (most restrictive wins).

### Feature Accumulation
Each build accumulates features from all previous builds. Query `accumulated_features` to see exactly what's in the current running binary:
```bash
ue5 server build-info --json | jq '.accumulated_features'
```

### Agent Registration
Agents register once to inform the daemon how many consumers exist. All registered agents receive MCP notifications when builds complete or the editor restarts.

## AI Debugging Workflow

### 1. Register as an agent (once per session)
Via MCP `register_agent` tool or CLI.

### 2. Check current build state
```bash
ue5 server build-info --json
```

### 3. After making code changes, trigger a rebuild
```bash
ue5 server rebuild --label "Fixed null pointer in PlayerController" --mode full
```

### 4. Query errors from the new build
```bash
ue5 server logs --level error --since 2m --json
```

### 5. Search for specific patterns
```bash
ue5 server logs --pattern "NullPtr|nullptr|Access violation" --lines 200
```

### 6. Verify the fix
```bash
ue5 server logs --level error --since 1m
```

## Build Record Format

```json
{
  "id": "build-1234567890",
  "project_path": "/path/to/MyGame.uproject",
  "labels": ["Added inventory system", "Fixed health component"],
  "features": ["Login system", "Combat system", "Added inventory system", "Fixed health component"],
  "contributions": [
    {"agent_id": "agent-A", "label": "Added inventory system"},
    {"agent_id": "agent-B", "label": "Fixed health component"}
  ],
  "mode": "full",
  "status": "succeeded",
  "started_at": "2026-02-15T10:00:00Z",
  "completed_at": "2026-02-15T10:03:00Z",
  "target": "MyGameEditor",
  "platform": "Mac",
  "configuration": "Development"
}
```

## Instance States

| State | Meaning |
|-------|---------|
| `starting` | Process launched, waiting for initialization |
| `running` | Editor is active |
| `stopping` | SIGTERM sent, waiting for graceful exit |
| `stopped` | Exited cleanly (exit code 0) |
| `crashed` | Exited with non-zero exit code |

## Log File Location

Logs are stored at `~/.ue5/logs/<project-hash>/editor.log` with the format:
```
2026-02-15T10:00:00.123Z stdout | LogInit: Display: Initializing engine
2026-02-15T10:00:00.456Z stderr | LogCore:Warning: Some warning here
```

Build logs are stored at `~/.ue5/logs/<project-hash>/build.log`.

## Key Differences from Raw Shell Commands

| Before (raw commands) | After (server mode) |
|----------------------|---------------------|
| `pkill UnrealEditor` (kills ALL instances) | `ue5 server kill` (kills only current project) |
| `ue5 server kill && ue5 build && ue5 server run` | `ue5 server rebuild --label "..." --mode full` |
| `tail -f Saved/Logs/*.log` | `ue5 server logs -f` (captured from process stdout) |
| `grep Error Saved/Logs/*.log` | `ue5 server logs --level error` (parsed and filtered) |
| No process tracking | `ue5 server status --json` (full lifecycle tracking) |
| Logs lost on crash | Logs captured to persistent file |
| No build metadata | `ue5 server build-info` (features, history, status) |
| Manual multi-agent coordination | Automatic coalescing + MCP notifications |
