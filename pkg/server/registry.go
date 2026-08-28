package server

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// InstanceRegistry is the schema for ~/.ue5/instances.json — the on-disk
// discovery surface that tells a SadTire MCP bridge which port its project's
// editor is listening on. It is derived data: rewritten atomically from live
// InstanceManager state on every instance state change, truncated on daemon
// start/stop, and never read back by the daemon. Consumers must not trust it
// blindly — the SadTire ping's project_file_path is the verification contract.
type InstanceRegistry struct {
	Version   int                     `json:"version"`
	DaemonPID int                     `json:"daemon_pid"`
	UpdatedAt time.Time               `json:"updated_at"`
	Instances []InstanceRegistryEntry `json:"instances"`
}

// InstanceRegistryEntry is one active editor instance in the registry.
type InstanceRegistryEntry struct {
	ProjectPath string        `json:"project_path"`
	ProjectName string        `json:"project_name"`
	PID         int           `json:"pid"`
	MCPPort     int           `json:"mcp_port"`
	State       InstanceState `json:"state"`
	StartedAt   time.Time     `json:"started_at"`
}

// WriteInstanceRegistry snapshots the active (starting/running/stopping)
// instances to InstancesFile() atomically (temp file + rename).
func WriteInstanceRegistry(instances []InstanceInfo) error {
	reg := InstanceRegistry{
		Version:   1,
		DaemonPID: os.Getpid(),
		UpdatedAt: time.Now(),
		Instances: []InstanceRegistryEntry{},
	}
	for _, info := range instances {
		switch info.State {
		case StateStarting, StateRunning, StateStopping:
			reg.Instances = append(reg.Instances, InstanceRegistryEntry{
				ProjectPath: info.ProjectPath,
				ProjectName: info.ProjectName,
				PID:         info.PID,
				MCPPort:     info.MCPPort,
				State:       info.State,
				StartedAt:   info.StartedAt,
			})
		}
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal instance registry: %w", err)
	}

	path := InstancesFile()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write instance registry: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize instance registry: %w", err)
	}
	return nil
}
