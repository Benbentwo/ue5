package server

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPServerEnablesSampling(t *testing.T) {
	d := &Daemon{
		version: "test",
	}
	w := newMCPServer(d)
	if w.mcp == nil {
		t.Fatal("MCP server should be created")
	}
}

func TestRequestRestartApproval_NoSessions(t *testing.T) {
	d := &Daemon{version: "test"}
	w := newMCPServer(d)

	approved, blockers := w.RequestRestartApproval(context.Background())
	if !approved {
		t.Error("Expected approved=true when no sessions are connected")
	}
	if len(blockers) != 0 {
		t.Errorf("Expected no blockers, got %v", blockers)
	}
}

func TestRequestRestartApproval_UnaskableSessionDoesNotBlock(t *testing.T) {
	d := &Daemon{version: "test"}
	w := newMCPServer(d)

	// Simulate a connected session whose context carries no MCP server —
	// the daemon can never send it a sampling request, so it can never
	// answer. Such a session must not veto the restart: it hasn't said
	// "I'm busy", it simply can't be asked.
	w.sessionsMu.Lock()
	w.sessions["session-unaskable"] = context.Background()
	w.sessionsMu.Unlock()

	approved, blockers := w.RequestRestartApproval(context.Background())
	if !approved {
		t.Errorf("Expected approved=true for a session that cannot be asked, got blockers %v", blockers)
	}
	if len(blockers) != 0 {
		t.Errorf("Expected no blockers, got %v", blockers)
	}
}

func TestIsSamplingUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		// mcp-go: transport session lacks SessionWithSampling (true for all
		// SSE sessions in mcp-go v0.44.0).
		{"transport lacks sampling", errors.New("session does not support sampling"), true},
		// mcp-go: no client session bound to the request context.
		{"no session in context", errors.New("no active session"), true},
		// Client rejected the request with JSON-RPC -32601.
		{"client method not found", errors.New("sampling request failed: Method not found"), true},
		// Client explicitly says it can't do sampling.
		{"client says not supported", errors.New("sampling request failed: sampling not supported"), true},
		// Transient failures stay blocking — the client might genuinely be
		// busy; the approval deadline bounds these instead.
		{"timeout stays blocking", context.DeadlineExceeded, false},
		{"other failure stays blocking", errors.New("sampling request failed: model overloaded"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSamplingUnsupported(tc.err); got != tc.want {
				t.Errorf("isSamplingUnsupported(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestParseRestartResponse_No(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "no"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("'no' should mean not busy")
	}
}

func TestParseRestartResponse_Yes(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "yes"},
		},
	}
	if !parseRestartResponse(resp) {
		t.Error("'yes' should mean busy")
	}
}

func TestParseRestartResponse_CaseInsensitive(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "No"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("'No' (capitalized) should mean not busy")
	}
}

func TestParseRestartResponse_NilResponse(t *testing.T) {
	if !parseRestartResponse(nil) {
		t.Error("nil response should be treated as busy (conservative)")
	}
}

func TestParseRestartResponse_GarbageText(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "I'm not sure what you mean"},
		},
	}
	if !parseRestartResponse(resp) {
		t.Error("ambiguous response should be treated as busy (conservative)")
	}
}

func TestParseRestartResponse_NoInSentence(t *testing.T) {
	resp := &mcp.CreateMessageResult{
		SamplingMessage: mcp.SamplingMessage{
			Role:    mcp.RoleAssistant,
			Content: mcp.TextContent{Type: "text", Text: "No, I am not working on anything"},
		},
	}
	if parseRestartResponse(resp) {
		t.Error("Response containing 'no' should mean not busy")
	}
}
