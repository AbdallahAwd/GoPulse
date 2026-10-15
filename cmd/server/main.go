package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/amaa/gopulse/internal/config"
	"github.com/amaa/gopulse/internal/handlers"
	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/monitor"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
	"github.com/amaa/gopulse/web"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting GoPulse VPS Dashboard on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Initialize Server Store
	dataDir := os.Getenv("GOPULSE_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(os.Getenv("HOME"), ".gopulse")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	serverStore, err := store.NewServerStore(filepath.Join(dataDir, "servers.json"))
	if err != nil {
		log.Fatalf("Failed to initialize server store: %v", err)
	}

	// Initialize User Store
	userStore, err := store.NewUserStore(filepath.Join(dataDir, "users.json"))
	if err != nil {
		log.Fatalf("Failed to initialize user store: %v", err)
	}

	agentStore, err := store.NewAgentStore(filepath.Join(dataDir, "agents.json"))
	if err != nil {
		log.Fatalf("Failed to initialize agent credentials: %v", err)
	}

	// Initialize Auth Middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg.Auth.JWTSecret, userStore)
	authMiddleware.SetAgentStore(agentStore)

	// Initialize Service Manager
	serviceManager := services.NewServiceManager(serverStore)
	defer serviceManager.CloseAll()

	// Initialize Stats History (in-memory ring buffer, 24 hours at 1-minute interval = 1440 entries)
	statsHistory := monitor.NewStatsHistory(1440)

	// Initialize Stats Collector
	factory := func(server models.Server) (services.StatsProvider, error) {
		return serviceManager.GetStatsProvider(server.ID)
	}
	collector := monitor.NewCollector(serverStore, factory, cfg.Monitoring.PollInterval, statsHistory)

	// Initialize Handlers
	dashboardHandler := handlers.NewDashboardHandler(web.Assets, serverStore)
	serverHandler := handlers.NewServerHandler(serverStore)
	processHandler := handlers.NewProcessHandler(serviceManager)
	dockerHandler := handlers.NewDockerHandler(serviceManager)
	statsHandler := handlers.NewStatsHandler(collector)
	systemHandler := handlers.NewSystemHandler(serviceManager)
	terminalHandler := handlers.NewTerminalHandler(serviceManager)

	// Initialize Cron Jobs (persisted schedules, bounded execution, isolated SSH).
	cronStore, err := store.NewCronStore(filepath.Join(dataDir, "cron.json"))
	if err != nil {
		log.Fatalf("Failed to initialize cron store: %v", err)
	}
	cronService, err := services.NewCronService(cronStore, &services.ServerCronExecutor{Servers: serverStore})
	if err != nil {
		log.Fatalf("Failed to initialize cron scheduler: %v", err)
	}
	defer cronService.Close()
	cronHandler := &handlers.CronHandler{Service: cronService, Servers: serverStore}

	// Initialize Project Components
	projectStore, err := store.NewProjectStore(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		log.Fatalf("Failed to initialize project store: %v", err)
	}
	projectService := services.NewProjectService(projectStore, serviceManager, filepath.Join(dataDir, "projects"))
	deploymentStore, err := store.NewDeploymentStore(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		log.Fatalf("Failed to initialize deployment store: %v", err)
	}
	projectService.SetDeploymentStore(deploymentStore)
	deploymentManager := services.NewDeploymentManager(projectService, deploymentStore)
	defer deploymentManager.Close()
	projectHandler := handlers.NewProjectHandler(projectService, deploymentManager)
	webhookHandler := handlers.NewWebhookHandler(projectService, deploymentManager)
	githubHandler := handlers.NewGitHubHandler(cfg.GitHub, projectService)

	// Initialize Domain Components
	domainStore, err := store.NewDomainStore(filepath.Join(dataDir, "domains.json"))
	if err != nil {
		log.Fatalf("Failed to initialize domain store: %v", err)
	}
	projectService.SetDomainStore(domainStore)
	domainService := services.NewDomainService(domainStore, serviceManager)
	domainHandler := handlers.NewDomainHandler(domainService)

	// Initialize Database Components
	databaseStore, err := store.NewDatabaseStore(filepath.Join(dataDir, "databases.json"))
	if err != nil {
		log.Fatalf("Failed to initialize database store: %v", err)
	}
	databaseService := services.NewDatabaseService(databaseStore, serviceManager)
	databaseHandler := handlers.NewDatabaseHandler(databaseService, serviceManager)

	operationStore, err := store.NewOperationStore(filepath.Join(dataDir, "operations.json"))
	if err != nil {
		log.Fatal(err)
	}
	operations, err := services.NewOperations(operationStore)
	if err != nil {
		log.Fatal(err)
	}
	defer operations.Close()
	operationHandler := &handlers.OperationHandler{Operations: operations}
	qdrantHandler := &handlers.QdrantHandler{Service: databaseService, Operations: operations}
	dashboardSettingsStore, err := store.NewDashboardSettingsStore(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		log.Fatalf("Failed to initialize dashboard settings: %v", err)
	}
	dashboardDomainService, err := services.NewDashboardDomainService(dashboardSettingsStore, operations, cfg.Server.Host, cfg.Server.Port)
	if err != nil {
		log.Fatalf("Failed to initialize dashboard domain service: %v", err)
	}
	dashboardSettingsHandler := &handlers.DashboardSettingsHandler{Service: dashboardDomainService}
	authMiddleware.SetSecureCookieHost(func(host string) bool { return dashboardDomainService.PublicURL() == "https://"+host })

	// Initialize Load Balancer Components
	loadBalancerStore, err := store.NewLoadBalancerStore(filepath.Join(dataDir, "load_balancers.json"))
	if err != nil {
		log.Fatalf("Failed to initialize load balancer store: %v", err)
	}
	loadBalancerService := services.NewLoadBalancerService(loadBalancerStore, serverStore, projectStore, serviceManager)
	loadBalancerHandler := handlers.NewLoadBalancerHandler(loadBalancerService)
	loadBalancerHandler.SetOperations(operations)

	// Initialize Firewall Components
	firewallStore, err := store.NewFirewallStore(filepath.Join(dataDir, "firewalls.json"))
	if err != nil {
		log.Fatalf("Failed to initialize firewall store: %v", err)
	}
	firewallService := services.NewFirewallService(firewallStore, serviceManager)
	firewallHandler := handlers.NewFirewallHandler(firewallService, serverStore, serviceManager)

	// Initialize Export/Import Components
	exportStore := store.NewExportStore(serverStore, projectStore, databaseStore, domainStore, userStore, loadBalancerStore)
	exportStore.CronJobs = cronStore
	exportStore.ImportCronJob = cronService.ImportJob
	exportHandler := handlers.NewExportHandler(exportStore)

	// Start Collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Start(ctx)
	go cronService.Start(ctx)

	// Start Backup Scheduler
	backupScheduler := services.NewBackupScheduler(databaseService, databaseStore)
	go backupScheduler.Start(ctx)

	// Start Firewall Scheduler
	go firewallService.StartScheduler(ctx, serverStore)

	// Start Load Balancer Health Checker
	go loadBalancerService.StartHealthChecker(ctx)

	// Auto-start marked projects in background after a short delay (to let SSH clients connect)
	go func() {
		time.Sleep(5 * time.Second)
		projectService.AutoStartProjects(ctx)
	}()

	mcpHandler := &handlers.MCPHandler{Servers: serverStore, Databases: databaseService, LoadBalancers: loadBalancerService, Manager: serviceManager, Collector: collector, Operations: operations, Auth: authMiddleware}

	// Setup Router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.RecoveryMiddleware)
	r.Use(middleware.OriginGuard)
	r.Use(middleware.NewRateLimiter(600, time.Minute).APIOnlyHandler)
	r.Use(authMiddleware.Handler)
	r.Use(middleware.PermissionGuard)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// Static Files (Embedded)
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Web Routes
	r.Get("/login", dashboardHandler.Login)
	r.Get("/register", dashboardHandler.Register)
	r.Get("/logout", authMiddleware.LogoutHandler)
	r.Get("/", dashboardHandler.Index)
	r.Get("/servers", dashboardHandler.Servers)
	r.Get("/applications", dashboardHandler.Workspace)
	r.Get("/databases", dashboardHandler.Workspace)
	r.Get("/networking", dashboardHandler.Workspace)
	r.Get("/activity", dashboardHandler.Workspace)
	r.Get("/settings", dashboardHandler.Workspace)
	r.Get("/agents", dashboardHandler.Workspace)
	r.Get(services.DashboardIdentityPath, dashboardSettingsHandler.Identity)
	r.With(authMiddleware.RequireServerAccess).Get("/servers/{serverID}/dashboard", dashboardHandler.ServerDashboard)
	r.With(authMiddleware.RequirePermission("user:manage")).Get("/users", dashboardHandler.Users)

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// HTML and embedded assets never consume the API budget. A page reload
		// must be able to recover even when a client has exhausted its API quota.
		r.Use(middleware.NewRateLimiter(240, time.Minute).APIHandler)
		authLimiter := middleware.NewRateLimiter(10, time.Minute)

		r.Get("/operations", operationHandler.List)
		r.Get("/operations/{operationID}", operationHandler.Get)
		r.Post("/operations/{operationID}/approve", operationHandler.Approve)
		r.Post("/operations/{operationID}/reject", operationHandler.Reject)
		r.Handle("/mcp", mcpHandler)
		r.Route("/agents", (&handlers.AgentHandler{Store: agentStore, Servers: serverStore, MCP: mcpHandler, PublicURL: dashboardDomainService.PublicURL}).Routes)
		r.Route("/settings/dashboard", dashboardSettingsHandler.Routes)
		r.Post("/agent-token", authMiddleware.AgentTokenHandler)
		// Auth Endpoints
		r.With(authLimiter.Handler).Post("/auth/login", authMiddleware.LoginHandler)
		r.With(authLimiter.Handler).Post("/auth/register", authMiddleware.RegisterHandler)
		r.Get("/auth/has-users", authMiddleware.CheckUsersHandler)

		// User Management API
		r.Route("/users", func(r chi.Router) {
			r.Use(authMiddleware.RequirePermission("user:manage"))
			r.Get("/", authMiddleware.ListUsersHandler)
			r.Post("/", authMiddleware.CreateUserHandler)
			r.Delete("/{id}", authMiddleware.DeleteUserHandler)
		})

		// Server Management
		r.Route("/servers", func(r chi.Router) {
			r.Get("/", serverHandler.ListServers)
			r.Get("/inventory", serverHandler.Inventory)
			r.Post("/", serverHandler.AddServer)
			r.Post("/upload-key", serverHandler.UploadKey)

			// Per-Server Stats & Operations
			r.Route("/{serverID}", func(r chi.Router) {
				r.Use(authMiddleware.RequireServerAccess)
				r.Delete("/", serverHandler.RemoveServer)
				r.Get("/", serverHandler.GetServer)
				r.Put("/", serverHandler.UpdateServer)
				r.With(authMiddleware.RequirePermission("terminal:write")).Get("/terminal", terminalHandler.HandleTerminal)
				r.With(authMiddleware.RequireServerAccess).Get("/stats/history", statsHandler.GetStatsHistory)

				// Cron jobs
				r.Route("/cron", cronHandler.Routes)

				// Processes
				r.Route("/processes", func(r chi.Router) {
					r.Get("/", processHandler.ListProcesses)
					r.Get("/{pid}", processHandler.GetProcess)
					r.Delete("/{pid}", processHandler.KillProcess)
				})

				// files
				r.Route("/files", func(r chi.Router) {
					fileHandler := handlers.NewFileHandler(serviceManager)
					r.Get("/list", fileHandler.ListFiles)
					r.Get("/content", fileHandler.DownloadFile)
					r.Post("/save", fileHandler.SaveFile)
					r.Post("/upload", fileHandler.UploadFile)
					r.Post("/action", fileHandler.PerformAction)
					r.Post("/exec", fileHandler.ExecuteCommand)
				})

				// Services (Systemd)
				r.Route("/services", func(r chi.Router) {
					r.Post("/{name}/restart", processHandler.RestartService)
					r.Get("/{name}/status", processHandler.GetServiceStatus)
				})

				// Docker
				r.Route("/containers", func(r chi.Router) {
					r.Get("/", dockerHandler.ListContainers)
					r.Get("/{id}", dockerHandler.GetContainer)
					r.Get("/{id}/stats", dockerHandler.GetContainerStats)
					r.Get("/{id}/logs", dockerHandler.GetContainerLogs)
					r.Post("/{id}/start", dockerHandler.StartContainer)
					r.Post("/{id}/stop", dockerHandler.StopContainer)
					r.Post("/{id}/restart", dockerHandler.RestartContainer)
					r.Delete("/{id}", dockerHandler.RemoveContainer)
				})

				// Images
				r.Route("/images", func(r chi.Router) {
					r.Get("/", dockerHandler.ListImages)
					r.Post("/pull", dockerHandler.PullImage)
					r.Post("/run", dockerHandler.RunImage) // Added RunImage route
					r.Delete("/{id}", dockerHandler.RemoveImage)
					r.Post("/build", dockerHandler.BuildImage) // Added BuildImage route
				})

				// Volumes
				r.Route("/volumes", func(r chi.Router) {
					r.Get("/", dockerHandler.ListVolumes)
					r.Delete("/{name}", dockerHandler.RemoveVolume)
				})

				// Networks
				r.Route("/networks", func(r chi.Router) {
					r.Get("/", dockerHandler.ListNetworks)
					r.Delete("/{id}", dockerHandler.RemoveNetwork)
				})

				// Compose
				r.Route("/compose", func(r chi.Router) {
					r.Post("/build", dockerHandler.BuildCompose)
				})

				// Projects
				r.Route("/projects", func(r chi.Router) {
					r.Get("/", projectHandler.ListProjects)
					r.Post("/", projectHandler.CreateProject)
					r.Post("/upload", projectHandler.UploadProject)

					r.Route("/{id}", func(r chi.Router) {
						r.Use(projectHandler.RequireOwnership)
						r.Get("/", projectHandler.GetProject)
						r.Put("/", projectHandler.UpdateProject) // Added Update route
						r.Post("/setup", projectHandler.SetupProject)
						r.Get("/detect", projectHandler.DetectStrategy)
						r.Post("/build", projectHandler.BuildProject)
						r.Post("/start", projectHandler.StartProject) // Fixed method name reference
						r.Post("/stop", projectHandler.StopProject)
						r.Post("/redeploy", projectHandler.RedeployProject)
						r.Get("/deployments", projectHandler.ListDeployments)
						r.Get("/deployments/{deploymentID}", projectHandler.GetDeployment)
						r.Delete("/", projectHandler.DeleteProject)
						r.Post("/webhook/enable", projectHandler.EnableWebhook)
						r.Post("/webhook/disable", projectHandler.DisableWebhook)
						r.Get("/env", projectHandler.GetEnvVars)
						r.Put("/env", projectHandler.UpdateEnvVars)
						r.Get("/ops/releases", projectHandler.GetReleases)
						r.Get("/ops/logs", projectHandler.GetLogs)
						r.Get("/ops/health", projectHandler.GetHealth)
						r.Get("/ops/automation", projectHandler.GetAutomation)
						r.Put("/ops/automation", projectHandler.UpdateAutomation)
						r.Get("/ops/integrations", projectHandler.GetIntegrations)
					})
				})

				// Remote System Operations
				r.Route("/system", func(r chi.Router) {
					r.Post("/restart", systemHandler.RestartRemote)
					r.Post("/shutdown", systemHandler.ShutdownRemote)
					r.Get("/check/{software}", systemHandler.CheckSoftware)
					r.Post("/install/{software}", systemHandler.InstallSoftware)

					// Linux User Management Endpoints
					r.Get("/users", systemHandler.ListLinuxUsers)
					r.Post("/users", systemHandler.CreateLinuxUser)
					r.Delete("/users/{username}", systemHandler.DeleteLinuxUser)
				})

				// Domains
				r.Route("/domains", func(r chi.Router) {
					r.Get("/", domainHandler.ListDomains)
					r.Post("/", domainHandler.AddDomain)
					r.Delete("/{id}", domainHandler.RemoveDomain)
					r.Get("/nginx/status", domainHandler.NginxStatus)
				})

				// Load Balancers
				r.Route("/load-balancers", func(r chi.Router) {
					owned := r.With(loadBalancerHandler.RequireOwnership)
					owned.Get("/", loadBalancerHandler.ListLoadBalancers)
					owned.Post("/", loadBalancerHandler.AddLoadBalancer)
					owned.Get("/{id}", loadBalancerHandler.GetLoadBalancer)
					owned.Get("/{id}/logs", loadBalancerHandler.GetLogs)
					owned.Put("/{id}", loadBalancerHandler.UpdateLoadBalancer)
					owned.Post("/{id}/deploy", loadBalancerHandler.Retry)
					owned.Delete("/{id}", loadBalancerHandler.RemoveLoadBalancer)
					owned.Post("/{id}/backends", loadBalancerHandler.AddBackend)
					owned.Delete("/{id}/backends/{backendID}", loadBalancerHandler.RemoveBackend)
				})

				// Databases
				r.Route("/databases", func(r chi.Router) {
					owned := r.With(databaseHandler.RequireOwnership)
					owned.Post("/qdrant/install", qdrantHandler.Install)
					owned.Get("/{dbID}/qdrant/collections", qdrantHandler.Collections)
					owned.Post("/{dbID}/qdrant/collections", qdrantHandler.CreateCollection)
					owned.Get("/{dbID}/qdrant/collections/{collection}", qdrantHandler.Collection)
					owned.Delete("/{dbID}/qdrant/collections/{collection}", qdrantHandler.DeleteCollection)
					owned.Get("/{dbID}/qdrant/diagnostics", qdrantHandler.Diagnostics)
					owned.Get("/{dbID}/qdrant/config", qdrantHandler.Configuration)
					owned.Get("/{dbID}/qdrant/collections/{collection}/snapshots", qdrantHandler.Snapshots)
					owned.Post("/{dbID}/qdrant/collections/{collection}/snapshots", qdrantHandler.Snapshots)
					owned.Post("/{dbID}/qdrant/collections/{collection}/snapshots/restore", qdrantHandler.RestoreSnapshot)
					owned.Get("/", databaseHandler.ListDatabases)
					owned.Post("/", databaseHandler.ProvisionDatabase)
					owned.Get("/{dbID}", databaseHandler.GetDatabase)
					owned.With(authMiddleware.RequirePermission("database:write")).Get("/{dbID}/credentials", databaseHandler.Credentials)
					owned.Post("/{dbID}/start", databaseHandler.StartDatabase)
					owned.Post("/{dbID}/stop", databaseHandler.StopDatabase)
					owned.Post("/{dbID}/restart", databaseHandler.RestartDatabase)
					owned.Delete("/{dbID}", databaseHandler.DeleteDatabase)
					owned.Post("/{dbID}/query", databaseHandler.ExecuteQuery)
					owned.Post("/{dbID}/backup", databaseHandler.CreateBackup)
					owned.Get("/{dbID}/backups", databaseHandler.ListBackups)
					owned.Post("/{dbID}/backups/restore", databaseHandler.RestoreBackup)
					owned.Post("/{dbID}/backups/import", databaseHandler.ImportBackup)
					owned.Get("/{dbID}/backups/download", databaseHandler.DownloadBackup)
					owned.Get("/{dbID}/auto-backup", databaseHandler.GetAutoBackupConfig)
					owned.Put("/{dbID}/auto-backup", databaseHandler.SetAutoBackupConfig)
					owned.Get("/{dbID}/telemetry", databaseHandler.GetDatabaseTelemetry)

					// User management endpoints
					owned.Get("/{dbID}/users", databaseHandler.ListDatabaseUsers)
					owned.Post("/{dbID}/users", databaseHandler.CreateDatabaseUser)
					owned.Delete("/{dbID}/users/{username}", databaseHandler.DeleteDatabaseUser)

					// Explorer endpoints
					owned.Get("/{dbID}/schemas", databaseHandler.ListSchemas)
					owned.Post("/{dbID}/schemas", databaseHandler.CreateSchema)
					owned.Delete("/{dbID}/schemas/{schema}", databaseHandler.DeleteSchema)
					owned.Get("/{dbID}/schemas/{schema}/tables", databaseHandler.ListTables)
					owned.Post("/{dbID}/schemas/{schema}/tables", databaseHandler.CreateTable)
					owned.Delete("/{dbID}/schemas/{schema}/tables/{table}", databaseHandler.DeleteTable)
					owned.Get("/{dbID}/schemas/{schema}/tables/{table}/columns", databaseHandler.DescribeTable)
					owned.Get("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.BrowseTableData)
					owned.Post("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.InsertRow)
					owned.Put("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.UpdateRow)
					owned.Delete("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.DeleteRow)
				})

				// Firewall
				r.Route("/firewall", func(r chi.Router) {
					r.Get("/", firewallHandler.GetFirewall)
					r.Post("/enable", firewallHandler.EnableFirewall)
					r.Post("/disable", firewallHandler.DisableFirewall)
					r.Post("/reload", firewallHandler.ReloadFirewall)
					r.Post("/reset", firewallHandler.ResetFirewall)
					r.Post("/policies", firewallHandler.ChangePolicies)
					r.Post("/install", firewallHandler.InstallUFW)
					r.Post("/rules", firewallHandler.AddRule)
					r.Put("/rules/{ruleId}", firewallHandler.UpdateRule)
					r.Delete("/rules/{ruleId}", firewallHandler.DeleteRule)
					r.Post("/rules/bulk", firewallHandler.BulkAction)
					r.Get("/backups", firewallHandler.ListBackups)
					r.Post("/backups", firewallHandler.CreateBackup)
					r.Post("/backups/{backupId}/restore", firewallHandler.RestoreBackup)
					r.Delete("/backups/{backupId}", firewallHandler.DeleteBackup)
					r.Get("/schedules", firewallHandler.ListSchedules)
					r.Post("/schedules", firewallHandler.CreateSchedule)
					r.Delete("/schedules/{scheduleId}", firewallHandler.CancelSchedule)
					r.Get("/logs", firewallHandler.GetLogs)
				})

			})
		})

		// GitHub API routes
		r.Route("/github", func(r chi.Router) {
			r.Get("/login", githubHandler.Login)
			r.Get("/callback", githubHandler.Callback)
			r.Get("/repos", githubHandler.ListRepos)
			r.Post("/clone", githubHandler.CloneRepo)
			r.Post("/connect", githubHandler.ConnectRepo)
		})

		// Global Stats WebSocket (broadcasts updates for all servers)
		r.Get("/stats/ws", statsHandler.WebSocketHandler)

		// System Operations
		r.Route("/system", func(r chi.Router) {
			r.Use(authMiddleware.RequirePermission("*")) // These actions control the dashboard host itself.
			r.Post("/restart", systemHandler.Restart)
			r.Post("/shutdown", systemHandler.Shutdown)
		})

		// GitHub Webhook Receiver (CI/CD)
		r.Post("/webhooks/github/{projectID}", webhookHandler.HandleGitHubWebhook)

		// Global Stats Snapshot
		r.With(authMiddleware.RequireServerAccess).Get("/stats", statsHandler.GetStats)

		// Global Stats History
		r.With(authMiddleware.RequireServerAccess).Get("/stats/history", statsHandler.GetStatsHistory)

		// Export/Import (admin only)
		r.Route("/export-import", func(r chi.Router) {
			r.Use(authMiddleware.RequirePermission("*"))
			r.Get("/export", exportHandler.Export)
			r.Get("/export/preview", exportHandler.ExportPreview)
			r.Post("/import", exportHandler.Import)
		})

	})

	mcpHandler.API = r

	// GitHub OAuth callback at top level (matches config.yaml callback_url)
	r.Get("/auth/github/login", githubHandler.Login)
	r.Get("/auth/github/callback", githubHandler.Callback)

	// Start Server
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           r,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	go func() {
		log.Printf("Starting server on %d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen: %s\n", err)
		}
	}()

	// Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")

	// Shutdown server with timeout
	// log.Printf format %s has arg cfg.Server.Port of wrong type int
	log.Printf("Server forced to shutdown on port %d", cfg.Server.Port)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}

// Trigger rebuild for air hot-reload - dynamic authentication pool and system user filtering
