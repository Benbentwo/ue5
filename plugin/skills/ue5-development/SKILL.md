---
name: ue5-server-mode
description: This skill should be used when working with the UE5 server daemon for managing Unreal Editor instances, querying captured logs, debugging with log analysis, or when the user mentions "ue5 server", "editor server", "server mode", "captured logs", "log query", or needs to manage editor lifecycle programmatically. Provides knowledge about the server daemon architecture, commands, log querying, and AI-driven debugging workflows.
---

# UE5 Server Mode

The UE5 CLI includes a Server Mode that runs a background daemon to manage Unreal Editor instances. The daemon captures all editor output (stdout/stderr), tracks process lifecycle, and provides a JSON API for querying status and logs.

## Architecture

```
AI Agent ──► ue5 CLI ──► Unix Socket ──► Daemon Process
                          ~/.ue5/          │
                          server.sock      ├─ Instance Manager
                                           │    ├─ Project A (PID, state, logs)
                                           │    └─ Project B (PID, state, logs)
                                           └─ Log Capture
                                                └─ ~/.ue5/logs/<hash>/editor.log
```

- **Protocol**: Newline-delimited JSON over Unix domain socket
- **Multi-project**: Tracks multiple editor instances by project path
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

## AI Debugging Workflow

The server mode is designed specifically to support AI-driven debugging workflows:

### 1. Start a managed session
```bash
ue5 server run
```

### 2. After observing an issue, query errors
```bash
ue5 server logs --level error --since 2m --json
```

### 3. Search for specific patterns
```bash
ue5 server logs --pattern "NullPtr|nullptr|Access violation" --lines 200
```

### 4. Rebuild cycle (stop → build → start)
```bash
ue5 server kill && ue5 build && ue5 server run
```

### 5. Verify the fix
```bash
ue5 server logs --level error --since 1m
```

## Status JSON Format

```json
{
  "daemon_running": true,
  "version": "1.0.0",
  "uptime": "2h30m",
  "instances": [
    {
      "project_path": "/path/to/MyGame.uproject",
      "project_name": "MyGame",
      "engine_path": "/Users/Shared/Epic Games/UE_5.4",
      "engine_version": "5.4",
      "pid": 12345,
      "state": "running",
      "started_at": "2026-02-15T10:00:00Z",
      "log_file": "/Users/user/.ue5/logs/a1b2c3d4e5f6/editor.log"
    }
  ]
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

## Key Differences from Raw Shell Commands

| Before (raw commands) | After (server mode) |
|----------------------|---------------------|
| `pkill UnrealEditor` (kills ALL instances) | `ue5 server kill` (kills only current project) |
| `tail -f Saved/Logs/*.log` | `ue5 server logs -f` (captured from process stdout) |
| `grep Error Saved/Logs/*.log` | `ue5 server logs --level error` (parsed and filtered) |
| No process tracking | `ue5 server status --json` (full lifecycle tracking) |
| Logs lost on crash | Logs captured to persistent file |
