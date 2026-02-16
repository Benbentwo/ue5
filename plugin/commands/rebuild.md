---
name: rebuild
description: Stop editor, rebuild the project, and restart editor
argument-hint: "[config]"
allowed-tools:
  - Bash
  - Read
  - Glob
---

# Rebuild UE5 Project

Perform a complete rebuild cycle: stop the running editor, build the project, and restart the editor.

## Arguments

- `config` (optional): Build configuration - Development (default), DebugGame, Shipping, Test

## Process

1. **Stop the editor via server daemon**
   ```bash
   ue5 server kill
   ```
   - Gracefully stops the managed editor instance
   - Wait for confirmation before proceeding

2. **Build the project**
   ```bash
   ue5 build
   ```
   - Uses the existing build command with the project in the current directory
   - Streams build output for progress monitoring

3. **Check build result**
   - If successful: proceed to start
   - If failed: display error summary, do NOT start editor
   - Use `/ue:logs` from previous session to correlate with runtime errors

4. **Start the editor via server daemon**
   ```bash
   ue5 server run
   ```
   - Launches editor managed by the daemon
   - Logs are automatically captured from the new session

5. **Verify and report**
   ```bash
   ue5 server status --json
   ```
   - Confirm editor is running
   - Report build time, any warnings, and new PID

## Build Configurations

| Config | Use Case |
|--------|----------|
| `Development` | Default, good for iteration |
| `DebugGame` | Deep debugging with symbols |
| `Shipping` | Release build, optimized |
| `Test` | Testing builds |

## Important Notes

- The full cycle takes time - builds can be several minutes
- If build fails, editor will NOT be started
- Check build output for compilation errors
- After build failure, fix code and run `/ue:rebuild` again
- Logs from the previous editor session are preserved and queryable
