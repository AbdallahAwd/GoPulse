package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

const DashboardIdentityPath = "/.well-known/gopulse-dashboard"
const dashboardNginxPath = "/etc/nginx/conf.d/gopulse-dashboard.conf"

type DashboardHostExecutor interface {
	Run(context.Context, string) (string, error)
}
type localDashboardHost struct{}

func (localDashboardHost) Run(ctx context.Context, script string) (string, error) {
	// Fixed scripts with validated inputs; never accept an arbitrary command API.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cleanup := configureDashboardCommand(cmd)
	defer cleanup()
	var out dashboardCommandOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

type dashboardCommandOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *dashboardCommandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := 65536 - len(b.data)
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		b.data = append(b.data, p[:remaining]...)
	}
	return len(p), nil
}
func (b *dashboardCommandOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

type DashboardDomainService struct {
	Store      *store.DashboardSettingsStore
	Operations *Operations
	Executor   DashboardHostExecutor
	OS         string
	Upstream   string
	Identity   string
	Lookup     func(context.Context, string) ([]net.IPAddr, error)
	mu         sync.Mutex
}

func NewDashboardDomainService(st *store.DashboardSettingsStore, operations *Operations, host string, port int) (*DashboardDomainService, error) {
	identity, err := st.PublicIdentity()
	if err != nil {
		return nil, err
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid dashboard listening port")
	}
	upstreamHost := host
	if host == "" || host == "0.0.0.0" {
		upstreamHost = "127.0.0.1"
	}
	if host == "::" {
		upstreamHost = "::1"
	}
	if host == "localhost" {
		upstreamHost = "127.0.0.1"
	}
	if !ValidTargetHost(upstreamHost) {
		return nil, fmt.Errorf("invalid dashboard bind host for domain setup")
	}
	return &DashboardDomainService{Store: st, Operations: operations, Executor: localDashboardHost{}, OS: runtime.GOOS, Upstream: net.JoinHostPort(upstreamHost, fmt.Sprint(port)), Identity: identity, Lookup: net.DefaultResolver.LookupIPAddr}, nil
}

func ValidateDashboardDomain(input models.DashboardDomainInput) (models.DashboardDomainInput, error) {
	input.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Domain), "."))
	input.Email, input.ExpectedIP = strings.TrimSpace(input.Email), strings.TrimSpace(input.ExpectedIP)
	if !ValidTargetHost(input.Domain) || net.ParseIP(input.Domain) != nil || !strings.Contains(input.Domain, ".") || len(input.Domain) > 253 {
		return input, fmt.Errorf("enter a hostname such as dashboard.example.com, without a scheme, port, path, wildcard, or IP address")
	}
	if input.ExpectedIP != "" {
		ip := net.ParseIP(input.ExpectedIP)
		if ip == nil || !publicDashboardIP(ip) {
			return input, fmt.Errorf("expected DNS address must be a public IPv4 or IPv6 address")
		}
		input.ExpectedIP = ip.String()
	}
	if input.HTTPS {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email || len(input.Email) > 254 || strings.ContainsAny(input.Email, "\r\n") {
			return input, fmt.Errorf("enter a valid certificate contact email")
		}
		if !input.AgreeTerms {
			return input, fmt.Errorf("accept the Let's Encrypt subscriber agreement before requesting HTTPS")
		}
	}
	return input, nil
}

func (s *DashboardDomainService) Save(input models.DashboardDomainInput) (models.DashboardDomainSettings, error) {
	if !s.mu.TryLock() {
		return models.DashboardDomainSettings{}, fmt.Errorf("domain setup is busy; wait for the current operation")
	}
	defer s.mu.Unlock()
	validated, err := ValidateDashboardDomain(input)
	if err != nil {
		return models.DashboardDomainSettings{}, err
	}
	settings, err := s.Store.Domain()
	if err != nil {
		return settings, err
	}
	if settings.Domain != "" && settings.DashboardDomainInput == validated {
		return settings, nil
	}
	settings.DashboardDomainInput, settings.Revision, settings.UpdatedAt = validated, uuid.NewString(), time.Now().UTC()
	settings.Check, settings.LastError = models.DashboardDomainCheck{}, ""
	err = s.Store.SaveDomain(settings)
	return settings, err
}

func (s *DashboardDomainService) PublicURL() string {
	settings, err := s.Store.Domain()
	if err != nil || settings.AppliedDomain == "" {
		return ""
	}
	scheme := "http"
	if settings.AppliedHTTPS {
		scheme = "https"
	}
	return scheme + "://" + settings.AppliedDomain
}

func publicDashboardIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	address, err := netip.ParseAddr(ip.String())
	if err != nil {
		return false
	}
	address = address.Unmap()
	for _, block := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(block).Contains(address) {
			return false
		}
	}
	return true
}

// Public verification pins the connection to the addresses just resolved. It
// sends no cookies/credentials, follows no redirects, and never dials private IPs.
func (s *DashboardDomainService) Check(ctx context.Context) (models.DashboardDomainCheck, error) {
	if !s.mu.TryLock() {
		return models.DashboardDomainCheck{}, fmt.Errorf("domain setup is busy")
	}
	defer s.mu.Unlock()
	settings, err := s.Store.Domain()
	if err != nil {
		return models.DashboardDomainCheck{}, err
	}
	if settings.Domain == "" {
		return models.DashboardDomainCheck{}, fmt.Errorf("save a dashboard domain first")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	check := models.DashboardDomainCheck{Addresses: []string{}, CheckedAt: time.Now().UTC()}
	addresses, err := s.Lookup(ctx, settings.Domain)
	if err != nil || len(addresses) == 0 {
		check.Message = "DNS does not resolve yet. Add the master host's A/AAAA record and check again."
	} else {
		check.DNSResolved = true
		seen := map[string]bool{}
		public := true
		for _, item := range addresses {
			ip := item.IP.String()
			if !seen[ip] {
				check.Addresses = append(check.Addresses, ip)
				seen[ip] = true
			}
			if ip == settings.ExpectedIP {
				check.ExpectedMatch = true
			}
			public = public && publicDashboardIP(item.IP)
		}
		sort.Strings(check.Addresses)
		if settings.ExpectedIP != "" && !check.ExpectedMatch {
			check.Message = "DNS resolves, but it does not include the expected master IP. Correct the DNS record before applying."
		} else if !public {
			check.Message = "DNS points to a private or reserved address. Public connection checks are unavailable."
		} else {
			check = s.probePublic(ctx, settings, check)
		}
	}
	settings.Check = check
	if err := s.Store.SaveDomain(settings); err != nil {
		return check, err
	}
	return check, nil
}

func (s *DashboardDomainService) probePublic(ctx context.Context, settings models.DashboardDomainSettings, check models.DashboardDomainCheck) models.DashboardDomainCheck {
	scheme, port := "http", "80"
	if settings.HTTPS {
		scheme, port = "https", "443"
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		var last error
		for _, ip := range check.Addresses {
			conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, "GET", scheme+"://"+settings.Domain+DashboardIdentityPath, nil)
	response, err := client.Do(req)
	if err != nil {
		check.Message = "DNS resolves, but the dashboard is not reachable with a valid connection at the configured domain. Check ports 80/443, DNS, routing, and certificates."
		return check
	}
	defer response.Body.Close()
	var identity struct {
		InstanceID string `json:"instance_id"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&identity) != nil || identity.InstanceID != s.Identity {
		check.Message = "The domain responds, but it does not reach this GoPulse master dashboard."
		return check
	}
	check.DashboardMatch = true
	check.Message = "Verified: this domain reaches your master dashboard."
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		check.CertificateEnd = response.TLS.PeerCertificates[0].NotAfter
	}
	return check
}

// DashboardNginxConfig keeps WebSockets and streamed API/MCP responses working.
func DashboardNginxConfig(input models.DashboardDomainInput, upstream string) string {
	proxy := fmt.Sprintf(`    client_max_body_size 1g;
    location / {
        proxy_pass http://%s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $gopulse_dashboard_connection;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
`, upstream)
	header := "# Managed by GoPulse: master dashboard only.\nmap $http_upgrade $gopulse_dashboard_connection {\n    default upgrade;\n    '' close;\n}\n"
	challenge := "    location ^~ /.well-known/acme-challenge/ {\n        root /var/lib/gopulse/acme;\n        try_files $uri =404;\n    }\n"
	if !input.HTTPS {
		return header + fmt.Sprintf("server {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n%s%s}\n", input.Domain, challenge, proxy)
	}
	cert := "/etc/letsencrypt/live/gopulse-dashboard-" + input.Domain
	return header + fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
%s    location / { return 308 https://%s$request_uri; }
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name %s;
    ssl_certificate %s/fullchain.pem;
    ssl_certificate_key %s/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
%s}
`, input.Domain, challenge, input.Domain, input.Domain, cert, cert, proxy)
}

func (s *DashboardDomainService) Deploy(owner string) (models.Operation, error) {
	if !s.mu.TryLock() {
		return models.Operation{}, fmt.Errorf("domain setup is busy")
	}
	settings, err := s.Store.Domain()
	if err == nil {
		_, err = ValidateDashboardDomain(settings.DashboardDomainInput)
	}
	if err == nil && s.OS != "linux" {
		err = fmt.Errorf("managed setup requires a Linux master host with systemd and non-interactive sudo; use the Nginx preview for other environments")
	}
	if err != nil {
		s.mu.Unlock()
		return models.Operation{}, err
	}
	ready := make(chan struct{})
	var trackingErr error
	op, err := s.Operations.Submit(owner, "", "dashboard-domain", "setup_dashboard_domain", "*", false, func(ctx context.Context, step func(string)) (interface{}, error) {
		defer s.mu.Unlock()
		<-ready
		if trackingErr != nil {
			return nil, fmt.Errorf("operation metadata could not be saved; no host action executed")
		}
		result, applyErr := s.apply(ctx, settings, step)
		current, readErr := s.Store.Domain()
		if readErr != nil {
			return result, fmt.Errorf("read settings after setup: %w", readErr)
		}
		if applyErr != nil {
			current.LastError = applyErr.Error()
		} else {
			current.AppliedDomain = settings.Domain
			current.AppliedRevision = settings.Revision
			current.AppliedHTTPS = settings.HTTPS
			current.AppliedAt = time.Now().UTC()
			current.LastError = ""
			current.Check = models.DashboardDomainCheck{}
		}
		if saveErr := s.Store.SaveDomain(current); saveErr != nil {
			return result, fmt.Errorf("routing outcome could not be saved; inspect host configuration: %w", saveErr)
		}
		return result, applyErr
	})
	if err != nil {
		s.mu.Unlock()
		return op, err
	}
	settings.OperationID = op.ID
	// The worker cannot pass the command boundary until this operation ID is saved.
	trackingErr = s.Store.SaveDomain(settings)
	close(ready)
	if trackingErr != nil {
		return op, fmt.Errorf("operation tracking metadata could not be saved; no host action will execute: %w", trackingErr)
	}
	return op, nil
}

func (s *DashboardDomainService) apply(ctx context.Context, settings models.DashboardDomainSettings, step func(string)) (interface{}, error) {
	step("Check Linux host, privileges, DNS, and existing routing")
	addresses, err := s.Lookup(ctx, settings.Domain)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("dashboard domain does not resolve; configure DNS before applying")
	}
	match := settings.ExpectedIP == ""
	for _, address := range addresses {
		if !publicDashboardIP(address.IP) {
			return nil, fmt.Errorf("HTTPS/public dashboard setup requires public DNS addresses")
		}
		if address.IP.String() == settings.ExpectedIP {
			match = true
		}
	}
	if !match {
		return nil, fmt.Errorf("DNS does not include the expected master IP")
	}
	prereq := `set -eu
sudo -n true
command -v systemctl >/dev/null
test -d /run/systemd/system
command -v curl >/dev/null
if ! command -v nginx >/dev/null; then
    command -v apt-get >/dev/null
    sudo -n apt-get update -qq
    sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y nginx
fi
sudo -n nginx -t
sudo -n nginx -T 2>&1
`
	output, err := s.Executor.Run(ctx, prereq)
	if err != nil {
		return nil, fmt.Errorf("host prerequisites failed: Linux/systemd, curl, Nginx, and root or passwordless sudo are required; inspect the host's Nginx configuration and package manager")
	}
	if dashboardDomainConflict(output, settings.Domain) {
		return nil, fmt.Errorf("this hostname is already configured in another Nginx file; remove that conflicting virtual host before applying")
	}
	if !strings.Contains(output, "# configuration file /etc/nginx/conf.d/") && !strings.Contains(output, "include /etc/nginx/conf.d/") {
		return nil, fmt.Errorf("Nginx must include /etc/nginx/conf.d/*.conf inside its http block")
	}
	if settings.HTTPS {
		step("Prepare certificate tools and automatic renewal")
		output, err = s.Executor.Run(ctx, `set -eu
if ! command -v certbot >/dev/null; then
    command -v apt-get >/dev/null
    sudo -n apt-get update -qq
    sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y certbot
fi
sudo -n systemctl cat certbot.timer >/dev/null
`)
		if err != nil {
			return nil, fmt.Errorf("certificate prerequisites failed: install Certbot with certbot.timer; %s", strings.TrimSpace(output))
		}
	}
	step("Apply dashboard routing, validate Nginx, and provision HTTPS")
	output, err = s.Executor.Run(ctx, dashboardApplyScript(settings, s.Upstream, s.Identity))
	if err != nil {
		return nil, fmt.Errorf("domain setup failed; previous dashboard Nginx file is restored when the host command remains available. Inspect host routing before retrying: %s", strings.TrimSpace(output))
	}
	if settings.HTTPS {
		step("HTTPS dashboard route verified locally; certificate renewal enabled")
	} else {
		step("HTTP dashboard route verified locally")
	}
	scheme := "http"
	if settings.HTTPS {
		scheme = "https"
	}
	return map[string]interface{}{"url": scheme + "://" + settings.Domain, "mcp_url": scheme + "://" + settings.Domain + "/api/mcp", "https": settings.HTTPS}, nil
}

func dashboardDomainConflict(dump, domain string) bool {
	for _, section := range strings.Split(dump, "# configuration file ") {
		if strings.HasPrefix(section, dashboardNginxPath+":") {
			continue
		}
		lines := strings.Split(section, "\n")
		for i, line := range lines {
			lines[i] = strings.SplitN(line, "#", 2)[0]
		}
		for _, directive := range dashboardServerNames.FindAllStringSubmatch(strings.Join(lines, "\n"), -1) {
			for _, name := range strings.Fields(directive[1]) {
				if strings.EqualFold(strings.Trim(name, "\"'"), domain) {
					return true
				}
			}
		}
	}
	return false
}

var dashboardServerNames = regexp.MustCompile(`(?m)(?:^|[{};])\s*server_name\s+([^;]+);`)

func dashboardApplyScript(settings models.DashboardDomainSettings, upstream, identity string) string {
	input := settings.DashboardDomainInput
	encode := func(config string) string { return shellQuote(base64.StdEncoding.EncodeToString([]byte(config))) }
	bootstrap := input
	bootstrap.HTTPS = false
	write := func(config string) string {
		return "printf %s " + encode(config) + " | base64 -d | sudo -n tee " + shellQuote(dashboardNginxPath) + " >/dev/null\nsudo -n nginx -t\nif ! sudo -n systemctl is-active --quiet nginx; then sudo -n systemctl start nginx; fi\nsudo -n systemctl reload nginx\n"
	}
	verifyHTTP := "curl --noproxy '*' --fail --silent --max-time 10 -H " + shellQuote("Host: "+input.Domain) + " " + shellQuote("http://127.0.0.1"+DashboardIdentityPath) + " | grep -F " + shellQuote(identity) + " >/dev/null\n"
	// One outer rollback spans HTTP bootstrap, certificate issuance, final TLS
	// configuration and verification. Certbot never edits other virtual hosts.
	script := `set -eu
backup=$(mktemp -d)
had_file=0
if sudo -n test -e /etc/nginx/conf.d/gopulse-dashboard.conf || sudo -n test -L /etc/nginx/conf.d/gopulse-dashboard.conf; then
    sudo -n test ! -L /etc/nginx/conf.d/gopulse-dashboard.conf
    sudo -n cp -a /etc/nginx/conf.d/gopulse-dashboard.conf "$backup/dashboard.conf"
    had_file=1
fi
rollback() {
    trap - EXIT HUP INT TERM
    sudo -n rm -f /etc/nginx/conf.d/gopulse-dashboard.conf
    if [ "$had_file" = 1 ]; then sudo -n cp -a "$backup/dashboard.conf" /etc/nginx/conf.d/gopulse-dashboard.conf; fi
    sudo -n nginx -t && sudo -n systemctl reload nginx || true
    sudo -n rm -rf -- "$backup"
}
trap rollback EXIT HUP INT TERM
sudo -n install -d -m 0755 /etc/nginx/conf.d /var/lib/gopulse/acme/.well-known/acme-challenge
`
	// Keep the previous HTTPS host available during a domain migration: add its
	// existing server blocks to the temporary HTTP bootstrap with one shared map.
	bootstrapConfig := DashboardNginxConfig(bootstrap, upstream)
	if input.HTTPS {
		// Before the certificate exists, expose only ACME and the public instance
		// probe over HTTP. Login/API traffic must never cross plaintext bootstrap.
		proxyStart := strings.Index(bootstrapConfig, "    client_max_body_size")
		bootstrapConfig = bootstrapConfig[:proxyStart] + fmt.Sprintf(`    location = %s {
        proxy_pass http://%s;
        proxy_set_header Host $host;
    }
    location / { return 308 https://%s$request_uri; }
}
`, DashboardIdentityPath, upstream, input.Domain)
	}
	if settings.AppliedDomain != "" {
		prior := DashboardNginxConfig(models.DashboardDomainInput{Domain: settings.AppliedDomain, HTTPS: settings.AppliedHTTPS}, upstream)
		if settings.AppliedDomain != input.Domain {
			bootstrapConfig += prior[strings.Index(prior, "server {"):]
		} else if settings.AppliedHTTPS {
			bootstrapConfig += prior[strings.Index(prior, "server {\n    listen 443 ssl;"):]
		}
	}
	script += write(bootstrapConfig) + verifyHTTP
	if input.HTTPS {
		script += "sudo -n certbot certonly --webroot --webroot-path /var/lib/gopulse/acme --cert-name " + shellQuote("gopulse-dashboard-"+input.Domain) + " --domain " + shellQuote(input.Domain) + " --email " + shellQuote(input.Email) + " --non-interactive --agree-tos --keep-until-expiring --deploy-hook 'nginx -t && systemctl reload nginx'\n"
		script += write(DashboardNginxConfig(input, upstream))
		script += "curl --noproxy '*' --fail --silent --max-time 10 --resolve " + shellQuote(input.Domain+":443:127.0.0.1") + " " + shellQuote("https://"+input.Domain+DashboardIdentityPath) + " | grep -F " + shellQuote(identity) + " >/dev/null\n"
		script += "sudo -n systemctl enable --now certbot.timer\n"
	}
	script += "trap - EXIT HUP INT TERM\nsudo -n rm -rf -- \"$backup\"\n"
	return script
}
