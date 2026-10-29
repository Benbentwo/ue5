package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	// mcpPortRangeStart is scanned first so single-project setups keep the
	// default 55560 and existing .mcp.json configs keep working.
	mcpPortRangeStart = 55560
	mcpPortRangeEnd   = 55660
)

// allocateMCPPortLocked picks the editor MCP port for a new editor instance:
// the preferred port if given and free, otherwise the lowest port in
// [mcpPortRangeStart, mcpPortRangeEnd] that is neither assigned to an active
// tracked instance nor currently bound on 127.0.0.1 (the bind probe catches
// editors this daemon never launched). Caller must hold m.mu.
//
// The probe-then-launch TOCTOU window is accepted: within the daemon it is
// closed by m.mu plus the in-use map, and an external steal surfaces loudly
// in the editor (the editor-side bridge binds with SO_REUSEADDR off and toasts
// on failure) and is rejected by the Python bridge's ping verification.
func (m *InstanceManager) allocateMCPPortLocked(preferred int) (int, error) {
	inUse := make(map[int]bool)
	for _, inst := range m.instances {
		switch inst.Info.State {
		case StateStarting, StateRunning, StateStopping:
			// A stopping editor still holds its socket.
			if inst.Info.MCPPort != 0 {
				inUse[inst.Info.MCPPort] = true
			}
		}
	}

	if preferred != 0 && !inUse[preferred] && portFree(preferred) {
		return preferred, nil
	}

	for port := mcpPortRangeStart; port <= mcpPortRangeEnd; port++ {
		if inUse[port] {
			continue
		}
		if portFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free editor MCP port in %d-%d", mcpPortRangeStart, mcpPortRangeEnd)
}

// portFree reports whether port is bindable on 127.0.0.1 right now.
func portFree(port int) bool {
	l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// MCPPortArgNames is the editor command-line names the allocated MCP port is
// passed under, without the leading dash. "MCPPort" always; an editor plugin
// that reads a different name can be served by listing it in the
// UE5_MCP_PORT_ARGS environment variable (comma separated) or in
// ~/.ue5/mcp_port_args (one name per line or comma separated).
func MCPPortArgNames() []string {
	names := []string{"MCPPort"}
	seen := map[string]bool{"MCPPort": true}
	add := func(raw string) {
		for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' }) {
			f = strings.TrimLeft(strings.TrimSpace(f), "-")
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			names = append(names, f)
		}
	}
	add(os.Getenv("UE5_MCP_PORT_ARGS"))
	if b, err := os.ReadFile(filepath.Join(HomeDir(), "mcp_port_args")); err == nil {
		add(string(b))
	}
	return names
}
