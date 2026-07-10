package server

import "time"

// RequestType enumerates all valid request types.
type RequestType string

const (
	ReqPing            RequestType = "ping"
	ReqShutdown        RequestType = "shutdown"
	ReqStartEditor     RequestType = "start_editor"
	ReqStopEditor      RequestType = "stop_editor"
	ReqListInstances   RequestType = "list_instances"
	ReqGetLogs         RequestType = "get_logs"
	ReqStreamLogs      RequestType = "stream_logs"
	ReqRebuild         RequestType = "rebuild"
	ReqGetBuildInfo    RequestType = "get_build_info"
	ReqRegisterAgent   RequestType = "register_agent"
	ReqUnregisterAgent RequestType = "unregister_agent"
	ReqListAgents      RequestType = "list_agents"
)

// Request is the envelope for all client-to-server messages.
type Request struct {
	ID   string      `json:"id"`
	Type RequestType `json:"type"`

	StartEditor     *StartEditorRequest     `json:"start_editor,omitempty"`
	StopEditor      *StopEditorRequest      `json:"stop_editor,omitempty"`
	GetLogs         *GetLogsRequest         `json:"get_logs,omitempty"`
	StreamLogs      *StreamLogsRequest      `json:"stream_logs,omitempty"`
	Rebuild         *RebuildRequest         `json:"rebuild,omitempty"`
	RegisterAgent   *RegisterAgentRequest   `json:"register_agent,omitempty"`
	UnregisterAgent *UnregisterAgentRequest `json:"unregister_agent,omitempty"`
}

// StartEditorRequest contains the parameters for starting an editor instance.
type StartEditorRequest struct {
	ProjectPath string   `json:"project_path"`
	EnginePath  string   `json:"engine_path"`
	ExtraArgs   []string `json:"extra_args,omitempty"`
}

// StopEditorRequest contains the parameters for stopping an editor instance.
type StopEditorRequest struct {
	ProjectPath string `json:"project_path"`
	Force       bool   `json:"force"`
}

// GetLogsRequest contains the parameters for querying logs.
type GetLogsRequest struct {
	ProjectPath string `json:"project_path"`
	Lines       int    `json:"lines"`
	Level       string `json:"level"`
	Category    string `json:"category"`
	Pattern     string `json:"pattern"`
	Since       string `json:"since"`
}

// StreamLogsRequest contains the parameters for streaming logs.
type StreamLogsRequest struct {
	ProjectPath string `json:"project_path"`
	Level       string `json:"level"`
	Category    string `json:"category"`
	Pattern     string `json:"pattern"`
}

// BuildMode determines how a rebuild is executed.
type BuildMode string

const (
	BuildModeFull      BuildMode = "full"       // stop editor -> build -> restart
	BuildModeHotReload BuildMode = "hot_reload" // build in background, editor stays running
)

// BuildStatus represents the lifecycle state of a build.
type BuildStatus string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusBuilding  BuildStatus = "building"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
)

// BuildContribution tracks a single agent's contribution to a coalesced build.
type BuildContribution struct {
	AgentID string `json:"agent_id"`
	Label   string `json:"label"`
}

// BuildRecord represents a single build event, potentially coalesced from multiple agent requests.
type BuildRecord struct {
	ID            string              `json:"id"`
	ProjectPath   string              `json:"project_path"`
	EnginePath    string              `json:"engine_path,omitempty"`
	Labels        []string            `json:"labels"`
	Features      []string            `json:"features"`
	Contributions []BuildContribution `json:"contributions"`
	Mode          BuildMode           `json:"mode"`
	Status        BuildStatus         `json:"status"`
	StartedAt     time.Time           `json:"started_at"`
	CompletedAt   *time.Time          `json:"completed_at,omitempty"`
	Error         string              `json:"error,omitempty"`
	Target        string              `json:"target"`
	Platform      string              `json:"platform"`
	Configuration string              `json:"configuration"`
}

// AgentInfo is the serializable representation of a registered AI agent.
type AgentInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	RegisteredAt time.Time `json:"registered_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// RebuildRequest contains the parameters for triggering a project rebuild.
type RebuildRequest struct {
	ProjectPath   string    `json:"project_path"`
	EnginePath    string    `json:"engine_path"`
	Mode          BuildMode `json:"mode"`
	Label         string    `json:"label"`
	Target        string    `json:"target,omitempty"`
	Configuration string    `json:"configuration,omitempty"`
	AgentID       string    `json:"agent_id,omitempty"`
}

// RegisterAgentRequest contains the parameters for registering an AI agent.
type RegisterAgentRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UnregisterAgentRequest contains the parameters for unregistering an AI agent.
type UnregisterAgentRequest struct {
	ID string `json:"id"`
}

// BuildInfoResponse is the rich response returned over the daemon socket
// protocol (CLI consumption). The MCP server composes a slimmer response
// inline — see mcpserver.go handleGetBuildInfo for the agent-facing shape,
// which trims AccumulatedFeatures and RecentBuilds to keep polling cheap.
type BuildInfoResponse struct {
	CurrentBuild        *BuildRecord  `json:"current_build,omitempty"`
	AccumulatedFeatures []string      `json:"accumulated_features"`
	TotalBuilds         int           `json:"total_builds"`
	RecentBuilds        []BuildRecord `json:"recent_builds"`
}

// BuildSummary is a compact representation of a BuildRecord for MCP responses.
// Strips ProjectPath, Features, Contributions, CompletedAt, Target, Platform,
// Configuration — fields agents rarely branch on, keeping list responses small.
type BuildSummary struct {
	ID        string      `json:"id"`
	Status    BuildStatus `json:"status"`
	Labels    []string    `json:"labels"`
	Mode      BuildMode   `json:"mode"`
	StartedAt time.Time   `json:"started_at"`
	Error     string      `json:"error,omitempty"`
}

// NewBuildSummary creates a compact summary from a full BuildRecord.
func NewBuildSummary(r BuildRecord) BuildSummary {
	return BuildSummary{
		ID:        r.ID,
		Status:    r.Status,
		Labels:    r.Labels,
		Mode:      r.Mode,
		StartedAt: r.StartedAt,
		Error:     r.Error,
	}
}

// BuildStatusResponse is the minimal polling response.
//
// Three fields, by design:
//   - ID: handle the agent uses to query failure details via get_build_failure
//   - Prompt: the human-readable label(s) supplied at rebuild time, joined for
//     coalesced builds (e.g. "Added jump + Fixed damage"). Lets agents tell
//     "is my work in this build?" without paging through Labels/Contributions.
//   - Status: succeeded / failed / building / pending — the only thing a
//     polling loop needs to branch on.
//
// When no build has ever run, all fields are zero-valued and the agent can
// detect "no build yet" via ID == "". Even smaller than BuildSummary on
// purpose — this is the polling-hot path.
type BuildStatusResponse struct {
	ID     string      `json:"id"`
	Prompt string      `json:"prompt"`
	Status BuildStatus `json:"status"`
}

// BuildHistoryResponse is returned for get_build_history requests (opt-in).
// Uses BuildSummary to keep each record compact even when returning many.
type BuildHistoryResponse struct {
	Builds              []BuildSummary `json:"builds"`
	AccumulatedFeatures []string       `json:"accumulated_features"`
	TotalBuilds         int            `json:"total_builds"`
}

// BuildFailureResponse is returned for get_build_failure requests.
type BuildFailureResponse struct {
	BuildID    string   `json:"build_id"`
	Error      string   `json:"error,omitempty"`
	ErrorLines []string `json:"error_lines"`
	LogPath    string   `json:"log_path"`
}

// Response is the envelope for all server-to-client messages.
type Response struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	Instance  *InstanceInfo      `json:"instance,omitempty"`
	Instances []InstanceInfo     `json:"instances,omitempty"`
	Logs      *LogsResponse      `json:"logs,omitempty"`
	LogLine   *LogLineEvent      `json:"log_line,omitempty"`
	Pong      *PongResponse      `json:"pong,omitempty"`
	Build     *BuildRecord       `json:"build,omitempty"`
	Builds    []BuildRecord      `json:"builds,omitempty"`
	Agent     *AgentInfo         `json:"agent,omitempty"`
	Agents    []AgentInfo        `json:"agents,omitempty"`
	BuildInfo *BuildInfoResponse `json:"build_info,omitempty"`
}

// InstanceState represents the lifecycle state of an editor instance.
type InstanceState string

const (
	StateStarting InstanceState = "starting"
	StateRunning  InstanceState = "running"
	StateStopping InstanceState = "stopping"
	StateStopped  InstanceState = "stopped"
	StateCrashed  InstanceState = "crashed"
)

// InstanceInfo is the serializable state of a managed editor instance.
type InstanceInfo struct {
	ProjectPath   string        `json:"project_path"`
	ProjectName   string        `json:"project_name"`
	EnginePath    string        `json:"engine_path"`
	EngineVersion string        `json:"engine_version"`
	PID           int           `json:"pid"`
	State         InstanceState `json:"state"`
	StartedAt     time.Time     `json:"started_at"`
	LogFile       string        `json:"log_file"`
	ExitCode      *int          `json:"exit_code,omitempty"`
}

// PongResponse is returned in response to a ping request.
type PongResponse struct {
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	Instances int    `json:"instances"`
}

// LogLineEvent represents a single log line for streaming responses.
type LogLineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"`
	Raw       string    `json:"raw"`
	Level     string    `json:"level"`
	Category  string    `json:"category"`
}

// LogsResponse is returned for get_logs requests.
type LogsResponse struct {
	Lines         []LogLineEvent `json:"lines"`
	TotalLines    int            `json:"total_lines"`
	FilteredLines int            `json:"filtered_lines"`
}
