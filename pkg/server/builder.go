package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
)

const (
	// defaultApprovalRetryInterval is how long to wait between restart
	// approval rounds while agents report busy.
	defaultApprovalRetryInterval = 5 * time.Second
	// defaultApprovalTimeout bounds the total time spent waiting for agent
	// approval. Once exceeded the build proceeds anyway — a client that
	// cannot or will not answer must not hold full rebuilds hostage.
	defaultApprovalTimeout = 2 * time.Minute
)

// BuildOrchestrator manages the build lifecycle with coalescing support.
type BuildOrchestrator struct {
	manager               *InstanceManager
	state                 *StateStore
	agents                *AgentRegistry
	mcpServer             *mcpServerWrapper
	restartApprover       func(ctx context.Context) (bool, []string)
	buildRunner           func(record *BuildRecord) error // defaults to runBuild; overridable for testing
	approvalRetryInterval time.Duration
	approvalTimeout       time.Duration
	mu                    sync.Mutex
	building              bool
	activeProject         string // project path of the in-progress build; guarded by mu
	queue                 []RebuildRequest
	pending               *BuildRecord // shared record for queued requests; guarded by mu
	buildCapture          *LogCapture
	buildCaptureMu        sync.RWMutex
}

// NewBuildOrchestrator creates a new build orchestrator.
func NewBuildOrchestrator(manager *InstanceManager, state *StateStore, agents *AgentRegistry) *BuildOrchestrator {
	return &BuildOrchestrator{
		manager:               manager,
		state:                 state,
		agents:                agents,
		queue:                 []RebuildRequest{},
		approvalRetryInterval: defaultApprovalRetryInterval,
		approvalTimeout:       defaultApprovalTimeout,
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
		// The queue coalesces into ONE record, so cross-project requests would
		// silently build the wrong project. Refuse them instead.
		if req.ProjectPath != b.activeProject {
			return nil, fmt.Errorf("a build for %s is in progress; cross-project build queueing is not supported — retry after it completes", b.activeProject)
		}
		// Queue the request for coalescing
		b.queue = append(b.queue, *req)
		log.Info("Rebuild queued for coalescing", "label", req.Label, "agent", req.AgentID, "queue_size", len(b.queue))

		// All queued requests share ONE pending record, and the coalesced
		// build keeps its ID when it starts (see the drain in executeBuild).
		// Minting a fresh ID per queued caller — as this used to — hands out
		// IDs that never appear in state or events, so clients polling them
		// wait forever.
		if b.pending == nil {
			b.pending = &BuildRecord{
				ID:            fmt.Sprintf("build-%d", time.Now().UnixNano()),
				ProjectPath:   req.ProjectPath,
				Labels:        []string{},
				Contributions: []BuildContribution{},
				Mode:          BuildModeHotReload,
				Status:        BuildStatusPending,
			}
		}
		b.pending.Labels = append(b.pending.Labels, req.Label)
		b.pending.Contributions = append(b.pending.Contributions, BuildContribution{
			AgentID: req.AgentID,
			Label:   req.Label,
		})
		// Mode escalation: "full" wins over "hot_reload"
		if req.Mode == BuildModeFull {
			b.pending.Mode = BuildModeFull
		}
		return b.pendingCopyLocked(), nil
	}

	// Start build immediately
	record := b.createRecord([]RebuildRequest{*req})
	b.building = true
	b.activeProject = record.ProjectPath

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
		if record.EnginePath == "" {
			record.EnginePath = req.EnginePath
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
			"build_id":             record.ID,
			"status":               string(status),
			"error":                errMsg,
			"contributions":        record.Contributions,
			"accumulated_features": b.state.GetAccumulatedFeatures(),
			"duration":             now.Sub(record.StartedAt).String(),
		},
	})

	// Check for queued requests
	b.mu.Lock()
	b.building = false
	b.activeProject = ""
	if len(b.queue) > 0 {
		queued := b.queue
		b.queue = nil
		nextRecord := b.createRecord(queued)
		if b.pending != nil {
			// Queued callers hold the pending record's ID; the coalesced
			// build must keep it so those IDs stay pollable end-to-end.
			nextRecord.ID = b.pending.ID
			b.pending = nil
		}
		b.building = true
		b.activeProject = nextRecord.ProjectPath
		go b.executeBuild(ctx, nextRecord)
		log.Info("Starting coalesced build from queue", "build_id", nextRecord.ID, "coalesced_count", len(queued))
	}
	b.mu.Unlock()
}

// PendingBuild returns a copy of the queued-but-not-started coalesced build
// record, or nil when nothing is queued. Lets status lookups resolve IDs that
// were handed out for queued requests before the build starts.
func (b *BuildOrchestrator) PendingBuild() *BuildRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pendingCopyLocked()
}

// pendingCopyLocked deep-copies the pending record's caller-visible slices so
// later queue merges don't mutate a record already returned. Caller holds mu.
func (b *BuildOrchestrator) pendingCopyLocked() *BuildRecord {
	if b.pending == nil {
		return nil
	}
	record := *b.pending
	record.Labels = append([]string(nil), b.pending.Labels...)
	record.Contributions = append([]BuildContribution(nil), b.pending.Contributions...)
	return &record
}

// executeFullRebuild performs a strict stop -> build -> restart cycle.
//
// The ordering is load-bearing: UBT decides at startup whether to build in
// hot-reload mode by checking for live UnrealEditor processes from the same
// engine. If any editor is alive when UBT starts — tracked, orphaned from a
// previous daemon, or launched by hand — the build emits
// libUnrealEditor-<Module>-000N patch binaries instead of relinking the base
// binaries, the build reports success, and the restarted editor silently runs
// stale code. The editor must be verifiably exited (process gone, not just
// signalled) before UBT starts.
func (b *BuildOrchestrator) executeFullRebuild(ctx context.Context, record *BuildRecord) error {
	enginePath := b.resolveEnginePath(record)

	// Step 1: Check with connected agents before disrupting the editor.
	// Approval is best-effort with a hard deadline: agents that answer "busy"
	// delay the restart, but a client that cannot or will not answer must not
	// hold the build hostage forever — past the deadline we log the blockers
	// loudly and proceed.
	if b.restartApprover != nil {
		deadline := time.Now().Add(b.approvalTimeout)
		for {
			approved, blockers := b.restartApprover(ctx)
			if approved {
				break
			}
			if time.Now().After(deadline) {
				log.Warn("Restart approval deadline exceeded; proceeding with rebuild despite blockers",
					"build_id", record.ID, "blockers", blockers, "waited", b.approvalTimeout)
				b.agents.Emit(AgentEvent{
					Type:      "restart_forced",
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"build_id":        record.ID,
						"blocking_agents": blockers,
						"waited":          b.approvalTimeout.String(),
					},
				})
				break
			}
			log.Info("Restart blocked by agents, retrying",
				"build_id", record.ID, "blockers", blockers, "retry_in", b.approvalRetryInterval)
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
			case <-time.After(b.approvalRetryInterval):
				// retry
			}
		}
	}

	// Step 2: Stop the tracked editor instance (waits for actual process
	// exit, force-killing after 10s). Remember its SadTire MCP port so the
	// restarted editor keeps it and connected sessions don't have to re-resolve.
	var prevMCPPort int
	for _, inst := range b.manager.ListInstances() {
		if inst.ProjectPath == record.ProjectPath && (inst.State == StateRunning || inst.State == StateStarting) {
			log.Info("Stopping editor for rebuild", "project", inst.ProjectName, "pid", inst.PID)
			prevMCPPort = inst.MCPPort
			if _, err := b.manager.StopEditor(inst.ProjectPath, false); err != nil {
				log.Warn("Failed to stop editor via instance manager, relying on process gate", "error", err)
			}
			break
		}
	}

	// Step 3: Gate — verify at the process level that NO UnrealEditor from
	// this engine is still alive (catches orphans the daemon never tracked).
	// Refuses to start UBT if any editor survives termination.
	if enginePath != "" {
		if err := ensureNoEditorProcesses(pkg.EditorBinaryPath(enginePath), record.ProjectPath); err != nil {
			return fmt.Errorf("refusing to start UBT: %w", err)
		}
	} else {
		log.Warn("No engine path resolved; skipping editor process gate", "build_id", record.ID)
	}

	// Step 4: Delete stale hot-reload patch binaries from earlier incidents
	// so module manifests can only reference freshly linked base binaries.
	if removed := cleanHotReloadArtifacts(filepath.Dir(record.ProjectPath)); len(removed) > 0 {
		log.Warn("Removed stale hot-reload patch binaries", "count", len(removed), "files", removed)
	}

	// Step 5: Run the build with no editor alive
	buildFn := b.runBuild
	if b.buildRunner != nil {
		buildFn = b.buildRunner
	}
	buildErr := buildFn(record)

	// Step 6: Restart the editor. On build failure the previous binaries are
	// still intact, so restarting keeps the machine usable either way.
	if enginePath == "" {
		log.Warn("No engine path found, skipping editor restart")
		if buildErr != nil {
			return fmt.Errorf("build failed: %w", buildErr)
		}
		return nil
	}
	if _, err := b.manager.StartEditor(&StartEditorRequest{
		ProjectPath: record.ProjectPath,
		EnginePath:  enginePath,
		MCPPort:     prevMCPPort,
	}); err != nil {
		if buildErr != nil {
			return fmt.Errorf("build failed: %w (editor restart also failed: %v)", buildErr, err)
		}
		return fmt.Errorf("editor restart failed: %w", err)
	}

	if buildErr != nil {
		return fmt.Errorf("build failed: %w", buildErr)
	}
	return nil
}

// resolveEnginePath finds the engine for a build: a tracked instance's engine
// first, then the engine_path carried on the rebuild request, then the
// .uproject's engine association manifest lookup.
func (b *BuildOrchestrator) resolveEnginePath(record *BuildRecord) string {
	for _, inst := range b.manager.ListInstances() {
		if inst.ProjectPath == record.ProjectPath && inst.EnginePath != "" {
			return inst.EnginePath
		}
	}
	if record.EnginePath != "" {
		return record.EnginePath
	}
	uproject, err := pkg.NewUprojectE(record.ProjectPath)
	if err == nil && uproject.EngineAssociation != "" {
		return pkg.GetEnginePath(uproject.EngineAssociation)
	}
	return ""
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

	log.Info("Running UBT build", "target", record.Target, "platform", record.Platform, "config", record.Configuration, "log", logPath)

	enginePath := b.resolveEnginePath(record)
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

	// Full builds must never produce hot-reload patch binaries, even if an
	// editor slips in between the process gate and UBT startup — the flag
	// forces UBT to link base binaries regardless of editor detection.
	var extraArgs []string
	if record.Mode == BuildModeFull {
		extraArgs = append(extraArgs, "-NoHotReloadFromIDE")
	}

	// Get piped command
	cmd, stdout, stderr, err := pkg.RunBuildScriptPiped(
		enginePath,
		record.Target,
		record.Platform,
		record.Configuration,
		record.ProjectPath,
		extraArgs...,
	)
	if err != nil {
		return fmt.Errorf("failed to create build command: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start build: %w", err)
	}

	// Capture streams in background goroutines
	var streams sync.WaitGroup
	streams.Add(2)
	go func() {
		defer streams.Done()
		capture.CaptureStream(stdout, "stdout")
	}()
	go func() {
		defer streams.Done()
		capture.CaptureStream(stderr, "stderr")
	}()

	// Drain the pipes before Wait(): Wait closes them the moment UBT exits, so
	// waiting first would truncate the tail of the build log — precisely the
	// lines that explain a failure.
	if !pkg.WaitForStreams(&streams, pkg.StreamDrainTimeout) {
		log.Warn("Timed out draining build output; trailing build log lines may be missing", "timeout", pkg.StreamDrainTimeout)
	}

	// Wait for build to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	return nil
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
