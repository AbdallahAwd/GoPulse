package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/go-chi/chi/v5"
)

type webhookProjectStub struct {
	project models.Project
	err     error
}

func (s webhookProjectStub) GetProject(context.Context, string) (models.Project, error) {
	if s.err != nil {
		return models.Project{}, s.err
	}
	return s.project, nil
}

type recordingDeploymentEnqueuer struct {
	deployments []models.Deployment
	created     bool
	result      models.Deployment
	err         error
}

func (e *recordingDeploymentEnqueuer) Enqueue(deployment models.Deployment) (models.Deployment, bool, error) {
	if e.err != nil {
		return models.Deployment{}, false, e.err
	}
	e.deployments = append(e.deployments, deployment)
	if e.result.ID == "" {
		e.result = deployment
		e.result.ID = "deployment-1"
	}
	return e.result, e.created, nil
}

func signedWebhookRequest(t *testing.T, projectID, secret, event, delivery string, payload []byte) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github/"+projectID, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("projectID", projectID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func githubPushJSON(t *testing.T, ref, after, repository, defaultBranch string) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"ref":     ref,
		"after":   after,
		"deleted": false,
		"repository": map[string]string{
			"full_name":      repository,
			"default_branch": defaultBranch,
		},
		"pusher":      map[string]string{"name": "octocat"},
		"head_commit": map[string]string{"id": after, "message": "ship it"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return body
}

func newWebhookHandlerForTest(enqueuer *recordingDeploymentEnqueuer) *WebhookHandler {
	return NewWebhookHandler(webhookProjectStub{project: models.Project{
		ID:             "project-1",
		Name:           "web",
		SourceType:     "git",
		SourceURL:      "https://github.com/acme/web.git",
		WebhookEnabled: true,
		WebhookSecret:  "secret",
	}}, enqueuer)
}

func TestHandleGitHubWebhookQueuesVerifiedDefaultBranchPush(t *testing.T) {
	enqueuer := &recordingDeploymentEnqueuer{created: true}
	handler := newWebhookHandlerForTest(enqueuer)
	commit := strings.Repeat("a", 40)
	req := signedWebhookRequest(t, "project-1", "secret", "push", "delivery-1", githubPushJSON(t, "refs/heads/main", commit, "acme/web", "main"))
	rec := httptest.NewRecorder()

	handler.HandleGitHubWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(enqueuer.deployments) != 1 {
		t.Fatalf("queued deployments = %d, want 1", len(enqueuer.deployments))
	}
	got := enqueuer.deployments[0]
	if got.ProjectID != "project-1" || got.DeliveryID != "delivery-1" || got.CommitSHA != commit || got.Branch != "main" {
		t.Fatalf("queued deployment = %#v", got)
	}
}

func TestHandleGitHubWebhookRejectsInvalidSignature(t *testing.T) {
	enqueuer := &recordingDeploymentEnqueuer{created: true}
	handler := newWebhookHandlerForTest(enqueuer)
	commit := strings.Repeat("b", 40)
	req := signedWebhookRequest(t, "project-1", "secret", "push", "delivery-1", githubPushJSON(t, "refs/heads/main", commit, "acme/web", "main"))
	req.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	rec := httptest.NewRecorder()

	handler.HandleGitHubWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(enqueuer.deployments) != 0 {
		t.Fatal("invalid signature queued a deployment")
	}
}

func TestHandleGitHubWebhookRejectsProjectWithoutGitSource(t *testing.T) {
	enqueuer := &recordingDeploymentEnqueuer{created: true}
	handler := NewWebhookHandler(webhookProjectStub{project: models.Project{
		ID:             "project-1",
		Name:           "uploaded-app",
		SourceType:     "upload",
		WebhookEnabled: true,
		WebhookSecret:  "secret",
	}}, enqueuer)
	commit := strings.Repeat("a", 40)
	req := signedWebhookRequest(t, "project-1", "secret", "push", "delivery-1", githubPushJSON(t, "refs/heads/main", commit, "acme/web", "main"))
	rec := httptest.NewRecorder()

	handler.HandleGitHubWebhook(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if len(enqueuer.deployments) != 0 {
		t.Fatal("upload project queued a deployment")
	}
}

func TestHandleGitHubWebhookFiltersRepositoryAndBranch(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   []byte
	}{
		{
			name:   "wrong repository",
			status: http.StatusForbidden,
			body:   githubPushJSON(t, "refs/heads/main", strings.Repeat("c", 40), "acme/other", "main"),
		},
		{
			name:   "non default branch",
			status: http.StatusOK,
			body:   githubPushJSON(t, "refs/heads/feature", strings.Repeat("d", 40), "acme/web", "main"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enqueuer := &recordingDeploymentEnqueuer{created: true}
			handler := newWebhookHandlerForTest(enqueuer)
			req := signedWebhookRequest(t, "project-1", "secret", "push", "delivery-1", tt.body)
			rec := httptest.NewRecorder()

			handler.HandleGitHubWebhook(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if len(enqueuer.deployments) != 0 {
				t.Fatal("filtered webhook queued a deployment")
			}
		})
	}
}

func TestHandleGitHubWebhookRequiresEventDeliveryAndCommit(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{
			name: "missing event",
			mutate: func(r *http.Request) {
				r.Header.Del("X-GitHub-Event")
			},
			status: http.StatusBadRequest,
		},
		{
			name: "missing delivery",
			mutate: func(r *http.Request) {
				r.Header.Del("X-GitHub-Delivery")
			},
			status: http.StatusBadRequest,
		},
		{
			name:   "short commit",
			mutate: func(r *http.Request) {},
			status: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enqueuer := &recordingDeploymentEnqueuer{created: true}
			handler := newWebhookHandlerForTest(enqueuer)
			commit := strings.Repeat("e", 40)
			if tt.name == "short commit" {
				commit = "deadbee"
			}
			req := signedWebhookRequest(t, "project-1", "secret", "push", "delivery-1", githubPushJSON(t, "refs/heads/main", commit, "acme/web", "main"))
			tt.mutate(req)
			rec := httptest.NewRecorder()

			handler.HandleGitHubWebhook(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if len(enqueuer.deployments) != 0 {
				t.Fatal("invalid webhook queued a deployment")
			}
		})
	}
}

func TestHandleGitHubWebhookPingIsSuccessful(t *testing.T) {
	enqueuer := &recordingDeploymentEnqueuer{created: true}
	handler := newWebhookHandlerForTest(enqueuer)
	body := []byte(`{"zen":"Keep it simple"}`)
	req := signedWebhookRequest(t, "project-1", "secret", "ping", "", body)
	req.Header.Del("X-GitHub-Delivery")
	rec := httptest.NewRecorder()

	handler.HandleGitHubWebhook(rec, req)

	if rec.Code != http.StatusOK || len(enqueuer.deployments) != 0 {
		t.Fatalf("ping status=%d queued=%d body=%s", rec.Code, len(enqueuer.deployments), rec.Body.String())
	}
}

func TestGitHubRepositoryNameSupportsHTTPSAndSSH(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/acme/web.git",
		"https://github.com/acme/web",
		"git@github.com:acme/web.git",
		"ssh://git@github.com/acme/web.git",
	} {
		got, ok := githubRepositoryName(raw)
		if !ok || got != "acme/web" {
			t.Fatalf("githubRepositoryName(%q) = %q, %t", raw, got, ok)
		}
	}
}
