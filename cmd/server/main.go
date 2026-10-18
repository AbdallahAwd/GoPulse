package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/config"
	"github.com/AbdallahAwd/GoPulse/internal/handlers"
	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/monitor"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/AbdallahAwd/GoPulse/web"
)

// Set by the release build. Source builds remain identifiable as development builds.
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "Print version and build information")
	flag.Parse()
	if *showVersion {
		fmt.Printf("GoPulse %s (commit %s, built %s)\n", version, commit, builtAt)
		return
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting GoPulse VPS Dashboard on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Initialize Server Store
	dataDir := store.DataDirectory()
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
	authMiddleware.SetSessionOptions(cfg.Auth.TokenDuration, cfg.Auth.SecureCookies)
	authMiddleware.SetAgentStore(agentStore)

	// Initialize Service Manager
	serviceManager := services.NewServiceManager(serverStore)
	defer serviceManager.CloseAll()

	monitoringStore, err := store.NewMonitoringStore(filepath.Join(dataDir, "monitoring.json"))
	if err != nil {
		log.Fatalf("Failed to initialize monitoring: %v", err)
	}
	monitoringService := services.NewMonitoringService(monitoringStore, serverStore, cfg.Monitoring.PollInterval)
	statsHistory := monitor.NewPersistentStatsHistory(monitoringStore)
	monitoringHandler := &handlers.MonitoringHandler{Service: monitoringService}

	// Initialize Stats Collector
	factory := func(server models.Server) (services.StatsProvider, error) {
		return serviceManager.GetStatsProvider(server.ID)
	}
	collector := monitor.NewCollector(serverStore, factory, cfg.Monitoring.PollInterval, statsHistory)
	collector.SetMonitoring(monitoringService)

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
	remoteBackupStore, err := store.NewRemoteBackupStore(filepath.Join(dataDir, "remote_backups.json"))
	if err != nil {
		log.Fatalf("Failed to initialize remote backups: %v", err)
	}
	remoteBackupService, err := services.NewRemoteBackupService(remoteBackupStore, serverStore, databaseStore, userStore, services.NewSSHBackupBackend(serverStore, databaseStore), filepath.Join(dataDir, "remote-backup-keys"))
	if err != nil {
		log.Fatalf("Failed to initialize remote backup scheduler: %v", err)
	}
	defer remoteBackupService.Close()
	functionStore, err := store.NewFunctionStore(filepath.Join(dataDir, "functions.json"))
	if err != nil {
		log.Fatalf("Failed to initialize serverless functions: %v", err)
	}
	functionService, err := services.NewFunctionService(functionStore, serverStore, userStore, services.NewSSHFunctionBackend(serverStore))
	if err != nil {
		log.Fatalf("Failed to initialize function execution: %v", err)
	}
	defer functionService.Close()
	functionHandler := &handlers.FunctionHandler{Service: functionService}

	remoteBackupHandler := &handlers.RemoteBackupHandler{Service: remoteBackupService}

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
	go monitoringService.Start(ctx)
	go remoteBackupService.Start(ctx)
	go functionService.Start(ctx)
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

	mcpHandler := &handlers.MCPHandler{Servers: serverStore, Databases: databaseService, LoadBalancers: loadBalancerService, Manager: serviceManager, Collector: collector, Operations: operations, Auth: authMiddleware, Functions: functionService}

	fileHandler := handlers.NewFileHandler(serviceManager)
	agentHandler := &handlers.AgentHandler{Store: agentStore, Servers: serverStore, MCP: mcpHandler, PublicURL: dashboardDomainService.PublicURL}

	r, err := newRouter(routerDependencies{
		Auth:              authMiddleware,
		Dashboard:         dashboardHandler,
		Server:            serverHandler,
		Process:           processHandler,
		Docker:            dockerHandler,
		Stats:             statsHandler,
		Monitoring:        monitoringHandler,
		System:            systemHandler,
		Terminal:          terminalHandler,
		Cron:              cronHandler,
		Project:           projectHandler,
		Webhook:           webhookHandler,
		GitHub:            githubHandler,
		Domain:            domainHandler,
		Database:          databaseHandler,
		RemoteBackup:      remoteBackupHandler,
		Function:          functionHandler,
		Operation:         operationHandler,
		Qdrant:            qdrantHandler,
		DashboardSettings: dashboardSettingsHandler,
		LoadBalancer:      loadBalancerHandler,
		Firewall:          firewallHandler,
		Export:            exportHandler,
		MCP:               mcpHandler,
		Agent:             agentHandler,
		File:              fileHandler,
	})
	if err != nil {
		log.Fatalf("Failed to initialize router: %v", err)
	}

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
	cancel()

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
