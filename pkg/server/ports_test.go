package server

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestAllocateSkipsBoundPort(t *testing.T) {
	l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", mcpPortRangeStart))
	if err != nil {
		t.Skipf("cannot bind %d on this machine: %v", mcpPortRangeStart, err)
	}
	defer func() { _ = l.Close() }()

	m := NewInstanceManager()
	m.mu.Lock()
	port, err := m.allocateMCPPortLocked(0)
	m.mu.Unlock()
	if err != nil {
		t.Fatalf("allocation failed: %v", err)
	}
	if port == mcpPortRangeStart {
		t.Fatalf("allocated externally-bound port %d", port)
	}
	if port != mcpPortRangeStart+1 {
		t.Errorf("expected next free port %d, got %d", mcpPortRangeStart+1, port)
	}
}

func TestAllocatePreferredHonored(t *testing.T) {
	m := NewInstanceManager()
	preferred := mcpPortRangeStart + 7
	m.mu.Lock()
	port, err := m.allocateMCPPortLocked(preferred)
	m.mu.Unlock()
	if err != nil {
		t.Fatalf("allocation failed: %v", err)
	}
	if port != preferred {
		t.Errorf("expected preferred port %d, got %d", preferred, port)
	}
}

func TestAllocatePreferredTakenFallsThrough(t *testing.T) {
	preferred := mcpPortRangeStart + 3
	l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err != nil {
		t.Skipf("cannot bind %d on this machine: %v", preferred, err)
	}
	defer func() { _ = l.Close() }()

	m := NewInstanceManager()
	m.mu.Lock()
	port, err := m.allocateMCPPortLocked(preferred)
	m.mu.Unlock()
	if err != nil {
		t.Fatalf("allocation failed: %v", err)
	}
	if port == preferred {
		t.Fatalf("allocated externally-bound preferred port %d", port)
	}
}

func TestAllocateSkipsActiveInstancePorts(t *testing.T) {
	m := NewInstanceManager()
	m.instances["/fake/A.uproject"] = &ProjectInstance{
		Info: InstanceInfo{ProjectPath: "/fake/A.uproject", State: StateRunning, MCPPort: mcpPortRangeStart},
	}
	m.instances["/fake/B.uproject"] = &ProjectInstance{
		Info: InstanceInfo{ProjectPath: "/fake/B.uproject", State: StateStopping, MCPPort: mcpPortRangeStart + 1},
	}
	// Stopped instances release their port.
	m.instances["/fake/C.uproject"] = &ProjectInstance{
		Info: InstanceInfo{ProjectPath: "/fake/C.uproject", State: StateStopped, MCPPort: mcpPortRangeStart + 2},
	}

	m.mu.Lock()
	port, err := m.allocateMCPPortLocked(0)
	m.mu.Unlock()
	if err != nil {
		t.Fatalf("allocation failed: %v", err)
	}
	if port == mcpPortRangeStart || port == mcpPortRangeStart+1 {
		t.Errorf("allocated a port held by an active instance: %d", port)
	}
	if port != mcpPortRangeStart+2 {
		t.Errorf("expected stopped instance's port %d to be reused, got %d", mcpPortRangeStart+2, port)
	}
}

func TestBuildEditorArgs(t *testing.T) {
	args := buildEditorArgs("/p/G.uproject", 55561, []string{"-log"})
	want := []string{"/p/G.uproject", "-SadTireMCPPort=55561", "-log"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("expected %v, got %v", want, args)
	}

	// Caller-supplied port suppresses injection.
	args = buildEditorArgs("/p/G.uproject", 55561, []string{"-SadTireMCPPort=60000"})
	want = []string{"/p/G.uproject", "-SadTireMCPPort=60000"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("expected %v, got %v", want, args)
	}

	// Port 0 (not allocated) injects nothing.
	args = buildEditorArgs("/p/G.uproject", 0, nil)
	want = []string{"/p/G.uproject"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("expected %v, got %v", want, args)
	}
}
