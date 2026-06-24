package handlers

import (
	"net/http/httptest"
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
