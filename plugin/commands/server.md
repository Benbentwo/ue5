---
name: server
description: Manage the ue5 server daemon that tracks editor instances and captures logs
allowed-tools:
  - Bash
---

# UE5 Server Management

Manage the ue5 server daemon that tracks Unreal Editor instances and captures logs.

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

## Process

1. Determine what the user needs (status check, start/stop daemon, etc.)
2. Run the appropriate `ue5 server` subcommand
3. Report the result to the user

## Important Notes

- The daemon auto-starts when using `ue5 server run` or `ue5 server logs`
- Use `--json` flag on `status` for machine-readable output
- The daemon manages editor lifecycle, log capture, and process tracking
- State is stored in `~/.ue5/` (socket, PID file, logs, state)
- Logs are stored per-project in `~/.ue5/logs/<hash>/editor.log`
