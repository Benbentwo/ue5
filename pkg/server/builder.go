package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
)

// BuildOrchestrator manages the build lifecycle with coalescing support.
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

// NewBuildOrchestrator creates a new build orchestrator.
func NewBuildOrchestrator(manager *InstanceManager, state *StateStore, agents *AgentRegistry) *BuildOrchestrator {
	return &BuildOrchestrator{
		manager: manager,
		state:   state,
		agents:  agents,
		queue:   []RebuildRequest{},
	}
}

// SetMCPServer sets the MCP server reference for restart approval checks.
func (b *BuildOrchestrator) SetMCPServer(s *mcpServerWrapper) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mcpServer = s
	b.restartApprover = s.RequestRestartApproval
}

// RequestRebuild queues a rebuild request. If no build is in progress, it starts immediately.
// If a build IS in progress, the request is queued and will be coalesced with other
// pending requests when the current build finishes.
// Returns the build record (pending status) immediately.
func (b *BuildOrchestrator) RequestRebuild(ctx context.Context, req *RebuildRequest) (*BuildRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if req.Label == "" {
		return nil, fmt.Errorf("label is required for rebuild requests")
	}

	if b.building {
		// Queue the request for coalescing
		b.queue = append(b.queue, *req)
		log.Info("Rebuild queued for coalescing", "label", req.Label, "agent", req.AgentID, "queue_size", len(b.queue))

		record := &BuildRecord{
			ID:          fmt.Sprintf("build-%d", time.Now().UnixNano()),
			ProjectPath: req.ProjectPath,
			Labels:      []string{req.Label},
			Contributions: []BuildContribution{{
				AgentID: req.AgentID,
				Label:   req.Label,
			}},
			Mode:   req.Mode,
			Status: BuildStatusPending,
		}
		return record, nil
	}

	// Start build immediately
	record := b.createRecord([]RebuildRequest{*req})
	b.building = true

	go b.executeBuild(ctx, record)

	return record, nil
}

// IsBuilding returns whether a build is currently in progress.
func (b *BuildOrchestrator) IsBuilding() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.building
}

// createRecord creates a BuildRecord from one or more coalesced requests.
func (b *BuildOrchestrator) createRecord(requests []RebuildRequest) *BuildRecord {
	record := &BuildRecord{
		ID:            fmt.Sprintf("build-%d", time.Now().UnixNano()),
		Status:        BuildStatusBuilding,
		StartedAt:     time.Now(),
		Labels:        []string{},
		Contributions: []BuildContribution{},
		Mode:          BuildModeHotReload, // default to least disruptive
	}

	for _, req := range requests {
		record.Labels = append(record.Labels, req.Label)
		record.Contributions = append(record.Contributions, BuildContribution{
			AgentID: req.AgentID,
			Label:   req.Label,
		})

		// Mode escalation: "full" wins over "hot_reload"
		if req.Mode == BuildModeFull {
			record.Mode = BuildModeFull
		}

		// Use the first request's project/engine info
		if record.ProjectPath == "" {
			record.ProjectPath = req.ProjectPath
		}
		if record.Target == "" {
			record.Target = req.Target
		}
		if record.Configuration == "" {
			record.Configuration = req.Configuration
		}
	}

	// Resolve defaults from the first request
	first := requests[0]
	projectName := strings.TrimSuffix(filepath.Base(first.ProjectPath), filepath.Ext(first.ProjectPath))
	if record.Target == "" || record.Target == "Editor" {
		record.Target = projectName + "Editor"
	}
	if record.Configuration == "" {
		record.Configuration = "Development"
	}
	record.Platform = pkg.GetPlatform()

	return record
}

// executeBuild runs the actual build process and handles the lifecycle.
func (b *BuildOrchestrator) executeBuild(ctx context.Context, record *BuildRecord) {
	// Store the record
	b.state.AddBuildRecord(*record)
	_ = b.state.Save()

	// Emit rebuild_started notification
	b.agents.Emit(AgentEvent{
		Type:      "rebuild_started",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"build_id":      record.ID,
			"mode":          string(record.Mode),
			"labels":        record.Labels,
			"contributions": record.Contributions,
		},
	})

	log.Info("Build started",
		"build_id", record.ID,
		"mode", record.Mode,
		"labels", record.Labels,
		"contributions", len(record.Contributions),
	)

	var buildErr error

	if record.Mode == BuildModeFull {
		buildErr = b.executeFullRebuild(ctx, record)
	} else {
		buildErr = b.executeHotReload(ctx, record)
	}

	// Update status
	now := time.Now()
	status := BuildStatusSucceeded
	errMsg := ""
	if buildErr != nil {
		status = BuildStatusFailed
		errMsg = buildErr.Error()
		log.Error("Build failed", "build_id", record.ID, "error", buildErr)
	} else {
		log.Info("Build succeeded", "build_id", record.ID, "duration", now.Sub(record.StartedAt))
	}

	b.state.UpdateBuildStatus(record.ID, status, &now, errMsg)
	_ = b.state.Save()

	// Emit rebuild_complete notification
	b.agents.Emit(AgentEvent{
		Type:      "rebuild_complete",
		Timestamp: now,
		Data: map[string]interface{}{
			"build_id":            record.ID,
			"status":              string(status),
			"error":               errMsg,
			"contributions":       record.Contributions,
			"accumulated_features": b.state.GetAccumulatedFeatures(),
			"duration":            now.Sub(record.StartedAt).String(),
		},
	})

	// Check for queued requests
	b.mu.Lock()
	b.building = false
	if len(b.queue) > 0 {
		queued := b.queue
		b.queue = nil
		nextRecord := b.createRecord(queued)
		b.building = true
		go b.executeBuild(ctx, nextRecord)
		log.Info("Starting coalesced build from queue", "build_id", nextRecord.ID, "coalesced_count", len(queued))
	}
	b.mu.Unlock()
}

// executeFullRebuild stops the editor, builds, and restarts.
func (b *BuildOrchestrator) executeFullRebuild(ctx context.Context, record *BuildRecord) error {
	// Step 0: Check with connected agents before stopping editor
	if b.restartApprover != nil {
		for {
			approved, blockers := b.restartApprover(ctx)
			if approved {
				break
			}
			log.Info("Restart blocked by agents, retrying in 5s",
				"build_id", record.ID, "blockers", blockers)
			b.agents.Emit(AgentEvent{
				Type:      "restart_blocked",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"build_id":        record.ID,
					"blocking_agents": blockers,
				},
			})
			select {
			case <-ctx.Done():
				return fmt.Errorf("build cancelled while waiting for agent approval")
			case <-time.After(5 * time.Second):
				// retry
			}
		}
	}

	// Step 1: Stop the editor
	instances := b.manager.ListInstances()
	for _, inst := range instances {
		if inst.ProjectPath == record.ProjectPath && (inst.State == StateRunning || inst.State == StateStarting) {
			log.Info("Stopping editor for rebuild", "project", inst.ProjectName, "pid", inst.PID)
			if _, err := b.manager.StopEditor(inst.ProjectPath, false); err != nil {
				log.Warn("Failed to stop editor, continuing with build", "error", err)
			}
			break
		}
	}

	// Step 2: Run the build
	if err := b.runBuild(record); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Step 3: Restart the editor
	// Find the engine path from the last known instance, then fall back to .uproject manifest lookup
	enginePath := ""
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
		log.Warn("No engine path found, skipping editor restart")
		return nil
	}

	_, err := b.manager.StartEditor(&StartEditorRequest{
		ProjectPath: record.ProjectPath,
		EnginePath:  enginePath,
	})
	if err != nil {
		return fmt.Errorf("editor restart failed: %w", err)
	}

	return nil
}

// executeHotReload builds in the background while the editor stays running.
func (b *BuildOrchestrator) executeHotReload(_ context.Context, record *BuildRecord) error {
	if err := b.runBuild(record); err != nil {
		return fmt.Errorf("hot reload build failed: %w", err)
	}
	return nil
}

// runBuild executes the UBT build script, capturing output to a log file.
func (b *BuildOrchestrator) runBuild(record *BuildRecord) error {
	if err := EnsureLogDir(record.ProjectPath); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := BuildLogFile(record.ProjectPath)
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create build log file: %w", err)
	}
	defer logFile.Close() //nolint:errcheck

	log.Info("Running UBT build", "target", record.Target, "platform", record.Platform, "config", record.Configuration, "log", logPath)

	// Resolve engine path: try active instances first, then fall back to .uproject manifest lookup
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

	return pkg.RunBuildScriptToWriter(
		enginePath,
		record.Target,
		record.Platform,
		record.Configuration,
		record.ProjectPath,
		logFile,
	)
}

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
