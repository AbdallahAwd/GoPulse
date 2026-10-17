package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/go-chi/chi/v5"
)

// GitHub documents a 25 MB maximum webhook payload. Keep the same ceiling so
// large push events are not rejected before signature verification.
const maxGitHubWebhookBody = 25 << 20

type webhookProjectReader interface {
	GetProject(ctx context.Context, id string) (models.Project, error)
}

type deploymentEnqueuer interface {
	Enqueue(deployment models.Deployment) (models.Deployment, bool, error)
}

// WebhookHandler authenticates GitHub deliveries and turns accepted pushes into
// durable deployments.
type WebhookHandler struct {
	projects    webhookProjectReader
	deployments deploymentEnqueuer
}

func NewWebhookHandler(projects webhookProjectReader, deployments ...deploymentEnqueuer) *WebhookHandler {
	var queue deploymentEnqueuer
	if len(deployments) > 0 {
		queue = deployments[0]
	}
	return &WebhookHandler{projects: projects, deployments: queue}
}

type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
	HeadCommit *struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

// HandleGitHubWebhook handles POST /api/webhooks/github/{projectID}.
func (h *WebhookHandler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		respondError(w, http.StatusMethodNotAllowed, "Webhook must use POST", nil)
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		respondError(w, http.StatusBadRequest, "Project ID required", nil)
		return
	}
	if h.projects == nil {
		respondError(w, http.StatusServiceUnavailable, "Project service is unavailable", nil)
		return
	}

	project, err := h.projects.GetProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Project not found", err)
		return
	}
	if !project.WebhookEnabled {
		respondError(w, http.StatusForbidden, "Webhook is disabled for this project", nil)
		return
	}
	if strings.TrimSpace(project.WebhookSecret) == "" {
		respondError(w, http.StatusServiceUnavailable, "Webhook secret is not configured; disable and re-enable the webhook", nil)
		return
	}
	if h.deployments == nil {
		respondError(w, http.StatusServiceUnavailable, "Deployment queue is unavailable", nil)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json", err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxGitHubWebhookBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondError(w, http.StatusRequestEntityTooLarge, "Webhook payload is too large", err)
			return
		}
		respondError(w, http.StatusBadRequest, "Failed to read webhook payload", err)
		return
	}
	defer r.Body.Close()

	if !verifyGitHubSignature(body, project.WebhookSecret, r.Header.Get("X-Hub-Signature-256")) {
		respondError(w, http.StatusUnauthorized, "Invalid webhook signature", nil)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(project.SourceType), "git") || strings.TrimSpace(project.SourceURL) == "" {
		respondError(w, http.StatusConflict, "Project is not connected to a Git repository; import it from Git before using CI/CD", nil)
		return
	}

	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if event == "" {
		respondError(w, http.StatusBadRequest, "X-GitHub-Event is required", nil)
		return
	}
	if event == "ping" {
		respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Message: "GitHub webhook verified"})
		return
	}
	if event != "push" {
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: true,
			Message: fmt.Sprintf("Ignored GitHub event %q", event),
		})
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	if !validGitHubDeliveryID(deliveryID) {
		respondError(w, http.StatusBadRequest, "X-GitHub-Delivery is required", nil)
		return
	}

	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid GitHub push payload", err)
		return
	}
	if !repositoryMatchesProject(project.SourceURL, payload.Repository.FullName) {
		respondError(w, http.StatusForbidden, "Webhook repository does not match the project source", nil)
		return
	}
	if payload.Deleted || allZeroGitCommit(payload.After) {
		respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Message: "Ignored branch deletion"})
		return
	}
	if !validGitCommitSHA(payload.After) {
		respondError(w, http.StatusBadRequest, "GitHub payload does not contain a valid commit SHA", nil)
		return
	}

	const headsPrefix = "refs/heads/"
	if !strings.HasPrefix(payload.Ref, headsPrefix) {
		respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Message: fmt.Sprintf("Ignored non-branch ref %s", payload.Ref)})
		return
	}
	branch := strings.TrimPrefix(payload.Ref, headsPrefix)
	defaultBranch := strings.TrimSpace(payload.Repository.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if branch != defaultBranch {
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: true,
			Message: fmt.Sprintf("Ignored push to %s; production branch is %s", branch, defaultBranch),
		})
		return
	}

	commitMessage := ""
	if payload.HeadCommit != nil {
		commitMessage = payload.HeadCommit.Message
	}
	source := "github_webhook"
	if strings.HasPrefix(deliveryID, "actions-") {
		source = "github_actions"
	}
	deployment, created, err := h.deployments.Enqueue(models.Deployment{
		ProjectID:     project.ID,
		DeliveryID:    deliveryID,
		Source:        source,
		Repository:    payload.Repository.FullName,
		Ref:           payload.Ref,
		Branch:        branch,
		CommitSHA:     payload.After,
		CommitMessage: commitMessage,
		Pusher:        payload.Pusher.Name,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to queue deployment", err)
		return
	}

	log.Printf("Webhook accepted: deployment=%s project=%s repo=%s ref=%s commit=%s pusher=%s duplicate=%t",
		deployment.ID, project.Name, payload.Repository.FullName, payload.Ref, payload.After, payload.Pusher.Name, !created)
	message := "Deployment queued"
	if !created {
		message = "Delivery already accepted"
	}
	respondJSON(w, http.StatusAccepted, models.APIResponse{Success: true, Message: message, Data: deployment})
}

func verifyGitHubSignature(body []byte, secret, signature string) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), provided)
}

func allZeroGitCommit(commit string) bool {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return true
	}
	return strings.Trim(commit, "0") == ""
}

func validGitCommitSHA(commit string) bool {
	commit = strings.TrimSpace(commit)
	if len(commit) != sha1CommitLength && len(commit) != sha256CommitLength {
		return false
	}
	for _, r := range commit {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

const (
	sha1CommitLength   = 40
	sha256CommitLength = 64
)

func validGitHubDeliveryID(deliveryID string) bool {
	if deliveryID == "" || len(deliveryID) > 200 {
		return false
	}
	for _, r := range deliveryID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func repositoryMatchesProject(sourceURL string, payloadFullName string) bool {
	want, ok := githubRepositoryName(sourceURL)
	if !ok {
		return false
	}
	got := strings.Trim(strings.TrimSpace(payloadFullName), "/")
	return strings.EqualFold(want, trimGitSuffix(got))
}

func githubRepositoryName(rawURL string) (string, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "git@github.com:") {
		name := strings.TrimPrefix(rawURL, "git@github.com:")
		name = trimGitSuffix(strings.Trim(name, "/"))
		return name, strings.Count(name, "/") == 1
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", false
	}
	name := trimGitSuffix(strings.Trim(parsed.Path, "/"))
	return name, strings.Count(name, "/") == 1
}

func trimGitSuffix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 && strings.EqualFold(value[len(value)-4:], ".git") {
		return value[:len(value)-4]
	}
	return value
}
