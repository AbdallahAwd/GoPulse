package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/amaa/gopulse/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHubService struct {
	clientID     string
	clientSecret string
	scopes       []string
	endpoint     oauth2.Endpoint
}

func NewGitHubService(cfg config.GitHubConfig) *GitHubService {
	return &GitHubService{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scopes:       []string{"repo", "user:email"},
		endpoint:     github.Endpoint,
	}
}

func (s *GitHubService) oauthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       s.scopes,
		Endpoint:     s.endpoint,
	}
}

func (s *GitHubService) GetAuthURL(state string, redirectURL string) string {
	return s.oauthConfig(redirectURL).AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (s *GitHubService) Exchange(ctx context.Context, code string, redirectURL string) (*oauth2.Token, error) {
	return s.oauthConfig(redirectURL).Exchange(ctx, code)
}

type GitHubRepo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	CloneURL    string `json:"clone_url"`
}

func (s *GitHubService) ListRepos(ctx context.Context, token *oauth2.Token) ([]GitHubRepo, error) {
	client := s.oauthConfig("").Client(ctx, token)

	// API call to list user repos
	// Using basic HTTP client for now to avoid dragging in the full google/go-github dependency if not needed,
	// but using existing oauth client which handles auth headers.
	resp, err := client.Get("https://api.github.com/user/repos?sort=updated&per_page=100")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: %s", resp.Status)
	}

	var repos []GitHubRepo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to decode repos: %w", err)
	}

	return repos, nil
}
