package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type handlerCronExecutor struct{}

func (handlerCronExecutor) Execute(ctx context.Context, job models.CronJob) services.CronResult {
	<-ctx.Done()
	return services.CronResult{Err: ctx.Err()}
}

func cronRouter(t *testing.T) (http.Handler, *services.CronService) {
	t.Helper()
	dir := t.TempDir()
	servers, err := store.NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := servers.AddServer(models.Server{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.NewCronStore(filepath.Join(dir, "cron.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := services.NewCronService(st, handlerCronExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	h := &CronHandler{Service: service, Servers: servers}
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			if role == "" {
				role = "admin"
			}
			ctx := context.WithValue(r.Context(), middleware.RoleContextKey, role)
			ctx = context.WithValue(ctx, middleware.UserContextKey, "alice")
			ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, []string{"a"})
			ctx = context.WithValue(ctx, middleware.PermissionsContextKey, strings.Fields(r.Header.Get("X-Test-Permissions")))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	router.Use(middleware.PermissionGuard)
	router.Route("/api/servers/{serverID}/cron", h.Routes)
	return router, service
}
func cronHTTP(router http.Handler, method, path, body, role, permissions string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Test-Role", role)
	r.Header.Set("X-Test-Permissions", permissions)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

const validCronJSON = `{"name":"Report","schedule":"0 9 * * *","timezone":"Africa/Cairo","command":"printf ok","working_dir":"/srv","timeout_seconds":300,"enabled":false}`

func TestCronAPIManagementAndServerOwnership(t *testing.T) {
	router, _ := cronRouter(t)
	created := cronHTTP(router, "POST", "/api/servers/a/cron", validCronJSON, "admin", "")
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var response struct {
		Data models.CronJob `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	id := response.Data.ID
	if id == "" || response.Data.ServerID != "a" || response.Data.CreatedBy != "alice" {
		t.Fatal("missing job context")
	}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/servers/a/cron", "", 200},
		{"GET", "/api/servers/b/cron/" + id + "/runs", "", 404},
		{"PUT", "/api/servers/b/cron/" + id, validCronJSON, 404},
		{"POST", "/api/servers/b/cron/" + id + "/run", "{}", 404},
		{"DELETE", "/api/servers/b/cron/" + id, "", 404},
		{"PUT", "/api/servers/a/cron/" + id + "/enabled", `{"enabled":true}`, 200},
		{"PUT", "/api/servers/a/cron/" + id + "/enabled", `{"enabled":false}`, 200},
		{"PUT", "/api/servers/a/cron/" + id, validCronJSON, 200},
		{"POST", "/api/servers/a/cron/" + id + "/run", "{}", 202},
		{"POST", "/api/servers/a/cron/" + id + "/run", "{}", 409},
		{"DELETE", "/api/servers/a/cron/" + id, "", 409},
		{"GET", "/api/servers/a/cron/" + id + "/runs", "", 200},
		{"POST", "/api/servers/a/cron/" + id + "/cancel", "{}", 202},
	} {
		w := cronHTTP(router, test.method, test.path, test.body, "admin", "")
		if w.Code != test.status {
			t.Errorf("%s %s: %d %s", test.method, test.path, w.Code, w.Body.String())
		}
	}
}
func TestCronAPIRequiresDedicatedWritePermissionAndServerScope(t *testing.T) {
	router, _ := cronRouter(t)
	for _, test := range []struct {
		method, path, body, role, permissions string
		status                                int
	}{
		{"GET", "/api/servers/a/cron", "", "viewer", "", 200},
		{"GET", "/api/servers/b/cron", "", "viewer", "", 403},
		{"POST", "/api/servers/a/cron", validCronJSON, "viewer", "", 403},
		{"POST", "/api/servers/a/cron", validCronJSON, "operator", "terminal:write", 403},
		{"POST", "/api/servers/a/cron", validCronJSON, "operator", "cron:write", 201},
		{"POST", "/api/servers/b/cron", validCronJSON, "operator", "cron:write", 403},
		{"GET", "/api/servers/missing/cron", "", "admin", "", 404},
	} {
		w := cronHTTP(router, test.method, test.path, test.body, test.role, test.permissions)
		if w.Code != test.status {
			t.Errorf("%+v: %d %s", test, w.Code, w.Body.String())
		}
	}
}
func TestCronAPIRejectsInvalidAndOversizedInput(t *testing.T) {
	router, _ := cronRouter(t)
	for _, body := range []string{"{}", validCronJSON + " {}", strings.Replace(validCronJSON, `"enabled":false`, `"enabled":false,"server_id":"b"`, 1), strings.Replace(validCronJSON, `"timeout_seconds":300`, `"timeout_seconds":0`, 1), strings.Replace(validCronJSON, `"command":"printf ok"`, `"command":"`+strings.Repeat("x", 40000)+`"`, 1)} {
		w := cronHTTP(router, "POST", "/api/servers/a/cron", body, "admin", "")
		if w.Code != 400 {
			t.Errorf("invalid data accepted: %d", w.Code)
		}
	}
	for _, path := range []string{"/api/servers/a/cron/preview?schedule=bad&timezone=UTC", "/api/servers/a/cron/id/runs?limit=101", "/api/servers/a/cron/id/runs?offset=-1"} {
		w := cronHTTP(router, "GET", path, "", "admin", "")
		if w.Code != 400 {
			t.Errorf("invalid request accepted: %s %d", path, w.Code)
		}
	}
	w := cronHTTP(router, "GET", "/api/servers/a/cron/preview?schedule=0+9+*+*+*&timezone=UTC", "", "viewer", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "next_runs") {
		t.Fatal("preview not available", w.Body.String())
	}
	w = cronHTTP(router, "PUT", "/api/servers/a/cron/id/enabled", `{}`, "admin", "")
	if w.Code != 400 {
		t.Fatal("missing enabled accepted")
	}
}
