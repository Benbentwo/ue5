package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
)

// ProjectInstance is the runtime representation of a managed editor instance.
type ProjectInstance struct {
	Info    InstanceInfo
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	capture *LogCapture
	done    chan struct{} // closed when cmd.Wait() completes
	mu      sync.Mutex
}

// InstanceManager tracks all managed editor instances.
type InstanceManager struct {
	instances map[string]*ProjectInstance // keyed by absolute project path
	mu        sync.RWMutex
}

// NewInstanceManager creates a new instance manager.
func NewInstanceManager() *InstanceManager {
	return &InstanceManager{
		instances: make(map[string]*ProjectInstance),
	}
}

// Count returns the number of tracked instances.
func (m *InstanceManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.instances)
}

// StartEditor starts an editor process for the given project.
func (m *InstanceManager) StartEditor(req *StartEditorRequest) (*InstanceInfo, error) {
	projectPath := req.ProjectPath

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if inst, exists := m.instances[projectPath]; exists {
		if inst.Info.State == StateRunning || inst.Info.State == StateStarting {
			return &inst.Info, fmt.Errorf("editor already running for project %s (PID %d)", projectPath, inst.Info.PID)
		}
	}

	// Resolve the editor binary path
	editorBinary := pkg.EditorBinaryPath(req.EnginePath)

	// Verify binary exists on disk
	if _, err := os.Stat(editorBinary); err != nil {
		return nil, fmt.Errorf("editor binary not found at %s: %w", editorBinary, err)
	}

	// Ensure log directory exists
	if err := EnsureLogDir(projectPath); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logFilePath := LogFile(projectPath)

	// Build command args
	args := []string{projectPath}
	args = append(args, req.ExtraArgs...)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, editorBinary, args...)

	// Create log capture
	capture, err := NewLogCapture(logFilePath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create log capture: %w", err)
	}

	// Pipe stdout and stderr to log capture
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		capture.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		capture.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Derive project name from path
	fileName := filepath.Base(projectPath)
	projectName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Parse uproject for engine version (use error-returning variant to avoid panic in daemon)
	engineVersion := ""
	uproject, uErr := pkg.NewUprojectE(projectPath)
	if uErr == nil && uproject != nil {
		engineVersion = uproject.EngineAssociation
	}

	info := InstanceInfo{
		ProjectPath:   projectPath,
		ProjectName:   projectName,
		EnginePath:    req.EnginePath,
		EngineVersion: engineVersion,
		State:         StateStarting,
		StartedAt:     time.Now(),
		LogFile:       logFilePath,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		cancel()
		capture.Close()
		return nil, fmt.Errorf("failed to start editor: %w", err)
	}

	info.PID = cmd.Process.Pid
	info.State = StateRunning

	inst := &ProjectInstance{
		Info:    info,
		cmd:     cmd,
		cancel:  cancel,
		capture: capture,
		done:    make(chan struct{}),
	}

	m.instances[projectPath] = inst

	// Start log capture goroutines
	go capture.CaptureStream(stdout, "stdout")
	go capture.CaptureStream(stderr, "stderr")

	// Monitor process lifecycle — this is the sole owner of cmd.Wait()
	go m.monitorProcess(inst)

	log.Info("Started editor", "project", projectName, "pid", info.PID, "log", logFilePath)
	return &info, nil
}

// monitorProcess waits for the editor process to exit and updates the instance state.
// This is the sole goroutine that calls cmd.Wait() — no other code should call it.
func (m *InstanceManager) monitorProcess(inst *ProjectInstance) {
	err := inst.cmd.Wait()
	close(inst.done) // Signal that the process has exited

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.capture != nil {
		inst.capture.Close()
	}

	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		inst.Info.ExitCode = &exitCode
		inst.Info.State = StateCrashed
		log.Warn("Editor process exited with error", "project", inst.Info.ProjectName, "pid", inst.Info.PID, "error", err)
	} else {
		exitCode := 0
		inst.Info.ExitCode = &exitCode
		inst.Info.State = StateStopped
		log.Info("Editor process exited cleanly", "project", inst.Info.ProjectName, "pid", inst.Info.PID)
	}
}

// StopEditor stops the editor for a given project.
func (m *InstanceManager) StopEditor(projectPath string, force bool) (*InstanceInfo, error) {
	m.mu.Lock()
	inst, exists := m.instances[projectPath]
	m.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("no editor instance found for project: %s", projectPath)
	}

	// Read current state without holding the lock for the entire stop duration
	inst.mu.Lock()
	if inst.Info.State != StateRunning && inst.Info.State != StateStarting {
		info := inst.Info
		inst.mu.Unlock()
		return &info, nil
	}
	inst.Info.State = StateStopping
	inst.mu.Unlock()

	if force {
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Kill()
		}
	} else {
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Signal(syscall.SIGTERM)
		}

		// Wait for monitorProcess to detect exit, with timeout
		select {
		case <-inst.done:
			// Process exited
		case <-time.After(10 * time.Second):
			log.Warn("Editor did not exit gracefully, force killing", "project", inst.Info.ProjectName)
			if inst.cmd.Process != nil {
				_ = inst.cmd.Process.Kill()
			}
			// Wait for monitorProcess to finish
			<-inst.done
		}
	}

	// Return the final state (set by monitorProcess)
	inst.mu.Lock()
	info := inst.Info
	inst.mu.Unlock()
	return &info, nil
}

// ListInstances returns info for all tracked instances.
func (m *InstanceManager) ListInstances() []InstanceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]InstanceInfo, 0, len(m.instances))
	for _, inst := range m.instances {
		inst.mu.Lock()
		result = append(result, inst.Info)
		inst.mu.Unlock()
	}
	return result
}

// StopAll stops all running editor instances.
func (m *InstanceManager) StopAll() {
	m.mu.RLock()
	projects := make([]string, 0, len(m.instances))
	for path, inst := range m.instances {
		if inst.Info.State == StateRunning || inst.Info.State == StateStarting {
			projects = append(projects, path)
		}
	}
	m.mu.RUnlock()

	for _, path := range projects {
		if _, err := m.StopEditor(path, false); err != nil {
			log.Error("Failed to stop editor", "project", path, "error", err)
		}
	}
}

// GetLogs queries logs for a given project.
func (m *InstanceManager) GetLogs(req *GetLogsRequest) (*LogsResponse, error) {
	// Check if we have the project
	m.mu.RLock()
	_, exists := m.instances[req.ProjectPath]
	m.mu.RUnlock()

	if !exists {
		// Check if log file exists anyway (from a previous session)
		logFile := LogFile(req.ProjectPath)
		if _, err := os.Stat(logFile); os.IsNotExist(err) {
			return nil, fmt.Errorf("no logs found for project: %s", req.ProjectPath)
		}
	}

	return QueryLogs(req)
}

// StreamLogs returns a channel that streams log lines for a project.
func (m *InstanceManager) StreamLogs(req *StreamLogsRequest) (<-chan LogLineEvent, error) {
	m.mu.RLock()
	inst, exists := m.instances[req.ProjectPath]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no active editor instance for project: %s", req.ProjectPath)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.capture == nil {
		return nil, fmt.Errorf("log capture not active for project: %s", req.ProjectPath)
	}

	return inst.capture.Subscribe(req), nil
}
