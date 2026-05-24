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

	// Initialize Service Manager
	serviceManager := services.NewServiceManager(serverStore)
	defer serviceManager.CloseAll()

	// Initialize Stats Collector
	factory := func(server models.Server) (services.StatsProvider, error) {
		return serviceManager.GetStatsProvider(server.ID)
	}
	collector := monitor.NewCollector(serverStore, factory, cfg.Monitoring.PollInterval)

	// Initialize Handlers
	dashboardHandler := handlers.NewDashboardHandler(web.Assets, serverStore)
	serverHandler := handlers.NewServerHandler(serverStore)
	processHandler := handlers.NewProcessHandler(serviceManager)
	dockerHandler := handlers.NewDockerHandler(serviceManager)
	statsHandler := handlers.NewStatsHandler(collector)
	systemHandler := handlers.NewSystemHandler(serviceManager)

	// Initialize Project Components
	projectStore, err := store.NewProjectStore(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		log.Fatalf("Failed to initialize project store: %v", err)
	}
	projectService := services.NewProjectService(projectStore, serviceManager, filepath.Join(dataDir, "projects"))
	projectHandler := handlers.NewProjectHandler(projectService)
	githubHandler := handlers.NewGitHubHandler(cfg.GitHub, projectService)

	// Initialize Domain Components
	domainStore, err := store.NewDomainStore(filepath.Join(dataDir, "domains.json"))
	if err != nil {
		log.Fatalf("Failed to initialize domain store: %v", err)
	}
	domainService := services.NewDomainService(domainStore, serviceManager)
	domainHandler := handlers.NewDomainHandler(domainService)

	// Start Collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Start(ctx)

	// Setup Router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.RecoveryMiddleware)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// Static Files (Embedded)
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Web Routes
	r.Get("/", dashboardHandler.Index)
	r.Get("/servers/{serverID}/dashboard", dashboardHandler.ServerDashboard)

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// Server Management
		r.Route("/servers", func(r chi.Router) {
			r.Get("/", serverHandler.ListServers)
			r.Post("/", serverHandler.AddServer)
			r.Post("/upload-key", serverHandler.UploadKey)

			// Per-Server Stats & Operations
			r.Route("/{serverID}", func(r chi.Router) {
				r.Delete("/", serverHandler.RemoveServer)
				r.Get("/", serverHandler.GetServer)
				// Stats (Historical or snapshot if needed via REST, but WS is preferred)
				// r.Get("/stats", ...)

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
					})
				})

				// Remote System Operations
				r.Route("/system", func(r chi.Router) {
					r.Post("/restart", systemHandler.RestartRemote)
					r.Post("/shutdown", systemHandler.ShutdownRemote)
					r.Get("/check/{software}", systemHandler.CheckSoftware)
					r.Post("/install/{software}", systemHandler.InstallSoftware)
				})

				// Domains
				r.Route("/domains", func(r chi.Router) {
					r.Get("/", domainHandler.ListDomains)
					r.Post("/", domainHandler.AddDomain)
					r.Delete("/{id}", domainHandler.RemoveDomain)
					r.Get("/nginx/status", domainHandler.NginxStatus)
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

		// Global Stats Snapshot
		r.Get("/stats", statsHandler.GetStats)

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
