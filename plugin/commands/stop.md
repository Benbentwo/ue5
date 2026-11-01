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
   - Asks the editor to quit by itself over its SadTire MCP bridge: waits for a
     booting editor to become ready, ends Play-In-Editor, then runs the editor's
     own File > Quit (closing asset editors) - or a plain `QUIT_EDITOR` when
     packages are dirty, so no save prompt can block it
   - Falls back to SIGTERM, then SIGKILL after 10 seconds, only if that fails or
     times out
   - Unsaved changes are dropped either way, as before

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
