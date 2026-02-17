package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

type dashboardServer struct {
	httpServer *http.Server
	state      *StateStore
	agents     *AgentRegistry
	manager    *InstanceManager
	builder    *BuildOrchestrator
	version    string
	startedAt  time.Time

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

func (ds *dashboardServer) Start(addr string) error {
	mux := ds.routes()
	ds.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	log.Info("Starting dashboard server", "addr", addr)
	return ds.httpServer.ListenAndServe()
}

func (ds *dashboardServer) Shutdown(ctx context.Context) error {
	if ds.httpServer != nil {
		return ds.httpServer.Shutdown(ctx)
	}
	return nil
}

func (ds *dashboardServer) BroadcastEvent(event AgentEvent) {
	ds.sseClientsMu.Lock()
	defer ds.sseClientsMu.Unlock()
	for ch := range ds.sseClients {
		select {
		case ch <- event:
		default:
		}
	}
}

func (ds *dashboardServer) routes() *http.ServeMux {
	mux := http.NewServeMux()

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

	// Handle CORS preflight for all API routes
	corsOnly := cors(func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("OPTIONS /api/", corsOnly)

	mux.HandleFunc("GET /api/status", cors(ds.handleStatus))
	mux.HandleFunc("GET /api/build", cors(ds.handleBuild))
	mux.HandleFunc("GET /api/instances", cors(ds.handleInstances))
	mux.HandleFunc("GET /api/agents", cors(ds.handleAgents))
	mux.HandleFunc("POST /api/rebuild", cors(ds.handleRebuild))
	mux.HandleFunc("POST /api/editor/start", cors(ds.handleEditorStart))
	mux.HandleFunc("POST /api/editor/stop", cors(ds.handleEditorStop))
	mux.HandleFunc("GET /api/events", cors(ds.handleSSE))

	// Serve embedded static files (production) or nothing (dev — use Vite)
	if hasDashboardStatic() {
		staticFS := dashboardStaticFS()
		fileServer := http.FileServer(http.FS(staticFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/" {
				path = "index.html"
			} else {
				path = path[1:] // strip leading /
			}
			if _, err := fs.Stat(staticFS, path); err != nil {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return mux
}

func (ds *dashboardServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(ds.startedAt).Round(time.Second)
	writeJSON(w, map[string]interface{}{
		"version":    ds.version,
		"uptime":     uptime.String(),
		"started_at": ds.startedAt,
		"instances":  ds.manager.Count(),
		"agents":     ds.agents.Count(),
		"building":   ds.builder.IsBuilding(),
	})
}

func (ds *dashboardServer) handleBuild(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	history := ds.state.GetBuildHistory(20)
	if project != "" {
		filtered := make([]BuildRecord, 0)
		for _, b := range history {
			if b.ProjectPath == project {
				filtered = append(filtered, b)
			}
		}
		history = filtered
	}
	writeJSON(w, BuildInfoResponse{
		CurrentBuild:        ds.state.GetCurrentBuild(),
		AccumulatedFeatures: ds.state.GetAccumulatedFeatures(),
		TotalBuilds:         len(ds.state.GetState().BuildHistory),
		RecentBuilds:        history,
	})
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
	json.NewEncoder(w).Encode(v)
}
