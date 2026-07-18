package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/amaa/gopulse/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHubService struct {
	clientID     string
	clientSecret string
	scopes       []string
	endpoint     oauth2.Endpoint
	apiBaseURL   string
	clientFor    func(context.Context, *oauth2.Token) *http.Client
}

func NewGitHubService(cfg config.GitHubConfig) *GitHubService {
	return &GitHubService{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scopes:       []string{"repo", "user:email"},
		endpoint:     github.Endpoint,
		apiBaseURL:   "https://api.github.com",
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
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
}

func (s *GitHubService) ListRepos(ctx context.Context, token *oauth2.Token) ([]GitHubRepo, error) {
	var repos []GitHubRepo
	if err := s.getJSON(ctx, token, "/user/repos?sort=updated&per_page=100", &repos); err != nil {
		return nil, fmt.Errorf("failed to fetch repos: %w", err)
	}
	return repos, nil
}

// GetRepo verifies that the connected GitHub account can read the selected
// repository and returns canonical repository metadata from GitHub.
func (s *GitHubService) GetRepo(ctx context.Context, token *oauth2.Token, fullName string) (GitHubRepo, error) {
	parts := strings.Split(strings.Trim(fullName, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return GitHubRepo{}, fmt.Errorf("repository must use owner/name format")
	}
	path := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	var repo GitHubRepo
	if err := s.getJSON(ctx, token, path, &repo); err != nil {
		return GitHubRepo{}, fmt.Errorf("failed to verify repository: %w", err)
	}
	if repo.FullName == "" || repo.CloneURL == "" {
		return GitHubRepo{}, fmt.Errorf("github returned incomplete repository metadata")
	}
	return repo, nil
}

func (s *GitHubService) getJSON(ctx context.Context, token *oauth2.Token, path string, target interface{}) error {
	client := s.oauthConfig("").Client(ctx, token)
	if s.clientFor != nil {
		client = s.clientFor(ctx, token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.apiBaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api error: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode github response: %w", err)
	}
	return nil
}
