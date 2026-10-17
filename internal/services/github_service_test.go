package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/config"
	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGitHubServiceGetRepoVerifiesCanonicalMetadata(t *testing.T) {
	service := NewGitHubService(config.GitHubConfig{})
	service.apiBaseURL = "https://api.github.test"
	service.clientFor = func(context.Context, *oauth2.Token) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/repos/acme/web" {
				t.Fatalf("path = %q", r.URL.Path)
			}
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Fatalf("accept header = %q", r.Header.Get("Accept"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":1,"name":"web","full_name":"acme/web","clone_url":"https://github.com/acme/web.git","default_branch":"main"}`)),
				Request:    r,
			}, nil
		})}
	}
	repo, err := service.GetRepo(t.Context(), &oauth2.Token{AccessToken: "token-123"}, "acme/web")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if repo.FullName != "acme/web" || repo.CloneURL != "https://github.com/acme/web.git" || repo.DefaultBranch != "main" {
		t.Fatalf("repo = %#v", repo)
	}
}

func TestGitHubServiceGetRepoRejectsInvalidFullName(t *testing.T) {
	service := NewGitHubService(config.GitHubConfig{})
	if _, err := service.GetRepo(t.Context(), &oauth2.Token{AccessToken: "token"}, "not-a-full-name"); err == nil {
		t.Fatal("GetRepo unexpectedly accepted invalid repository name")
	}
}
