package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
)

// Daemon is the long-running server process that manages editor instances.
type Daemon struct {
	socketPath string
	listener   net.Listener
	manager    *InstanceManager
	state      *StateStore
	agents     *AgentRegistry
	builder    *BuildOrchestrator
	mcpServer  *mcpServerWrapper
	dashboard  *dashboardServer
	version    string
	startedAt  time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewDaemon creates a new daemon instance.
func NewDaemon(version string) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())

	stateStore := NewStateStore()
	agentRegistry := NewAgentRegistry()
	instanceManager := NewInstanceManager()
	builder := NewBuildOrchestrator(instanceManager, stateStore, agentRegistry)

	return &Daemon{
		socketPath: SocketPath(),
		manager:    instanceManager,
		state:      stateStore,
		agents:     agentRegistry,
		builder:    builder,
		version:    version,
		startedAt:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Run starts the daemon, listens for connections, and blocks until shutdown.
func (d *Daemon) Run() error {
	if err := EnsureHomeDir(); err != nil {
		return fmt.Errorf("failed to create home directory: %w", err)
	}

	// Load persisted state
	if err := d.state.Load(); err != nil {
		log.Warn("Failed to load state, starting fresh", "error", err)
	}

	// Wire up instance state change notifications
	d.manager.SetStateChangeCallback(func(info InstanceInfo, oldState, newState InstanceState) {
		d.agents.Emit(AgentEvent{
			Type:      "editor_state_changed",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"project_path": info.ProjectPath,
				"project_name": info.ProjectName,
				"old_state":    string(oldState),
				"new_state":    string(newState),
				"pid":          info.PID,
			},
		})
	})

	// Write PID file
	if err := d.writePIDFile(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer d.removePIDFile()

	// Remove stale socket file if it exists
	if err := d.cleanupSocket(); err != nil {
		return fmt.Errorf("failed to cleanup socket: %w", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", d.socketPath, err)
	}
	d.listener = listener
	defer d.cleanup()

	// Start MCP SSE server alongside Unix socket
	d.mcpServer = newMCPServer(d)
	go func() {
		mcpAddr := MCPAddr()
		if err := d.mcpServer.Start(mcpAddr); err != nil {
			log.Error("MCP server failed", "error", err)
		}
	}()

	// Start dashboard web server alongside MCP
	d.dashboard = newDashboardServer(d)
	go func() {
		dashAddr := DashboardAddr()
		if err := d.dashboard.Start(dashAddr); err != nil && err != http.ErrServerClosed {
			log.Error("Dashboard server failed", "error", err)
		}
	}()

	// Wire agent event callback to broadcast via MCP and dashboard
	d.agents.SetEventCallback(func(event AgentEvent) {
		if d.mcpServer != nil {
			d.mcpServer.BroadcastEvent(event)
		}
		if d.dashboard != nil {
			d.dashboard.BroadcastEvent(event)
		}
	})

	log.Info("Daemon started", "socket", d.socketPath, "mcp", MCPAddr(), "dashboard", DashboardAddr(), "pid", os.Getpid(), "version", d.version)

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		select {
		case sig := <-sigCh:
			log.Info("Received signal, shutting down", "signal", sig)
			d.cancel()
		case <-d.ctx.Done():
		}
	}()

	// Accept connections
	go d.acceptLoop()

	// Block until context is cancelled
	<-d.ctx.Done()

	log.Info("Shutting down daemon")
	d.shutdown()
	return nil
}

// acceptLoop accepts incoming connections and handles them in goroutines.
func (d *Daemon) acceptLoop() {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.ctx.Done():
				return
			default:
				log.Error("Failed to accept connection", "error", err)
				continue
			}
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.handleConnection(conn)
		}()
	}
}

// handleConnection reads a request from the connection and dispatches it.
func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close() //nolint:errcheck

	// Set a read deadline to prevent hung connections
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			log.Error("Failed to read request", "error", err)
		}
		return
	}

	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		d.sendError(conn, "", fmt.Sprintf("invalid request: %v", err))
		return
	}

	log.Debug("Received request", "type", req.Type, "id", req.ID)

	switch req.Type {
	case ReqPing:
		d.handlePing(conn, req)
	case ReqShutdown:
		d.handleShutdown(conn, req)
	case ReqStartEditor:
		d.handleStartEditor(conn, req)
	case ReqStopEditor:
		d.handleStopEditor(conn, req)
	case ReqListInstances:
		d.handleListInstances(conn, req)
	case ReqGetLogs:
		d.handleGetLogs(conn, req)
	case ReqStreamLogs:
		d.handleStreamLogs(conn, req)
	case ReqRebuild:
		d.handleRebuild(conn, req)
	case ReqGetBuildInfo:
		d.handleGetBuildInfo(conn, req)
	case ReqRegisterAgent:
		d.handleRegisterAgent(conn, req)
	case ReqUnregisterAgent:
		d.handleUnregisterAgent(conn, req)
	case ReqListAgents:
		d.handleListAgents(conn, req)
	default:
		d.sendError(conn, req.ID, fmt.Sprintf("unknown request type: %s", req.Type))
	}
}

func (d *Daemon) handlePing(conn net.Conn, req Request) {
	uptime := time.Since(d.startedAt).Round(time.Second)
	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		Pong: &PongResponse{
			Version:   d.version,
			Uptime:    uptime.String(),
			Instances: d.manager.Count(),
		},
	})
}

func (d *Daemon) handleShutdown(conn net.Conn, req Request) {
	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
	})
	// Trigger shutdown after responding
	d.cancel()
}

func (d *Daemon) handleStartEditor(conn net.Conn, req Request) {
	if req.StartEditor == nil {
		d.sendError(conn, req.ID, "start_editor field is required")
		return
	}

	info, err := d.manager.StartEditor(req.StartEditor)
	if err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	d.sendResponse(conn, Response{
		ID:       req.ID,
		Success:  true,
		Instance: info,
	})
}

func (d *Daemon) handleStopEditor(conn net.Conn, req Request) {
	if req.StopEditor == nil {
		d.sendError(conn, req.ID, "stop_editor field is required")
		return
	}

	info, err := d.manager.StopEditor(req.StopEditor.ProjectPath, req.StopEditor.Force)
	if err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	d.sendResponse(conn, Response{
		ID:       req.ID,
		Success:  true,
		Instance: info,
	})
}

func (d *Daemon) handleListInstances(conn net.Conn, req Request) {
	instances := d.manager.ListInstances()
	d.sendResponse(conn, Response{
		ID:        req.ID,
		Success:   true,
		Instances: instances,
	})
}

func (d *Daemon) handleGetLogs(conn net.Conn, req Request) {
	if req.GetLogs == nil {
		d.sendError(conn, req.ID, "get_logs field is required")
		return
	}

	result, err := d.manager.GetLogs(req.GetLogs)
	if err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		Logs:    result,
	})
}

func (d *Daemon) handleStreamLogs(conn net.Conn, req Request) {
	if req.StreamLogs == nil {
		d.sendError(conn, req.ID, "stream_logs field is required")
		return
	}

	ch, err := d.manager.StreamLogs(req.StreamLogs)
	if err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	// Remove all deadlines for streaming (both read and write)
	_ = conn.SetDeadline(time.Time{})

	encoder := json.NewEncoder(conn)
	for line := range ch {
		resp := Response{
			ID:      req.ID,
			Success: true,
			LogLine: &line,
		}
		if err := encoder.Encode(resp); err != nil {
			// Client disconnected
			return
		}
	}
}

func (d *Daemon) handleRebuild(conn net.Conn, req Request) {
	if req.Rebuild == nil {
		d.sendError(conn, req.ID, "rebuild field is required")
		return
	}

	// Update agent last-seen if agent ID is provided
	if req.Rebuild.AgentID != "" {
		d.agents.UpdateLastSeen(req.Rebuild.AgentID)
	}

	record, err := d.builder.RequestRebuild(d.ctx, req.Rebuild)
	if err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		Build:   record,
	})
}

func (d *Daemon) handleGetBuildInfo(conn net.Conn, req Request) {
	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		BuildInfo: &BuildInfoResponse{
			CurrentBuild:        d.state.GetCurrentBuild(),
			AccumulatedFeatures: d.state.GetAccumulatedFeatures(),
			TotalBuilds:         len(d.state.GetState().BuildHistory),
			RecentBuilds:        d.state.GetBuildHistory(10),
		},
	})
}

func (d *Daemon) handleRegisterAgent(conn net.Conn, req Request) {
	if req.RegisterAgent == nil {
		d.sendError(conn, req.ID, "register_agent field is required")
		return
	}

	info := AgentInfo{
		ID:          req.RegisterAgent.ID,
		Name:        req.RegisterAgent.Name,
		Description: req.RegisterAgent.Description,
	}
	if err := d.agents.Register(info); err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	// Persist agent list to state
	d.state.SetActiveAgents(d.agents.List())
	_ = d.state.Save()

	agentCopy, _ := d.agents.Get(info.ID)
	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		Agent:   agentCopy,
	})
}

func (d *Daemon) handleUnregisterAgent(conn net.Conn, req Request) {
	if req.UnregisterAgent == nil {
		d.sendError(conn, req.ID, "unregister_agent field is required")
		return
	}

	if err := d.agents.Unregister(req.UnregisterAgent.ID); err != nil {
		d.sendError(conn, req.ID, err.Error())
		return
	}

	// Persist agent list to state
	d.state.SetActiveAgents(d.agents.List())
	_ = d.state.Save()

	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
	})
}

func (d *Daemon) handleListAgents(conn net.Conn, req Request) {
	d.sendResponse(conn, Response{
		ID:      req.ID,
		Success: true,
		Agents:  d.agents.List(),
	})
}

func (d *Daemon) sendResponse(conn net.Conn, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Error("Failed to marshal response", "error", err)
		return
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}

func (d *Daemon) sendError(conn net.Conn, id string, msg string) {
	d.sendResponse(conn, Response{
		ID:      id,
		Success: false,
		Error:   msg,
	})
}

func (d *Daemon) shutdown() {
	// Stop MCP server
	if d.mcpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := d.mcpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("Failed to shutdown MCP server", "error", err)
		}
	}

	// Stop dashboard server
	if d.dashboard != nil {
		dashCtx, dashCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dashCancel()
		if err := d.dashboard.Shutdown(dashCtx); err != nil {
			log.Error("Failed to shutdown dashboard server", "error", err)
		}
	}

	// Stop all running editor instances
	d.manager.StopAll()

	// Close the listener to stop accepting new connections
	if d.listener != nil {
		_ = d.listener.Close()
	}

	// Wait for in-flight connections to finish
	d.wg.Wait()
}

func (d *Daemon) cleanup() {
	_ = os.Remove(d.socketPath)
}

func (d *Daemon) writePIDFile() error {
	pid := os.Getpid()
	return os.WriteFile(DaemonPIDFile(), []byte(fmt.Sprintf("%d", pid)), 0644)
}

func (d *Daemon) removePIDFile() {
	_ = os.Remove(DaemonPIDFile())
}

func (d *Daemon) cleanupSocket() error {
	if _, err := os.Stat(d.socketPath); err == nil {
		return os.Remove(d.socketPath)
	}
	return nil
}
