package services

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

func TestEnableWebhookRequiresGitSource(t *testing.T) {
	dir := t.TempDir()
	projectStore, err := store.NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	project := models.Project{ID: "upload-project", SourceType: "upload"}
	if err := projectStore.AddProject(project); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	service := NewProjectService(projectStore, nil, "")
	if _, err := service.EnableWebhook(t.Context(), project.ID); !errors.Is(err, ErrWebhookRequiresGitSource) {
		t.Fatalf("EnableWebhook error = %v, want ErrWebhookRequiresGitSource", err)
	}

	stored, err := projectStore.GetProject(project.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if stored.WebhookEnabled || stored.WebhookSecret != "" {
		t.Fatalf("invalid project webhook changed: enabled=%t secret=%q", stored.WebhookEnabled, stored.WebhookSecret)
	}
}

func TestNormalizeGitHubRepositoryURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "clone URL", input: "https://github.com/acme/web.git", want: "https://github.com/acme/web.git"},
		{name: "browser URL", input: "https://github.com/acme/web", want: "https://github.com/acme/web.git"},
		{name: "credentials removed", input: "https://oauth2:secret@github.com/acme/web.git", want: "https://github.com/acme/web.git"},
		{name: "wrong host", input: "https://example.com/acme/web.git", wantErr: true},
		{name: "missing owner", input: "https://github.com/web.git", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeGitHubRepositoryURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeGitHubRepositoryURL(%q) unexpectedly succeeded with %q", tt.input, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeGitHubRepositoryURL(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestGitRemoteCredentialsAreSanitized(t *testing.T) {
	raw := "https://oauth2:top-secret@github.com/acme/web.git"
	if got := sanitizeGitRemoteURL(raw); got != "https://github.com/acme/web.git" {
		t.Fatalf("sanitizeGitRemoteURL() = %q", got)
	}
	if got := redactSecret("fetch failed for top-secret", "top-secret"); got != "fetch failed for [REDACTED]" {
		t.Fatalf("redactSecret() = %q", got)
	}
}

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
