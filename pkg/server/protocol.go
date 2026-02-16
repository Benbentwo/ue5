package server

import "time"

// RequestType enumerates all valid request types.
type RequestType string

const (
	ReqPing          RequestType = "ping"
	ReqShutdown      RequestType = "shutdown"
	ReqStartEditor   RequestType = "start_editor"
	ReqStopEditor    RequestType = "stop_editor"
	ReqListInstances RequestType = "list_instances"
	ReqGetLogs       RequestType = "get_logs"
	ReqStreamLogs    RequestType = "stream_logs"
)

// Request is the envelope for all client-to-server messages.
type Request struct {
	ID   string      `json:"id"`
	Type RequestType `json:"type"`

	StartEditor *StartEditorRequest `json:"start_editor,omitempty"`
	StopEditor  *StopEditorRequest  `json:"stop_editor,omitempty"`
	GetLogs     *GetLogsRequest     `json:"get_logs,omitempty"`
	StreamLogs  *StreamLogsRequest  `json:"stream_logs,omitempty"`
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

// Response is the envelope for all server-to-client messages.
type Response struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	Instance  *InstanceInfo   `json:"instance,omitempty"`
	Instances []InstanceInfo  `json:"instances,omitempty"`
	Logs      *LogsResponse   `json:"logs,omitempty"`
	LogLine   *LogLineEvent   `json:"log_line,omitempty"`
	Pong      *PongResponse   `json:"pong,omitempty"`
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
