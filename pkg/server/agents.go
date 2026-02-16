package server

import (
	"fmt"
	"sync"
	"time"
)

// AgentEvent represents something that happened that agents should know about.
type AgentEvent struct {
	Type        string      `json:"type"`
	Timestamp   time.Time   `json:"timestamp"`
	Data        interface{} `json:"data"`
	SourceAgent string      `json:"source_agent,omitempty"`
}

// AgentRegistry manages registered agent sessions.
type AgentRegistry struct {
	mu      sync.RWMutex
	agents  map[string]*AgentInfo
	onEvent func(event AgentEvent)
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentInfo),
	}
}

// SetEventCallback sets the function called when events are emitted.
func (r *AgentRegistry) SetEventCallback(fn func(AgentEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEvent = fn
}

// Register adds an agent to the registry.
func (r *AgentRegistry) Register(info AgentInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if info.ID == "" {
		return fmt.Errorf("agent ID is required")
	}

	info.RegisteredAt = time.Now()
	info.LastSeenAt = time.Now()
	r.agents[info.ID] = &info

	if r.onEvent != nil {
		r.onEvent(AgentEvent{
			Type:      "agent_registered",
			Timestamp: time.Now(),
			Data:      info,
		})
	}

	return nil
}

// Unregister removes an agent from the registry.
func (r *AgentRegistry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[id]; !exists {
		return fmt.Errorf("agent not found: %s", id)
	}

	delete(r.agents, id)

	if r.onEvent != nil {
		r.onEvent(AgentEvent{
			Type:      "agent_unregistered",
			Timestamp: time.Now(),
			Data:      map[string]string{"id": id},
		})
	}

	return nil
}

// UpdateLastSeen updates the last-seen timestamp for an agent.
func (r *AgentRegistry) UpdateLastSeen(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if agent, exists := r.agents[id]; exists {
		agent.LastSeenAt = time.Now()
	}
}

// List returns all registered agents.
func (r *AgentRegistry) List() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]AgentInfo, 0, len(r.agents))
	for _, agent := range r.agents {
		result = append(result, *agent)
	}
	return result
}

// Get returns a specific agent by ID.
func (r *AgentRegistry) Get(id string) (*AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, exists := r.agents[id]
	if !exists {
		return nil, false
	}
	copy := *agent
	return &copy, true
}

// Count returns the number of registered agents.
func (r *AgentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// Emit broadcasts an event to the event callback.
func (r *AgentRegistry) Emit(event AgentEvent) {
	r.mu.RLock()
	fn := r.onEvent
	r.mu.RUnlock()

	if fn != nil {
		fn(event)
	}
}
