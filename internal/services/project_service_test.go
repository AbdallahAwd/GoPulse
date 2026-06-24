package services

import (
	"path/filepath"
	"testing"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

func TestNormalizeLaravelStartCommandUsesProjectPortForUIPreset(t *testing.T) {
	got := normalizeLaravelStartCommand("php artisan serve --port=8000 --host=0.0.0.0", 3000)
	want := "php artisan serve --port=3000 --host=0.0.0.0"

	if got != want {
		t.Fatalf("normalizeLaravelStartCommand() = %q, want %q", got, want)
	}
}

func TestNormalizeLaravelStartCommandPreservesCustomCommand(t *testing.T) {
	got := normalizeLaravelStartCommand("php artisan octane:start --server=swoole --host=0.0.0.0 --port=9000", 3000)
	want := "php artisan octane:start --server=swoole --host=0.0.0.0 --port=9000"

	if got != want {
		t.Fatalf("normalizeLaravelStartCommand() = %q, want %q", got, want)
	}
}

func TestPM2ShellStartCommandQuotesArguments(t *testing.T) {
	got := pm2ShellStartCommand("amy's api", "php artisan serve --port=3000 --host=0.0.0.0")
	want := "pm2 start /bin/sh --name 'amy'\"'\"'s api' -- -c 'exec php artisan serve --port=3000 --host=0.0.0.0'"

	if got != want {
		t.Fatalf("pm2ShellStartCommand() = %q, want %q", got, want)
	}
}

func TestProjectIntegrationsResolveDomainsByServerAndPort(t *testing.T) {
	dir := t.TempDir()

	projectStore, err := store.NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	domainStore, err := store.NewDomainStore(filepath.Join(dir, "domains.json"))
	if err != nil {
		t.Fatalf("NewDomainStore: %v", err)
	}

	project := models.Project{
		ID:       "project-1",
		ServerID: "server-1",
		Name:     "api",
		Type:     "laravel",
		Port:     3000,
		HostPort: 8080,
	}
	if err := projectStore.AddProject(project); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	if err := domainStore.AddDomain(models.Domain{
		ID:       "domain-1",
		ServerID: "server-1",
		Domain:   "api.example.com",
		Port:     3000,
	}); err != nil {
		t.Fatalf("AddDomain matching: %v", err)
	}
	if err := domainStore.AddDomain(models.Domain{
		ID:       "domain-2",
		ServerID: "server-1",
		Domain:   "other.example.com",
		Port:     9000,
	}); err != nil {
		t.Fatalf("AddDomain nonmatching: %v", err)
	}

	service := NewProjectService(projectStore, nil, "")
	service.SetDomainStore(domainStore)

	integrations, err := service.GetProjectIntegrations(t.Context(), project.ID, "http://localhost/webhook")
	if err != nil {
		t.Fatalf("GetProjectIntegrations: %v", err)
	}

	for _, integration := range integrations.Integrations {
		if integration.Key == "domain" {
			if integration.Status != "configured" {
				t.Fatalf("domain status = %q, want configured", integration.Status)
			}
			if integration.Message != "api.example.com" {
				t.Fatalf("domain message = %q, want api.example.com", integration.Message)
			}
			return
		}
	}
	t.Fatal("domain integration not found")
}
