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
		state:      state,
		agents:     agents,
		manager:    manager,
		builder:    builder,
		version:    "test-v1",
		startedAt:  time.Now(),
		sseClients: make(map[chan AgentEvent]struct{}),
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
