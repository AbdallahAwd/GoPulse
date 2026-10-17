package services

import (
	"context"
	"fmt"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testLB() models.LoadBalancer {
	return models.LoadBalancer{ID: "lb-test", ServerID: "entry", Name: "Test", Domain: "example.test", Port: 8080, Algorithm: "round-robin", HealthCheckPath: "/", HealthCheckInterval: 10, Backends: []models.LoadBalancerBackend{{ID: "backend-1", Host: "127.0.0.1", Port: 3000, Weight: 1}}}
}
func TestLoadBalancerRejectsConfigInjection(t *testing.T) {
	for _, change := range []func(*models.LoadBalancer){func(lb *models.LoadBalancer) { lb.Domain = "x;touch /tmp/evil" }, func(lb *models.LoadBalancer) { lb.Algorithm = "least_conn; include /etc/passwd" }, func(lb *models.LoadBalancer) { lb.Backends[0].Host = "x\n}" }, func(lb *models.LoadBalancer) { lb.HealthCheckPath = "/;shutdown" }, func(lb *models.LoadBalancer) { lb.Port = 65536 }, func(lb *models.LoadBalancer) { lb.Backends = nil }, func(lb *models.LoadBalancer) { lb.Backends[0].Status = "disabled" }} {
		lb := testLB()
		change(&lb)
		if ValidateLoadBalancer(lb) == nil {
			t.Fatalf("accepted malicious/invalid config %+v", lb)
		}
	}
}
func TestLoadBalancerIPv6BackendRendering(t *testing.T) {
	lb := testLB()
	lb.Backends[0].Host = "::1"
	config, err := (&LoadBalancerService{}).GenerateNginxConfig(lb)
	if err != nil || !strings.Contains(config, "server [::1]:3000") {
		t.Fatalf("invalid IPv6 upstream: %v %s", err, config)
	}
}
func TestLoadBalancerTLSRenderingRequiresDNSAndCertificatePaths(t *testing.T) {
	lb := testLB()
	lb.SSL = true
	lb.Port = 443
	config, err := (&LoadBalancerService{}).GenerateNginxConfig(lb)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"listen 443 ssl", "ssl_certificate /etc/letsencrypt/live/example.test/fullchain.pem", "ssl_certificate_key /etc/letsencrypt/live/example.test/privkey.pem", "return 301 https://$host$request_uri"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("missing TLS directive %s", expected)
		}
	}
	lb.Domain = "127.0.0.1"
	if _, err := (&LoadBalancerService{}).GenerateNginxConfig(lb); err == nil {
		t.Fatal("TLS IP accepted")
	}
}
func TestNginxScriptRollsBackSyntaxReloadAndRouting(t *testing.T) {
	for _, failure := range []string{"syntax", "reload", "routing"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			available := filepath.Join(dir, "available")
			enabled := filepath.Join(dir, "enabled")
			os.WriteFile(available, []byte("previous"), 0600)
			os.Symlink(available, enabled)
			// sudo fixture executes commands as the current unprivileged user, exclusively inside t.TempDir.
			shim := filepath.Join(dir, "sudo")
			os.WriteFile(shim, []byte("#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n"), 0700)
			validate, reload, verify := "true", "true", "true"
			switch failure {
			case "syntax":
				validate = "false"
			case "reload":
				reload = "false"
			case "routing":
				verify = "false"
			}
			cmd := exec.Command("sh", "-c", nginxApplyScript("candidate", available, enabled, false, validate, reload, verify))
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("expected %s failure: %s", failure, out)
			}
			got, _ := os.ReadFile(available)
			if string(got) != "previous" {
				t.Fatalf("previous config lost: %s", got)
			}
			target, err := os.Readlink(enabled)
			if err != nil || target != available {
				t.Fatalf("symlink not restored: %s %v", target, err)
			}
		})
	}
}
func TestLoadBalancerFailedUpdateKeepsBackends(t *testing.T) {
	dir := t.TempDir()
	lbs, _ := store.NewLoadBalancerStore(filepath.Join(dir, "load_balancers.json"))
	servers, _ := store.NewServerStore(filepath.Join(dir, "servers.json"))
	servers.AddServer(models.Server{ID: "entry", Host: "10.0.0.1"})
	projects, _ := store.NewProjectStore(filepath.Join(dir, "projects.json"))
	lb := testLB()
	lb.Status = "active"
	if err := lbs.AddLoadBalancer(lb); err != nil {
		t.Fatal(err)
	}
	runner := &fixtureExecutor{run: func(_, command string) (string, error) {
		if strings.Contains(command, "trap rollback") {
			return "", fmt.Errorf("syntax failure")
		}
		return "ok", nil
	}}
	svc := NewLoadBalancerService(lbs, servers, projects, &ServiceManager{executor: runner})
	lb.Backends[0].Port = 4000
	if err := svc.Update(context.Background(), lb, func(string) {}); err == nil {
		t.Fatal("expected failure")
	}
	saved, err := lbs.GetLoadBalancer(lb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Backends[0].Port != 3000 || saved.Status != "active" {
		t.Fatalf("configuration changed on failure: %+v", saved)
	}
}

// Real local Nginx and two real HTTP backends; no SSH, systemd, DNS, TLS or firewall effects.
func TestNginxLocalTrafficAndBackendFailure(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("local nginx unavailable")
	}
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "first") }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "second") }))
	defer second.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	lb := testLB()
	lb.Port = port
	lb.Backends = nil
	for i, server := range []*httptest.Server{first, second} {
		u, _ := url.Parse(server.URL)
		host, p, _ := net.SplitHostPort(u.Host)
		n, _ := strconv.Atoi(p)
		lb.Backends = append(lb.Backends, models.LoadBalancerBackend{ID: fmt.Sprintf("backend-%d", i), Host: host, Port: n, Weight: 1})
	}
	svc := NewLoadBalancerService(nil, nil, nil, nil)
	rendered, err := svc.GenerateNginxConfig(lb)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rendered = strings.ReplaceAll(rendered, "/var/log/nginx", dir)
	rendered = strings.ReplaceAll(rendered, "/var/lib/gopulse/acme", dir)
	config := fmt.Sprintf("daemon off; master_process off; pid %s; error_log %s; events { worker_connections 128; } http { access_log off; client_body_temp_path %s; proxy_temp_path %s; %s }", filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "error.log"), dir, dir, rendered)
	path := filepath.Join(dir, "nginx.conf")
	os.WriteFile(path, []byte(config), 0600)
	if out, err := exec.Command(binary, "-t", "-c", path, "-p", dir).CombinedOutput(); err != nil {
		t.Fatalf("nginx syntax: %v %s", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "-c", path, "-p", dir)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	client := &http.Client{Timeout: time.Second}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/", port)
	get := func() (string, error) {
		req, _ := http.NewRequest("GET", endpoint, nil)
		req.Host = lb.Domain
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if resp.Header.Get("X-GoPulse-Load-Balancer") != lb.ID {
			return "", fmt.Errorf("request reached a different virtual host")
		}
		return string(raw), nil
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := get(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nginx did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		body, err := get()
		if err != nil {
			t.Fatal(err)
		}
		seen[body] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("round robin did not route to both backends: %v", seen)
	}
	first.Close()
	for i := 0; i < 10; i++ {
		body, err := get()
		if err != nil || body != "second" {
			t.Fatalf("backend failure routing: %s %v", body, err)
		}
	}
}
func TestLoadBalancerDuplicateDomainRejectedBeforeRemoteExecution(t *testing.T) {
	dir := t.TempDir()
	lbs, _ := store.NewLoadBalancerStore(filepath.Join(dir, "load_balancers.json"))
	lb := testLB()
	if err := lbs.AddLoadBalancer(lb); err != nil {
		t.Fatal(err)
	}
	runner := &fixtureExecutor{run: func(string, string) (string, error) { t.Fatal("duplicate domain executed remotely"); return "", nil }}
	servers, err := store.NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.AddServer(models.Server{ID: "entry", Host: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	svc := NewLoadBalancerService(lbs, servers, nil, &ServiceManager{executor: runner})
	lb.ID = "lb-second"
	if err := svc.Create(context.Background(), lb, func(string) {}); err == nil {
		t.Fatal("duplicate domain accepted")
	}
}
