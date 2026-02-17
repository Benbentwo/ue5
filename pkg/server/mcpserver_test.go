package server

import (
	"context"
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
