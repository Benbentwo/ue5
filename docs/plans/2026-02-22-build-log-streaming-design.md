# Build Log Streaming to Dashboard

## Goal

Stream build output in real-time to the dashboard, displayed as a collapsible terminal-style log viewer underneath each editor instance card. Only active during ongoing builds.

## Architecture

### Backend: Build Log Capture (`builder.go`, `pkg/build.go`)

Replace direct file I/O in `runBuild()` with `LogCapture`-based streaming:

- Add `buildCapture *LogCapture` and `buildCaptureMu sync.RWMutex` to `BuildOrchestrator`
- New `RunBuildScriptPiped()` in `pkg/build.go` that returns `cmd`, `stdout`, `stderr` pipes instead of writing to an `io.Writer`
- In `runBuild()`: create a `LogCapture`, pipe `cmd.StdoutPipe()`/`cmd.StderrPipe()` through `CaptureStream()`, store as `b.buildCapture`
- Clear `buildCapture` after build completes
- Expose `SubscribeBuildLogs(filter)` and `RecentBuildLines(n)` on `BuildOrchestrator`

### Backend: SSE Endpoint (`dashboard.go`)

New route: `GET /api/build/logs/stream?project={path}`

1. If no active build, return 204 No Content
2. Send `build_log_history` event with recent lines from ring buffer (scroll-back)
3. Subscribe to `buildCapture` and stream each line as `build_log` SSE event
4. On build completion (channel close), send `build_log_end` event

Event payloads:
- `build_log_history`: `{"lines": [LogLineEvent, ...]}`
- `build_log`: `{"timestamp":"...","raw":"...","level":"...","category":"..."}`
- `build_log_end`: `{"build_id":"...","status":"succeeded|failed"}`

### Frontend: Hook (`useBuildLogStream.ts`)

- Takes `projectPath` and `active` boolean
- Opens `EventSource` to `/api/build/logs/stream?project={path}` when active
- Accumulates lines from `build_log_history` (initial) and `build_log` (live)
- Clears and disconnects on `build_log_end` or when `active` becomes false
- Auto-reconnect with exponential backoff

### Frontend: Component (`BuildLogViewer.tsx`)

- Monospace, dark terminal-style container with ~300px max-height
- Color-coded by level: red (error), yellow (warning), default (info)
- Auto-scrolls to bottom; pauses auto-scroll when user scrolls up, resumes at bottom
- Line count indicator

### Frontend: Integration (`InstancePanel.tsx`)

- Collapsible "Build Logs" section under each instance card
- Visible when `buildInfo.current_build` exists with status `building`/`pending` and matching `project_path`
- Auto-expands when build starts, collapses when build ends

## Files Changed

| File | Change |
|------|--------|
| `pkg/build.go` | Add `RunBuildScriptPiped()` |
| `pkg/server/builder.go` | Add `buildCapture` field, rewrite `runBuild()`, add subscriber methods |
| `pkg/server/dashboard.go` | Add `GET /api/build/logs/stream` route and handler |
| `dashboard/src/hooks/useBuildLogStream.ts` | New hook for SSE log streaming |
| `dashboard/src/components/BuildLogViewer.tsx` | New terminal-style log viewer component |
| `dashboard/src/components/InstancePanel.tsx` | Integrate BuildLogViewer under instance cards |
| `dashboard/src/types.ts` | Add `LogLineEvent` type |
