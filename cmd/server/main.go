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
	dataDir := filepath.Join(os.Getenv("HOME"), ".gopulse")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
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

	// Initialize Auth Middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg.Auth.JWTSecret, userStore)

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

	// Initialize Project Components
	projectStore, err := store.NewProjectStore(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		log.Fatalf("Failed to initialize project store: %v", err)
	}
	projectService := services.NewProjectService(projectStore, serviceManager, filepath.Join(dataDir, "projects"))
	projectHandler := handlers.NewProjectHandler(projectService)
	webhookHandler := handlers.NewWebhookHandler(projectService)
	githubHandler := handlers.NewGitHubHandler(cfg.GitHub, projectService)

	// Initialize Domain Components
	domainStore, err := store.NewDomainStore(filepath.Join(dataDir, "domains.json"))
	if err != nil {
		log.Fatalf("Failed to initialize domain store: %v", err)
	}
	domainService := services.NewDomainService(domainStore, serviceManager)
	domainHandler := handlers.NewDomainHandler(domainService)

	// Initialize Database Components
	databaseStore, err := store.NewDatabaseStore(filepath.Join(dataDir, "databases.json"))
	if err != nil {
		log.Fatalf("Failed to initialize database store: %v", err)
	}
	databaseService := services.NewDatabaseService(databaseStore, serviceManager)
	databaseHandler := handlers.NewDatabaseHandler(databaseService, serviceManager)

	// Start Collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Start(ctx)

	// Start Backup Scheduler
	backupScheduler := services.NewBackupScheduler(databaseService, databaseStore)
	go backupScheduler.Start(ctx)

	// Auto-start marked projects in background after a short delay (to let SSH clients connect)
	go func() {
		time.Sleep(5 * time.Second)
		projectService.AutoStartProjects(ctx)
	}()

	// Setup Router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.RecoveryMiddleware)
	r.Use(authMiddleware.Handler)
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
	r.With(authMiddleware.RequireServerAccess).Get("/servers/{serverID}/dashboard", dashboardHandler.ServerDashboard)
	r.With(authMiddleware.RequirePermission("user:manage")).Get("/users", dashboardHandler.Users)

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// Auth Endpoints
		r.Post("/auth/login", authMiddleware.LoginHandler)
		r.Post("/auth/register", authMiddleware.RegisterHandler)
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
			r.Post("/", serverHandler.AddServer)
			r.Post("/upload-key", serverHandler.UploadKey)

			// Per-Server Stats & Operations
			r.Route("/{serverID}", func(r chi.Router) {
				r.Use(authMiddleware.RequireServerAccess)
				r.Delete("/", serverHandler.RemoveServer)
				r.Get("/", serverHandler.GetServer)
				r.Put("/", serverHandler.UpdateServer)
				r.Get("/terminal", terminalHandler.HandleTerminal)
				r.Get("/stats/history", statsHandler.GetStatsHistory)

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
						r.Get("/", projectHandler.GetProject)
						r.Put("/", projectHandler.UpdateProject) // Added Update route
						r.Post("/setup", projectHandler.SetupProject)
						r.Get("/detect", projectHandler.DetectStrategy)
						r.Post("/build", projectHandler.BuildProject)
						r.Post("/start", projectHandler.StartProject) // Fixed method name reference
						r.Post("/stop", projectHandler.StopProject)
						r.Delete("/", projectHandler.DeleteProject)
						r.Post("/webhook/enable", projectHandler.EnableWebhook)
						r.Post("/webhook/disable", projectHandler.DisableWebhook)
						r.Get("/env", projectHandler.GetEnvVars)
						r.Put("/env", projectHandler.UpdateEnvVars)
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

				// Databases
				r.Route("/databases", func(r chi.Router) {
					r.Get("/", databaseHandler.ListDatabases)
					r.Post("/", databaseHandler.ProvisionDatabase)
					r.Get("/{dbID}", databaseHandler.GetDatabase)
					r.Post("/{dbID}/start", databaseHandler.StartDatabase)
					r.Post("/{dbID}/stop", databaseHandler.StopDatabase)
					r.Post("/{dbID}/restart", databaseHandler.RestartDatabase)
					r.Delete("/{dbID}", databaseHandler.DeleteDatabase)
					r.Post("/{dbID}/query", databaseHandler.ExecuteQuery)
					r.Post("/{dbID}/backup", databaseHandler.CreateBackup)
					r.Get("/{dbID}/backups", databaseHandler.ListBackups)
					r.Post("/{dbID}/backups/restore", databaseHandler.RestoreBackup)
					r.Post("/{dbID}/backups/import", databaseHandler.ImportBackup)
					r.Get("/{dbID}/backups/download", databaseHandler.DownloadBackup)
					r.Get("/{dbID}/auto-backup", databaseHandler.GetAutoBackupConfig)
					r.Put("/{dbID}/auto-backup", databaseHandler.SetAutoBackupConfig)
					r.Get("/{dbID}/telemetry", databaseHandler.GetDatabaseTelemetry)

					// User management endpoints
					r.Get("/{dbID}/users", databaseHandler.ListDatabaseUsers)
					r.Post("/{dbID}/users", databaseHandler.CreateDatabaseUser)
					r.Delete("/{dbID}/users/{username}", databaseHandler.DeleteDatabaseUser)

					// Explorer endpoints
					r.Get("/{dbID}/schemas", databaseHandler.ListSchemas)
					r.Post("/{dbID}/schemas", databaseHandler.CreateSchema)
					r.Delete("/{dbID}/schemas/{schema}", databaseHandler.DeleteSchema)
					r.Get("/{dbID}/schemas/{schema}/tables", databaseHandler.ListTables)
					r.Post("/{dbID}/schemas/{schema}/tables", databaseHandler.CreateTable)
					r.Delete("/{dbID}/schemas/{schema}/tables/{table}", databaseHandler.DeleteTable)
					r.Get("/{dbID}/schemas/{schema}/tables/{table}/columns", databaseHandler.DescribeTable)
					r.Get("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.BrowseTableData)
					r.Post("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.InsertRow)
					r.Put("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.UpdateRow)
					r.Delete("/{dbID}/schemas/{schema}/tables/{table}/rows", databaseHandler.DeleteRow)
				})

			})
		})

		// GitHub API routes
		r.Route("/github", func(r chi.Router) {
			r.Get("/login", githubHandler.Login)
			r.Get("/callback", githubHandler.Callback)
			r.Get("/repos", githubHandler.ListRepos)
			r.Post("/clone", githubHandler.CloneRepo)
		})

		// Global Stats WebSocket (broadcasts updates for all servers)
		r.Get("/stats/ws", statsHandler.WebSocketHandler)

		// System Operations
		r.Route("/system", func(r chi.Router) {
			r.Post("/restart", systemHandler.Restart)
			r.Post("/shutdown", systemHandler.Shutdown)
		})

		// GitHub Webhook Receiver (CI/CD)
		r.Post("/webhooks/github/{projectID}", webhookHandler.HandleGitHubWebhook)

		// Global Stats Snapshot
		r.Get("/stats", statsHandler.GetStats)
		
		// Global Stats History
		r.Get("/stats/history", statsHandler.GetStatsHistory)

	})

	// GitHub OAuth callback at top level (matches config.yaml callback_url)
	r.Get("/auth/github/login", githubHandler.Login)
	r.Get("/auth/github/callback", githubHandler.Callback)

	// Start Server
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: r,
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
