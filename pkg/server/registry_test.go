package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWriteInstanceRegistryFiltersInactive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := EnsureHomeDir(); err != nil {
		t.Fatal(err)
	}

	instances := []InstanceInfo{
		{ProjectPath: "/a/A.uproject", ProjectName: "A", PID: 1, MCPPort: 55560, State: StateRunning, StartedAt: time.Now()},
		{ProjectPath: "/b/B.uproject", ProjectName: "B", PID: 2, MCPPort: 55561, State: StateStarting, StartedAt: time.Now()},
		{ProjectPath: "/c/C.uproject", ProjectName: "C", PID: 3, MCPPort: 55562, State: StateStopped, StartedAt: time.Now()},
		{ProjectPath: "/d/D.uproject", ProjectName: "D", PID: 4, MCPPort: 55563, State: StateCrashed, StartedAt: time.Now()},
	}
	if err := WriteInstanceRegistry(instances); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(InstancesFile())
	if err != nil {
		t.Fatal(err)
	}
	var reg InstanceRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("registry is not valid JSON: %v", err)
	}

	if reg.Version != 1 {
		t.Errorf("expected version 1, got %d", reg.Version)
	}
	if reg.DaemonPID != os.Getpid() {
		t.Errorf("expected daemon_pid %d, got %d", os.Getpid(), reg.DaemonPID)
	}
	if len(reg.Instances) != 2 {
		t.Fatalf("expected 2 active instances, got %d", len(reg.Instances))
	}
	if reg.Instances[0].ProjectName != "A" || reg.Instances[0].MCPPort != 55560 {
		t.Errorf("unexpected first entry: %+v", reg.Instances[0])
	}
	if reg.Instances[1].State != StateStarting {
		t.Errorf("expected starting state preserved, got %s", reg.Instances[1].State)
	}

	// No temp file left behind.
	if _, err := os.Stat(InstancesFile() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
}

func TestWriteInstanceRegistryNilIsEmptyArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := EnsureHomeDir(); err != nil {
		t.Fatal(err)
	}

	if err := WriteInstanceRegistry(nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(InstancesFile())
	if err != nil {
		t.Fatal(err)
	}
	// The Python bridge iterates registry["instances"]; null would break it.
	if strings.Contains(string(data), `"instances": null`) {
		t.Fatalf("instances serialized as null:\n%s", data)
	}
	var reg InstanceRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Instances == nil || len(reg.Instances) != 0 {
		t.Errorf("expected empty instances array, got %v", reg.Instances)
	}
}
