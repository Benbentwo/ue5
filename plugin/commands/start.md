---
name: start
description: Start Unreal Editor with the current project (managed by ue5 server)
allowed-tools:
  - Bash
  - Read
  - Glob
---

# Start Unreal Editor

Launch Unreal Editor with the project in the current directory, managed by the ue5 server daemon. Logs are automatically captured and queryable via `/uem:logs`.

## Process

1. **Start the editor via the server daemon**
   ```bash
   ue5 server run
   ```
   - This auto-starts the daemon if not already running
   - The daemon manages the editor process lifecycle and captures all logs
   - If a `--project` flag is needed, use `ue5 server run -p /path/to/Project.uproject`

2. **Verify startup**
   ```bash
   ue5 server status --json
   ```
   - Check that the instance state is "running"
   - Note the PID and log file path

3. **Report success to user**
   - Project name, PID, and log file location
   - Remind that `/uem:logs` can be used to view captured logs
   - Remind that `/uem:stop` stops the managed instance

## Important Notes

- The editor is managed by the daemon -- use `/uem:stop` to stop it (not pkill)
- Logs are captured automatically and queryable via `/uem:logs`
- The daemon persists across terminal sessions
- If the editor is already running for this project, the command will report the existing instance
- Use `ue5 server status` to see all managed instances
