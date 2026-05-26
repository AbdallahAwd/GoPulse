package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/amaa/gopulse/internal/config"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

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

// 1. Redirect to GitHub
func (h *GitHubHandler) Login(w http.ResponseWriter, r *http.Request) {
	state := "random-state-string" // In production, use a random string and verify it in callback
	url := h.service.GetAuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// 2. Callback from GitHub
func (h *GitHubHandler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}

	token, err := h.service.Exchange(r.Context(), code)
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
		Secure:   false, // Set to true in production
		Expires:  token.Expiry,
	}
	http.SetCookie(w, cookie)

	http.Redirect(w, r, "/?github_connected=true", http.StatusTemporaryRedirect)
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
