package models

import "time"

// ProcessInfo represents system process information
type ProcessInfo struct {
	PID         int32   `json:"pid"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryMB    float64 `json:"memory_mb"`
	Username    string  `json:"username"`
	CreateTime  int64   `json:"create_time"`
	CommandLine string  `json:"command_line"`
}

// ContainerInfo represents Docker container information
type ContainerInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Ports   []string          `json:"ports"`
	Created int64             `json:"created"`
	Labels  map[string]string `json:"labels"`
}

// ContainerStats represents Docker container statistics
type ContainerStats struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	MemoryLimitMB float64 `json:"memory_limit_mb"`
	NetworkRxMB   float64 `json:"network_rx_mb"`
	NetworkTxMB   float64 `json:"network_tx_mb"`
}

// ImageInfo represents Docker image information
type ImageInfo struct {
	ID          string   `json:"id"`
	RepoTags    []string `json:"repo_tags"`
	Size        int64    `json:"size"`
	Created     int64    `json:"created"`
	VirtualSize int64    `json:"virtual_size"`
}

// VolumeInfo represents Docker volume information
type VolumeInfo struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	CreatedAt  string `json:"created_at"`
}

// NetworkInfo represents Docker network information
type NetworkInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Internal   bool   `json:"internal"`
	Attachable bool   `json:"attachable"`
}

// DatabaseHealth represents database health status
type DatabaseHealth struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // "healthy", "unhealthy", "unknown"
	Message   string    `json:"message"`
	Uptime    string    `json:"uptime,omitempty"`
	Version   string    `json:"version,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// SystemStats represents overall system statistics
type SystemStats struct {
	CPU     CPUStats     `json:"cpu"`
	Memory  MemoryStats  `json:"memory"`
	Disk    []DiskStats  `json:"disk"`
	Network NetworkStats `json:"network"`
	Uptime  uint64       `json:"uptime"`
}

// CPUStats represents CPU usage information
type CPUStats struct {
	Percent     float64   `json:"percent"`
	PerCPU      []float64 `json:"per_cpu"`
	CoreCount   int       `json:"core_count"`
	LoadAverage []float64 `json:"load_average"`
}

// MemoryStats represents memory usage information
type MemoryStats struct {
	Total       uint64  `json:"total"`
	Available   uint64  `json:"available"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
	Free        uint64  `json:"free"`
	Cached      uint64  `json:"cached"`
}

// DiskStats represents disk usage information
type DiskStats struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

// NetworkStats represents network I/O statistics
type NetworkStats struct {
	BytesSent   uint64 `json:"bytes_sent"`
	BytesRecv   uint64 `json:"bytes_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
	ErrorsIn    uint64 `json:"errors_in"`
	ErrorsOut   uint64 `json:"errors_out"`
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// LogEntry represents a log line
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// ServiceAction represents an action to perform on a service
type ServiceAction struct {
	Action string `json:"action"` // "start", "stop", "restart"
}

// Server represents a remote VPS instance
type Server struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	User          string   `json:"user"`
	AuthType      string   `json:"auth_type"` // "key" or "password"
	KeyPath       string   `json:"key_path"`
	Password      string   `json:"password"` // Encrypt if possible, or warn user
	Tags          []string `json:"tags"`
	IsDbOnly      bool     `json:"is_db_only"`
	SSHKeyContent string   `json:"ssh_key_content,omitempty"`
}

// ProjectStatus represents the status of a project
type ProjectStatus string

const (
	ProjectStatusStopped  ProjectStatus = "Stopped"
	ProjectStatusRunning  ProjectStatus = "Running"
	ProjectStatusBuilding ProjectStatus = "Building"
	ProjectStatusError    ProjectStatus = "Error"
)

// Project represents a user project (Node.js/Next.js)
type Project struct {
	ID             string            `json:"id"`
	ServerID       string            `json:"server_id"`
	Name           string            `json:"name"`
	Type           string            `json:"type"`        // "nodejs", "nextjs"
	SourceType     string            `json:"source_type"` // "upload", "git"
	SourceURL      string            `json:"source_url"`  // For git
	BuildType      string            `json:"build_type"`  // "docker", "process"
	Port           int               `json:"port"`        // Internal app port
	HostPort       int               `json:"host_port"`   // Exposed port
	Status         ProjectStatus     `json:"status"`
	Path           string            `json:"path"`         // /home/projects/[Type]/[Name]
	ContainerID    string            `json:"container_id"` // If docker
	ProcessID      int               `json:"process_id"`   // If process
	Domain         string            `json:"domain"`       // Future use
	CreatedAt      time.Time         `json:"created_at"`
	StartCommand   string            `json:"start_command"` // e.g. "npm start"
	BuildCommand   string            `json:"build_command"` // e.g. "npm install && npm run build"
	LastBuild      time.Time         `json:"last_build,omitempty"`
	WebhookSecret  string            `json:"webhook_secret,omitempty"` // HMAC secret for GitHub webhook verification
	WebhookEnabled bool              `json:"webhook_enabled"`          // Toggle webhook auto-deploy
	LastDeploy     time.Time         `json:"last_deploy,omitempty"`    // Timestamp of last auto-deploy
	EnvVars        map[string]string `json:"env_vars,omitempty"`       // KEY=VALUE environment variables
	RestartOnBoot  bool              `json:"restart_on_boot"`          // Auto-start project on system boot / server reboot
}

// DeploymentStatus is the durable lifecycle state of a project deployment.
type DeploymentStatus string

const (
	DeploymentStatusQueued    DeploymentStatus = "queued"
	DeploymentStatusRunning   DeploymentStatus = "running"
	DeploymentStatusSucceeded DeploymentStatus = "succeeded"
	DeploymentStatusFailed    DeploymentStatus = "failed"
)

// Deployment records one immutable CI/CD attempt. DeliveryID makes webhook
// processing idempotent when GitHub retries the same delivery.
type Deployment struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"project_id"`
	DeliveryID    string           `json:"delivery_id"`
	Source        string           `json:"source"` // github_webhook, github_actions, or manual
	Repository    string           `json:"repository,omitempty"`
	Ref           string           `json:"ref,omitempty"`
	Branch        string           `json:"branch,omitempty"`
	CommitSHA     string           `json:"commit_sha,omitempty"`
	CommitMessage string           `json:"commit_message,omitempty"`
	Pusher        string           `json:"pusher,omitempty"`
	Status        DeploymentStatus `json:"status"`
	QueuedAt      time.Time        `json:"queued_at"`
	StartedAt     time.Time        `json:"started_at,omitempty"`
	FinishedAt    time.Time        `json:"finished_at,omitempty"`
	Error         string           `json:"error,omitempty"`
}

// ProjectReleaseInfo describes deploy/build history that can be inferred for a project.
type ProjectReleaseInfo struct {
	ProjectID string                `json:"project_id"`
	Git       ProjectGitInfo        `json:"git"`
	Releases  []ProjectReleaseEvent `json:"releases"`
}

// ProjectGitInfo describes the current git state for a project directory.
type ProjectGitInfo struct {
	IsRepository bool   `json:"is_repository"`
	Branch       string `json:"branch,omitempty"`
	Commit       string `json:"commit,omitempty"`
	Message      string `json:"message,omitempty"`
	Remote       string `json:"remote,omitempty"`
}

// ProjectReleaseEvent is a timeline item for project build/deploy/source activity.
type ProjectReleaseEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// ProjectLogInfo contains recent runtime logs for a project.
type ProjectLogInfo struct {
	ProjectID string    `json:"project_id"`
	Source    string    `json:"source"`
	Tail      int       `json:"tail"`
	Raw       string    `json:"raw"`
	Lines     []string  `json:"lines"`
	FetchedAt time.Time `json:"fetched_at"`
}

// ProjectHealthReport contains the latest runtime checks for a project.
type ProjectHealthReport struct {
	ProjectID string               `json:"project_id"`
	Status    string               `json:"status"` // healthy, warning, unhealthy
	CheckedAt time.Time            `json:"checked_at"`
	Checks    []ProjectHealthCheck `json:"checks"`
}

// ProjectHealthCheck is one operational check in the health report.
type ProjectHealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // healthy, warning, unhealthy
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// ProjectAutomationInfo describes automation settings that are currently backed by the project model.
type ProjectAutomationInfo struct {
	ProjectID      string    `json:"project_id"`
	RestartOnBoot  bool      `json:"restart_on_boot"`
	WebhookEnabled bool      `json:"webhook_enabled"`
	WebhookReady   bool      `json:"webhook_ready"`
	WebhookURL     string    `json:"webhook_url"`
	WebhookSecret  string    `json:"webhook_secret,omitempty"`
	RepositoryURL  string    `json:"repository_url,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ProjectAutomationUpdate updates project automation settings.
type ProjectAutomationUpdate struct {
	RestartOnBoot  *bool `json:"restart_on_boot,omitempty"`
	WebhookEnabled *bool `json:"webhook_enabled,omitempty"`
}

// ProjectIntegrationsInfo describes project-adjacent app surfaces that can be opened from the operations dialog.
type ProjectIntegrationsInfo struct {
	ProjectID    string               `json:"project_id"`
	Integrations []ProjectIntegration `json:"integrations"`
}

// ProjectIntegration describes one integration surface for a project.
type ProjectIntegration struct {
	Key     string                 `json:"key"`
	Label   string                 `json:"label"`
	Status  string                 `json:"status"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// StatsSnapshot represents a point-in-time system stats summary for history tracking
type StatsSnapshot struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemPercent  float64   `json:"mem_percent"`
	DiskPercent float64   `json:"disk_percent"`
	NetRxBytes  uint64    `json:"net_rx_bytes"`
	NetTxBytes  uint64    `json:"net_tx_bytes"`
}

// Domain represents a domain/subdomain mapped to a port via Nginx reverse proxy
type Domain struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Domain    string    `json:"domain"` // e.g. "app.example.com"
	Port      int       `json:"port"`   // backend port, e.g. 3000
	SSL       bool      `json:"ssl"`    // future: enable HTTPS via certbot
	CreatedAt time.Time `json:"created_at"`
}

// DatabaseStatus represents the status of a database
type DatabaseStatus string

const (
	DatabaseStatusStopped  DatabaseStatus = "Stopped"
	DatabaseStatusRunning  DatabaseStatus = "Running"
	DatabaseStatusStarting DatabaseStatus = "Starting"
	DatabaseStatusError    DatabaseStatus = "Error"
)

// Database represents a containerized database
type Database struct {
	ID            string         `json:"id"`
	ServerID      string         `json:"server_id"`
	Name          string         `json:"name"`          // Friendly name
	Type          string         `json:"type"`          // "mysql", "postgres", "mongodb"
	DatabaseName  string         `json:"database_name"` // Default database name
	Username      string         `json:"username"`
	Password      string         `json:"password"`
	HostPort      int            `json:"host_port"`    // Port on host
	ContainerID   string         `json:"container_id"` // Docker container ID
	Status        DatabaseStatus `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	AutoBackup    bool           `json:"auto_backup"`    // Enable/disable daily automatic backups
	RetentionDays int            `json:"retention_days"` // Number of days to keep backups (default: 7, 0 = keep forever)
}

// DatabaseBackup represents a database backup dump
type DatabaseBackup struct {
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// TableColumn represents a column definition when creating a table
type TableColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Unique   bool   `json:"unique"`
}

// DatabaseTelemetry represents instant diagnostic performance metrics for a database cluster
type DatabaseTelemetry struct {
	ActiveConnections int   `json:"active_connections"`
	DataSize          int64 `json:"data_size"` // Footprint in bytes
}

// DatabaseUser represents a database user and their privilege scope
type DatabaseUser struct {
	Username string `json:"username"`
	Scope    string `json:"scope"`              // "read", "readWrite", "admin"
	Host     string `json:"host,omitempty"`     // For SQL engines
	Database string `json:"database,omitempty"` // Targeted database scope
}

// LoadBalancer represents a reverse-proxy load balancer on a server
type LoadBalancer struct {
	ID                  string                `json:"id"`
	ServerID            string                `json:"server_id"`
	Name                string                `json:"name"`
	Domain              string                `json:"domain"`
	Port                int                   `json:"port"`
	Algorithm           string                `json:"algorithm"` // "round-robin", "least_conn", "ip_hash"
	SSL                 bool                  `json:"ssl"`
	HealthCheckPath     string                `json:"health_check_path"`
	HealthCheckInterval int                   `json:"health_check_interval"`
	Status              string                `json:"status"` // "active", "error", "configuring"
	ErrorMessage        string                `json:"error_message,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	Backends            []LoadBalancerBackend `json:"backends,omitempty"`
}

// LoadBalancerBackend represents a node destination for a LoadBalancer
type LoadBalancerBackend struct {
	ID              string    `json:"id"`
	LoadBalancerID  string    `json:"load_balancer_id"`
	TargetServerID  string    `json:"target_server_id,omitempty"`
	TargetProjectID string    `json:"target_project_id,omitempty"`
	Host            string    `json:"host"`
	Port            int       `json:"port"`
	Weight          int       `json:"weight"`
	Backup          bool      `json:"backup"`
	Status          string    `json:"status"` // "healthy", "unhealthy", "unknown"
	CreatedAt       time.Time `json:"created_at"`
}

// LoadBalancerLogs represents both deployment and runtime log messages
type LoadBalancerLogs struct {
	LoadBalancerID  string    `json:"load_balancer_id"`
	DeploymentError string    `json:"deployment_error"`
	AccessLogs      string    `json:"access_logs"`
	ErrorLogs       string    `json:"error_logs"`
	FetchedAt       time.Time `json:"fetched_at"`
}
