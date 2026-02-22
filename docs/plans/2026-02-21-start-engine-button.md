# Start Engine Button Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a "Start Engine" button to InstancePanel that launches the UE editor using the most recent successful build, disabled when the build is in-progress or failed.

**Architecture:** Backend makes `engine_path` optional in the start handler and auto-resolves it. Frontend adds the button to InstancePanel with conditional visibility/disabled state based on build status.

**Tech Stack:** Go (backend handler), React + TypeScript + Tailwind (frontend)

---

### Task 1: Backend — auto-resolve engine_path in handleEditorStart

**Files:**
- Modify: `pkg/server/dashboard.go:3-13` (imports) and `pkg/server/dashboard.go:172-184` (handler)

**Step 1: Write the failing test**

Add to `pkg/server/dashboard_test.go`:

```go
func TestEditorStartResolvesEnginePath(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	// POST with empty engine_path — should not panic or 400
	body := strings.NewReader(`{"project_path":"/tmp/fake.uproject"}`)
	req := httptest.NewRequest("POST", "/api/editor/start", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Expect 500 (no real binary on disk) rather than 400 (bad request)
	// This confirms the handler accepted the empty engine_path and attempted resolution
	if w.Code == http.StatusBadRequest {
		t.Fatalf("handler rejected empty engine_path with 400; should attempt resolution")
	}
}
```

Also add `"strings"` to the import block in `dashboard_test.go`.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestEditorStartResolvesEnginePath -v`
Expected: FAIL — currently the handler passes the empty `engine_path` straight through to `StartEditor`, which calls `pkg.EditorBinaryPath("")` and gets a bad path, but we want to verify the test itself works.

Actually: the current handler does NOT reject empty engine_path with 400, so this test may already pass. Run it to see current behavior first.

**Step 3: Add engine resolution to handleEditorStart**

In `pkg/server/dashboard.go`, add `"github.com/Benbentwo/ue5/pkg"` to the import block.

Replace the `handleEditorStart` method body (lines 172-184):

```go
func (ds *dashboardServer) handleEditorStart(w http.ResponseWriter, r *http.Request) {
	var req StartEditorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Auto-resolve engine_path if not provided
	if req.EnginePath == "" {
		for _, inst := range ds.manager.ListInstances() {
			if inst.ProjectPath == req.ProjectPath {
				req.EnginePath = inst.EnginePath
				break
			}
		}
	}
	if req.EnginePath == "" {
		uproject, err := pkg.NewUprojectE(req.ProjectPath)
		if err == nil && uproject.EngineAssociation != "" {
			req.EnginePath = pkg.GetEnginePath(uproject.EngineAssociation)
		}
	}
	if req.EnginePath == "" {
		http.Error(w, `{"error":"could not resolve engine path"}`, http.StatusBadRequest)
		return
	}

	info, err := ds.manager.StartEditor(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}
```

Note: `ds.manager.ListInstances()` — verify this method exists. Check `instance.go` for a method that returns instances. If it's named differently, use the correct name.

**Step 4: Verify ListInstances exists or add it**

Check `pkg/server/instance.go` for a method like `ListInstances()`. The `handleInstances` handler in `dashboard.go` reads instances somehow — match that pattern. If no public list method exists, add one:

```go
func (m *InstanceManager) ListInstances() []InstanceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]InstanceInfo, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst.Info)
	}
	return result
}
```

**Step 5: Run tests**

Run: `go test ./pkg/server/ -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add pkg/server/dashboard.go pkg/server/dashboard_test.go pkg/server/instance.go
git commit -m "feat: auto-resolve engine_path in editor start handler"
```

---

### Task 2: Frontend — pass buildInfo and selectedProject to InstancePanel

**Files:**
- Modify: `dashboard/src/App.tsx:122` (InstancePanel props)

**Step 1: Update InstancePanel invocation in App.tsx**

Change line 122 from:
```tsx
<InstancePanel instances={filteredInstances} onAction={refetchInstances} />
```
to:
```tsx
<InstancePanel
  instances={filteredInstances}
  onAction={refetchInstances}
  buildInfo={buildInfo}
  selectedProject={selectedProject}
/>
```

This will cause a TypeScript error until Task 3 updates the component — that's expected.

**Step 2: Commit**

```bash
git add dashboard/src/App.tsx
git commit -m "feat: pass buildInfo and selectedProject to InstancePanel"
```

---

### Task 3: Frontend — add Start Engine button to InstancePanel

**Files:**
- Modify: `dashboard/src/components/InstancePanel.tsx`

**Step 1: Rewrite InstancePanel with start button**

Replace the full contents of `dashboard/src/components/InstancePanel.tsx`:

```tsx
import { useState } from 'react'
import type { InstanceInfo, BuildInfo, BuildRecord } from '../types'
import { postAPI } from '../hooks/useAPI'

interface InstancePanelProps {
  instances: InstanceInfo[]
  onAction: () => void
  buildInfo: BuildInfo | null
  selectedProject: string | null
}

function stateColor(state: InstanceInfo['state']): string {
  switch (state) {
    case 'running':
      return 'bg-green-400'
    case 'starting':
      return 'bg-yellow-400'
    case 'stopping':
      return 'bg-yellow-400'
    case 'stopped':
      return 'bg-gray-500'
    case 'crashed':
      return 'bg-red-400'
  }
}

function stateLabel(state: InstanceInfo['state']): string {
  return state.charAt(0).toUpperCase() + state.slice(1)
}

function latestBuild(buildInfo: BuildInfo | null): BuildRecord | null {
  if (buildInfo?.current_build) return buildInfo.current_build
  if (buildInfo?.recent_builds?.length) return buildInfo.recent_builds[0]
  return null
}

function startDisabledReason(build: BuildRecord | null): string | null {
  if (!build) return 'No builds yet'
  switch (build.status) {
    case 'pending':
    case 'building':
      return 'Build in progress'
    case 'failed':
      return 'Last build failed'
    case 'succeeded':
      return null
  }
}

export default function InstancePanel({
  instances,
  onAction,
  buildInfo,
  selectedProject,
}: InstancePanelProps) {
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleStop(pid: number) {
    try {
      await postAPI(`/api/instances/${pid}/stop`, {})
      onAction()
    } catch {
      // Errors are non-critical for the UI
    }
  }

  const hasRunning = instances.some(
    (i) => i.state === 'running' || i.state === 'starting',
  )

  const build = latestBuild(buildInfo)
  const disabledReason = startDisabledReason(build)

  async function handleStart() {
    if (!build || disabledReason) return
    setStarting(true)
    setError(null)
    try {
      await postAPI('/api/editor/start', {
        project_path: build.project_path,
      })
      onAction()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start engine')
    } finally {
      setStarting(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500">
        Editor Instances
      </h2>

      {instances.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {instances.map((inst) => (
            <li
              key={inst.pid}
              className="rounded-lg bg-gray-900 p-4 flex flex-col gap-2"
            >
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
                    onClick={() => handleStop(inst.pid)}
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
            </li>
          ))}
        </ul>
      ) : (
        <div className="rounded-lg bg-gray-900 p-4">
          <p className="text-sm text-gray-500">No editor instances</p>
        </div>
      )}

      {/* Start Engine button — shown only when no instance is running */}
      {!hasRunning && (
        <div className="flex flex-col gap-2">
          <button
            onClick={handleStart}
            disabled={!!disabledReason || starting}
            className="rounded-lg bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {starting ? 'Starting...' : 'Start Engine'}
          </button>
          {disabledReason && (
            <p className="text-xs text-gray-500">{disabledReason}</p>
          )}
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
      )}
    </div>
  )
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd dashboard && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add dashboard/src/components/InstancePanel.tsx
git commit -m "feat: add Start Engine button to InstancePanel"
```

---

### Task 4: Verify full build

**Step 1: Run Go tests**

Run: `go test ./...`
Expected: All PASS

**Step 2: Run frontend type check**

Run: `cd dashboard && npx tsc --noEmit`
Expected: No errors

**Step 3: Final commit if any fixups needed**
