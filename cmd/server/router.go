package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/handlers"
	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/web"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// routerDependencies contains the handlers initialized by main.
type routerDependencies struct {
	Auth              *middleware.AuthMiddleware
	Dashboard         *handlers.DashboardHandler
	Server            *handlers.ServerHandler
	Process           *handlers.ProcessHandler
	Docker            *handlers.DockerHandler
	Stats             *handlers.StatsHandler
	Monitoring        *handlers.MonitoringHandler
	System            *handlers.SystemHandler
	Terminal          *handlers.TerminalHandler
	Cron              *handlers.CronHandler
	Project           *handlers.ProjectHandler
	Webhook           *handlers.WebhookHandler
	GitHub            *handlers.GitHubHandler
	Domain            *handlers.DomainHandler
	Database          *handlers.DatabaseHandler
	RemoteBackup      *handlers.RemoteBackupHandler
	Function          *handlers.FunctionHandler
	Operation         *handlers.OperationHandler
	Qdrant            *handlers.QdrantHandler
	DashboardSettings *handlers.DashboardSettingsHandler
	LoadBalancer      *handlers.LoadBalancerHandler
	Firewall          *handlers.FirewallHandler
	Export            *handlers.ExportHandler
	MCP               *handlers.MCPHandler
	Agent             *handlers.AgentHandler
	File              *handlers.FileHandler
}

// newRouter registers the web and API routes and their middleware.
func newRouter(deps routerDependencies) (chi.Router, error) {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.RecoveryMiddleware)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.OriginGuard)
	r.Use(middleware.NewRateLimiter(600, time.Minute).APIOnlyHandler)
	r.Use(deps.Auth.Handler)
	r.Use(middleware.PermissionGuard)
	r.Use(func(next http.Handler) http.Handler {
		timed := chiMiddleware.Timeout(60 * time.Second)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/servers/") && strings.Contains(r.URL.Path, "/remote-backups/") && strings.HasSuffix(r.URL.Path, "/download") {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	})

	// Static Files (Embedded)
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		return nil, fmt.Errorf("create static file system: %w", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Web Routes
	r.Get("/login", deps.Dashboard.Login)
	r.Get("/register", deps.Dashboard.Register)
	r.Get("/logout", deps.Auth.LogoutHandler)
	r.Get("/", deps.Dashboard.Index)
	r.Get("/servers", deps.Dashboard.Servers)
	r.Get("/applications", deps.Dashboard.Workspace)
	r.Get("/databases", deps.Dashboard.Workspace)
	r.Get("/networking", deps.Dashboard.Workspace)
	r.Get("/activity", deps.Dashboard.Workspace)
	r.Get("/settings", deps.Dashboard.Workspace)
	r.Get("/agents", deps.Dashboard.Workspace)
	r.Get(services.DashboardIdentityPath, deps.DashboardSettings.Identity)
	r.With(deps.Auth.RequireServerAccess).Get("/servers/{serverID}/dashboard", deps.Dashboard.ServerDashboard)
	r.With(deps.Auth.RequirePermission("user:manage")).Get("/users", deps.Dashboard.Users)

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// HTML and embedded assets never consume the API budget. A page reload
		// must be able to recover even when a client has exhausted its API quota.
		r.Use(middleware.NewRateLimiter(240, time.Minute).APIHandler)
		authLimiter := middleware.NewRateLimiter(10, time.Minute)

		r.Post("/function-triggers/{serverID}/{functionID}", deps.Function.Trigger)
		r.Get("/operations", deps.Operation.List)
		r.Get("/operations/{operationID}", deps.Operation.Get)
		r.Post("/operations/{operationID}/approve", deps.Operation.Approve)
		r.Post("/operations/{operationID}/reject", deps.Operation.Reject)
		r.Handle("/mcp", deps.MCP)
		r.Route("/agents", deps.Agent.Routes)
		r.Route("/settings/dashboard", deps.DashboardSettings.Routes)
		r.Post("/agent-token", deps.Auth.AgentTokenHandler)
		// Auth Endpoints
		r.With(authLimiter.Handler).Post("/auth/login", deps.Auth.LoginHandler)
		r.With(authLimiter.Handler).Post("/auth/register", deps.Auth.RegisterHandler)
		r.Get("/auth/has-users", deps.Auth.CheckUsersHandler)

		// User Management API
		r.Route("/users", func(r chi.Router) {
			r.Use(deps.Auth.RequirePermission("user:manage"))
			r.Get("/", deps.Auth.ListUsersHandler)
			r.Post("/", deps.Auth.CreateUserHandler)
			r.Delete("/{id}", deps.Auth.DeleteUserHandler)
		})

		// Server Management
		r.Route("/servers", func(r chi.Router) {
			r.Get("/", deps.Server.ListServers)
			r.Get("/inventory", deps.Server.Inventory)
			r.Post("/", deps.Server.AddServer)
			r.Post("/upload-key", deps.Server.UploadKey)
			r.Post("/ssh-host-key", deps.Server.ProbeSSHHostKey)

			// Per-Server Stats & Operations
			r.Route("/{serverID}", func(r chi.Router) {
				r.Use(deps.Auth.RequireServerAccess)
				r.Delete("/", deps.Server.RemoveServer)
				r.Get("/", deps.Server.GetServer)
				r.Put("/", deps.Server.UpdateServer)
				r.Post("/ssh-host-key", deps.Server.ProbeSSHHostKey)
				r.Put("/ssh-host-key", deps.Server.TrustSSHHostKey)
				r.With(deps.Auth.RequirePermission("terminal:write")).Get("/terminal", deps.Terminal.HandleTerminal)
				r.With(deps.Auth.RequireServerAccess).Get("/stats/history", deps.Stats.GetStatsHistory)

				// Monitoring rules, incident history and webhook delivery.
				r.Route("/alerts", deps.Monitoring.Routes)
				r.Route("/remote-backups", deps.RemoteBackup.Routes)
				r.Route("/functions", deps.Function.Routes)

				// Cron jobs
				r.Route("/cron", deps.Cron.Routes)

				// Processes
				r.Route("/processes", func(r chi.Router) {
					r.Get("/", deps.Process.ListProcesses)
					r.Get("/{pid}", deps.Process.GetProcess)
					r.Delete("/{pid}", deps.Process.KillProcess)
				})

				// files
				r.Route("/files", func(r chi.Router) {
					r.Get("/list", deps.File.ListFiles)
					r.Get("/content", deps.File.DownloadFile)
					r.Post("/save", deps.File.SaveFile)
					r.Post("/upload", deps.File.UploadFile)
					r.Post("/action", deps.File.PerformAction)
					r.Post("/exec", deps.File.ExecuteCommand)
				})

				// Services (Systemd)
				r.Route("/services", func(r chi.Router) {
					r.Post("/{name}/restart", deps.Process.RestartService)
					r.Get("/{name}/status", deps.Process.GetServiceStatus)
				})

				// Docker
				r.Route("/containers", func(r chi.Router) {
					r.Get("/", deps.Docker.ListContainers)
					r.Get("/{id}", deps.Docker.GetContainer)
					r.Get("/{id}/stats", deps.Docker.GetContainerStats)
					r.Get("/{id}/logs", deps.Docker.GetContainerLogs)
					r.Post("/{id}/start", deps.Docker.StartContainer)
					r.Post("/{id}/stop", deps.Docker.StopContainer)
					r.Post("/{id}/restart", deps.Docker.RestartContainer)
					r.Delete("/{id}", deps.Docker.RemoveContainer)
				})

				// Images
				r.Route("/images", func(r chi.Router) {
					r.Get("/", deps.Docker.ListImages)
					r.Post("/pull", deps.Docker.PullImage)
					r.Post("/run", deps.Docker.RunImage) // Added RunImage route
					r.Delete("/{id}", deps.Docker.RemoveImage)
					r.Post("/build", deps.Docker.BuildImage) // Added BuildImage route
				})

				// Volumes
				r.Route("/volumes", func(r chi.Router) {
					r.Get("/", deps.Docker.ListVolumes)
					r.Delete("/{name}", deps.Docker.RemoveVolume)
				})

				// Networks
				r.Route("/networks", func(r chi.Router) {
					r.Get("/", deps.Docker.ListNetworks)
					r.Delete("/{id}", deps.Docker.RemoveNetwork)
				})

				// Compose
				r.Route("/compose", func(r chi.Router) {
					r.Post("/build", deps.Docker.BuildCompose)
				})

				// Projects
				r.Route("/projects", func(r chi.Router) {
					r.Get("/", deps.Project.ListProjects)
					r.Post("/", deps.Project.CreateProject)
					r.Post("/upload", deps.Project.UploadProject)

					r.Route("/{id}", func(r chi.Router) {
						r.Use(deps.Project.RequireOwnership)
						r.Get("/", deps.Project.GetProject)
						r.Put("/", deps.Project.UpdateProject) // Added Update route
						r.Post("/setup", deps.Project.SetupProject)
						r.Get("/detect", deps.Project.DetectStrategy)
						r.Post("/build", deps.Project.BuildProject)
						r.Post("/start", deps.Project.StartProject) // Fixed method name reference
						r.Post("/stop", deps.Project.StopProject)
						r.Post("/redeploy", deps.Project.RedeployProject)
						r.Get("/deployments", deps.Project.ListDeployments)
						r.Get("/deployments/{deploymentID}", deps.Project.GetDeployment)
						r.Delete("/", deps.Project.DeleteProject)
						r.Post("/webhook/enable", deps.Project.EnableWebhook)
						r.Post("/webhook/disable", deps.Project.DisableWebhook)
						r.Get("/env", deps.Project.GetEnvVars)
						r.Put("/env", deps.Project.UpdateEnvVars)
						r.Get("/ops/releases", deps.Project.GetReleases)
						r.Get("/ops/logs", deps.Project.GetLogs)
						r.Get("/ops/health", deps.Project.GetHealth)
						r.Get("/ops/automation", deps.Project.GetAutomation)
						r.Put("/ops/automation", deps.Project.UpdateAutomation)
						r.Get("/ops/integrations", deps.Project.GetIntegrations)
					})
				})

				// Remote System Operations
				r.Route("/system", func(r chi.Router) {
					r.Post("/restart", deps.System.RestartRemote)
					r.Post("/shutdown", deps.System.ShutdownRemote)
					r.Get("/check/{software}", deps.System.CheckSoftware)
					r.Post("/install/{software}", deps.System.InstallSoftware)

					// Linux User Management Endpoints
					r.Get("/users", deps.System.ListLinuxUsers)
					r.Post("/users", deps.System.CreateLinuxUser)
					r.Delete("/users/{username}", deps.System.DeleteLinuxUser)
				})

				// Domains
				r.Route("/domains", func(r chi.Router) {
					r.Get("/", deps.Domain.ListDomains)
					r.Post("/", deps.Domain.AddDomain)
					r.Delete("/{id}", deps.Domain.RemoveDomain)
					r.Get("/nginx/status", deps.Domain.NginxStatus)
				})

				// Load Balancers
				r.Route("/load-balancers", func(r chi.Router) {
					owned := r.With(deps.LoadBalancer.RequireOwnership)
					owned.Get("/", deps.LoadBalancer.ListLoadBalancers)
					owned.Post("/", deps.LoadBalancer.AddLoadBalancer)
					owned.Get("/{id}", deps.LoadBalancer.GetLoadBalancer)
					owned.Get("/{id}/logs", deps.LoadBalancer.GetLogs)
					owned.Put("/{id}", deps.LoadBalancer.UpdateLoadBalancer)
					owned.Post("/{id}/deploy", deps.LoadBalancer.Retry)
					owned.Delete("/{id}", deps.LoadBalancer.RemoveLoadBalancer)
					owned.Post("/{id}/backends", deps.LoadBalancer.AddBackend)
					owned.Delete("/{id}/backends/{backendID}", deps.LoadBalancer.RemoveBackend)
				})

				// Databases
				r.Route("/databases", func(r chi.Router) {
					owned := r.With(deps.Database.RequireOwnership)
					owned.Post("/qdrant/install", deps.Qdrant.Install)
					owned.Get("/{dbID}/qdrant/collections", deps.Qdrant.Collections)
					owned.Post("/{dbID}/qdrant/collections", deps.Qdrant.CreateCollection)
					owned.Get("/{dbID}/qdrant/collections/{collection}", deps.Qdrant.Collection)
					owned.Delete("/{dbID}/qdrant/collections/{collection}", deps.Qdrant.DeleteCollection)
					owned.Get("/{dbID}/qdrant/diagnostics", deps.Qdrant.Diagnostics)
					owned.Get("/{dbID}/qdrant/config", deps.Qdrant.Configuration)
					owned.Put("/{dbID}/qdrant/network", deps.Qdrant.ConfigureNetwork)
					owned.Get("/{dbID}/qdrant/collections/{collection}/snapshots", deps.Qdrant.Snapshots)
					owned.Post("/{dbID}/qdrant/collections/{collection}/snapshots", deps.Qdrant.Snapshots)
					owned.Post("/{dbID}/qdrant/collections/{collection}/snapshots/restore", deps.Qdrant.RestoreSnapshot)
					owned.Get("/", deps.Database.ListDatabases)
					owned.Post("/", deps.Database.ProvisionDatabase)
					owned.Get("/{dbID}", deps.Database.GetDatabase)
					owned.With(deps.Auth.RequirePermission("database:write")).Get("/{dbID}/credentials", deps.Database.Credentials)
					owned.Post("/{dbID}/start", deps.Database.StartDatabase)
					owned.Post("/{dbID}/stop", deps.Database.StopDatabase)
					owned.Post("/{dbID}/restart", deps.Database.RestartDatabase)
					owned.Delete("/{dbID}", deps.Database.DeleteDatabase)
					owned.Post("/{dbID}/query", deps.Database.ExecuteQuery)
					owned.Post("/{dbID}/backup", deps.Database.CreateBackup)
					owned.Get("/{dbID}/backups", deps.Database.ListBackups)
					owned.Post("/{dbID}/backups/restore", deps.Database.RestoreBackup)
					owned.Post("/{dbID}/backups/import", deps.Database.ImportBackup)
					owned.Get("/{dbID}/backups/download", deps.Database.DownloadBackup)
					owned.Get("/{dbID}/auto-backup", deps.Database.GetAutoBackupConfig)
					owned.Put("/{dbID}/auto-backup", deps.Database.SetAutoBackupConfig)
					owned.Get("/{dbID}/telemetry", deps.Database.GetDatabaseTelemetry)

					// User management endpoints
					owned.Get("/{dbID}/users", deps.Database.ListDatabaseUsers)
					owned.Post("/{dbID}/users", deps.Database.CreateDatabaseUser)
					owned.Delete("/{dbID}/users/{username}", deps.Database.DeleteDatabaseUser)

					// Explorer endpoints
					owned.Get("/{dbID}/schemas", deps.Database.ListSchemas)
					owned.Post("/{dbID}/schemas", deps.Database.CreateSchema)
					owned.Delete("/{dbID}/schemas/{schema}", deps.Database.DeleteSchema)
					owned.Get("/{dbID}/schemas/{schema}/tables", deps.Database.ListTables)
					owned.Post("/{dbID}/schemas/{schema}/tables", deps.Database.CreateTable)
					owned.Delete("/{dbID}/schemas/{schema}/tables/{table}", deps.Database.DeleteTable)
					owned.Get("/{dbID}/schemas/{schema}/tables/{table}/columns", deps.Database.DescribeTable)
					owned.Get("/{dbID}/schemas/{schema}/tables/{table}/rows", deps.Database.BrowseTableData)
					owned.Post("/{dbID}/schemas/{schema}/tables/{table}/rows", deps.Database.InsertRow)
					owned.Put("/{dbID}/schemas/{schema}/tables/{table}/rows", deps.Database.UpdateRow)
					owned.Delete("/{dbID}/schemas/{schema}/tables/{table}/rows", deps.Database.DeleteRow)
				})

				// Firewall
				r.Route("/firewall", func(r chi.Router) {
					r.Get("/", deps.Firewall.GetFirewall)
					r.Post("/enable", deps.Firewall.EnableFirewall)
					r.Post("/disable", deps.Firewall.DisableFirewall)
					r.Post("/reload", deps.Firewall.ReloadFirewall)
					r.Post("/reset", deps.Firewall.ResetFirewall)
					r.Post("/policies", deps.Firewall.ChangePolicies)
					r.Post("/install", deps.Firewall.InstallUFW)
					r.Post("/rules", deps.Firewall.AddRule)
					r.Put("/rules/{ruleId}", deps.Firewall.UpdateRule)
					r.Delete("/rules/{ruleId}", deps.Firewall.DeleteRule)
					r.Post("/rules/bulk", deps.Firewall.BulkAction)
					r.Get("/backups", deps.Firewall.ListBackups)
					r.Post("/backups", deps.Firewall.CreateBackup)
					r.Post("/backups/{backupId}/restore", deps.Firewall.RestoreBackup)
					r.Delete("/backups/{backupId}", deps.Firewall.DeleteBackup)
					r.Get("/schedules", deps.Firewall.ListSchedules)
					r.Post("/schedules", deps.Firewall.CreateSchedule)
					r.Delete("/schedules/{scheduleId}", deps.Firewall.CancelSchedule)
					r.Get("/logs", deps.Firewall.GetLogs)
				})

			})
		})

		// GitHub API routes
		r.Route("/github", func(r chi.Router) {
			r.Get("/login", deps.GitHub.Login)
			r.Get("/callback", deps.GitHub.Callback)
			r.Get("/repos", deps.GitHub.ListRepos)
			r.Post("/clone", deps.GitHub.CloneRepo)
			r.Post("/connect", deps.GitHub.ConnectRepo)
		})

		// Global Stats WebSocket (broadcasts updates for all servers)
		r.Get("/stats/ws", deps.Stats.WebSocketHandler)

		// System Operations
		r.Route("/system", func(r chi.Router) {
			r.Use(deps.Auth.RequirePermission("*")) // These actions control the dashboard host itself.
			r.Post("/restart", deps.System.Restart)
			r.Post("/shutdown", deps.System.Shutdown)
		})

		// GitHub Webhook Receiver (CI/CD)
		r.Post("/webhooks/github/{projectID}", deps.Webhook.HandleGitHubWebhook)

		// Global Stats Snapshot
		r.With(deps.Auth.RequireServerAccess).Get("/stats", deps.Stats.GetStats)

		// Global Stats History
		r.With(deps.Auth.RequireServerAccess).Get("/stats/history", deps.Stats.GetStatsHistory)

		// Export/Import (admin only)
		r.Route("/export-import", func(r chi.Router) {
			r.Use(deps.Auth.RequirePermission("*"))
			r.Get("/export", deps.Export.Export)
			r.Get("/export/preview", deps.Export.ExportPreview)
			r.Post("/import", deps.Export.Import)
		})

	})

	deps.MCP.API = r

	// GitHub OAuth callback at top level (matches config.yaml callback_url)
	r.Get("/auth/github/login", deps.GitHub.Login)
	r.Get("/auth/github/callback", deps.GitHub.Callback)

	return r, nil
}
