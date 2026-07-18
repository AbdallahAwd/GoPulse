package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/amaa/gopulse/internal/config"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const githubOAuthCallbackPath = "/auth/github/callback"
const githubOAuthStateCookie = "github_oauth_state"

type GitHubHandler struct {
	service        *services.GitHubService
	projectService *services.ProjectService
	config         config.GitHubConfig
}

func NewGitHubHandler(cfg config.GitHubConfig, ps *services.ProjectService) *GitHubHandler {
	return &GitHubHandler{
		service:        services.NewGitHubService(cfg),
		projectService: ps,
		config:         cfg,
	}
}

func (h *GitHubHandler) getRedirectURL(r *http.Request) string {
	if configuredURL := strings.TrimSpace(h.config.CallbackURL); configuredURL != "" {
		return configuredURL
	}

	scheme := "http"
	if forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}

	return fmt.Sprintf("%s://%s%s", scheme, host, githubOAuthCallbackPath)
}

func firstForwardedValue(value string) string {
	if comma := strings.Index(value, ","); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}

// 1. Redirect to GitHub
func (h *GitHubHandler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := newGitHubOAuthState()
	if err != nil {
		http.Error(w, "Failed to initialize GitHub login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    state,
		Path:     githubOAuthCallbackPath,
		MaxAge:   10 * 60,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	redirectURL := h.getRedirectURL(r)
	url := h.service.GetAuthURL(state, redirectURL)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// 2. Callback from GitHub
func (h *GitHubHandler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}
	stateCookie, err := r.Cookie(githubOAuthStateCookie)
	if err != nil || !constantTimeEqual(stateCookie.Value, r.URL.Query().Get("state")) {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    "",
		Path:     githubOAuthCallbackPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	redirectURL := h.getRedirectURL(r)
	token, err := h.service.Exchange(r.Context(), code, redirectURL)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	// Store token in a cookie (simplified for MVP)
	// ideally execute a javascript that posts the token to parent window if using popup
	// OR set a cookie and redirect to dashboard

	// We'll set a cookie and redirect to dashboard with a success param
	cookie := &http.Cookie{
		Name:     "github_token",
		Value:    token.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}
	if !token.Expiry.IsZero() {
		cookie.Expires = token.Expiry
	} else {
		cookie.Expires = time.Now().Add(30 * 24 * time.Hour)
	}
	http.SetCookie(w, cookie)

	http.Redirect(w, r, "/?github_connected=true", http.StatusTemporaryRedirect)
}

func newGitHubOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func requestIsHTTPS(r *http.Request) bool {
	if strings.EqualFold(firstForwardedValue(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	return r.TLS != nil
}

// 3. List Repos (API)
func (h *GitHubHandler) ListRepos(w http.ResponseWriter, r *http.Request) {
	// Retrieve token from cookie
	cookie, err := r.Cookie("github_token")
	if err != nil {
		respondError(w, http.StatusUnauthorized, "GitHub not connected", err)
		return
	}

	token := &oauth2.Token{AccessToken: cookie.Value}

	repos, err := h.service.ListRepos(r.Context(), token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list projects", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    repos,
	})
}

// 4. Clone Repo (API)
func (h *GitHubHandler) CloneRepo(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("github_token")
	if err != nil {
		respondError(w, http.StatusUnauthorized, "GitHub not connected", err)
		return
	}
	token := cookie.Value

	var req struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		Type          string `json:"type"`
		Port          int    `json:"port"`
		BuildCmd      string `json:"build_cmd"`
		StartCmd      string `json:"start_cmd"`
		ServerID      string `json:"server_id"`
		RestartOnBoot bool   `json:"restart_on_boot"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.ServerID == "" {
		respondError(w, http.StatusBadRequest, "server_id is required", nil)
		return
	}

	project := models.Project{
		ID:            uuid.New().String(),
		ServerID:      req.ServerID,
		Name:          req.Name,
		Type:          req.Type,
		Port:          req.Port,
		HostPort:      req.Port, // Default to same port
		BuildCommand:  req.BuildCmd,
		StartCommand:  req.StartCmd,
		BuildType:     "process", // Default
		RestartOnBoot: req.RestartOnBoot,
	}

	newProject, err := h.projectService.CreateProjectFromRepo(r.Context(), project, req.RepoURL, token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create project from repo", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    newProject,
	})
}

// ConnectRepo attaches a repository selected through the authenticated GitHub
// account to an existing uploaded project. Repository metadata comes from
// GitHub rather than trusting a client-supplied clone URL.
func (h *GitHubHandler) ConnectRepo(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("github_token")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		respondError(w, http.StatusUnauthorized, "GitHub not connected", err)
		return
	}

	var req struct {
		ProjectID string `json:"project_id"`
		FullName  string `json:"full_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.FullName) == "" {
		respondError(w, http.StatusBadRequest, "project_id and full_name are required", nil)
		return
	}

	token := &oauth2.Token{AccessToken: cookie.Value}
	repo, err := h.service.GetRepo(r.Context(), token, req.FullName)
	if err != nil {
		respondError(w, http.StatusBadGateway, "Unable to verify the selected GitHub repository", err)
		return
	}
	project, err := h.projectService.ConnectProjectToGitHub(
		r.Context(),
		req.ProjectID,
		repo.CloneURL,
		cookie.Value,
		repo.DefaultBranch,
	)
	if err != nil {
		if errors.Is(err, services.ErrProjectAlreadyConnectedToGit) {
			respondError(w, http.StatusConflict, "Project is already connected to a Git repository", err)
			return
		}
		respondError(w, http.StatusUnprocessableEntity, "Failed to connect project to GitHub", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "GitHub repository connected; CI/CD can now be enabled",
		Data: map[string]interface{}{
			"project_id":     project.ID,
			"source_type":    project.SourceType,
			"repository_url": project.SourceURL,
			"full_name":      repo.FullName,
			"default_branch": repo.DefaultBranch,
		},
	})
}
