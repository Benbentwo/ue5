# Server Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a real-time web dashboard to the UE5 daemon showing build status, editor instances, and AI agents.

**Architecture:** A new HTTP server runs inside the daemon on port 9516 (configurable). It exposes JSON REST endpoints + an SSE stream for real-time updates. A React frontend (Vite + Tailwind) consumes this API. Production builds are embedded via `go:embed`.

**Tech Stack:** Go stdlib `net/http` (backend), React 18 + TypeScript + Vite + Tailwind CSS (frontend)

---

### Task 1: Add DashboardAddr helper to paths.go

**Files:**
- Modify: `pkg/server/paths.go`
- Test: `pkg/server/paths_test.go`

**Step 1: Write the test**

Create `pkg/server/paths_test.go` (if it doesn't exist) with:

```go
package server

import (
	"os"
	"testing"
)

func TestDashboardAddr(t *testing.T) {
	// Default
	os.Unsetenv("UE5_DASHBOARD_PORT")
	addr := DashboardAddr()
	if addr != ":9516" {
		t.Errorf("expected :9516, got %s", addr)
	}

	// Custom
	os.Setenv("UE5_DASHBOARD_PORT", "8080")
	defer os.Unsetenv("UE5_DASHBOARD_PORT")
	addr = DashboardAddr()
	if addr != ":8080" {
		t.Errorf("expected :8080, got %s", addr)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestDashboardAddr -v`
Expected: FAIL — `DashboardAddr` undefined

**Step 3: Implement DashboardAddr**

Add to `pkg/server/paths.go` after the `MCPAddr` function:

```go
const dashboardDefaultPort = "9516"

// DashboardAddr returns the dashboard server listen address.
func DashboardAddr() string {
	if port := os.Getenv("UE5_DASHBOARD_PORT"); port != "" {
		return ":" + port
	}
	return ":" + dashboardDefaultPort
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/server/ -run TestDashboardAddr -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/paths.go pkg/server/paths_test.go
git commit -m "feat: add DashboardAddr helper for dashboard port config"
```

---

### Task 2: Create dashboard HTTP server with SSE support

**Files:**
- Create: `pkg/server/dashboard.go`
- Test: `pkg/server/dashboard_test.go`

This is the core backend. It provides JSON API endpoints + an SSE endpoint for real-time events.

**Step 1: Write the test**

Create `pkg/server/dashboard_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestDashboard() *dashboardServer {
	state := NewStateStore()
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	builder := NewBuildOrchestrator(manager, state, agents)

	return &dashboardServer{
		state:     state,
		agents:    agents,
		manager:   manager,
		builder:   builder,
		version:   "test-v1",
		startedAt: time.Now(),
	}
}

func TestDashboardStatusEndpoint(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp["version"] != "test-v1" {
		t.Errorf("expected version test-v1, got %v", resp["version"])
	}
}

func TestDashboardBuildEndpoint(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	req := httptest.NewRequest("GET", "/api/build", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp BuildInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp.TotalBuilds != 0 {
		t.Errorf("expected 0 builds, got %d", resp.TotalBuilds)
	}
}

func TestDashboardInstancesEndpoint(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	req := httptest.NewRequest("GET", "/api/instances", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDashboardAgentsEndpoint(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	// Register an agent first
	ds.agents.Register(AgentInfo{ID: "test-1", Name: "Test Agent", Description: "Testing"})

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var agents []AgentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &agents); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}
}

func TestDashboardCORSHeaders(t *testing.T) {
	ds := newTestDashboard()
	mux := ds.routes()

	req := httptest.NewRequest("OPTIONS", "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/server/ -run TestDashboard -v`
Expected: FAIL — `dashboardServer` undefined

**Step 3: Implement dashboard.go**

Create `pkg/server/dashboard.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// dashboardServer serves a web dashboard for monitoring the daemon.
type dashboardServer struct {
	httpServer *http.Server
	state      *StateStore
	agents     *AgentRegistry
	manager    *InstanceManager
	builder    *BuildOrchestrator
	version    string
	startedAt  time.Time

	// SSE subscribers
	sseClients   map[chan AgentEvent]struct{}
	sseClientsMu sync.Mutex
}

func newDashboardServer(d *Daemon) *dashboardServer {
	return &dashboardServer{
		state:      d.state,
		agents:     d.agents,
		manager:    d.manager,
		builder:    d.builder,
		version:    d.version,
		startedAt:  d.startedAt,
		sseClients: make(map[chan AgentEvent]struct{}),
	}
}

// Start starts the dashboard HTTP server on the given address.
func (ds *dashboardServer) Start(addr string) error {
	mux := ds.routes()
	ds.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	log.Info("Starting dashboard server", "addr", addr)
	return ds.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the dashboard server.
func (ds *dashboardServer) Shutdown(ctx context.Context) error {
	if ds.httpServer != nil {
		return ds.httpServer.Shutdown(ctx)
	}
	return nil
}

// BroadcastEvent sends an event to all connected SSE clients.
func (ds *dashboardServer) BroadcastEvent(event AgentEvent) {
	ds.sseClientsMu.Lock()
	defer ds.sseClientsMu.Unlock()

	for ch := range ds.sseClients {
		select {
		case ch <- event:
		default:
			// Drop event if client is slow
		}
	}
}

// routes builds the HTTP mux with all API endpoints.
func (ds *dashboardServer) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Wrap all handlers with CORS middleware
	cors := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("GET /api/status", cors(ds.handleStatus))
	mux.HandleFunc("GET /api/build", cors(ds.handleBuild))
	mux.HandleFunc("GET /api/instances", cors(ds.handleInstances))
	mux.HandleFunc("GET /api/agents", cors(ds.handleAgents))
	mux.HandleFunc("POST /api/rebuild", cors(ds.handleRebuild))
	mux.HandleFunc("POST /api/editor/start", cors(ds.handleEditorStart))
	mux.HandleFunc("POST /api/editor/stop", cors(ds.handleEditorStop))
	mux.HandleFunc("GET /api/events", cors(ds.handleSSE))

	return mux
}

func (ds *dashboardServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(ds.startedAt).Round(time.Second)
	resp := map[string]interface{}{
		"version":    ds.version,
		"uptime":     uptime.String(),
		"started_at": ds.startedAt,
		"instances":  ds.manager.Count(),
		"agents":     ds.agents.Count(),
		"building":   ds.builder.IsBuilding(),
	}
	writeJSON(w, resp)
}

func (ds *dashboardServer) handleBuild(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	history := ds.state.GetBuildHistory(20)

	// Filter by project if specified
	if project != "" {
		filtered := make([]BuildRecord, 0)
		for _, b := range history {
			if b.ProjectPath == project {
				filtered = append(filtered, b)
			}
		}
		history = filtered
	}

	resp := BuildInfoResponse{
		CurrentBuild:        ds.state.GetCurrentBuild(),
		AccumulatedFeatures: ds.state.GetAccumulatedFeatures(),
		TotalBuilds:         len(ds.state.GetState().BuildHistory),
		RecentBuilds:        history,
	}
	writeJSON(w, resp)
}

func (ds *dashboardServer) handleInstances(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ds.manager.ListInstances())
}

func (ds *dashboardServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ds.agents.List())
}

func (ds *dashboardServer) handleRebuild(w http.ResponseWriter, r *http.Request) {
	var req RebuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	record, err := ds.builder.RequestRebuild(r.Context(), &req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, record)
}

func (ds *dashboardServer) handleEditorStart(w http.ResponseWriter, r *http.Request) {
	var req StartEditorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	info, err := ds.manager.StartEditor(&req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, info)
}

func (ds *dashboardServer) handleEditorStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectPath string `json:"project_path"`
		Force       bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	info, err := ds.manager.StopEditor(req.ProjectPath, req.Force)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, info)
}

func (ds *dashboardServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan AgentEvent, 32)
	ds.sseClientsMu.Lock()
	ds.sseClients[ch] = struct{}{}
	ds.sseClientsMu.Unlock()

	defer func() {
		ds.sseClientsMu.Lock()
		delete(ds.sseClients, ch)
		ds.sseClientsMu.Unlock()
	}()

	// Send initial state snapshot
	ds.sendSSEEvent(w, flusher, "snapshot", map[string]interface{}{
		"instances":     ds.manager.ListInstances(),
		"agents":        ds.agents.List(),
		"current_build": ds.state.GetCurrentBuild(),
		"building":      ds.builder.IsBuilding(),
	})

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			ds.sendSSEEvent(w, flusher, event.Type, event.Data)
		}
	}
}

func (ds *dashboardServer) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/server/ -run TestDashboard -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/server/dashboard.go pkg/server/dashboard_test.go
git commit -m "feat: add dashboard HTTP server with JSON API and SSE"
```

---

### Task 3: Wire dashboard into daemon lifecycle

**Files:**
- Modify: `pkg/server/daemon.go`

**Step 1: Add dashboard field to Daemon struct**

In `pkg/server/daemon.go`, add `dashboard` field to the `Daemon` struct (line 26, after `mcpServer`):

```go
dashboard  *dashboardServer
```

**Step 2: Start dashboard in Daemon.Run()**

In `Daemon.Run()`, after the MCP server startup block (after line 108), add:

```go
// Start dashboard web server alongside MCP
d.dashboard = newDashboardServer(d)
go func() {
	dashAddr := DashboardAddr()
	if err := d.dashboard.Start(dashAddr); err != nil && err != http.ErrServerClosed {
		log.Error("Dashboard server failed", "error", err)
	}
}()
```

**Step 3: Update the agent event callback to fan out**

In `Daemon.Run()`, modify the agent event callback (around line 111-115) to broadcast to both MCP and dashboard:

```go
d.agents.SetEventCallback(func(event AgentEvent) {
	if d.mcpServer != nil {
		d.mcpServer.BroadcastEvent(event)
	}
	if d.dashboard != nil {
		d.dashboard.BroadcastEvent(event)
	}
})
```

**Step 4: Update the log line to include dashboard address**

Change the log line (around line 117) to:

```go
log.Info("Daemon started", "socket", d.socketPath, "mcp", MCPAddr(), "dashboard", DashboardAddr(), "pid", os.Getpid(), "version", d.version)
```

**Step 5: Add dashboard shutdown to Daemon.shutdown()**

In `Daemon.shutdown()`, after the MCP shutdown block (after line 455), add:

```go
// Stop dashboard server
if d.dashboard != nil {
	dashCtx, dashCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dashCancel()
	if err := d.dashboard.Shutdown(dashCtx); err != nil {
		log.Error("Failed to shutdown dashboard server", "error", err)
	}
}
```

**Step 6: Add `net/http` import**

Add `"net/http"` to the imports in `daemon.go` (needed for `http.ErrServerClosed`).

**Step 7: Run existing tests**

Run: `go test ./pkg/server/ -v`
Expected: PASS (all existing tests still pass)

**Step 8: Commit**

```bash
git add pkg/server/daemon.go
git commit -m "feat: wire dashboard server into daemon lifecycle"
```

---

### Task 4: Scaffold React frontend project

**Files:**
- Create: `dashboard/package.json`
- Create: `dashboard/tsconfig.json`
- Create: `dashboard/tsconfig.app.json`
- Create: `dashboard/vite.config.ts`
- Create: `dashboard/tailwind.config.js`
- Create: `dashboard/postcss.config.js`
- Create: `dashboard/index.html`
- Create: `dashboard/src/main.tsx`
- Create: `dashboard/src/App.tsx`
- Create: `dashboard/src/index.css`
- Modify: `.gitignore`

**Step 1: Create dashboard directory and scaffold via Vite**

```bash
cd /path/to/project
npm create vite@latest dashboard -- --template react-ts
cd dashboard
npm install
npm install -D tailwindcss @tailwindcss/vite
```

**Step 2: Configure Vite proxy**

Replace `dashboard/vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:9516',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
  },
})
```

**Step 3: Configure Tailwind**

Replace `dashboard/src/index.css`:

```css
@import "tailwindcss";
```

**Step 4: Verify build**

```bash
cd dashboard && npm run build
```
Expected: Build succeeds, output in `dashboard/dist/`

**Step 5: Add dashboard/node_modules and dashboard/dist to .gitignore**

Append to the root `.gitignore`:

```
dashboard/node_modules/
dashboard/dist/
```

**Step 6: Commit**

```bash
git add dashboard/ .gitignore
git commit -m "feat: scaffold React dashboard with Vite and Tailwind"
```

---

### Task 5: Create API hooks and SSE hook

**Files:**
- Create: `dashboard/src/hooks/useAPI.ts`
- Create: `dashboard/src/hooks/useSSE.ts`
- Create: `dashboard/src/types.ts`

**Step 1: Create shared types**

Create `dashboard/src/types.ts`:

```ts
export interface BuildRecord {
  id: string
  project_path: string
  labels: string[]
  features: string[]
  contributions: BuildContribution[]
  mode: 'full' | 'hot_reload'
  status: 'pending' | 'building' | 'succeeded' | 'failed'
  started_at: string
  completed_at?: string
  error?: string
  target: string
  platform: string
  configuration: string
}

export interface BuildContribution {
  agent_id: string
  label: string
}

export interface BuildInfo {
  current_build?: BuildRecord
  accumulated_features: string[]
  total_builds: number
  recent_builds: BuildRecord[]
}

export interface InstanceInfo {
  project_path: string
  project_name: string
  engine_path: string
  engine_version: string
  pid: number
  state: 'starting' | 'running' | 'stopping' | 'stopped' | 'crashed'
  started_at: string
  log_file: string
  exit_code?: number
}

export interface AgentInfo {
  id: string
  name: string
  description: string
  registered_at: string
  last_seen_at: string
}

export interface DaemonStatus {
  version: string
  uptime: string
  started_at: string
  instances: number
  agents: number
  building: boolean
}
```

**Step 2: Create useAPI hook**

Create `dashboard/src/hooks/useAPI.ts`:

```ts
import { useState, useEffect, useCallback } from 'react'

export function useFetch<T>(url: string) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const refetch = useCallback(() => {
    setLoading(true)
    fetch(url)
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json()
      })
      .then(setData)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [url])

  useEffect(() => {
    refetch()
  }, [refetch])

  return { data, error, loading, refetch, setData }
}

export async function postAPI<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || res.statusText)
  }
  return res.json()
}
```

**Step 3: Create useSSE hook**

Create `dashboard/src/hooks/useSSE.ts`:

```ts
import { useEffect, useRef, useCallback } from 'react'

type EventHandler = (data: unknown) => void

export function useSSE(url: string, handlers: Record<string, EventHandler>) {
  const handlersRef = useRef(handlers)
  handlersRef.current = handlers

  const connect = useCallback(() => {
    const es = new EventSource(url)
    let retryDelay = 1000

    es.onerror = () => {
      es.close()
      setTimeout(() => connect(), retryDelay)
      retryDelay = Math.min(retryDelay * 2, 30000)
    }

    // Listen for all event types we care about
    const eventTypes = [
      'snapshot',
      'editor_state_changed',
      'rebuild_started',
      'rebuild_complete',
      'agent_registered',
      'agent_unregistered',
    ]

    for (const type of eventTypes) {
      es.addEventListener(type, (e) => {
        const data = JSON.parse(e.data)
        if (handlersRef.current[type]) {
          handlersRef.current[type](data)
        }
      })
    }

    return es
  }, [url])

  useEffect(() => {
    const es = connect()
    return () => es.close()
  }, [connect])
}
```

**Step 4: Verify TypeScript compiles**

```bash
cd dashboard && npx tsc --noEmit
```
Expected: No errors

**Step 5: Commit**

```bash
git add dashboard/src/types.ts dashboard/src/hooks/
git commit -m "feat: add TypeScript types, API hooks, and SSE hook"
```

---

### Task 6: Build the dashboard UI components

**Files:**
- Create: `dashboard/src/components/Header.tsx`
- Create: `dashboard/src/components/BuildPanel.tsx`
- Create: `dashboard/src/components/InstancePanel.tsx`
- Create: `dashboard/src/components/AgentPanel.tsx`
- Modify: `dashboard/src/App.tsx`

**Step 1: Create Header component**

Create `dashboard/src/components/Header.tsx`:

```tsx
import { DaemonStatus } from '../types'

interface Props {
  status: DaemonStatus | null
  projects: string[]
  selectedProject: string | null
  onSelectProject: (project: string | null) => void
}

export function Header({ status, projects, selectedProject, onSelectProject }: Props) {
  return (
    <header className="bg-gray-900 border-b border-gray-700 px-6 py-3 flex items-center justify-between">
      <div className="flex items-center gap-4">
        <h1 className="text-lg font-semibold text-white">UE5 Daemon</h1>
        {projects.length > 1 && (
          <select
            value={selectedProject ?? ''}
            onChange={(e) => onSelectProject(e.target.value || null)}
            className="bg-gray-800 text-gray-300 text-sm rounded px-2 py-1 border border-gray-600"
          >
            <option value="">All Projects</option>
            {projects.map((p) => (
              <option key={p} value={p}>
                {p.split('/').pop()?.replace('.uproject', '')}
              </option>
            ))}
          </select>
        )}
        {projects.length === 1 && (
          <span className="text-gray-400 text-sm">
            {projects[0].split('/').pop()?.replace('.uproject', '')}
          </span>
        )}
      </div>
      <div className="flex items-center gap-3 text-sm">
        {status && (
          <>
            <span className={`inline-block w-2 h-2 rounded-full ${status.building ? 'bg-yellow-400 animate-pulse' : 'bg-green-400'}`} />
            <span className="text-gray-400">{status.building ? 'Building' : 'Idle'}</span>
            <span className="text-gray-500">|</span>
            <span className="text-gray-400">Up {status.uptime}</span>
            <span className="text-gray-500">|</span>
            <span className="text-gray-500">v{status.version}</span>
          </>
        )}
      </div>
    </header>
  )
}
```

**Step 2: Create BuildPanel component**

Create `dashboard/src/components/BuildPanel.tsx`:

```tsx
import { useState } from 'react'
import { BuildInfo, BuildRecord } from '../types'
import { postAPI } from '../hooks/useAPI'

interface Props {
  buildInfo: BuildInfo | null
  selectedProject: string | null
  onBuildTriggered: () => void
}

const statusColors: Record<string, string> = {
  succeeded: 'text-green-400',
  building: 'text-yellow-400',
  pending: 'text-yellow-400',
  failed: 'text-red-400',
}

const statusIcons: Record<string, string> = {
  succeeded: '\u2713',
  building: '\u25CB',
  pending: '\u25CB',
  failed: '\u2717',
}

export function BuildPanel({ buildInfo, selectedProject, onBuildTriggered }: Props) {
  const [showForm, setShowForm] = useState(false)
  const [label, setLabel] = useState('')
  const [mode, setMode] = useState<'full' | 'hot_reload'>('hot_reload')
  const [error, setError] = useState<string | null>(null)

  const current = buildInfo?.current_build
  const builds = buildInfo?.recent_builds ?? []

  const handleRebuild = async () => {
    if (!selectedProject || !label) return
    setError(null)
    try {
      await postAPI('/api/rebuild', {
        project_path: selectedProject,
        mode,
        label,
      })
      setLabel('')
      setShowForm(false)
      onBuildTriggered()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Rebuild failed')
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Build Status</h2>

      {current && (
        <div className="bg-gray-800 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-2">
            <span className={`text-lg ${statusColors[current.status]}`}>
              {statusIcons[current.status]}
            </span>
            <span className="text-white font-medium capitalize">{current.status}</span>
            {current.status === 'building' && (
              <span className="text-yellow-400 animate-pulse text-xs">...</span>
            )}
          </div>
          <div className="text-sm text-gray-400 space-y-1">
            <div>Target: {current.target}</div>
            <div>Mode: {current.mode.replace('_', ' ')}</div>
            {current.labels.length > 0 && (
              <div>
                Labels:
                <ul className="ml-4 mt-1">
                  {current.labels.map((l, i) => (
                    <li key={i} className="text-gray-300">- {l}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </div>
      )}

      {!current && (
        <div className="bg-gray-800 rounded-lg p-4 text-gray-500 text-sm">No builds yet</div>
      )}

      {builds.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold text-gray-500 uppercase mb-2">Recent Builds</h3>
          <div className="space-y-1">
            {builds.slice(0, 10).map((build: BuildRecord) => (
              <div key={build.id} className="flex items-center gap-2 text-sm">
                <span className={statusColors[build.status]}>{statusIcons[build.status]}</span>
                <span className="text-gray-300 truncate flex-1">
                  {build.labels.join(', ')}
                </span>
                <span className="text-gray-500 text-xs">
                  {build.mode === 'full' ? 'full' : 'hot'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {!showForm ? (
        <button
          onClick={() => setShowForm(true)}
          disabled={!selectedProject}
          className="bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white text-sm py-2 px-4 rounded transition-colors"
        >
          Rebuild
        </button>
      ) : (
        <div className="bg-gray-800 rounded-lg p-3 space-y-2">
          <input
            type="text"
            placeholder="Build label (e.g., Added feature X)"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            className="w-full bg-gray-700 text-white text-sm rounded px-3 py-2 border border-gray-600 focus:border-blue-500 outline-none"
          />
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as 'full' | 'hot_reload')}
            className="w-full bg-gray-700 text-white text-sm rounded px-3 py-2 border border-gray-600"
          >
            <option value="hot_reload">Hot Reload</option>
            <option value="full">Full Rebuild</option>
          </select>
          {error && <p className="text-red-400 text-xs">{error}</p>}
          <div className="flex gap-2">
            <button
              onClick={handleRebuild}
              disabled={!label}
              className="flex-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 text-white text-sm py-1.5 rounded"
            >
              Start Build
            </button>
            <button
              onClick={() => { setShowForm(false); setError(null) }}
              className="bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm py-1.5 px-3 rounded"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
```

**Step 3: Create InstancePanel component**

Create `dashboard/src/components/InstancePanel.tsx`:

```tsx
import { InstanceInfo } from '../types'
import { postAPI } from '../hooks/useAPI'

interface Props {
  instances: InstanceInfo[]
  onAction: () => void
}

const stateColors: Record<string, string> = {
  running: 'bg-green-400',
  starting: 'bg-yellow-400 animate-pulse',
  stopping: 'bg-yellow-400',
  stopped: 'bg-gray-500',
  crashed: 'bg-red-400',
}

export function InstancePanel({ instances, onAction }: Props) {
  const handleStop = async (projectPath: string) => {
    try {
      await postAPI('/api/editor/stop', { project_path: projectPath, force: false })
      onAction()
    } catch (err) {
      console.error('Failed to stop editor:', err)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Editor Instances</h2>

      {instances.length === 0 && (
        <div className="bg-gray-800 rounded-lg p-4 text-gray-500 text-sm">No editor instances</div>
      )}

      {instances.map((inst) => (
        <div key={inst.project_path} className="bg-gray-800 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-2">
            <span className={`inline-block w-2 h-2 rounded-full ${stateColors[inst.state]}`} />
            <span className="text-white font-medium">{inst.project_name}</span>
          </div>
          <div className="text-sm text-gray-400 space-y-1">
            <div>State: <span className="text-gray-300 capitalize">{inst.state}</span></div>
            <div>PID: <span className="text-gray-300">{inst.pid}</span></div>
            {inst.engine_version && (
              <div>Engine: <span className="text-gray-300">{inst.engine_version}</span></div>
            )}
          </div>
          {(inst.state === 'running' || inst.state === 'starting') && (
            <button
              onClick={() => handleStop(inst.project_path)}
              className="mt-3 bg-red-600/20 hover:bg-red-600/30 text-red-400 text-sm py-1.5 px-3 rounded border border-red-600/30 transition-colors"
            >
              Stop
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
```

**Step 4: Create AgentPanel component**

Create `dashboard/src/components/AgentPanel.tsx`:

```tsx
import { AgentInfo } from '../types'

interface Props {
  agents: AgentInfo[]
}

function timeAgo(dateStr: string): string {
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ago`
}

export function AgentPanel({ agents }: Props) {
  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">AI Agents</h2>

      {agents.length === 0 && (
        <div className="bg-gray-800 rounded-lg p-4 text-gray-500 text-sm">No agents registered</div>
      )}

      {agents.map((agent) => (
        <div key={agent.id} className="bg-gray-800 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-1">
            <span className="inline-block w-2 h-2 rounded-full bg-blue-400" />
            <span className="text-white font-medium text-sm">{agent.name}</span>
          </div>
          <p className="text-gray-400 text-sm mb-2">{agent.description}</p>
          <div className="text-xs text-gray-500">
            Last seen: {timeAgo(agent.last_seen_at)}
          </div>
        </div>
      ))}
    </div>
  )
}
```

**Step 5: Wire up App.tsx**

Replace `dashboard/src/App.tsx`:

```tsx
import { useState, useCallback } from 'react'
import { useFetch } from './hooks/useAPI'
import { useSSE } from './hooks/useSSE'
import { Header } from './components/Header'
import { BuildPanel } from './components/BuildPanel'
import { InstancePanel } from './components/InstancePanel'
import { AgentPanel } from './components/AgentPanel'
import { DaemonStatus, BuildInfo, InstanceInfo, AgentInfo } from './types'

function App() {
  const { data: status, refetch: refetchStatus } = useFetch<DaemonStatus>('/api/status')
  const { data: buildInfo, refetch: refetchBuild, setData: setBuildInfo } = useFetch<BuildInfo>('/api/build')
  const { data: instances, refetch: refetchInstances, setData: setInstances } = useFetch<InstanceInfo[]>('/api/instances')
  const { data: agents, refetch: refetchAgents, setData: setAgents } = useFetch<AgentInfo[]>('/api/agents')

  const [selectedProject, setSelectedProject] = useState<string | null>(null)

  const refetchAll = useCallback(() => {
    refetchStatus()
    refetchBuild()
    refetchInstances()
    refetchAgents()
  }, [refetchStatus, refetchBuild, refetchInstances, refetchAgents])

  // Real-time updates via SSE
  useSSE('/api/events', {
    snapshot: (data: any) => {
      setInstances(data.instances ?? [])
      setAgents(data.agents ?? [])
      if (data.current_build) {
        setBuildInfo((prev) => prev ? { ...prev, current_build: data.current_build } : prev)
      }
    },
    editor_state_changed: () => refetchInstances(),
    rebuild_started: () => { refetchBuild(); refetchStatus() },
    rebuild_complete: () => { refetchBuild(); refetchStatus() },
    agent_registered: () => { refetchAgents(); refetchStatus() },
    agent_unregistered: () => { refetchAgents(); refetchStatus() },
  })

  // Collect project paths from instances and build history
  const projects = Array.from(new Set([
    ...(instances?.map((i) => i.project_path) ?? []),
    ...(buildInfo?.recent_builds?.map((b) => b.project_path) ?? []),
  ]))

  // Auto-select if only one project
  const effectiveProject = selectedProject ?? (projects.length === 1 ? projects[0] : null)

  // Filter instances by project
  const filteredInstances = effectiveProject
    ? (instances ?? []).filter((i) => i.project_path === effectiveProject)
    : (instances ?? [])

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      <Header
        status={status}
        projects={projects}
        selectedProject={effectiveProject}
        onSelectProject={setSelectedProject}
      />
      <main className="grid grid-cols-3 gap-6 p-6 max-w-7xl mx-auto">
        <BuildPanel
          buildInfo={buildInfo}
          selectedProject={effectiveProject}
          onBuildTriggered={refetchAll}
        />
        <InstancePanel instances={filteredInstances} onAction={refetchAll} />
        <AgentPanel agents={agents ?? []} />
      </main>
    </div>
  )
}

export default App
```

**Step 6: Update main.tsx**

Replace `dashboard/src/main.tsx`:

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
```

**Step 7: Verify build**

```bash
cd dashboard && npm run build
```
Expected: Build succeeds

**Step 8: Commit**

```bash
git add dashboard/src/
git commit -m "feat: add dashboard UI components with real-time SSE updates"
```

---

### Task 7: Add go:embed for production static files

**Files:**
- Create: `pkg/server/dashboard_embed.go`
- Create: `dashboard/dist/.gitkeep`
- Modify: `pkg/server/dashboard.go`

**Step 1: Create embed file**

Create `pkg/server/dashboard_embed.go`:

```go
package server

import "embed"

//go:embed dashboard_dist/*
var dashboardFS embed.FS
```

Note: We use `dashboard_dist` as the embedded directory name. The build process will copy `dashboard/dist/` contents into `pkg/server/dashboard_dist/` before `go build`.

**Step 2: Alternative approach — use build tag for dev vs prod**

Actually, a simpler approach: use a build tag. Create two files:

Create `pkg/server/dashboard_static_dev.go` (default, no embedded files):

```go
//go:build !embed_dashboard

package server

import (
	"io/fs"
	"testing/fstest"
)

// dashboardStaticFS returns nil in dev mode (no embedded static files).
func dashboardStaticFS() fs.FS {
	return fstest.MapFS{}
}

func hasDashboardStatic() bool {
	return false
}
```

Create `pkg/server/dashboard_static_prod.go` (used when building with `-tags embed_dashboard`):

```go
//go:build embed_dashboard

package server

import (
	"embed"
	"io/fs"
)

//go:embed dashboard_dist/*
var embeddedDashboard embed.FS

func dashboardStaticFS() fs.FS {
	sub, _ := fs.Sub(embeddedDashboard, "dashboard_dist")
	return sub
}

func hasDashboardStatic() bool {
	return true
}
```

**Step 3: Add static file serving to dashboard routes**

In `pkg/server/dashboard.go`, add to `routes()` after the API handlers:

```go
// Serve embedded static files (production) or nothing (dev — use Vite)
if hasDashboardStatic() {
	staticFS := dashboardStaticFS()
	fileServer := http.FileServer(http.FS(staticFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file; fall back to index.html for SPA routing
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else {
			path = path[1:] // strip leading /
		}
		if _, err := fs.Stat(staticFS, path); err != nil {
			// Serve index.html for SPA client-side routing
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
```

Add `"io/fs"` to imports in dashboard.go.

**Step 4: Verify it compiles**

```bash
go build ./...
```
Expected: Build succeeds (no embedded files in dev mode)

**Step 5: Commit**

```bash
git add pkg/server/dashboard_static_dev.go pkg/server/dashboard_static_prod.go pkg/server/dashboard.go
git commit -m "feat: add build-tag-based static file embedding for dashboard"
```

---

### Task 8: Add build script for production dashboard

**Files:**
- Create: `scripts/build-dashboard.sh`

**Step 1: Create build script**

Create `scripts/build-dashboard.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Building dashboard frontend..."
cd "$PROJECT_ROOT/dashboard"
npm ci
npm run build

echo "Copying dashboard dist to Go embed directory..."
rm -rf "$PROJECT_ROOT/pkg/server/dashboard_dist"
cp -r "$PROJECT_ROOT/dashboard/dist" "$PROJECT_ROOT/pkg/server/dashboard_dist"

echo "Building Go binary with embedded dashboard..."
cd "$PROJECT_ROOT"
go build -tags embed_dashboard -o ue5 .

echo "Done! Binary at: $PROJECT_ROOT/ue5"
```

**Step 2: Make executable**

```bash
chmod +x scripts/build-dashboard.sh
```

**Step 3: Commit**

```bash
git add scripts/build-dashboard.sh
git commit -m "feat: add build script for production dashboard embedding"
```

---

### Task 9: Update README with dashboard documentation

**Files:**
- Modify: `README.md`

**Step 1: Add dashboard section to README**

Add a "Dashboard" subsection under the Server Mode section explaining:
- Dashboard starts automatically with the daemon on port 9516
- Configurable via `UE5_DASHBOARD_PORT` env var
- Shows build status, editor instances, and AI agents in real-time
- Supports triggering rebuilds and start/stop editor from the UI
- Development: run `cd dashboard && npm run dev` for hot-reloading frontend

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add dashboard documentation to README"
```

---

### Task 10: Integration test — verify end-to-end

**Step 1: Run all Go tests**

```bash
go test ./... -v
```
Expected: All pass

**Step 2: Build frontend**

```bash
cd dashboard && npm run build
```
Expected: Succeeds

**Step 3: Verify Go compiles clean**

```bash
go build ./...
go vet ./...
```
Expected: No errors

**Step 4: Manual smoke test (optional)**

Start daemon in foreground and open dashboard:
```bash
go run . server start -f
# In another terminal: open http://localhost:9516/api/status
```
Expected: JSON response with version, uptime, etc.

**Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: integration test fixes"
```
