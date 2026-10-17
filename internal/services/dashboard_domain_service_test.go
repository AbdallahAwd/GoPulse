package services

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

func domainFixture(t *testing.T) *DashboardDomainService {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewDashboardSettingsStore(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	opStore, err := store.NewOperationStore(filepath.Join(dir, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := NewOperations(opStore)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	s, err := NewDashboardDomainService(st, ops, "0.0.0.0", 2886)
	if err != nil {
		t.Fatal(err)
	}
	s.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	return s
}
func domainInput() models.DashboardDomainInput {
	return models.DashboardDomainInput{Domain: "dashboard.example.com", HTTPS: true, Email: "admin@example.com", AgreeTerms: true}
}

func TestDashboardDomainValidationAndPersistence(t *testing.T) {
	s := domainFixture(t)
	for _, domain := range []string{"https://example.com", "example.com/path", "example.com:443", "*.example.com", "-bad.example.com", "bad..example.com", "127.0.0.1", "example.com;id", "example.com\nserver"} {
		input := domainInput()
		input.Domain = domain
		if _, err := s.Save(input); err == nil {
			t.Fatal("invalid domain accepted", domain)
		}
	}
	input := domainInput()
	input.AgreeTerms = false
	if _, err := s.Save(input); err == nil {
		t.Fatal("TLS without consent accepted")
	}
	input = domainInput()
	input.Email = "bad"
	if _, err := s.Save(input); err == nil {
		t.Fatal("bad certificate email accepted")
	}
	input = domainInput()
	input.ExpectedIP = "127.0.0.1"
	if _, err := s.Save(input); err == nil {
		t.Fatal("private DNS target accepted")
	}
	input = domainInput()
	input.Domain = " Dashboard.Example.com. "
	saved, err := s.Save(input)
	if err != nil || saved.Domain != "dashboard.example.com" || saved.Revision == "" {
		t.Fatal(saved, err)
	}
	again, err := s.Save(input)
	if err != nil || again.Revision != saved.Revision {
		t.Fatal("unchanged save reset revision", err)
	}
	loaded, err := s.Store.Domain()
	if err != nil || loaded.Domain != saved.Domain || s.PublicURL() != "" {
		t.Fatal("saved domain incorrectly published before deployment", err)
	}
	identity, err := s.Store.PublicIdentity()
	if err != nil || identity != s.Identity {
		t.Fatal("public instance identity changed", err)
	}
}

func TestDashboardDomainCheckDoesNotDialPrivateOrWrongTargets(t *testing.T) {
	s := domainFixture(t)
	if _, err := s.Save(domainInput()); err != nil {
		t.Fatal(err)
	}
	s.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	check, err := s.Check(context.Background())
	if err != nil || !check.DNSResolved || check.DashboardMatch || !strings.Contains(check.Message, "private") {
		t.Fatal(check, err)
	}
	for _, address := range []string{"169.254.169.254", "100.64.1.2", "10.0.0.1", "192.0.2.1", "::1", "64:ff9b::a9fe:a9fe"} {
		if publicDashboardIP(net.ParseIP(address)) {
			t.Fatal("reserved destination accepted", address)
		}
	}
	input := domainInput()
	input.ExpectedIP = "8.8.4.4"
	if _, err := s.Save(input); err != nil {
		t.Fatal(err)
	}
	s.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	check, err = s.Check(context.Background())
	if err != nil || check.ExpectedMatch || check.DashboardMatch || !strings.Contains(check.Message, "expected") {
		t.Fatal(check, err)
	}
	s.Lookup = func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("missing") }
	check, err = s.Check(context.Background())
	if err != nil || check.DNSResolved {
		t.Fatal(check, err)
	}
}

type domainFakeHost struct {
	mu      sync.Mutex
	scripts []string
	fail    bool
}

func (e *domainFakeHost) Run(_ context.Context, script string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scripts = append(e.scripts, script)
	if strings.Contains(script, "nginx -T") {
		return "# configuration file /etc/nginx/nginx.conf:\nhttp { include /etc/nginx/conf.d/*.conf; }", nil
	}
	if e.fail && strings.Contains(script, "backup=$(mktemp") {
		return "fixture certificate failure", errors.New("failed")
	}
	return "", nil
}
func waitDomain(t *testing.T, s *DashboardDomainService, id string) models.Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, err := s.Operations.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status != "running" {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return models.Operation{}
}

func TestDashboardDomainDeploymentPublishesOnlySuccessfulConfiguration(t *testing.T) {
	s := domainFixture(t)
	s.OS = "linux"
	host := &domainFakeHost{}
	s.Executor = host
	if _, err := s.Save(domainInput()); err != nil {
		t.Fatal(err)
	}
	op, err := s.Deploy("admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDomain(t, s, op.ID); done.Status != "completed" {
		t.Fatal(done)
	}
	saved, err := s.Store.Domain()
	if err != nil || saved.AppliedRevision != saved.Revision || saved.OperationID != op.ID || s.PublicURL() != "https://dashboard.example.com" {
		t.Fatal(saved, err)
	}
	input := domainInput()
	input.Domain = "new.example.com"
	if _, err := s.Save(input); err != nil {
		t.Fatal(err)
	}
	host.fail = true
	op, err = s.Deploy("admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDomain(t, s, op.ID); done.Status != "failed" {
		t.Fatal(done)
	}
	saved, _ = s.Store.Domain()
	if s.PublicURL() != "https://dashboard.example.com" || saved.LastError == "" || saved.AppliedRevision == saved.Revision {
		t.Fatal("failed setup published desired domain", saved)
	}
}

func TestDashboardDomainPreviewAndPlatformRestriction(t *testing.T) {
	s := domainFixture(t)
	host := &domainFakeHost{}
	s.Executor = host
	s.OS = "darwin"
	if _, err := s.Save(domainInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy("admin"); err == nil || len(host.scripts) > 0 {
		t.Fatal("non-Linux setup executed")
	}
	config := DashboardNginxConfig(domainInput(), "[::1]:2886")
	for _, fragment := range []string{"proxy_pass http://[::1]:2886", "proxy_buffering off", "proxy_set_header Upgrade", "location ^~ /.well-known/acme-challenge/", "listen 443 ssl", "TLSv1.2 TLSv1.3"} {
		if !strings.Contains(config, fragment) {
			t.Fatal("missing proxy support", fragment)
		}
	}
	if !dashboardDomainConflict("# configuration file /etc/nginx/conf.d/other.conf:\nserver_name dashboard.example.com;", domainInput().Domain) {
		t.Fatal("conflicting virtual host ignored")
	}
	if dashboardDomainConflict("# configuration file "+dashboardNginxPath+":\nserver_name dashboard.example.com;", domainInput().Domain) {
		t.Fatal("owned virtual host reported as conflicting")
	}
	for _, configuration := range []string{"server { server_name dashboard.example.com; }", "server_name\n\tdashboard.example.com other.example.com;", "server_name \"dashboard.example.com\";"} {
		if !dashboardDomainConflict("# configuration file /etc/nginx/conf.d/other.conf:\n"+configuration, domainInput().Domain) {
			t.Fatal("formatted hostname conflict ignored", configuration)
		}
	}
}

func TestDashboardTLSBootstrapExposesOnlyThePublicProbe(t *testing.T) {
	script := dashboardApplyScript(models.DashboardDomainSettings{DashboardDomainInput: domainInput(), AppliedDomain: "old.example.com", AppliedHTTPS: true}, "127.0.0.1:2886", "fixture-identity")
	encoded := regexp.MustCompile(`printf %s '([A-Za-z0-9+/=]+)'`).FindStringSubmatch(script)
	if len(encoded) != 2 {
		t.Fatal("bootstrap configuration missing")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded[1])
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	if !strings.Contains(config, "location = "+DashboardIdentityPath) || !strings.Contains(config, "location / { return 308 https://dashboard.example.com") {
		t.Fatal("plaintext bootstrap exposed dashboard API", config)
	}
	if !strings.Contains(config, "server_name old.example.com") || strings.Count(config, "map $http_upgrade") != 1 {
		t.Fatal("existing HTTPS route not retained during bootstrap", config)
	}
}

// Execute the real deployment script against temporary files and fake host
// binaries. Nothing reaches Nginx, systemd, Certbot, DNS, or managed resources.
func TestDashboardDomainScriptRollbackAndSuccess(t *testing.T) {
	for _, failure := range []string{"certificate", "syntax", "identity", ""} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(root, "etc/nginx/conf.d/gopulse-dashboard.conf")
			if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
				t.Fatal(err)
			}
			original := "ORIGINAL ROUTING\n"
			if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"sudo":      "#!/bin/sh\n[ \"$1\" != -n ] || shift\nexec \"$@\"\n",
				"systemctl": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOMAIN_LOG\"\n",
				"certbot":   "#!/bin/sh\n[ \"$DOMAIN_FAIL\" != certificate ]\n",
				"nginx":     "#!/bin/sh\nif [ \"$DOMAIN_FAIL\" = syntax ] && grep -q 'listen 443 ssl' \"$DOMAIN_CONFIG\"; then exit 1; fi\n",
				"curl":      "#!/bin/sh\n[ \"$DOMAIN_FAIL\" != identity ] || { printf '%s' 'wrong instance'; exit 0; }\nprintf '%s' '{\"instance_id\":\"fixture-identity\"}'\n",
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			settings := models.DashboardDomainSettings{DashboardDomainInput: domainInput()}
			script := dashboardApplyScript(settings, "127.0.0.1:2886", "fixture-identity")
			script = strings.ReplaceAll(script, "/etc/nginx/", root+"/etc/nginx/")
			script = strings.ReplaceAll(script, "/var/lib/gopulse/", root+"/var/lib/gopulse/")
			cmd := exec.Command("/bin/sh", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "DOMAIN_FAIL="+failure, "DOMAIN_CONFIG="+configPath, "DOMAIN_LOG="+filepath.Join(root, "commands.log"))
			output, err := cmd.CombinedOutput()
			if (err != nil) != (failure != "") {
				t.Fatalf("script outcome %v: %s", err, output)
			}
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if failure != "" && string(config) != original {
				t.Fatal("previous routing was not restored", string(config))
			}
			if failure == "" && !strings.Contains(string(config), "listen 443 ssl") {
				t.Fatal("successful configuration missing HTTPS")
			}
			commands, _ := os.ReadFile(filepath.Join(root, "commands.log"))
			if strings.Contains(string(commands), "restart") || strings.Contains(string(commands), "stop") {
				t.Fatal("setup restarted/stopped a service", string(commands))
			}
		})
	}
}
