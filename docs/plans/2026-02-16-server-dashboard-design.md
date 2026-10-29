# Server Dashboard Design

## Overview

Add a web dashboard to the UE5 daemon that shows real-time build status, editor instances, and registered AI agents. The dashboard runs inside the daemon process on a separate HTTP port alongside the existing MCP SSE server.

## Backend

### Server

- New `dashboardServer` struct in `pkg/server/dashboard.go`
- HTTP server on `:9516` (configurable via `UE5_DASHBOARD_PORT` env var)
- Started/stopped by `Daemon` alongside the MCP server
- CORS enabled for development (Vite dev server on different port)

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | Daemon version, uptime, summary counts |
| GET | `/api/build` | Current build + recent history (filtered by project query param) |
| GET | `/api/instances` | All editor instances |
| GET | `/api/agents` | All registered AI agents |
| POST | `/api/rebuild` | Trigger a rebuild (accepts JSON body with project_path, engine_path, mode, label) |
| POST | `/api/editor/start` | Start an editor instance |
| POST | `/api/editor/stop` | Stop an editor instance |
| GET | `/api/events` | SSE stream for real-time state changes |

### SSE Events

The `/api/events` endpoint sends events from the existing `AgentRegistry` event system:

- `editor_state_changed` — instance started, stopped, crashed
- `build_started`, `build_completed`, `build_failed` — build lifecycle
- `agent_registered`, `agent_unregistered` — agent changes

Each event is JSON with `type` and `data` fields.

### Static File Serving

- Production: React build output embedded via `go:embed` from `dashboard/dist/`
- Development: Vite dev server runs separately, proxies API to Go backend
- Fallback: all non-API routes serve `index.html` (SPA routing)

## Frontend

### Tech Stack

- React 18 + TypeScript
- Vite (build tool)
- Tailwind CSS (styling)
- No router needed (single page)

### Layout

```
┌─────────────────────────────────────────────────┐
│  UE5 Daemon    [Project ▼]     ● Running  0:42  │
├───────────────┬─────────────────┬───────────────┤
│  Build Status │  Editor         │  AI Agents    │
│               │  Instances      │               │
│  Current:     │                 │  [agent list] │
│  [build info] │  [instance      │               │
│               │   cards with    │               │
│  History:     │   start/stop]   │               │
│  [recent      │                 │               │
│   builds]     │                 │               │
│               │                 │               │
│  [Rebuild]    │                 │               │
└───────────────┴─────────────────┴───────────────┘
```

### Project Selector

- Dropdown in header populated from instances + build history project paths
- Single project: auto-selects, minimal dropdown UI
- Multiple projects: full dropdown, defaults to most recently active
- Filters build history and instance panel; agents are global

### Real-Time Updates

- `EventSource` connected to `/api/events`
- All panels update automatically on state change events
- Reconnects on disconnect with exponential backoff

### Interactivity

- Rebuild button: opens form with project path, mode (full/hot_reload), label
- Start/Stop editor: buttons on each instance card
- Color-coded statuses: green (succeeded/running), yellow (building/starting), red (failed/crashed)

## Integration with Daemon

- `Daemon` struct gains `dashboard *dashboardServer` field
- `NewDaemon()` does not create dashboard (created in `Run()`)
- `Daemon.Run()` starts dashboard HTTP server as goroutine
- `Daemon.shutdown()` calls `dashboard.Shutdown(ctx)` with 5s timeout
- Dashboard subscribes to `AgentRegistry` events for SSE broadcasting

## Project Structure

```
dashboard/
  ├── package.json
  ├── tsconfig.json
  ├── vite.config.ts
  ├── tailwind.config.js
  ├── index.html
  └── src/
      ├── main.tsx
      ├── App.tsx
      ├── hooks/
      │   ├── useSSE.ts          # SSE connection + reconnect
      │   └── useAPI.ts          # fetch helpers
      └── components/
          ├── Header.tsx         # project selector, status, uptime
          ├── BuildPanel.tsx     # current build + history
          ├── InstancePanel.tsx  # editor instances with controls
          └── AgentPanel.tsx     # registered agents

pkg/server/
  ├── dashboard.go              # HTTP server, API handlers, SSE
  └── dashboard_embed.go        # go:embed for production static files
```
