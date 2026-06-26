package services

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

// LoadBalancerService manages load balancer configurations and deployments
type LoadBalancerService struct {
	store        *store.LoadBalancerStore
	serverStore  *store.ServerStore
	projectStore *store.ProjectStore
	manager      *ServiceManager
}

// NewLoadBalancerService creates a new LoadBalancerService
func NewLoadBalancerService(
	lbStore *store.LoadBalancerStore,
	serverStore *store.ServerStore,
	projectStore *store.ProjectStore,
	manager *ServiceManager,
) *LoadBalancerService {
	return &LoadBalancerService{
		store:        lbStore,
		serverStore:  serverStore,
		projectStore: projectStore,
		manager:      manager,
	}
}

// ListLoadBalancers returns all load balancers for a server
func (s *LoadBalancerService) ListLoadBalancers(serverID string) []models.LoadBalancer {
	return s.store.ListLoadBalancersByServer(serverID)
}

// GetLoadBalancer returns details of a load balancer
func (s *LoadBalancerService) GetLoadBalancer(id string) (models.LoadBalancer, error) {
	return s.store.GetLoadBalancer(id)
}

// AddLoadBalancer creates a load balancer
func (s *LoadBalancerService) AddLoadBalancer(lb models.LoadBalancer) error {
	lb.Status = "configuring"
	if err := s.store.AddLoadBalancer(lb); err != nil {
		return err
	}

	// Deploy config asynchronously
	go func() {
		_ = s.Deploy(lb.ID)
	}()

	return nil
}

// RemoveLoadBalancer removes a load balancer and cleans up Nginx
func (s *LoadBalancerService) RemoveLoadBalancer(serverID, id string) error {
	lb, err := s.store.GetLoadBalancer(id)
	if err != nil {
		return err
	}

	// Try to clean up remote Nginx config
	go func() {
		if err := s.removeNginxConfig(lb); err != nil {
			log.Printf("Warning: Failed to clean up Nginx config for LB %s: %v", lb.ID, err)
		}
	}()

	return s.store.RemoveLoadBalancer(id)
}

// AddBackend adds a backend to a load balancer
func (s *LoadBalancerService) AddBackend(backend models.LoadBalancerBackend) error {
	if err := s.store.AddBackend(backend); err != nil {
		return err
	}

	// Re-deploy configuration to Nginx
	go func() {
		if err := s.Deploy(backend.LoadBalancerID); err != nil {
			log.Printf("Error re-deploying load balancer %s after adding backend: %v", backend.LoadBalancerID, err)
		}
	}()

	return nil
}

// RemoveBackend removes a backend from a load balancer
func (s *LoadBalancerService) RemoveBackend(loadBalancerID, backendID string) error {
	if err := s.store.RemoveBackend(loadBalancerID, backendID); err != nil {
		return err
	}

	// Re-deploy configuration to Nginx
	go func() {
		if err := s.Deploy(loadBalancerID); err != nil {
			log.Printf("Error re-deploying load balancer %s after removing backend: %v", loadBalancerID, err)
		}
	}()

	return nil
}

func (s *LoadBalancerService) resolveBackendTarget(b models.LoadBalancerBackend) (string, int, error) {
	if b.TargetProjectID == "" {
		return b.Host, b.Port, nil
	}

	proj, err := s.projectStore.GetProject(b.TargetProjectID)
	if err != nil {
		return "", 0, fmt.Errorf("failed to fetch project %s: %w", b.TargetProjectID, err)
	}

	srvID := b.TargetServerID
	if srvID == "" {
		srvID = proj.ServerID
	}
	srv, err := s.serverStore.GetServer(srvID)
	if err != nil {
		return "", 0, fmt.Errorf("failed to fetch target server %s: %w", srvID, err)
	}

	port := proj.Port
	if proj.BuildType == "docker" && proj.HostPort > 0 {
		port = proj.HostPort
	}

	return srv.Host, port, nil
}

// ErrNoBackends is returned when a load balancer has no backend target nodes configured.
var ErrNoBackends = fmt.Errorf("no backend targets defined")

// Deploy generates and uploads Nginx config, reloads Nginx, and provisions SSL
func (s *LoadBalancerService) Deploy(id string) error {
	lb, err := s.store.GetLoadBalancer(id)
	if err != nil {
		return err
	}

	// Set status to configuring
	lb.Status = "configuring"
	s.store.UpdateLoadBalancer(lb)

	deployErr := s.deployInner(lb)
	if deployErr != nil {
		if deployErr == ErrNoBackends {
			lb.Status = "configuring"
			lb.ErrorMessage = "No backend target nodes defined. Click 'Manage' -> 'Add Backend Target' to route traffic."
			s.store.UpdateLoadBalancer(lb)
			return nil
		}
		lb.Status = "error"
		lb.ErrorMessage = deployErr.Error()
		s.store.UpdateLoadBalancer(lb)
		return deployErr
	}

	lb.Status = "active"
	lb.ErrorMessage = ""
	s.store.UpdateLoadBalancer(lb)
	return nil
}

func (s *LoadBalancerService) deployInner(lb models.LoadBalancer) error {
	if len(lb.Backends) == 0 {
		// Clean up any existing config to prevent breaking Nginx startup/reload
		_ = s.removeNginxConfig(lb)
		return ErrNoBackends
	}

	// Resolve actual host and port for all backends (especially linked projects)
	var resolvedBackends []models.LoadBalancerBackend
	for _, b := range lb.Backends {
		host, port, err := s.resolveBackendTarget(b)
		if err == nil {
			b.Host = host
			b.Port = port
		}
		resolvedBackends = append(resolvedBackends, b)
	}
	lb.Backends = resolvedBackends

	config, err := s.GenerateNginxConfig(lb)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(lb.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	fileName := fmt.Sprintf("gopulse_lb_%s", strings.ReplaceAll(lb.Domain, ".", "_"))
	availablePath := fmt.Sprintf("/etc/nginx/sites-available/%s", fileName)
	enabledPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", fileName)

	escapedConfig := strings.ReplaceAll(config, "'", "'\\''")
	writeCmd := fmt.Sprintf("echo '%s' | sudo tee %s > /dev/null", escapedConfig, availablePath)
	if _, err := client.RunCommand(writeCmd); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	linkCmd := fmt.Sprintf("sudo ln -sf %s %s", availablePath, enabledPath)
	if _, err := client.RunCommand(linkCmd); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	testOutput, err := client.RunCommand("sudo nginx -t 2>&1")
	if err != nil {
		cleanupCmd := fmt.Sprintf("sudo rm -f %s %s", availablePath, enabledPath)
		client.RunCommand(cleanupCmd)
		return fmt.Errorf("nginx config test failed: %s", testOutput)
	}

	if _, err := client.RunCommand("sudo systemctl reload nginx"); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	if lb.SSL {
		if err := s.provisionSSL(lb); err != nil {
			log.Printf("Warning: SSL provisioning failed for load balancer %s: %v", lb.Domain, err)
			return fmt.Errorf("load balancer deployed over HTTP but SSL setup failed: %w", err)
		}
	}

	return nil
}

func (s *LoadBalancerService) removeNginxConfig(lb models.LoadBalancer) error {
	client, err := s.manager.GetSSHClient(lb.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	fileName := fmt.Sprintf("gopulse_lb_%s", strings.ReplaceAll(lb.Domain, ".", "_"))
	availablePath := fmt.Sprintf("/etc/nginx/sites-available/%s", fileName)
	enabledPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", fileName)

	removeCmd := fmt.Sprintf("sudo rm -f %s %s", availablePath, enabledPath)
	if _, err := client.RunCommand(removeCmd); err != nil {
		return fmt.Errorf("failed to remove config files: %w", err)
	}

	if _, err := client.RunCommand("sudo systemctl reload nginx"); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	return nil
}

func (s *LoadBalancerService) provisionSSL(lb models.LoadBalancer) error {
	client, err := s.manager.GetSSHClient(lb.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	if err := s.ensureCertbot(lb.ServerID); err != nil {
		return fmt.Errorf("failed to install certbot: %w", err)
	}

	certbotCmd := fmt.Sprintf(
		"sudo certbot --nginx -d %s --non-interactive --agree-tos --register-unsafely-without-email --redirect 2>&1",
		lb.Domain,
	)
	output, err := client.RunCommand(certbotCmd)
	if err != nil {
		return fmt.Errorf("certbot failed: %s (err: %v)", output, err)
	}

	log.Printf("SSL provisioned for Load Balancer %s: %s", lb.Domain, output)
	return nil
}

func (s *LoadBalancerService) ensureCertbot(serverID string) error {
	client, err := s.manager.GetSSHClient(serverID)
	if err != nil {
		return err
	}

	checkOutput, _ := client.RunCommand("command -v certbot 2>/dev/null && echo 'FOUND' || echo 'NOT_FOUND'")
	if strings.Contains(checkOutput, "FOUND") {
		return nil
	}

	log.Printf("Certbot not found, installing...")

	installCmd := "if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y certbot python3-certbot-nginx 2>&1; elif command -v dnf &>/dev/null; then sudo dnf install -y certbot python3-certbot-nginx 2>&1; elif command -v yum &>/dev/null; then sudo yum install -y certbot python3-certbot-nginx 2>&1; else echo 'ERROR: No supported package manager'; exit 1; fi"
	output, err := client.RunCommand(installCmd)
	if err != nil {
		return fmt.Errorf("certbot install failed: %s (err: %v)", output, err)
	}

	log.Printf("Certbot installed successfully")
	return nil
}

// GenerateNginxConfig renders the Nginx configuration for a load balancer
func (s *LoadBalancerService) GenerateNginxConfig(lb models.LoadBalancer) (string, error) {
	nginxTemplate := `# Upstream Definition
upstream gopulse_upstream_{{.ID}} {
    {{if ne .Algorithm "round-robin"}}{{.Algorithm}};{{end}}
    {{range .Backends}}
    server {{.Host}}:{{.Port}} weight={{.Weight}} max_fails=3 fail_timeout=10s {{if .Backup}}backup{{end}};
    {{end}}
}

# HTTP Frontend
server {
    listen {{.Port}};
    server_name {{.Domain}};

    location / {
        proxy_pass http://gopulse_upstream_{{.ID}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }
}
`
	tmpl, err := template.New("nginx_lb").Parse(nginxTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, lb); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// StartHealthChecker runs the background active HTTP check loop
func (s *LoadBalancerService) StartHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Println("Starting active background HTTP health poller for Load Balancers")

	lastCheck := make(map[string]time.Time)
	var mu sync.Mutex

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	for {
		select {
		case <-ticker.C:
			lbs, err := s.store.ListAllLoadBalancers()
			if err != nil {
				continue
			}

			for _, lb := range lbs {
				interval := lb.HealthCheckInterval
				if interval <= 0 {
					interval = 10
				}

				mu.Lock()
				lastTime := lastCheck[lb.ID]
				due := time.Since(lastTime) >= time.Duration(interval)*time.Second
				if due {
					lastCheck[lb.ID] = time.Now()
				}
				mu.Unlock()

				if !due {
					continue
				}

				for _, b := range lb.Backends {
					go func(backend models.LoadBalancerBackend, path string) {
						host, port, err := s.resolveBackendTarget(backend)
						if err != nil {
							s.store.UpdateBackendStatus(backend.ID, "unhealthy")
							return
						}

						proto := "http"
						if port == 443 {
							proto = "https"
						}
						url := fmt.Sprintf("%s://%s:%d%s", proto, host, port, path)

						resp, err := httpClient.Get(url)
						status := "unhealthy"
						if err == nil {
							if resp.StatusCode >= 200 && resp.StatusCode < 400 {
								status = "healthy"
							}
							resp.Body.Close()
						}

						s.store.UpdateBackendStatus(backend.ID, status)
					}(b, lb.HealthCheckPath)
				}
			}
		case <-ctx.Done():
			log.Println("Stopping Load Balancer health poller")
			return
		}
	}
}

// GetLogs returns the deployment error and Nginx logs for a load balancer
func (s *LoadBalancerService) GetLogs(ctx context.Context, id string) (models.LoadBalancerLogs, error) {
	lb, err := s.store.GetLoadBalancer(id)
	if err != nil {
		return models.LoadBalancerLogs{}, err
	}

	client, err := s.manager.GetSSHClient(lb.ServerID)
	if err != nil {
		return models.LoadBalancerLogs{
			LoadBalancerID:  lb.ID,
			DeploymentError: lb.ErrorMessage,
			FetchedAt:       time.Now(),
		}, fmt.Errorf("failed to connect to host via SSH: %w", err)
	}

	// Fetch Nginx access logs filtered by domain
	accessCmd := fmt.Sprintf("sudo grep -i %s /var/log/nginx/access.log 2>/dev/null | tail -n 100 || true", lbShellQuote(lb.Domain))
	accessLogs, err := client.RunCommand(accessCmd)
	if err != nil {
		accessLogs = fmt.Sprintf("Failed to fetch access logs: %v", err)
	}

	// Fetch Nginx error logs filtered by domain
	errorCmd := fmt.Sprintf("sudo grep -i %s /var/log/nginx/error.log 2>/dev/null | tail -n 100 || true", lbShellQuote(lb.Domain))
	errorLogs, err := client.RunCommand(errorCmd)
	if err != nil {
		errorLogs = fmt.Sprintf("Failed to fetch error logs: %v", err)
	}

	if strings.TrimSpace(errorLogs) == "" {
		// Fallback: show last 50 lines of global error log
		fallbackCmd := "sudo tail -n 50 /var/log/nginx/error.log 2>/dev/null || true"
		globalErrors, err := client.RunCommand(fallbackCmd)
		if err == nil && strings.TrimSpace(globalErrors) != "" {
			errorLogs = "# Showing recent global Nginx errors (no domain-specific errors found):\n" + globalErrors
		}
	}

	return models.LoadBalancerLogs{
		LoadBalancerID:  lb.ID,
		DeploymentError: lb.ErrorMessage,
		AccessLogs:      accessLogs,
		ErrorLogs:       errorLogs,
		FetchedAt:       time.Now(),
	}, nil
}

func lbShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

