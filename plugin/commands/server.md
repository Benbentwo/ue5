---
name: server
description: Manage the ue5 server daemon that tracks editor instances, captures logs, coordinates rebuilds, and provides MCP notifications
allowed-tools:
  - Bash
---

# UE5 Server Management

Manage the ue5 server daemon that tracks Unreal Editor instances, captures logs, coordinates AI-driven rebuilds, and provides MCP push notifications.

## Commands

### Start the daemon
```bash
ue5 server start
```

### Stop the daemon (and all managed editors)
```bash
ue5 server stop
```

### Check daemon and instance status
```bash
ue5 server status --json
```

### Start an editor instance
```bash
ue5 server run
```

### Stop an editor instance
```bash
ue5 server kill
```

### Query captured logs
```bash
ue5 server logs --level error --lines 50
```

### Trigger a rebuild
```bash
ue5 server rebuild --label "Description of changes" --mode full
ue5 server rebuild --label "Small fix" --mode hot_reload
```

### Check build metadata and accumulated features
```bash
ue5 server build-info --json
```

### List registered AI agents
```bash
ue5 server agents --json
```

## Process

1. Determine what the user needs (status check, rebuild, start/stop, etc.)
2. Run the appropriate `ue5 server` subcommand
3. Report the result to the user

## Important Notes

- The daemon auto-starts when using `ue5 server run`, `ue5 server logs`, or `ue5 server rebuild`
- Use `--json` flag on `status`, `build-info`, and `agents` for machine-readable output
- The daemon manages editor lifecycle, log capture, build orchestration, and agent tracking
- State is stored in `~/.ue5/` (socket, PID file, logs, state.json)
- Logs are stored per-project in `~/.ue5/logs/<hash>/editor.log`
- Build logs are stored per-project in `~/.ue5/logs/<hash>/build.log`
- MCP SSE server runs on port 9515 (configurable via `UE5_MCP_PORT`)
- Multiple agent rebuild requests are automatically coalesced into single builds
