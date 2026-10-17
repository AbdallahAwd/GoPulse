package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/config"
	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

func TestGitHubRedirectURLUsesConfiguredCallback(t *testing.T) {
	handler := NewGitHubHandler(config.GitHubConfig{
		CallbackURL: "http://localhost:2886/auth/github/callback",
	}, nil)
	request := httptest.NewRequest("GET", "http://127.0.0.1:9999/auth/github/login", nil)

	got := handler.getRedirectURL(request)
	want := "http://localhost:2886/auth/github/callback"
	if got != want {
		t.Fatalf("redirect URL = %q, want %q", got, want)
	}
}

func TestGitHubRedirectURLIgnoresUntrustedForwardedHost(t *testing.T) {
	handler := NewGitHubHandler(config.GitHubConfig{}, nil)
	request := httptest.NewRequest("GET", "http://10.0.0.5/auth/github/login", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "panel.example.com")

	got := handler.getRedirectURL(request)
	want := "http://10.0.0.5/auth/github/callback"
	if got != want {
		t.Fatalf("redirect URL = %q, want %q", got, want)
	}
}

func TestGitHubSecureCookieUsesConfiguredHTTPSCallback(t *testing.T) {
	h := NewGitHubHandler(config.GitHubConfig{CallbackURL: "https://panel.example.com/auth/github/callback"}, nil)
	r := httptest.NewRequest("GET", "http://localhost/auth/github/login", nil)
	if !h.secureCookie(r) {
		t.Fatal("HTTPS callback behind a proxy must use Secure cookies")
	}
}

func TestGitHubWritesRequireProjectPermissionAndServerScope(t *testing.T) {
	projects, err := store.NewProjectStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.AddProject(models.Project{ID: "private-project", ServerID: "private-server"}); err != nil {
		t.Fatal(err)
	}
	h := NewGitHubHandler(config.GitHubConfig{}, services.NewProjectService(projects, nil, ""))
	for _, tc := range []struct {
		name, path, body string
		permissions      []string
		call             http.HandlerFunc
		status           int
	}{
		{"clone needs permission", "/api/github/clone", `{}`, []string{"server:write"}, h.CloneRepo, 403},
		{"connect needs permission", "/api/github/connect", `{}`, []string{"server:write"}, h.ConnectRepo, 403},
		{"clone denies another server", "/api/github/clone", `{"server_id":"private-server"}`, []string{"projects:write"}, h.CloneRepo, 403},
		{"connect hides another server's project", "/api/github/connect", `{"project_id":"private-project","full_name":"acme/web"}`, []string{"projects:write"}, h.ConnectRepo, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: "github_token", Value: "dummy-test-token"})
			ctx := context.WithValue(r.Context(), middleware.PermissionsContextKey, tc.permissions)
			ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, []string{"allowed-server"})
			w := httptest.NewRecorder()
			tc.call(w, r.WithContext(ctx))
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestGitHubLoginUsesRandomStateCookie(t *testing.T) {
	handler := NewGitHubHandler(config.GitHubConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "https://panel.example.com/auth/github/login", nil)
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	state := location.Query().Get("state")
	if len(state) != 64 {
		t.Fatalf("OAuth state length = %d, want 64", len(state))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != githubOAuthStateCookie || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("OAuth state cookie = %#v", cookies)
	}
}

func TestGitHubCallbackRejectsMissingOAuthState(t *testing.T) {
	handler := NewGitHubHandler(config.GitHubConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/auth/github/callback?code=abc&state=wrong", nil)
	rec := httptest.NewRecorder()

	handler.Callback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
