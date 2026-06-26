package services

import (
	"strings"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

func TestGenerateNginxConfig(t *testing.T) {
	service := NewLoadBalancerService(nil, nil, nil, nil)

	lb := models.LoadBalancer{
		ID:                  "lb-123",
		ServerID:            "srv-1",
		Name:                "Test LB",
		Domain:              "lb.example.com",
		Port:                80,
		Algorithm:           "least_conn",
		SSL:                 false,
		HealthCheckPath:     "/health",
		HealthCheckInterval: 10,
		Status:              "active",
		CreatedAt:           time.Now(),
		Backends: []models.LoadBalancerBackend{
			{
				ID:             "b-1",
				LoadBalancerID: "lb-123",
				Host:           "10.0.0.1",
				Port:           3000,
				Weight:         3,
				Backup:         false,
				Status:         "healthy",
			},
			{
				ID:             "b-2",
				LoadBalancerID: "lb-123",
				Host:           "10.0.0.2",
				Port:           3000,
				Weight:         1,
				Backup:         true,
				Status:         "healthy",
			},
		},
	}

	got, err := service.GenerateNginxConfig(lb)
	if err != nil {
		t.Fatalf("GenerateNginxConfig failed: %v", err)
	}

	// Verify upstream name
	if !strings.Contains(got, "upstream gopulse_upstream_lb-123") {
		t.Errorf("config does not contain correct upstream name")
	}

	// Verify algorithm
	if !strings.Contains(got, "least_conn;") {
		t.Errorf("config does not contain algorithm directive: least_conn;")
	}

	// Verify backends
	if !strings.Contains(got, "server 10.0.0.1:3000 weight=3") {
		t.Errorf("config does not contain backend 10.0.0.1 details")
	}
	if !strings.Contains(got, "server 10.0.0.2:3000 weight=1 max_fails=3 fail_timeout=10s backup;") {
		t.Errorf("config does not contain backup backend 10.0.0.2 details")
	}

	// Verify frontend block
	if !strings.Contains(got, "listen 80;") {
		t.Errorf("config does not contain listen port")
	}
	if !strings.Contains(got, "server_name lb.example.com;") {
		t.Errorf("config does not contain server_name")
	}
	if !strings.Contains(got, "proxy_pass http://gopulse_upstream_lb-123;") {
		t.Errorf("config does not contain correct proxy_pass directive")
	}
}

func TestGenerateNginxConfigRoundRobin(t *testing.T) {
	service := NewLoadBalancerService(nil, nil, nil, nil)

	lb := models.LoadBalancer{
		ID:                  "lb-rr",
		ServerID:            "srv-1",
		Name:                "Test LB RR",
		Domain:              "rr.example.com",
		Port:                8080,
		Algorithm:           "round-robin",
		SSL:                 false,
		HealthCheckPath:     "/",
		HealthCheckInterval: 10,
		Status:              "active",
		CreatedAt:           time.Now(),
		Backends: []models.LoadBalancerBackend{
			{
				ID:             "b-1",
				LoadBalancerID: "lb-rr",
				Host:           "10.0.0.1",
				Port:           80,
				Weight:         1,
				Backup:         false,
			},
		},
	}

	got, err := service.GenerateNginxConfig(lb)
	if err != nil {
		t.Fatalf("GenerateNginxConfig failed: %v", err)
	}

	// For round robin, we don't output an algorithm directive in the upstream block (Nginx default)
	if strings.Contains(got, "round-robin;") {
		t.Errorf("config should not contain round-robin directive")
	}
	if !strings.Contains(got, "listen 8080;") {
		t.Errorf("config does not contain custom port 8080")
	}
}

func TestLbShellQuote(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"example.com", "'example.com'"},
		{"domain'with'quote", "'domain'\"'\"'with'\"'\"'quote'"},
	}

	for _, c := range cases {
		got := lbShellQuote(c.input)
		if got != c.expected {
			t.Errorf("lbShellQuote(%q) = %q, expected %q", c.input, got, c.expected)
		}
	}
}

