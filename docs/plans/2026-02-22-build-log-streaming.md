# Build Log Streaming Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stream real-time build output to the dashboard as a collapsible terminal log viewer underneath each editor instance card.

**Architecture:** Wrap build process output with existing `LogCapture` infrastructure (ring buffer + subscriber fan-out), expose via a dedicated SSE endpoint, and render in a new React component integrated into `InstancePanel`.

**Tech Stack:** Go (backend), React + TypeScript + Tailwind (frontend), SSE (streaming)

---

### Task 1: Add `RunBuildScriptPiped()` to `pkg/build.go`

**Files:**
- Modify: `pkg/build.go`

**Step 1: Write the function**

Add `RunBuildScriptPiped` which returns the `*exec.Cmd` with `StdoutPipe`/`StderrPipe` already created, so the caller can manage streaming. Unlike `RunBuildScriptToWriter`, the caller owns the lifecycle.

```go
// RunBuildScriptPiped prepares a build command with piped stdout/stderr.
// The caller is responsible for calling cmd.Start(), reading from the pipes,
// and calling cmd.Wait().
func RunBuildScriptPiped(enginePath, target, platform, state, projectPath string) (cmd *exec.Cmd, stdout io.ReadCloser, stderr io.ReadCloser, err error) {
	osPath := OsStringSliceSwitcher(WindowsBuildScript, UnixBuildScript, UnixBuildScript)
	basePath := []string{enginePath}
	pathElements := append(basePath, osPath...)
	buildScript := filepath.Join(pathElements...)

	cmd = exec.Command(buildScript, target, platform, state, projectPath)

	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	return cmd, stdout, stderr, nil
}
```

Add `"fmt"` and `"path/filepath"` to the import block if not already present (`"path/filepath"` is already there; `"fmt"` is not).

**Step 2: Run existing tests to verify no breakage**

Run: `go test ./pkg/... -v -count=1`
Expected: All existing tests pass (no tests directly exercise `RunBuildScriptPiped` yet — it's a new function).

**Step 3: Commit**

```bash
git add pkg/build.go
git commit -m "feat: add RunBuildScriptPiped for streaming build output"
```

---

### Task 2: Add build log capture to `BuildOrchestrator`

**Files:**
- Modify: `pkg/server/builder.go`

**Step 1: Write the failing test**

Add to `pkg/server/builder_test.go`:

```go
func TestBuildCaptureSubscribe(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// No active build — subscribe should return nil, error
	_, err := b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err == nil {
		t.Error("Expected error when no build is active")
	}

	// No active build — recent lines should return empty
	lines := b.RecentBuildLines(100)
	if len(lines) != 0 {
		t.Errorf("Expected 0 recent lines, got %d", len(lines))
	}
}

func TestBuildCaptureLifecycle(t *testing.T) {
	state := NewStateStore()
	state.path = filepath.Join(t.TempDir(), "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Simulate setting a build capture
	logPath := filepath.Join(t.TempDir(), "build.log")
	capture, err := NewLogCapture(logPath)
	if err != nil {
		t.Fatalf("Failed to create LogCapture: %v", err)
	}

	b.setBuildCapture(capture)

	// Should now be able to subscribe
	ch, err := b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if ch == nil {
		t.Fatal("Expected non-nil channel")
	}

	// Clear the capture
	b.clearBuildCapture()

	// Should error again
	_, err = b.SubscribeBuildLogs(&StreamLogsRequest{})
	if err == nil {
		t.Error("Expected error after clearing build capture")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestBuildCapture -v`
Expected: FAIL — `SubscribeBuildLogs`, `RecentBuildLines`, `setBuildCapture`, `clearBuildCapture` don't exist.

**Step 3: Add fields and methods to BuildOrchestrator**

In `pkg/server/builder.go`, add fields to `BuildOrchestrator`:

```go
type BuildOrchestrator struct {
	manager         *InstanceManager
	state           *StateStore
	agents          *AgentRegistry
	mcpServer       *mcpServerWrapper
	restartApprover func(ctx context.Context) (bool, []string)
	mu              sync.Mutex
	building        bool
	queue           []RebuildRequest
	buildCapture    *LogCapture
	buildCaptureMu  sync.RWMutex
}
```

Add methods:

```go
// setBuildCapture stores the active build's LogCapture.
func (b *BuildOrchestrator) setBuildCapture(lc *LogCapture) {
	b.buildCaptureMu.Lock()
	defer b.buildCaptureMu.Unlock()
	b.buildCapture = lc
}

// clearBuildCapture closes and removes the active build's LogCapture.
func (b *BuildOrchestrator) clearBuildCapture() {
	b.buildCaptureMu.Lock()
	defer b.buildCaptureMu.Unlock()
	if b.buildCapture != nil {
		b.buildCapture.Close()
		b.buildCapture = nil
	}
}

// SubscribeBuildLogs returns a channel streaming live build log lines.
// Returns an error if no build is currently active.
func (b *BuildOrchestrator) SubscribeBuildLogs(filter *StreamLogsRequest) (<-chan LogLineEvent, error) {
	b.buildCaptureMu.RLock()
	defer b.buildCaptureMu.RUnlock()
	if b.buildCapture == nil {
		return nil, fmt.Errorf("no active build")
	}
	return b.buildCapture.Subscribe(filter), nil
}

// RecentBuildLines returns the last n lines from the active build's ring buffer.
// Returns nil if no build is active.
func (b *BuildOrchestrator) RecentBuildLines(n int) []LogLineEvent {
	b.buildCaptureMu.RLock()
	defer b.buildCaptureMu.RUnlock()
	if b.buildCapture == nil {
		return nil
	}
	return b.buildCapture.RecentLines(n)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/server/ -run TestBuildCapture -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/builder.go pkg/server/builder_test.go
git commit -m "feat: add build log capture fields and subscriber methods to BuildOrchestrator"
```

---

### Task 3: Rewrite `runBuild()` to use LogCapture

**Files:**
- Modify: `pkg/server/builder.go`

**Step 1: Rewrite `runBuild` method**

Replace the `runBuild` method in `pkg/server/builder.go`. The new version creates a `LogCapture`, pipes build stdout/stderr through it, and stores it on the orchestrator:

```go
func (b *BuildOrchestrator) runBuild(record *BuildRecord) error {
	if err := EnsureLogDir(record.ProjectPath); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := BuildLogFile(record.ProjectPath)

	log.Info("Running UBT build", "target", record.Target, "platform", record.Platform, "config", record.Configuration, "log", logPath)

	// Resolve engine path
	enginePath := ""
	instances := b.manager.ListInstances()
	for _, inst := range instances {
		if inst.ProjectPath == record.ProjectPath {
			enginePath = inst.EnginePath
			break
		}
	}
	if enginePath == "" {
		uproject, err := pkg.NewUprojectE(record.ProjectPath)
		if err == nil && uproject.EngineAssociation != "" {
			enginePath = pkg.GetEnginePath(uproject.EngineAssociation)
		}
	}
	if enginePath == "" {
		return fmt.Errorf("cannot determine engine path for project: %s", record.ProjectPath)
	}

	// Create LogCapture for build output streaming
	capture, err := NewLogCapture(logPath)
	if err != nil {
		return fmt.Errorf("failed to create build log capture: %w", err)
	}
	b.setBuildCapture(capture)
	defer b.clearBuildCapture()

	// Get piped command
	cmd, stdout, stderr, err := pkg.RunBuildScriptPiped(
		enginePath,
		record.Target,
		record.Platform,
		record.Configuration,
		record.ProjectPath,
	)
	if err != nil {
		return fmt.Errorf("failed to create build command: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start build: %w", err)
	}

	// Capture streams in background goroutines
	go capture.CaptureStream(stdout, "stdout")
	go capture.CaptureStream(stderr, "stderr")

	// Wait for build to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	return nil
}
```

**Step 2: Run all builder tests**

Run: `go test ./pkg/server/ -run TestBuild -v`
Run: `go test ./pkg/server/ -v -count=1`
Expected: All existing tests still pass. (The tests that call `executeBuild`/`runBuild` with fake projects will fail at engine resolution as before — that's expected.)

**Step 3: Commit**

```bash
git add pkg/server/builder.go
git commit -m "feat: rewrite runBuild to stream output through LogCapture"
```

---

### Task 4: Add SSE endpoint for build log streaming

**Files:**
- Modify: `pkg/server/dashboard.go`
- Modify: `pkg/server/dashboard_test.go`

**Step 1: Write the failing test**

Add to `pkg/server/dashboard_test.go`:

```go
func TestBuildLogStreamNoBuild(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	req := httptest.NewRequest("GET", "/api/build/logs/stream?project=/test/P.uproject", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 when no build active, got %d", w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestBuildLogStreamNoBuild -v`
Expected: FAIL — route doesn't exist, likely returns 404.

**Step 3: Add the route and handler**

In `pkg/server/dashboard.go`, add the route in `routes()`:

```go
mux.HandleFunc("GET /api/build/logs/stream", cors(ds.handleBuildLogStream))
```

Add the handler:

```go
func (ds *dashboardServer) handleBuildLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, `{"error":"project query parameter required"}`, http.StatusBadRequest)
		return
	}

	// Send history from ring buffer
	history := ds.builder.RecentBuildLines(500)

	// Subscribe to live stream
	ch, err := ds.builder.SubscribeBuildLogs(&StreamLogsRequest{ProjectPath: project})
	if err != nil {
		// No active build
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send history as initial batch
	if len(history) > 0 {
		ds.sendSSEEvent(w, flusher, "build_log_history", map[string]interface{}{
			"lines": history,
		})
	}

	// Stream live lines
	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				// Channel closed — build ended
				current := ds.state.GetCurrentBuild()
				status := "unknown"
				buildID := ""
				if current != nil {
					status = string(current.Status)
					buildID = current.ID
				}
				ds.sendSSEEvent(w, flusher, "build_log_end", map[string]interface{}{
					"build_id": buildID,
					"status":   status,
				})
				return
			}
			ds.sendSSEEvent(w, flusher, "build_log", line)
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/server/ -run TestBuildLogStream -v`
Run: `go test ./pkg/server/ -v -count=1`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add pkg/server/dashboard.go pkg/server/dashboard_test.go
git commit -m "feat: add GET /api/build/logs/stream SSE endpoint"
```

---

### Task 5: Add `LogLineEvent` type to frontend

**Files:**
- Modify: `dashboard/src/types.ts`

**Step 1: Add the type**

Append to `dashboard/src/types.ts`:

```ts
export interface LogLineEvent {
  timestamp: string
  stream: string
  raw: string
  level: string
  category: string
}
```

**Step 2: Commit**

```bash
git add dashboard/src/types.ts
git commit -m "feat: add LogLineEvent type"
```

---

### Task 6: Create `useBuildLogStream` hook

**Files:**
- Create: `dashboard/src/hooks/useBuildLogStream.ts`

**Step 1: Write the hook**

```ts
import { useState, useEffect, useRef, useCallback } from 'react'
import type { LogLineEvent } from '../types'

const API_BASE = import.meta.env.DEV ? 'http://localhost:9516' : ''

export function useBuildLogStream(projectPath: string | null, active: boolean) {
  const [lines, setLines] = useState<LogLineEvent[]>([])
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  const disconnect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
      esRef.current = null
    }
    setConnected(false)
  }, [])

  useEffect(() => {
    if (!active || !projectPath) {
      disconnect()
      return
    }

    setLines([])

    const url = `${API_BASE}/api/build/logs/stream?project=${encodeURIComponent(projectPath)}`
    const es = new EventSource(url)
    esRef.current = es

    es.addEventListener('build_log_history', (e) => {
      const data = JSON.parse(e.data) as { lines: LogLineEvent[] }
      setLines(data.lines)
    })

    es.addEventListener('build_log', (e) => {
      const line = JSON.parse(e.data) as LogLineEvent
      setLines((prev) => [...prev, line])
    })

    es.addEventListener('build_log_end', () => {
      disconnect()
    })

    es.onopen = () => setConnected(true)

    es.onerror = () => {
      disconnect()
    }

    return () => disconnect()
  }, [active, projectPath, disconnect])

  return { lines, connected }
}
```

**Step 2: Verify frontend compiles**

Run: `cd dashboard && npx tsc --noEmit`
Expected: No type errors.

**Step 3: Commit**

```bash
git add dashboard/src/hooks/useBuildLogStream.ts
git commit -m "feat: add useBuildLogStream SSE hook"
```

---

### Task 7: Create `BuildLogViewer` component

**Files:**
- Create: `dashboard/src/components/BuildLogViewer.tsx`

**Step 1: Write the component**

```tsx
import { useEffect, useRef } from 'react'
import type { LogLineEvent } from '../types'

interface BuildLogViewerProps {
  lines: LogLineEvent[]
}

function levelColor(level: string): string {
  switch (level) {
    case 'error':
      return 'text-red-400'
    case 'warning':
      return 'text-yellow-400'
    default:
      return 'text-gray-300'
  }
}

export default function BuildLogViewer({ lines }: BuildLogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  function handleScroll() {
    const el = containerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 32
    autoScrollRef.current = atBottom
  }

  useEffect(() => {
    const el = containerRef.current
    if (el && autoScrollRef.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [lines])

  return (
    <div className="mt-2 flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500">Build Output</span>
        <span className="text-xs text-gray-600">{lines.length} lines</span>
      </div>
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="max-h-72 overflow-y-auto rounded bg-gray-950 border border-gray-800 p-2 font-mono text-xs leading-relaxed"
      >
        {lines.map((line, i) => (
          <div key={i} className={levelColor(line.level)}>
            {line.raw}
          </div>
        ))}
        {lines.length === 0 && (
          <div className="text-gray-600">Waiting for build output...</div>
        )}
      </div>
    </div>
  )
}
```

**Step 2: Verify frontend compiles**

Run: `cd dashboard && npx tsc --noEmit`
Expected: No type errors.

**Step 3: Commit**

```bash
git add dashboard/src/components/BuildLogViewer.tsx
git commit -m "feat: add BuildLogViewer terminal component"
```

---

### Task 8: Integrate BuildLogViewer into InstancePanel

**Files:**
- Modify: `dashboard/src/components/InstancePanel.tsx`

**Step 1: Add imports and hook usage**

At the top of `InstancePanel.tsx`, add imports:

```ts
import BuildLogViewer from './BuildLogViewer'
import { useBuildLogStream } from '../hooks/useBuildLogStream'
```

**Step 2: Add log streaming to each instance card**

Inside the `instances.map((inst) => (...))` block, after the existing `<div className="text-sm text-gray-400 space-y-0.5">` section and before the closing `</li>`, add the build log viewer.

The logic: show the viewer when `buildInfo?.current_build` has status `building` or `pending` and its `project_path` matches the instance's `project_path`.

Replace the instance `<li>` with a version that includes the log viewer. Each instance card needs its own hook call, so extract an `InstanceCard` sub-component:

Create a new component inside `InstancePanel.tsx` (above the default export):

```tsx
function InstanceCard({
  inst,
  onStop,
  buildInfo,
}: {
  inst: InstanceInfo
  onStop: (pid: number) => void
  buildInfo: BuildInfo | null
}) {
  const buildActive =
    buildInfo?.current_build != null &&
    (buildInfo.current_build.status === 'building' ||
      buildInfo.current_build.status === 'pending') &&
    buildInfo.current_build.project_path === inst.project_path

  const { lines } = useBuildLogStream(
    inst.project_path,
    buildActive,
  )

  return (
    <li className="rounded-lg bg-gray-900 p-4 flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex h-2.5 w-2.5 rounded-full ${stateColor(inst.state)}`}
          />
          <span className="font-medium text-gray-200">
            {inst.project_name}
          </span>
        </div>
        {(inst.state === 'running' || inst.state === 'starting') && (
          <button
            onClick={() => onStop(inst.pid)}
            className="rounded bg-gray-800 px-2 py-1 text-xs text-red-400 hover:bg-gray-700 transition-colors"
          >
            Stop
          </button>
        )}
      </div>
      <div className="text-sm text-gray-400 space-y-0.5">
        <p>State: {stateLabel(inst.state)}</p>
        <p>PID: {inst.pid}</p>
        <p>Engine: {inst.engine_version}</p>
      </div>
      {buildActive && <BuildLogViewer lines={lines} />}
    </li>
  )
}
```

Update the `instances.map` in the default export to use this new sub-component:

```tsx
{instances.map((inst) => (
  <InstanceCard
    key={inst.pid}
    inst={inst}
    onStop={handleStop}
    buildInfo={buildInfo}
  />
))}
```

Add `BuildInfo` to the import from `../types` (it's already imported as part of `BuildRecord`; check — if `BuildInfo` isn't imported, add it).

**Step 3: Verify frontend compiles**

Run: `cd dashboard && npx tsc --noEmit`
Expected: No type errors.

**Step 4: Run all Go tests as final check**

Run: `go test ./... -v -count=1`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add dashboard/src/components/InstancePanel.tsx
git commit -m "feat: integrate build log viewer into editor instance cards"
```

---

### Task 9: Final verification

**Step 1: Full Go test suite**

Run: `go test ./... -v -count=1`

**Step 2: Frontend type check**

Run: `cd dashboard && npx tsc --noEmit`

**Step 3: Frontend build**

Run: `cd dashboard && npm run build`

**Step 4: Verify the Go binary compiles**

Run: `go build -o /dev/null .`
