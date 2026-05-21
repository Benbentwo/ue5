package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Benbentwo/ue5/pkg"
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

	// Tool: get_build_status — minimal polling response.
	// Use this when waiting on a rebuild. Returns ID + prompt + status only.
	// If status == "failed", call get_build_failure with the ID for details.
	w.mcp.AddTool(
		mcp.NewTool("get_build_status",
			mcp.WithDescription("Minimal status check for the current build. "+
				"Returns only {id, prompt, status} — designed for cheap polling. "+
				"If status is 'failed', call get_build_failure with the id to fetch error details. "+
				"Returns empty id when no build has ever run."),
		),
		w.handleGetBuildStatus,
	)

	// Tool: get_build_info — slim default for "is my work in the queue?".
	// Returns the current build (with labels/contributions for verifying coalesced
	// work) plus counts. Drops accumulated_features and recent_builds — those
	// live behind get_build_history.
	w.mcp.AddTool(
		mcp.NewTool("get_build_info",
			mcp.WithDescription("Get the current build with its labels and contributions, "+
				"plus aggregate counts. Use this to verify your rebuild label landed in a "+
				"coalesced build. For full history, call get_build_history. For polling, "+
				"call get_build_status (cheaper)."),
		),
		w.handleGetBuildInfo,
	)

	// Tool: get_build_history — opt-in audit trail.
	w.mcp.AddTool(
		mcp.NewTool("get_build_history",
			mcp.WithDescription("Full build history and accumulated features across all builds. "+
				"Opt-in because the response grows with project lifetime — use sparingly. "+
				"Prefer get_build_info for typical 'what's the state?' queries."),
			mcp.WithNumber("limit",
				mcp.Description("Max number of recent builds to return (default 10, max 50)")),
		),
		w.handleGetBuildHistory,
	)

	// Tool: get_build_failure — read error lines from build.log on disk.
	w.mcp.AddTool(
		mcp.NewTool("get_build_failure",
			mcp.WithDescription("Fetch error details for a failed build by ID. "+
				"Reads ~/.ue5/logs/<hash>/build.log and extracts lines containing "+
				"'error:' / 'Error:' / 'Fatal error:' with surrounding context. "+
				"Bounded response — safe to call after get_build_status reports failed."),
			mcp.WithString("build_id",
				mcp.Description("Build ID to fetch failure for. Defaults to the current build.")),
		),
		w.handleGetBuildFailure,
	)

	// Tool: start_editor — launch the UE5 Editor for a project via the daemon.
	w.mcp.AddTool(
		mcp.NewTool("start_editor",
			mcp.WithDescription("Start the Unreal Engine editor for a project. "+
				"The daemon manages the process and captures all logs. "+
				"If project_path is omitted, pass your current working directory — "+
				"the daemon walks up the directory tree to find a .uproject file. "+
				"engine_path is auto-resolved from the .uproject's EngineAssociation if omitted."),
			mcp.WithString("project_path",
				mcp.Description("Path to the .uproject file, OR a directory under the project. "+
					"Defaults to caller's cwd if omitted (must be passed by the agent).")),
			mcp.WithString("engine_path",
				mcp.Description("Path to the Unreal Engine installation. Auto-resolved if omitted.")),
		),
		w.handleStartEditor,
	)

	// Tool: stop_editor — stop a managed editor instance.
	w.mcp.AddTool(
		mcp.NewTool("stop_editor",
			mcp.WithDescription("Stop the running editor for a project. "+
				"Use force=true to send SIGKILL instead of a graceful shutdown."),
			mcp.WithString("project_path", mcp.Required(),
				mcp.Description("Absolute path to the .uproject file (must match a running instance)")),
			mcp.WithBoolean("force",
				mcp.Description("Force-kill instead of graceful shutdown (default false)")),
		),
		w.handleStopEditor,
	)

	// Tool: list_instances — list managed editor instances.
	w.mcp.AddTool(
		mcp.NewTool("list_instances",
			mcp.WithDescription("List all editor instances managed by the daemon, including "+
				"PID, state (starting/running/stopping/stopped/crashed), and log file path."),
		),
		w.handleListInstances,
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

// slimBuildInfo is the agent-facing shape of get_build_info — trimmed from
// the rich socket-protocol BuildInfoResponse to keep typical polls cheap.
// CurrentBuild uses BuildSummary (not BuildRecord) so the Labels needed to
// verify "did my work land in this build?" are present, but project-path,
// platform, target, and other rarely-used fields are not.
type slimBuildInfo struct {
	CurrentBuild *BuildSummary `json:"current_build,omitempty"`
	TotalBuilds  int           `json:"total_builds"`
	FeatureCount int           `json:"feature_count"`
}

func (w *mcpServerWrapper) handleGetBuildInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	current := w.daemon.state.GetCurrentBuild()
	var currentSummary *BuildSummary
	if current != nil {
		s := NewBuildSummary(*current)
		currentSummary = &s
	}
	info := slimBuildInfo{
		CurrentBuild: currentSummary,
		TotalBuilds:  len(w.daemon.state.GetState().BuildHistory),
		FeatureCount: len(w.daemon.state.GetAccumulatedFeatures()),
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetBuildStatus returns the minimal {id, prompt, status} polling shape.
// Joins coalesced labels with " + " so the prompt reads naturally.
func (w *mcpServerWrapper) handleGetBuildStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	current := w.daemon.state.GetCurrentBuild()
	resp := BuildStatusResponse{}
	if current != nil {
		resp.ID = current.ID
		resp.Status = current.Status
		resp.Prompt = strings.Join(current.Labels, " + ")
	}
	data, _ := json.Marshal(resp) // no indent — every byte counts on this path
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetBuildHistory returns the full audit trail. Capped at 50 to bound
// the response even when an agent passes a large limit.
func (w *mcpServerWrapper) handleGetBuildHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := req.GetInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	history := w.daemon.state.GetBuildHistory(limit)
	summaries := make([]BuildSummary, len(history))
	for i, r := range history {
		summaries[i] = NewBuildSummary(r)
	}
	resp := BuildHistoryResponse{
		Builds:              summaries,
		AccumulatedFeatures: w.daemon.state.GetAccumulatedFeatures(),
		TotalBuilds:         len(w.daemon.state.GetState().BuildHistory),
	}
	data, _ := json.MarshalIndent(resp, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetBuildFailure extracts error context from the build log on disk.
// Returns up to maxErrorLines lines, each error line plus ±contextLines around
// it, deduplicated. Bounded response even on multi-thousand-line build logs.
func (w *mcpServerWrapper) handleGetBuildFailure(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	buildID := req.GetString("build_id", "")

	// Locate the target build record.
	var target *BuildRecord
	if buildID == "" {
		target = w.daemon.state.GetCurrentBuild()
	} else {
		for _, b := range w.daemon.state.GetState().BuildHistory {
			if b.ID == buildID {
				rec := b
				target = &rec
				break
			}
		}
	}
	if target == nil {
		return mcp.NewToolResultError(fmt.Sprintf("no build found with id %q", buildID)), nil
	}

	logPath := BuildLogFile(target.ProjectPath)
	resp := BuildFailureResponse{
		BuildID: target.ID,
		Error:   target.Error,
		LogPath: logPath,
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		// Log doesn't exist — return what we have from the build record.
		resp.ErrorLines = []string{}
		out, _ := json.MarshalIndent(resp, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}

	resp.ErrorLines = extractBuildErrors(string(data), 3, 50)
	out, _ := json.MarshalIndent(resp, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

// extractBuildErrors finds lines containing error markers and returns each
// with ±contextLines neighbors, capped at maxLines total. Markers are matched
// case-insensitively against common UBT/MSBuild/clang patterns.
func extractBuildErrors(logContent string, contextLines, maxLines int) []string {
	lines := strings.Split(logContent, "\n")
	markers := []string{"error:", "fatal error:", "error c", "error :", "undefined reference", "ld: error"}

	included := make(map[int]bool)
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				end := i + contextLines
				if end >= len(lines) {
					end = len(lines) - 1
				}
				for j := start; j <= end; j++ {
					included[j] = true
				}
				break
			}
		}
	}

	if len(included) == 0 {
		return []string{}
	}

	// Emit in order, capped at maxLines.
	out := make([]string, 0, len(included))
	for i := 0; i < len(lines); i++ {
		if included[i] {
			out = append(out, lines[i])
			if len(out) >= maxLines {
				out = append(out, fmt.Sprintf("... (truncated, %d more error-context lines available in %s)", len(included)-len(out), "log file"))
				return out
			}
		}
	}
	return out
}

// handleStartEditor wraps the daemon's instance manager.
// project_path is optional — the agent should pass its cwd if it doesn't know
// the .uproject path. We resolve either to a .uproject by walking up.
func (w *mcpServerWrapper) handleStartEditor(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawPath := req.GetString("project_path", "")
	if rawPath == "" {
		return mcp.NewToolResultError("project_path is required — pass your current working directory if you don't know the .uproject path"), nil
	}

	uprojectPath, err := resolveUprojectPath(rawPath)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	enginePath := req.GetString("engine_path", "")
	if enginePath == "" {
		uproject, uErr := pkg.NewUprojectE(uprojectPath)
		if uErr == nil && uproject.EngineAssociation != "" {
			enginePath = pkg.GetEnginePath(uproject.EngineAssociation)
		}
		if enginePath == "" {
			return mcp.NewToolResultError("engine_path could not be auto-resolved from .uproject; pass it explicitly"), nil
		}
	}

	info, err := w.daemon.manager.StartEditor(&StartEditorRequest{
		ProjectPath: uprojectPath,
		EnginePath:  enginePath,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	data, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// handleStopEditor stops a managed editor instance.
func (w *mcpServerWrapper) handleStopEditor(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectPath := req.GetString("project_path", "")
	if projectPath == "" {
		return mcp.NewToolResultError("project_path is required"), nil
	}
	force := req.GetBool("force", false)

	info, err := w.daemon.manager.StopEditor(projectPath, force)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// handleListInstances lists all editor instances managed by the daemon.
func (w *mcpServerWrapper) handleListInstances(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instances := w.daemon.manager.ListInstances()
	data, _ := json.MarshalIndent(map[string]interface{}{
		"instances": instances,
		"count":     len(instances),
	}, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// resolveUprojectPath accepts either a .uproject path or a directory and
// returns the absolute .uproject path. Mirrors UpdateProjectPath() in
// cmd/root.go: walks up from the given path until it finds a .uproject file.
func resolveUprojectPath(rawPath string) (string, error) {
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", rawPath, err)
	}

	// If it's already a .uproject file, verify and return.
	if strings.HasSuffix(strings.ToLower(abs), ".uproject") {
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf(".uproject not found at %s: %w", abs, err)
		}
		return abs, nil
	}

	// Otherwise, treat as a directory and walk up.
	stat, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path not found: %s", abs)
	}
	dir := abs
	if !stat.IsDir() {
		dir = filepath.Dir(abs)
	}

	for {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".uproject") {
					return filepath.Join(dir, e.Name()), nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .uproject found at or above %s", abs)
		}
		dir = parent
	}
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
