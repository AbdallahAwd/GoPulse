package models

import "time"

type RemoteBackupPlan struct {
	ID             string           `json:"id"`
	ServerID       string           `json:"server_id"`
	DatabaseID     string           `json:"database_id"`
	DestinationID  string           `json:"destination_id"`
	Directory      string           `json:"directory"`
	Name           string           `json:"name"`
	Schedule       string           `json:"schedule"`
	Timezone       string           `json:"timezone"`
	RetentionDays  int              `json:"retention_days"`
	KeepLast       int              `json:"keep_last"`
	Enabled        bool             `json:"enabled"`
	AutoTest       bool             `json:"auto_test"`
	TimeoutSeconds int              `json:"timeout_seconds"`
	MaxSizeGB      int              `json:"max_size_gb"`
	TestMemoryMB   int              `json:"test_memory_mb"`
	Recipient      string           `json:"recipient"`
	OwnerID        string           `json:"owner_id"`
	NextRunAt      time.Time        `json:"next_run_at"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	LastRun        *RemoteBackupRun `json:"last_run,omitempty"`
}

type RemoteBackupRun struct {
	ID           string    `json:"id"`
	PlanID       string    `json:"plan_id"`
	ServerID     string    `json:"server_id"`
	Kind         string    `json:"kind"`
	Trigger      string    `json:"trigger"`
	Actor        string    `json:"actor"`
	ArtifactID   string    `json:"artifact_id,omitempty"`
	Status       string    `json:"status"`
	Phase        string    `json:"phase"`
	Error        string    `json:"error,omitempty"`
	NeedsCleanup bool      `json:"needs_cleanup"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
}

type RemoteBackupArtifact struct {
	ID            string    `json:"id"`
	PlanID        string    `json:"plan_id"`
	ServerID      string    `json:"server_id"`
	DestinationID string    `json:"destination_id"`
	Path          string    `json:"path"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	Engine        string    `json:"engine"`
	ImageID       string    `json:"image_id,omitempty"`
	DatabaseName  string    `json:"database_name"`
	CreatedAt     time.Time `json:"created_at"`
	TestStatus    string    `json:"test_status"`
	TestError     string    `json:"test_error,omitempty"`
	TestedAt      time.Time `json:"tested_at"`
	TestSummary   string    `json:"test_summary,omitempty"`
	Pruned        bool      `json:"pruned"`
}

// BackupManifest travels inside the encrypted ZIP, making recovery possible even
// when the dashboard's SQLite database has been lost. It contains no credentials.
type BackupManifest struct {
	Version        int       `json:"version"`
	PlanID         string    `json:"plan_id"`
	RunID          string    `json:"run_id"`
	SourceServerID string    `json:"source_server_id"`
	DatabaseID     string    `json:"database_id"`
	Engine         string    `json:"engine"`
	ImageID        string    `json:"image_id,omitempty"`
	DatabaseName   string    `json:"database_name"`
	CreatedAt      time.Time `json:"created_at"`
}
