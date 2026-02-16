---
name: rebuild
description: Stop editor, rebuild the project, and restart editor using the server daemon's build orchestrator
argument-hint: "[--mode full|hot_reload] [--label description]"
allowed-tools:
  - Bash
  - Read
  - Glob
---

# Rebuild UE5 Project

Trigger a rebuild through the server daemon's build orchestrator. This handles the full lifecycle (stop → build → restart) atomically with build metadata tracking.

## Arguments

- `mode` (optional): `full` (default) for stop→build→restart, `hot_reload` for build-in-background
- `label` (optional): Description of what changed - if not provided, summarize the recent code changes

## Process

1. **Check current build state**
   ```bash
   ue5 server build-info --json
   ```
   - Review accumulated features to understand what's already built
   - Check if a build is currently in progress

2. **Determine the label**
   - If the user provided a label, use it
   - Otherwise, summarize what changed since the last build based on the conversation context

3. **Trigger the rebuild via daemon**
   ```bash
   ue5 server rebuild --label "Description of changes" --mode full
   ```
   - For C++ header changes or major refactoring: use `--mode full`
   - For .cpp-only changes: use `--mode hot_reload`
   - The daemon handles stop→build→restart atomically
   - If another build is in progress, the request is queued and coalesced

4. **Check build result**
   ```bash
   ue5 server build-info --json
   ```
   - If succeeded: report success and accumulated features
   - If failed: query build errors

5. **If build failed, query errors**
   ```bash
   ue5 server logs --level error --since 5m
   ```
   - Analyze compilation errors
   - Suggest fixes
   - Do NOT attempt to restart the editor

6. **Verify editor is running (full mode only)**
   ```bash
   ue5 server status --json
   ```
   - Confirm editor restarted with new build
   - Report build time and new PID

## Build Modes

| Mode | Use Case | What Happens |
|------|----------|--------------|
| `full` | Header changes, major refactoring, new classes | Stop editor → UBT build → restart editor |
| `hot_reload` | .cpp-only changes, small fixes | UBT build in background, editor stays running |

## Build Configurations

| Config | Use Case |
|--------|----------|
| `Development` | Default, good for iteration |
| `DebugGame` | Deep debugging with symbols |
| `Shipping` | Release build, optimized |
| `Test` | Testing builds |

## Important Notes

- The daemon's build orchestrator handles multi-agent coordination automatically
- If multiple agents request rebuilds, they are coalesced into a single build
- Build logs are captured to `~/.ue5/logs/<hash>/build.log`
- If build fails, editor will NOT be restarted (full mode)
- After build failure, fix code and run `/uem:rebuild` again
- Logs from the previous editor session are preserved and queryable
