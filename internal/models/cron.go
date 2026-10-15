package models

import "time"

// CronJob is a GoPulse-managed shell job on a registered server.
type CronJob struct {
	ID             string    `json:"id"`
	ServerID       string    `json:"server_id"`
	Name           string    `json:"name"`
	Schedule       string    `json:"schedule"`
	Timezone       string    `json:"timezone"`
	Command        string    `json:"command"`
	WorkingDir     string    `json:"working_dir"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Enabled        bool      `json:"enabled"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	NextRunAt      time.Time `json:"next_run_at"`
	LastRun        *CronRun  `json:"last_run,omitempty"`
}

// CronRun keeps an immutable command snapshot and a bounded combined output.
type CronRun struct {
	ID              string    `json:"id"`
	JobID           string    `json:"job_id"`
	ServerID        string    `json:"server_id"`
	Trigger         string    `json:"trigger"`
	Actor           string    `json:"actor"`
	Status          string    `json:"status"`
	Command         string    `json:"command"`
	WorkingDir      string    `json:"working_dir"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	DurationMS      int64     `json:"duration_ms"`
	ExitCode        *int      `json:"exit_code"`
	Output          string    `json:"output,omitempty"`
	OutputTruncated bool      `json:"output_truncated"`
	Error           string    `json:"error,omitempty"`
}
