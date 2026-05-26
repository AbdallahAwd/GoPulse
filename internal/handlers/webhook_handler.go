package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
)

// WebhookHandler handles incoming Git push webhooks for CI/CD
type WebhookHandler struct {
	projectService *services.ProjectService
}

// NewWebhookHandler creates a new WebhookHandler
func NewWebhookHandler(projectService *services.ProjectService) *WebhookHandler {
	return &WebhookHandler{projectService: projectService}
}

// githubPushPayload represents the relevant fields from a GitHub push event
type githubPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
}

// HandleGitHubWebhook handles POST /api/webhooks/github/{projectID}
func (h *WebhookHandler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		respondError(w, http.StatusBadRequest, "Project ID required", nil)
		return
	}

	// Get project
	project, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Project not found", err)
		return
	}

	if !project.WebhookEnabled {
		respondError(w, http.StatusForbidden, "Webhook is disabled for this project", nil)
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		respondError(w, http.StatusBadRequest, "Failed to read body", err)
		return
	}
	defer r.Body.Close()

	// Verify HMAC signature if secret is set
	if project.WebhookSecret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if signature == "" {
			respondError(w, http.StatusUnauthorized, "Missing signature", nil)
			return
		}

		if !verifyGitHubSignature(body, project.WebhookSecret, signature) {
			respondError(w, http.StatusUnauthorized, "Invalid signature", nil)
			return
		}
	}

	// Parse payload
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid payload", err)
		return
	}

	// Only deploy on pushes to main/master branch
	ref := payload.Ref
	if ref != "refs/heads/main" && ref != "refs/heads/master" {
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: true,
			Message: fmt.Sprintf("Ignored push to %s (only main/master triggers deploy)", ref),
		})
		return
	}

	log.Printf("Webhook received: project=%s repo=%s ref=%s pusher=%s",
		project.Name, payload.Repository.FullName, ref, payload.Pusher.Name)

	// Trigger async redeploy
	go func() {
		if err := h.projectService.RedeployProject(r.Context(), projectID); err != nil {
			log.Printf("Webhook redeploy failed for project %s: %v", projectID, err)
		} else {
			log.Printf("Webhook redeploy succeeded for project %s", projectID)
		}
	}()

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Deploy triggered",
	})
}

// verifyGitHubSignature verifies the HMAC-SHA256 signature from GitHub
func verifyGitHubSignature(body []byte, secret, signature string) bool {
	// GitHub sends: sha256=<hex>
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedMAC := signature[7:] // strip "sha256=" prefix

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	computedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(computedMAC), []byte(expectedMAC))
}
