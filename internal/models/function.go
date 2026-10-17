package models

import (
	"encoding/json"
	"time"
)

// FunctionSpec is a draft; a deployment snapshots it into an immutable version.
type FunctionSpec struct {
	Name            string `json:"name"`
	Runtime         string `json:"runtime"`
	Code            string `json:"code"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	MemoryMB        int    `json:"memory_mb"`
	CPUMillis       int    `json:"cpu_millis"`
	Concurrency     int    `json:"concurrency"`
	Schedule        string `json:"schedule"`
	Timezone        string `json:"timezone"`
	ScheduleEnabled bool   `json:"schedule_enabled"`
	HTTPEnabled     bool   `json:"http_enabled"`
}
type Function struct {
	ID         string           `json:"id"`
	ServerID   string           `json:"server_id"`
	OwnerID    string           `json:"owner_id"`
	Paused     bool             `json:"paused"`
	MCPEnabled bool             `json:"mcp_enabled"`
	Revision   int              `json:"revision"`
	Draft      FunctionSpec     `json:"draft"`
	Deployed   *FunctionVersion `json:"deployed,omitempty"`
	NextRunAt  time.Time        `json:"next_run_at"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}
type FunctionVersion struct {
	ID         string       `json:"id"`
	FunctionID string       `json:"function_id"`
	Revision   int          `json:"revision"`
	Spec       FunctionSpec `json:"spec"`
	ImageID    string       `json:"image_id"`
	CodeSHA256 string       `json:"code_sha256"`
	CreatedAt  time.Time    `json:"created_at"`
}
type FunctionRun struct {
	ID            string          `json:"id"`
	FunctionID    string          `json:"function_id"`
	ServerID      string          `json:"server_id"`
	VersionID     string          `json:"version_id"`
	Revision      int             `json:"revision"`
	Kind          string          `json:"kind"`
	Trigger       string          `json:"trigger"`
	ActorID       string          `json:"actor_id"`
	Status        string          `json:"status"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    time.Time       `json:"finished_at"`
	DurationMS    int64           `json:"duration_ms"`
	ExitCode      *int            `json:"exit_code"`
	Logs          string          `json:"logs"`
	LogsTruncated bool            `json:"logs_truncated"`
	Error         string          `json:"error,omitempty"`
	NeedsCleanup  bool            `json:"needs_cleanup"`
	Result        json.RawMessage `json:"result,omitempty"`
}
