package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type LoadBalancerService struct {
	store        *store.LoadBalancerStore
	serverStore  *store.ServerStore
	projectStore *store.ProjectStore
	manager      *ServiceManager
	locks        sync.Map
}

func NewLoadBalancerService(st *store.LoadBalancerStore, servers *store.ServerStore, projects *store.ProjectStore, manager *ServiceManager) *LoadBalancerService {
	return &LoadBalancerService{store: st, serverStore: servers, projectStore: projects, manager: manager}
}
func (s *LoadBalancerService) ListLoadBalancers(server string) []models.LoadBalancer {
	return s.store.ListLoadBalancersByServer(server)
}
func (s *LoadBalancerService) GetLoadBalancer(id string) (models.LoadBalancer, error) {
	return s.store.GetLoadBalancer(id)
}
func (s *LoadBalancerService) serverLock(id string) *sync.Mutex {
	lock, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

var validDNSName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

func ValidTargetHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if !validDNSName.MatchString(host) || strings.Contains(host, "..") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}
func ValidateLoadBalancer(lb models.LoadBalancer) error {
	if strings.TrimSpace(lb.Name) == "" || len(lb.Name) > 100 {
		return fmt.Errorf("display name is required (maximum 100 characters)")
	}
	if !safeResourceID.MatchString(lb.ID) || !ValidTargetHost(lb.Domain) || strings.Contains(lb.Domain, ":") {
		return fmt.Errorf("invalid load balancer ID or domain")
	}
	if lb.Port < 1 || lb.Port > 65535 {
		return fmt.Errorf("incoming port must be 1–65535")
	}
	if lb.Algorithm != "round-robin" && lb.Algorithm != "least_conn" && lb.Algorithm != "ip_hash" {
		return fmt.Errorf("unsupported balancing strategy")
	}
	if lb.SSL && (net.ParseIP(lb.Domain) != nil || lb.Port != 443) {
		return fmt.Errorf("HTTPS requires a DNS name and incoming port 443")
	}
	if lb.HealthCheckInterval < 5 || lb.HealthCheckInterval > 3600 {
		return fmt.Errorf("health check interval must be 5–3600 seconds")
	}
	if !strings.HasPrefix(lb.HealthCheckPath, "/") || len(lb.HealthCheckPath) > 512 || strings.ContainsAny(lb.HealthCheckPath, "\r\n #;'\"{}\\") {
		return fmt.Errorf("invalid health check path")
	}
	if len(lb.Backends) == 0 || len(lb.Backends) > 100 {
		return fmt.Errorf("select 1–100 backend servers")
	}
	primary := false
	seen := map[string]bool{}
	for _, b := range lb.Backends {
		if !safeResourceID.MatchString(b.ID) || !ValidTargetHost(b.Host) || b.Port < 1 || b.Port > 65535 || b.Weight < 1 || b.Weight > 256 {
			return fmt.Errorf("invalid backend ID, host, port or weight")
		}
		if b.Backup && lb.Algorithm == "ip_hash" {
			return fmt.Errorf("IP Hash does not support backup backends")
		}
		if seen[b.ID] {
			return fmt.Errorf("duplicate backend ID")
		}
		seen[b.ID] = true
		if !b.Backup && b.Status != "disabled" {
			primary = true
		}
	}
	if !primary {
		return fmt.Errorf("at least one enabled primary backend is required")
	}
	return nil
}
func (s *LoadBalancerService) resolveBackendTarget(b models.LoadBalancerBackend) (string, int, error) {
	if b.TargetProjectID != "" {
		proj, err := s.projectStore.GetProject(b.TargetProjectID)
		if err != nil {
			return "", 0, err
		}
		if b.TargetServerID != "" && proj.ServerID != b.TargetServerID {
			return "", 0, fmt.Errorf("project does not belong to target server")
		}
		srv, err := s.serverStore.GetServer(proj.ServerID)
		if err != nil {
			return "", 0, err
		}
		port := proj.Port
		if proj.BuildType == "docker" && proj.HostPort > 0 {
			port = proj.HostPort
		}
		return srv.Host, port, nil
	}
	if b.TargetServerID != "" {
		srv, err := s.serverStore.GetServer(b.TargetServerID)
		if err != nil {
			return "", 0, err
		}
		return srv.Host, b.Port, nil
	}
	return b.Host, b.Port, nil
}
func (s *LoadBalancerService) Prepare(lb *models.LoadBalancer) error {
	if _, err := s.serverStore.GetServer(lb.ServerID); err != nil {
		return err
	}
	for i := range lb.Backends {
		b := &lb.Backends[i]
		if b.ID == "" {
			b.ID = uuid.NewString()
		}
		b.LoadBalancerID = lb.ID
		if b.Weight == 0 {
			b.Weight = 1
		}
		host, port, err := s.resolveBackendTarget(*b)
		if err != nil {
			return fmt.Errorf("backend resolution failed: %w", err)
		}
		b.Host, b.Port = host, port
		if b.Status != "disabled" {
			b.Status = "unknown"
		}
	}
	return ValidateLoadBalancer(*lb)
}

var ErrNoBackends = fmt.Errorf("no backend targets defined")

func (s *LoadBalancerService) AddLoadBalancer(lb models.LoadBalancer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	return s.Create(ctx, lb, func(string) {})
}
func (s *LoadBalancerService) Create(ctx context.Context, lb models.LoadBalancer, progress func(string)) error {
	if err := s.Prepare(&lb); err != nil {
		return err
	}
	lock := s.serverLock(lb.ServerID)
	lock.Lock()
	defer lock.Unlock()
	for _, existing := range s.store.ListLoadBalancersByServer(lb.ServerID) {
		if strings.EqualFold(existing.Domain, lb.Domain) {
			return fmt.Errorf("this domain already has a load balancer on the entry server; edit or retry that load balancer")
		}
	}
	lb.Status = "configuring"
	if err := s.store.AddLoadBalancer(lb); err != nil {
		return err
	}
	if err := s.apply(ctx, lb, progress); err != nil {
		lb.Status = "error"
		lb.ErrorMessage = err.Error()
		s.store.UpdateLoadBalancer(lb)
		return err
	}
	lb.Status = "active"
	return s.store.ReplaceLoadBalancer(lb)
}
func (s *LoadBalancerService) Update(ctx context.Context, lb models.LoadBalancer, progress func(string)) error {
	if err := s.Prepare(&lb); err != nil {
		return err
	}
	lock := s.serverLock(lb.ServerID)
	lock.Lock()
	defer lock.Unlock()
	old, err := s.store.GetLoadBalancer(lb.ID)
	if err != nil {
		return err
	}
	if old.ServerID != lb.ServerID || old.Domain != lb.Domain {
		return fmt.Errorf("entry server and domain cannot change; create a new load balancer")
	}
	if err := s.apply(ctx, lb, progress); err != nil {
		old.ErrorMessage = err.Error()
		s.store.UpdateLoadBalancer(old)
		return err
	}
	lb.Status = "active"
	lb.ErrorMessage = ""
	if err := s.store.ReplaceLoadBalancer(lb); err != nil {
		if rollbackErr := s.apply(ctx, old, progress); rollbackErr != nil {
			return fmt.Errorf("metadata save and remote rollback failed; inspect remote state")
		}
		return err
	}
	return nil
}
func (s *LoadBalancerService) Deploy(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	return s.Retry(ctx, id, func(string) {})
}
func (s *LoadBalancerService) Retry(ctx context.Context, id string, step func(string)) error {
	lb, err := s.store.GetLoadBalancer(id)
	if err != nil {
		return err
	}
	return s.Update(ctx, lb, step)
}
func (s *LoadBalancerService) RemoveLoadBalancer(server, id string) error {
	lb, err := s.store.GetLoadBalancer(id)
	if err != nil {
		return err
	}
	if lb.ServerID != server {
		return fmt.Errorf("load balancer not found on server")
	}
	if !safeResourceID.MatchString(lb.ID) || !ValidTargetHost(lb.Domain) || strings.Contains(lb.Domain, ":") {
		return fmt.Errorf("invalid stored configuration path; removal refused")
	}
	lock := s.serverLock(server)
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	available, enabled := lbConfigPaths(lb)
	cmd := nginxApplyScript("", available, enabled, true, "sudo -n nginx -t", "sudo -n systemctl reload nginx", "")
	if _, err := s.manager.Run(ctx, server, cmd); err != nil {
		return fmt.Errorf("Nginx removal failed; configuration and metadata retained")
	}
	return s.store.RemoveLoadBalancer(id)
}
func (s *LoadBalancerService) AddBackend(b models.LoadBalancerBackend) error {
	lb, err := s.GetLoadBalancer(b.LoadBalancerID)
	if err != nil {
		return err
	}
	lb.Backends = append(lb.Backends, b)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	return s.Update(ctx, lb, func(string) {})
}
func (s *LoadBalancerService) RemoveBackend(id, backendID string) error {
	lb, err := s.GetLoadBalancer(id)
	if err != nil {
		return err
	}
	found := false
	backends := []models.LoadBalancerBackend{}
	for _, b := range lb.Backends {
		if b.ID == backendID {
			found = true
		} else {
			backends = append(backends, b)
		}
	}
	if !found {
		return fmt.Errorf("backend not found")
	}
	lb.Backends = backends
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	return s.Update(ctx, lb, func(string) {})
}
func lbConfigPaths(lb models.LoadBalancer) (string, string) {
	name := "gopulse_lb_" + strings.ReplaceAll(lb.Domain, ".", "_")
	return "/etc/nginx/sites-available/" + name, "/etc/nginx/sites-enabled/" + name
}

// nginxApplyScript preserves both the previous file and symlink. A trap rolls back syntax, reload or routing failures.
// Caller holds the per-server lock; timeout/transport loss is reported as uncertain, never retried automatically.
func nginxApplyScript(config, available, enabled string, remove bool, validate, reload, verify string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(config))
	mutation := "printf %s " + shellQuote(encoded) + " | base64 -d | sudo -n tee " + shellQuote(available) + " >/dev/null\nsudo -n ln -sfn " + shellQuote(available) + " " + shellQuote(enabled)
	if remove {
		mutation = "sudo -n rm -f -- " + shellQuote(available) + " " + shellQuote(enabled)
	}
	return fmt.Sprintf(`set -eu
backup=$(mktemp -d)
had_file=0; had_link=0
if sudo -n test -e %[1]s; then sudo -n cp -a %[1]s "$backup/available"; had_file=1; fi
if sudo -n test -e %[2]s || sudo -n test -L %[2]s; then sudo -n cp -a %[2]s "$backup/enabled"; had_link=1; fi
rollback() {
 trap - EXIT HUP INT TERM
 sudo -n rm -f -- %[1]s %[2]s
 if [ "$had_file" = 1 ]; then sudo -n cp -a "$backup/available" %[1]s; fi
 if [ "$had_link" = 1 ]; then sudo -n cp -a "$backup/enabled" %[2]s; fi
 %[4]s && %[5]s || true
 sudo -n rm -rf -- "$backup"
}
trap rollback EXIT HUP INT TERM
%[3]s
printf 'GOPULSE_STEP=syntax\n' >&2
%[4]s
printf 'GOPULSE_STEP=reload\n' >&2
%[5]s
printf 'GOPULSE_STEP=routing\n' >&2
%[6]s
trap - EXIT HUP INT TERM
sudo -n rm -rf -- "$backup"
`, shellQuote(available), shellQuote(enabled), mutation, validate, reload, verify)
}
func (s *LoadBalancerService) apply(ctx context.Context, lb models.LoadBalancer, progress func(string)) error {
	progress("Connect to entry VPS and check permissions")
	if _, err := s.manager.Run(ctx, lb.ServerID, "sudo -n true && command -v curl >/dev/null"); err != nil {
		return fmt.Errorf("connection/permissions: SSH, curl and noninteractive sudo required")
	}
	progress("Check Nginx installation and firewall policy")
	// Never modify firewall policy. Report it; routing verification below tests the entry listener.
	prereq := "set -eu\nif ! command -v nginx >/dev/null; then command -v apt-get >/dev/null; sudo -n apt-get update -qq; sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y nginx; fi; sudo -n install -d /etc/nginx/sites-available /etc/nginx/sites-enabled; sudo -n nginx -t; if command -v ufw >/dev/null; then sudo -n ufw status; fi"
	if _, err := s.manager.Run(ctx, lb.ServerID, prereq); err != nil {
		return fmt.Errorf("Nginx prerequisite or existing configuration validation failed")
	}
	progress("Verify backend health from entry VPS")
	for _, b := range lb.Backends {
		if b.Status == "disabled" {
			continue
		}
		if err := s.checkBackend(ctx, lb, b); err != nil {
			return fmt.Errorf("backend %s:%d is unreachable from entry VPS; check application port and firewall", b.Host, b.Port)
		}
	}
	config, err := s.GenerateNginxConfig(lb)
	if err != nil {
		return err
	}
	if lb.SSL {
		progress("Verify existing TLS certificate")
		// Certificate provisioning remains owned by the existing Domains workflow.
		certPath := "/etc/letsencrypt/live/" + lb.Domain
		_, certErr := s.manager.Run(ctx, lb.ServerID, "sudo -n test -s "+shellQuote(certPath+"/fullchain.pem")+" && sudo -n test -s "+shellQuote(certPath+"/privkey.pem"))
		if certErr != nil {
			return fmt.Errorf("HTTPS requires an existing certificate in %s; provision the domain certificate in Domains first, then retry", certPath)
		}
	}
	available, enabled := lbConfigPaths(lb)
	progress("Validate candidate configuration, reload safely and verify routing")
	scheme := "http"
	if lb.SSL {
		scheme = "https"
	}
	endpoint := fmt.Sprintf("%s://%s:%d%s", scheme, lb.Domain, lb.Port, lb.HealthCheckPath)
	verify := "headers=$(curl --fail --silent --show-error --max-time 10 -D - -o /dev/null --resolve " + shellQuote(fmt.Sprintf("%s:%d:127.0.0.1", lb.Domain, lb.Port)) + " " + shellQuote(endpoint) + ")\nprintf '%s\\n' \"$headers\" | grep -Fqi -- " + shellQuote("X-GoPulse-Load-Balancer: "+lb.ID)
	// Reject a listener collision owned by another service, but permit an existing Nginx listener for virtual hosts.
	portCheck := fmt.Sprintf("sudo -n ss -H -ltnp 'sport = :%d' | grep -v nginx | grep -q . && exit 1 || true", lb.Port)
	if _, err := s.manager.Run(ctx, lb.ServerID, "command -v ss >/dev/null && ("+portCheck+")"); err != nil {
		return fmt.Errorf("incoming port inspection failed or port is occupied by another service")
	}
	if output, err := s.manager.Run(ctx, lb.ServerID, nginxApplyScript(config, available, enabled, false, "sudo -n nginx -t", "sudo -n systemctl reload-or-restart nginx", verify)); err != nil {
		step := "configuration staging"
		for _, candidate := range []string{"syntax", "reload", "routing"} {
			if strings.Contains(output, "GOPULSE_STEP="+candidate) {
				step = candidate
			}
		}
		return fmt.Errorf("%s failed; check Nginx syntax, service, competing virtual hosts and backend response; previous configuration restored when SSH remained available", step)
	}
	progress("Nginx syntax validated, service applied, backend health and local routing verified")
	progress("External DNS, TLS clients and firewall reachability require external verification")
	return nil
}
func (s *LoadBalancerService) checkBackend(ctx context.Context, lb models.LoadBalancer, b models.LoadBalancerBackend) error {
	host, port, err := s.resolveBackendTarget(b)
	if err != nil {
		return err
	}
	if !ValidTargetHost(host) || port < 1 || port > 65535 {
		return fmt.Errorf("invalid target")
	}
	endpoint := "http://" + net.JoinHostPort(host, fmt.Sprint(port)) + lb.HealthCheckPath
	_, err = s.manager.Run(ctx, lb.ServerID, "curl --fail --silent --show-error --max-time 5 "+shellQuote(endpoint)+" >/dev/null")
	return err
}
func (s *LoadBalancerService) GenerateNginxConfig(lb models.LoadBalancer) (string, error) {
	if err := ValidateLoadBalancer(lb); err != nil {
		return "", err
	}
	nginxTemplate := `# Upstream Definition
upstream gopulse_upstream_{{.ID}} {
    {{if ne .Algorithm "round-robin"}}{{.Algorithm}};{{end}}
    {{range .Backends}}
    server {{address .Host .Port}} weight={{.Weight}} max_fails=3 fail_timeout=10s {{if .Backup}}backup{{end}}{{if eq .Status "disabled"}}down{{end}};
    {{end}}
}

# HTTP Frontend
server {
    {{if .SSL}}listen {{.Port}} ssl;
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;{{else}}listen {{.Port}};{{end}}
    server_name {{.Domain}};
    proxy_hide_header X-GoPulse-Load-Balancer;
    add_header X-GoPulse-Load-Balancer {{.ID}} always;

    access_log /var/log/nginx/gopulse_lb_{{.ID}}_access.log;
    error_log /var/log/nginx/gopulse_lb_{{.ID}}_error.log;
    location /.well-known/acme-challenge/ { root /var/lib/gopulse/acme; }
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
        proxy_connect_timeout 5s;
        proxy_read_timeout 60s;
        proxy_next_upstream error timeout http_502 http_503 http_504;
    }
}
`
	tmpl, err := template.New("nginx_lb").Funcs(template.FuncMap{"address": func(host string, port int) string { return net.JoinHostPort(host, fmt.Sprint(port)) }}).Parse(nginxTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, lb); err != nil {
		return "", err
	}

	if lb.SSL {
		buf.WriteString(fmt.Sprintf("\nserver { listen 80; server_name %s; location /.well-known/acme-challenge/ { root /var/lib/gopulse/acme; } location / { return 301 https://$host$request_uri; } }\n", lb.Domain))
	}
	return buf.String(), nil
}

func (s *LoadBalancerService) StartHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lbs, err := s.store.ListAllLoadBalancers()
			if err != nil {
				log.Printf("LB health list failed: %v", err)
				continue
			}
			for _, lb := range lbs {
				if time.Since(last[lb.ID]) < time.Duration(lb.HealthCheckInterval)*time.Second {
					continue
				}
				last[lb.ID] = time.Now()
				for _, b := range lb.Backends {
					if b.Status == "disabled" {
						continue
					}
					status := "healthy"
					if err := s.checkBackend(ctx, lb, b); err != nil {
						status = "unhealthy"
					}
					if err := s.store.UpdateBackendStatus(b.ID, status); err != nil {
						log.Printf("LB health persistence failed: %v", err)
					}
				}
			}
		}
	}
}
func (s *LoadBalancerService) GetLogs(ctx context.Context, id string) (models.LoadBalancerLogs, error) {
	lb, err := s.GetLoadBalancer(id)
	if err != nil {
		return models.LoadBalancerLogs{}, err
	}
	if !safeResourceID.MatchString(id) {
		return models.LoadBalancerLogs{}, fmt.Errorf("invalid ID")
	}
	access, err := s.manager.Run(ctx, lb.ServerID, "sudo -n tail -n 100 "+shellQuote("/var/log/nginx/gopulse_lb_"+id+"_access.log"))
	if err != nil {
		return models.LoadBalancerLogs{}, fmt.Errorf("access logs unavailable")
	}
	errors, err := s.manager.Run(ctx, lb.ServerID, "sudo -n tail -n 100 "+shellQuote("/var/log/nginx/gopulse_lb_"+id+"_error.log"))
	if err != nil {
		return models.LoadBalancerLogs{}, fmt.Errorf("error logs unavailable")
	}
	return models.LoadBalancerLogs{LoadBalancerID: id, DeploymentError: lb.ErrorMessage, AccessLogs: access, ErrorLogs: errors, FetchedAt: time.Now()}, nil
}
func lbShellQuote(value string) string { return shellQuote(value) }

func (s *LoadBalancerService) BackendServer(b models.LoadBalancerBackend) (string, error) {
	if b.TargetProjectID != "" {
		p, err := s.projectStore.GetProject(b.TargetProjectID)
		if err != nil {
			return "", err
		}
		if b.TargetServerID != "" && b.TargetServerID != p.ServerID {
			return "", fmt.Errorf("project/server mismatch")
		}
		return p.ServerID, nil
	}
	return b.TargetServerID, nil
}
