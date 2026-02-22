package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// mcpServerWrapper wraps the MCP server and SSE transport.
type mcpServerWrapper struct {
	mcp    *mcpserver.MCPServer
	sse    *mcpserver.SSEServer
	daemon *Daemon

	// Track connected sessions for broadcasting
	sessions   map[string]context.Context
	sessionsMu sync.RWMutex
}

func newMCPServer(d *Daemon) *mcpServerWrapper {
	s := mcpserver.NewMCPServer(
		"UE5 Editor Daemon",
		d.version,
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, true),
	)
	s.EnableSampling()

	w := &mcpServerWrapper{
		mcp:      s,
		daemon:   d,
		sessions: make(map[string]context.Context),
	}

	w.registerTools()
	w.registerResources()

	return w
}

// Start starts the MCP SSE server on the given address.
func (w *mcpServerWrapper) Start(addr string) error {
	w.sse = mcpserver.NewSSEServer(w.mcp,
		mcpserver.WithSSEContextFunc(w.trackSession),
	)
	log.Info("Starting MCP SSE server", "addr", addr)
	return w.sse.Start(addr)
}

// Shutdown gracefully shuts down the MCP server.
func (w *mcpServerWrapper) Shutdown(ctx context.Context) error {
	if w.sse != nil {
		return w.sse.Shutdown(ctx)
	}
	return nil
}

// trackSession is called when a new SSE client connects, allowing session tracking.
func (w *mcpServerWrapper) trackSession(ctx context.Context, _ *http.Request) context.Context {
	sessionID := fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	w.sessionsMu.Lock()
	w.sessions[sessionID] = ctx
	w.sessionsMu.Unlock()

	// Clean up when context is cancelled (client disconnects)
	go func() {
		<-ctx.Done()
		w.sessionsMu.Lock()
		delete(w.sessions, sessionID)
		w.sessionsMu.Unlock()
	}()

	return ctx
}

// BroadcastEvent sends an event notification to all connected MCP clients.
func (w *mcpServerWrapper) BroadcastEvent(event AgentEvent) {
	w.sessionsMu.RLock()
	sessions := make([]context.Context, 0, len(w.sessions))
	for _, ctx := range w.sessions {
		sessions = append(sessions, ctx)
	}
	w.sessionsMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	// Convert event to map for MCP notification
	eventData, _ := json.Marshal(event)
	var dataMap map[string]any
	_ = json.Unmarshal(eventData, &dataMap)

	// Get the MCPServer from any session context for sending notifications
	for _, ctx := range sessions {
		mcpSrv := mcpserver.ServerFromContext(ctx)
		if mcpSrv == nil {
			continue
		}

		if err := mcpSrv.SendNotificationToClient(ctx, event.Type, dataMap); err != nil {
			log.Debug("Failed to send MCP notification", "event", event.Type, "error", err)
		}
	}
}

// RequestRestartApproval asks all connected MCP clients whether they are okay
// with an editor restart. Returns true if all agents approve (or if no agents
// are connected). Returns false with a list of blocking session IDs if any
// agent is busy or fails to respond.
func (w *mcpServerWrapper) RequestRestartApproval(ctx context.Context) (approved bool, blockingAgents []string) {
	w.sessionsMu.RLock()
	if len(w.sessions) == 0 {
		w.sessionsMu.RUnlock()
		return true, nil
	}

	// Snapshot sessions
	type sessionEntry struct {
		id  string
		ctx context.Context
	}
	entries := make([]sessionEntry, 0, len(w.sessions))
	for id, sCtx := range w.sessions {
		entries = append(entries, sessionEntry{id: id, ctx: sCtx})
	}
	w.sessionsMu.RUnlock()

	samplingReq := mcp.CreateMessageRequest{
		CreateMessageParams: mcp.CreateMessageParams{
			Messages: []mcp.SamplingMessage{
				{
					Role: mcp.RoleUser,
					Content: mcp.TextContent{
						Type: "text",
						Text: "A rebuild has been requested that requires restarting the Unreal Editor. " +
							"Are you currently performing any work in the Unreal Editor that would be " +
							"disrupted by a restart? Respond with ONLY 'yes' or 'no'.",
					},
				},
			},
			MaxTokens: 10,
		},
	}

	type result struct {
		sessionID string
		busy      bool
	}

	results := make(chan result, len(entries))

	for _, entry := range entries {
		go func(e sessionEntry) {
			// Merge caller context (build orchestrator) with session context
			// so cancellation from either side stops the sampling request.
			mergedCtx, mergeCancel := context.WithCancel(e.ctx)
			go func() {
				select {
				case <-ctx.Done():
					mergeCancel()
				case <-mergedCtx.Done():
				}
			}()
			samplingCtx, cancel := context.WithTimeout(mergedCtx, 30*time.Second)
			defer cancel()
			defer mergeCancel()

			mcpSrv := mcpserver.ServerFromContext(e.ctx)
			if mcpSrv == nil {
				results <- result{sessionID: e.id, busy: true}
				return
			}

			resp, err := mcpSrv.RequestSampling(samplingCtx, samplingReq)
			if err != nil {
				log.Debug("Sampling request failed, treating as busy", "session", e.id, "error", err)
				results <- result{sessionID: e.id, busy: true}
				return
			}

			results <- result{sessionID: e.id, busy: parseRestartResponse(resp)}
		}(entry)
	}

	for range entries {
		r := <-results
		if r.busy {
			blockingAgents = append(blockingAgents, r.sessionID)
		}
	}

	return len(blockingAgents) == 0, blockingAgents
}

// noWordRegexp matches "no" as a standalone word (not as part of "not", "know", etc.).
var noWordRegexp = regexp.MustCompile(`(?i)\bno\b`)

// parseRestartResponse checks whether the agent indicated it is busy.
// The question asks "would you be disrupted?", so:
//   - "yes" -> agent IS busy -> return true (busy)
//   - "no"  -> agent is NOT busy -> return false (not busy)
//   - anything else -> conservative: return true (busy)
func parseRestartResponse(resp *mcp.CreateMessageResult) bool {
	if resp == nil {
		return true
	}
	text := ""
	if tc, ok := resp.Content.(mcp.TextContent); ok {
		text = tc.Text
	}
	trimmed := strings.TrimSpace(text)
	return !noWordRegexp.MatchString(trimmed)
}

// registerTools adds MCP tools for AI agent interaction.
func (w *mcpServerWrapper) registerTools() {
	// Tool: rebuild
	w.mcp.AddTool(
		mcp.NewTool("rebuild",
			mcp.WithDescription("Trigger a rebuild of the UE5 project. "+
				"Use mode 'full' for stop->build->restart cycle, "+
				"or 'hot_reload' to build while editor stays running. "+
				"Requests are coalesced when multiple agents request rebuilds concurrently."),
			mcp.WithString("project_path", mcp.Required(),
				mcp.Description("Absolute path to the .uproject file")),
			mcp.WithString("engine_path", mcp.Required(),
				mcp.Description("Absolute path to the Unreal Engine installation")),
			mcp.WithString("mode", mcp.Required(),
				mcp.Description("Build mode: 'full' or 'hot_reload'")),
			mcp.WithString("label", mcp.Required(),
				mcp.Description("Description of what changed requiring the rebuild")),
			mcp.WithString("agent_id",
				mcp.Description("ID of the requesting agent")),
			mcp.WithString("target",
				mcp.Description("Build target (defaults to {ProjectName}Editor)")),
			mcp.WithString("configuration",
				mcp.Description("Build configuration (defaults to Development)")),
		),
		w.handleRebuild,
	)

	// Tool: register_agent
	w.mcp.AddTool(
		mcp.NewTool("register_agent",
			mcp.WithDescription("Register an AI agent as a consumer of the UE5 daemon. "+
				"Registered agents receive notifications about editor state changes and rebuilds."),
			mcp.WithString("id", mcp.Required(),
				mcp.Description("Unique agent identifier")),
			mcp.WithString("name", mcp.Required(),
				mcp.Description("Human-readable agent name")),
			mcp.WithString("description", mcp.Required(),
				mcp.Description("Description of what the agent is working on")),
		),
		w.handleRegisterAgent,
	)

	// Tool: unregister_agent
	w.mcp.AddTool(
		mcp.NewTool("unregister_agent",
			mcp.WithDescription("Unregister an AI agent from the UE5 daemon."),
			mcp.WithString("id", mcp.Required(),
				mcp.Description("Agent ID to unregister")),
		),
		w.handleUnregisterAgent,
	)

	// Tool: get_build_info
	w.mcp.AddTool(
		mcp.NewTool("get_build_info",
			mcp.WithDescription("Get current build metadata, feature history, and recent build records. "+
				"Use this to determine if a rebuild is needed based on accumulated features."),
		),
		w.handleGetBuildInfo,
	)
}

// registerResources adds MCP resources for read access to daemon state.
func (w *mcpServerWrapper) registerResources() {
	// Resource: current build info
	w.mcp.AddResource(
		mcp.NewResource(
			"ue5://build/current",
			"Current Build Info",
			mcp.WithResourceDescription("Current build metadata, accumulated features, and status"),
			mcp.WithMIMEType("application/json"),
		),
		w.handleBuildResource,
	)

	// Resource: registered agents
	w.mcp.AddResource(
		mcp.NewResource(
			"ue5://agents",
			"Registered Agents",
			mcp.WithResourceDescription("List of currently registered AI agents"),
			mcp.WithMIMEType("application/json"),
		),
		w.handleAgentsResource,
	)

	// Resource: editor instances
	w.mcp.AddResource(
		mcp.NewResource(
			"ue5://instances",
			"Editor Instances",
			mcp.WithResourceDescription("Currently managed editor instances and their states"),
			mcp.WithMIMEType("application/json"),
		),
		w.handleInstancesResource,
	)
}

// --- Tool Handlers ---

func (w *mcpServerWrapper) handleRebuild(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rebuildReq := &RebuildRequest{
		ProjectPath:   req.GetString("project_path", ""),
		EnginePath:    req.GetString("engine_path", ""),
		Mode:          BuildMode(req.GetString("mode", "full")),
		Label:         req.GetString("label", ""),
		AgentID:       req.GetString("agent_id", ""),
		Target:        req.GetString("target", ""),
		Configuration: req.GetString("configuration", ""),
	}

	record, err := w.daemon.builder.RequestRebuild(w.daemon.ctx, rebuildReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, _ := json.MarshalIndent(record, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func (w *mcpServerWrapper) handleRegisterAgent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	info := AgentInfo{
		ID:          req.GetString("id", ""),
		Name:        req.GetString("name", ""),
		Description: req.GetString("description", ""),
	}

	if err := w.daemon.agents.Register(info); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	w.daemon.state.SetActiveAgents(w.daemon.agents.List())
	_ = w.daemon.state.Save()

	agent, _ := w.daemon.agents.Get(info.ID)
	data, _ := json.MarshalIndent(agent, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func (w *mcpServerWrapper) handleUnregisterAgent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")
	if err := w.daemon.agents.Unregister(id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	w.daemon.state.SetActiveAgents(w.daemon.agents.List())
	_ = w.daemon.state.Save()

	return mcp.NewToolResultText(fmt.Sprintf(`{"unregistered":"%s"}`, id)), nil
}

func (w *mcpServerWrapper) handleGetBuildInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	current := w.daemon.state.GetCurrentBuild()
	recent := w.daemon.state.GetBuildHistory(3)

	var currentSummary *BuildSummary
	if current != nil {
		s := NewBuildSummary(*current)
		currentSummary = &s
	}

	recentSummaries := make([]BuildSummary, len(recent))
	for i, r := range recent {
		recentSummaries[i] = NewBuildSummary(r)
	}

	info := &CompactBuildInfoResponse{
		CurrentBuild:        currentSummary,
		AccumulatedFeatures: w.daemon.state.GetAccumulatedFeatures(),
		TotalBuilds:         len(w.daemon.state.GetState().BuildHistory),
		RecentBuilds:        recentSummaries,
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// --- Resource Handlers ---

func (w *mcpServerWrapper) handleBuildResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	info := &BuildInfoResponse{
		CurrentBuild:        w.daemon.state.GetCurrentBuild(),
		AccumulatedFeatures: w.daemon.state.GetAccumulatedFeatures(),
		TotalBuilds:         len(w.daemon.state.GetState().BuildHistory),
		RecentBuilds:        w.daemon.state.GetBuildHistory(5),
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

func (w *mcpServerWrapper) handleAgentsResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	agents := w.daemon.agents.List()
	data, _ := json.MarshalIndent(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	}, "", "  ")
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

func (w *mcpServerWrapper) handleInstancesResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	instances := w.daemon.manager.ListInstances()
	data, _ := json.MarshalIndent(map[string]interface{}{
		"instances": instances,
		"count":     len(instances),
	}, "", "  ")
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}
