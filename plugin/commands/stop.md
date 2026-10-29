---
name: stop
description: Stop running Unreal Editor instances (managed by ue5 server)
allowed-tools:
  - Bash
---

# Stop Unreal Editor

Stop a managed Unreal Editor instance gracefully via the ue5 server daemon.

## Process

1. **Check current status**
   ```bash
   ue5 server status --json
   ```
   - See which instances are running

2. **Stop the editor for the current project**
   ```bash
   ue5 server kill
   ```
   - Sends SIGTERM for graceful shutdown
   - Waits up to 10 seconds for clean exit
   - Falls back to SIGKILL if needed

3. **If graceful stop fails, force kill**
   ```bash
   ue5 server kill --force
   ```

4. **To stop all managed instances**
   ```bash
   ue5 server kill --all
   ```

5. **Verify shutdown**
   ```bash
   ue5 server status --json
   ```

## Important Notes

- Graceful shutdown allows the editor to save autosave data
- Force kill (`--force`) may lose unsaved work
- Unlike raw `pkill`, this only stops the instance for the current project (not ALL editors)
- Use `--all` flag to stop all managed instances
- Wait for full shutdown before rebuilding to avoid file locks
