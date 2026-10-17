package services

import (
	"fmt"
	"log"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

// DomainService manages domain configurations and Nginx deployment
type DomainService struct {
	store   *store.DomainStore
	manager *ServiceManager
}

// NewDomainService creates a new DomainService
func NewDomainService(domainStore *store.DomainStore, manager *ServiceManager) *DomainService {
	return &DomainService{
		store:   domainStore,
		manager: manager,
	}
}

// ListDomains returns all domains for a server
func (s *DomainService) ListDomains(serverID string) []models.Domain {
	return s.store.ListDomainsByServer(serverID)
}

// AddDomain creates a domain record and deploys Nginx config
func (s *DomainService) AddDomain(domain models.Domain) error {
	// Save to store first
	if err := s.store.AddDomain(domain); err != nil {
		return err
	}

	// Try to deploy Nginx config via SSH
	if err := s.deployNginxConfig(domain); err != nil {
		log.Printf("Warning: Domain saved but Nginx deployment failed: %v", err)
		// Don't rollback the store save — the domain is saved, Nginx just couldn't be configured
		return fmt.Errorf("domain saved but Nginx config failed: %v", err)
	}

	return nil
}

// RemoveDomain removes a domain record and cleans up Nginx config
func (s *DomainService) RemoveDomain(serverID, domainID string) error {
	domain, err := s.store.GetDomain(domainID)
	if err != nil {
		return err
	}

	// Try to remove Nginx config via SSH
	if err := s.removeNginxConfig(domain); err != nil {
		log.Printf("Warning: Nginx config removal failed: %v", err)
	}

	return s.store.RemoveDomain(domainID)
}

// CheckNginxStatus checks if Nginx is running on the server
func (s *DomainService) CheckNginxStatus(serverID string) (string, error) {
	client, err := s.manager.GetSSHClient(serverID)
	if err != nil {
		return "", fmt.Errorf("SSH connection failed: %v", err)
	}

	output, err := client.RunCommand("systemctl is-active nginx 2>/dev/null || echo 'not-installed'")
	if err != nil {
		return "unknown", nil
	}
	return strings.TrimSpace(output), nil
}

// generateNginxConfig generates an Nginx reverse proxy config for a domain
func (s *DomainService) generateNginxConfig(domain models.Domain) string {
	return fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location / {
        proxy_pass http://127.0.0.1:%d;
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
`, domain.Domain, domain.Port)
}

// configFileName returns the Nginx config filename for a domain
func (s *DomainService) configFileName(domain models.Domain) string {
	// Replace dots with underscores for filename safety
	safe := strings.ReplaceAll(domain.Domain, ".", "_")
	return fmt.Sprintf("gopulse_%s", safe)
}

// deployNginxConfig writes the Nginx config to the remote server
func (s *DomainService) deployNginxConfig(domain models.Domain) error {
	client, err := s.manager.GetSSHClient(domain.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}

	config := s.generateNginxConfig(domain)
	fileName := s.configFileName(domain)
	availablePath := fmt.Sprintf("/etc/nginx/sites-available/%s", fileName)
	enabledPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", fileName)

	// Write config file
	escapedConfig := strings.ReplaceAll(config, "'", "'\\''")
	writeCmd := fmt.Sprintf("echo '%s' | sudo tee %s > /dev/null", escapedConfig, availablePath)
	if _, err := client.RunCommand(writeCmd); err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}

	// Create symlink
	linkCmd := fmt.Sprintf("sudo ln -sf %s %s", availablePath, enabledPath)
	if _, err := client.RunCommand(linkCmd); err != nil {
		return fmt.Errorf("failed to create symlink: %v", err)
	}

	// Test and reload Nginx
	testCmd := "sudo nginx -t 2>&1"
	testOutput, err := client.RunCommand(testCmd)
	if err != nil {
		// Rollback: remove the config files
		cleanupCmd := fmt.Sprintf("sudo rm -f %s %s", availablePath, enabledPath)
		client.RunCommand(cleanupCmd)
		return fmt.Errorf("nginx config test failed: %s", testOutput)
	}

	if _, err := client.RunCommand("sudo systemctl reload nginx"); err != nil {
		return fmt.Errorf("failed to reload nginx: %v", err)
	}

	// If SSL is enabled, provision a Let's Encrypt certificate via Certbot
	if domain.SSL {
		if err := s.provisionSSL(domain); err != nil {
			log.Printf("Warning: SSL provisioning failed for %s: %v", domain.Domain, err)
			return fmt.Errorf("domain deployed with HTTP but SSL setup failed: %v", err)
		}
	}

	return nil
}

// provisionSSL installs Certbot if needed and provisions a Let's Encrypt SSL certificate
func (s *DomainService) provisionSSL(domain models.Domain) error {
	client, err := s.manager.GetSSHClient(domain.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}

	// Ensure Certbot is installed
	if err := s.ensureCertbot(domain.ServerID); err != nil {
		return fmt.Errorf("failed to install certbot: %v", err)
	}

	// Run Certbot to obtain certificate and auto-configure Nginx
	certbotCmd := fmt.Sprintf(
		"sudo certbot --nginx -d %s --non-interactive --agree-tos --register-unsafely-without-email --redirect 2>&1",
		domain.Domain,
	)
	output, err := client.RunCommand(certbotCmd)
	if err != nil {
		return fmt.Errorf("certbot failed: %s (err: %v)", output, err)
	}

	log.Printf("SSL provisioned for %s: %s", domain.Domain, output)
	return nil
}

// ensureCertbot checks if Certbot is installed, installs it if not
func (s *DomainService) ensureCertbot(serverID string) error {
	client, err := s.manager.GetSSHClient(serverID)
	if err != nil {
		return err
	}

	// Check if certbot exists
	checkOutput, _ := client.RunCommand("command -v certbot 2>/dev/null && echo 'FOUND' || echo 'NOT_FOUND'")
	if strings.Contains(checkOutput, "FOUND") {
		return nil
	}

	log.Printf("Certbot not found, installing...")

	// Install certbot + nginx plugin
	installCmd := "if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y certbot python3-certbot-nginx 2>&1; elif command -v dnf &>/dev/null; then sudo dnf install -y certbot python3-certbot-nginx 2>&1; elif command -v yum &>/dev/null; then sudo yum install -y certbot python3-certbot-nginx 2>&1; else echo 'ERROR: No supported package manager'; exit 1; fi"
	output, err := client.RunCommand(installCmd)
	if err != nil {
		return fmt.Errorf("certbot install failed: %s (err: %v)", output, err)
	}

	log.Printf("Certbot installed successfully")
	return nil
}

// removeNginxConfig removes the Nginx config from the remote server
func (s *DomainService) removeNginxConfig(domain models.Domain) error {
	client, err := s.manager.GetSSHClient(domain.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %v", err)
	}

	fileName := s.configFileName(domain)
	availablePath := fmt.Sprintf("/etc/nginx/sites-available/%s", fileName)
	enabledPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", fileName)

	// Remove files
	removeCmd := fmt.Sprintf("sudo rm -f %s %s", availablePath, enabledPath)
	if _, err := client.RunCommand(removeCmd); err != nil {
		return fmt.Errorf("failed to remove config files: %v", err)
	}

	// Reload Nginx
	if _, err := client.RunCommand("sudo systemctl reload nginx"); err != nil {
		return fmt.Errorf("failed to reload nginx: %v", err)
	}

	return nil
}
