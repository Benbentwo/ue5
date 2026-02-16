package server

import (
	"sync"
	"testing"
)

func TestAgentRegistration(t *testing.T) {
	reg := NewAgentRegistry()

	err := reg.Register(AgentInfo{
		ID:          "agent-1",
		Name:        "Test Agent",
		Description: "Working on inventory system",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if reg.Count() != 1 {
		t.Errorf("Expected 1 agent, got %d", reg.Count())
	}

	agent, ok := reg.Get("agent-1")
	if !ok {
		t.Fatal("Agent not found after registration")
	}
	if agent.Name != "Test Agent" {
		t.Errorf("Expected name 'Test Agent', got '%s'", agent.Name)
	}
	if agent.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be set")
	}
	if agent.LastSeenAt.IsZero() {
		t.Error("LastSeenAt should be set")
	}
}

func TestAgentRegistrationEmptyID(t *testing.T) {
	reg := NewAgentRegistry()

	err := reg.Register(AgentInfo{Name: "No ID"})
	if err == nil {
		t.Error("Expected error for empty agent ID")
	}
}

func TestAgentUnregister(t *testing.T) {
	reg := NewAgentRegistry()

	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "Agent 1"})
	_ = reg.Register(AgentInfo{ID: "agent-2", Name: "Agent 2"})

	if reg.Count() != 2 {
		t.Fatalf("Expected 2 agents, got %d", reg.Count())
	}

	err := reg.Unregister("agent-1")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	if reg.Count() != 1 {
		t.Errorf("Expected 1 agent after unregister, got %d", reg.Count())
	}

	_, ok := reg.Get("agent-1")
	if ok {
		t.Error("Agent should not be found after unregister")
	}

	// Verify unregistering non-existent agent returns error
	err = reg.Unregister("agent-1")
	if err == nil {
		t.Error("Expected error when unregistering non-existent agent")
	}
}

func TestAgentList(t *testing.T) {
	reg := NewAgentRegistry()

	_ = reg.Register(AgentInfo{ID: "a", Name: "Agent A"})
	_ = reg.Register(AgentInfo{ID: "b", Name: "Agent B"})
	_ = reg.Register(AgentInfo{ID: "c", Name: "Agent C"})

	agents := reg.List()
	if len(agents) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(agents))
	}

	// Verify all agents are present (order not guaranteed due to map iteration)
	ids := map[string]bool{}
	for _, a := range agents {
		ids[a.ID] = true
	}
	for _, id := range []string{"a", "b", "c"} {
		if !ids[id] {
			t.Errorf("Agent '%s' missing from list", id)
		}
	}
}

func TestAgentUpdateLastSeen(t *testing.T) {
	reg := NewAgentRegistry()

	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "Agent"})
	agent1, _ := reg.Get("agent-1")
	firstSeen := agent1.LastSeenAt

	reg.UpdateLastSeen("agent-1")

	agent2, _ := reg.Get("agent-1")
	if agent2.LastSeenAt.Before(firstSeen) {
		t.Error("LastSeenAt should be >= first seen time after update")
	}

	// Should not panic on non-existent agent
	reg.UpdateLastSeen("nonexistent")
}

func TestAgentEventCallback(t *testing.T) {
	reg := NewAgentRegistry()

	var events []AgentEvent
	var mu sync.Mutex
	reg.SetEventCallback(func(event AgentEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "Agent"})
	_ = reg.Unregister("agent-1")

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if events[0].Type != "agent_registered" {
		t.Errorf("Expected 'agent_registered', got '%s'", events[0].Type)
	}
	if events[1].Type != "agent_unregistered" {
		t.Errorf("Expected 'agent_unregistered', got '%s'", events[1].Type)
	}
}

func TestAgentEmit(t *testing.T) {
	reg := NewAgentRegistry()

	var received AgentEvent
	reg.SetEventCallback(func(event AgentEvent) {
		received = event
	})

	reg.Emit(AgentEvent{
		Type: "custom_event",
		Data: "test_data",
	})

	if received.Type != "custom_event" {
		t.Errorf("Expected 'custom_event', got '%s'", received.Type)
	}
}

func TestAgentGetReturnsACopy(t *testing.T) {
	reg := NewAgentRegistry()
	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "Original"})

	agent, _ := reg.Get("agent-1")
	agent.Name = "Modified"

	// Original should be unchanged
	original, _ := reg.Get("agent-1")
	if original.Name != "Original" {
		t.Errorf("Get should return a copy; original was mutated to '%s'", original.Name)
	}
}

func TestAgentReRegister(t *testing.T) {
	reg := NewAgentRegistry()

	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "First", Description: "v1"})
	_ = reg.Register(AgentInfo{ID: "agent-1", Name: "Second", Description: "v2"})

	if reg.Count() != 1 {
		t.Errorf("Re-registration should not duplicate; got count %d", reg.Count())
	}

	agent, _ := reg.Get("agent-1")
	if agent.Name != "Second" {
		t.Errorf("Expected updated name 'Second', got '%s'", agent.Name)
	}
}
