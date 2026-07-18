package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/amaa/gopulse/internal/config"
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

func TestGitHubRedirectURLFallsBackToForwardedHost(t *testing.T) {
	handler := NewGitHubHandler(config.GitHubConfig{}, nil)
	request := httptest.NewRequest("GET", "http://10.0.0.5/auth/github/login", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "panel.example.com")

	got := handler.getRedirectURL(request)
	want := "https://panel.example.com/auth/github/callback"
	if got != want {
		t.Fatalf("redirect URL = %q, want %q", got, want)
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
