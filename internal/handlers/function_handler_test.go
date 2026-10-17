package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type apiFunctionBackend struct{}

func (apiFunctionBackend) Prepare(context.Context, models.Function, models.FunctionRun, io.Writer) (string, error) {
	return "sha256:" + strings.Repeat("a", 64), nil
}
func (apiFunctionBackend) Execute(context.Context, models.FunctionVersion, models.FunctionRun, json.RawMessage, io.Writer) (int, error) {
	return 0, nil
}
func (apiFunctionBackend) Cleanup(context.Context, models.FunctionRun) error { return nil }
func functionRouter(t *testing.T) (http.Handler, *services.FunctionService, *middleware.AuthMiddleware) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "state.json")
	servers, e := store.NewServerStore(file)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b"} {
		if e = servers.AddServer(models.Server{ID: id, Host: id + ".invalid"}); e != nil {
			t.Fatal(e)
		}
	}
	users, e := store.NewUserStore(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = users.AddUserRaw(models.User{ID: "owner", Username: "owner", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	st, e := store.NewFunctionStore(file)
	if e != nil {
		t.Fatal(e)
	}
	s, e := services.NewFunctionService(st, servers, users, apiFunctionBackend{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			if role == "" {
				role = "admin"
			}
			ctx := context.WithValue(r.Context(), middleware.UserContextKey, "owner")
			ctx = context.WithValue(ctx, middleware.RoleContextKey, role)
			ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, strings.Fields(r.Header.Get("X-Test-Scope")))
			ctx = context.WithValue(ctx, middleware.PermissionsContextKey, strings.Fields(r.Header.Get("X-Test-Permissions")))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.PermissionGuard)
	h := &FunctionHandler{s}
	r.Route("/api/servers/{serverID}/functions", h.Routes)
	r.Post("/api/function-triggers/{serverID}/{functionID}", h.Trigger)
	return r, s, middleware.NewAuthMiddleware("test-fixture-secret", users)
}

const functionInputJSON = `{"name":"Hello","runtime":"python","code":"def handler(event, context):\n return event\n","timeout_seconds":30,"memory_mb":256,"cpu_millis":500,"concurrency":1,"schedule":"","timezone":"UTC","schedule_enabled":false,"http_enabled":true}`

func TestFunctionsAPIScopePermissionsAndStrictJSON(t *testing.T) {
	router, s, _ := functionRouter(t)
	base := "/api/servers/a/functions"
	created := backupHTTP(router, "POST", base, functionInputJSON, "admin", "", "")
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var result struct {
		Data models.Function `json:"data"`
	}
	if e := json.Unmarshal(created.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	id := result.Data.ID
	for _, tc := range []struct {
		method, path, body, role, scope, permissions string
		code                                         int
	}{
		{"GET", base, "", "viewer", "a", "", 200},
		{"GET", base + "/" + id + "/runs", "", "viewer", "b", "", 403},
		{"GET", "/api/servers/b/functions/" + id + "/versions", "", "admin", "", "", 404},
		{"POST", base + "/" + id + "/deploy", "{}", "operator", "a", "docker:write", 403},
		{"POST", base + "/" + id + "/http-token", "{}", "operator", "a", "server:write", 403},
		{"POST", base + "/" + id + "/run", "{}", "viewer", "a", "", 403},
		{"POST", base, "null", "admin", "", "", 400},
		{"POST", base, functionInputJSON + "{}", "admin", "", "", 400},
		{"POST", base, strings.Replace(functionInputJSON, `"name":"Hello"`, `"privileged":true,"name":"Hello"`, 1), "admin", "", "", 400},
		{"POST", base + "/" + id + "/run", `{"huge":"` + strings.Repeat("a", 32768) + `"}`, "admin", "", "", 400},
		{"POST", base + "/" + id + "/run", "not JSON", "admin", "", "", 400},
	} {
		w := backupHTTP(router, tc.method, tc.path, tc.body, tc.role, tc.scope, tc.permissions)
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d != %d: %s", tc.method, tc.path, w.Code, tc.code, w.Body.String())
		}
	}
	if e := s.Servers.CheckFunctionDeletion("a"); e == nil {
		t.Fatal("server deletion dependency missing")
	}
}
func TestFunctionsBearerTriggerThroughRealAuthenticationMiddleware(t *testing.T) {
	_, s, auth := functionRouter(t)
	var spec models.FunctionSpec
	json.Unmarshal([]byte(functionInputJSON), &spec)
	f, e := s.Save("a", "", "owner", spec)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Begin("a", f.ID, "owner", "deploy", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := s.Store.Run(f.ID, r.ID)
		if got.Status != "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	token, e := s.RotateToken("a", f.ID)
	if e != nil {
		t.Fatal(e)
	}
	h := &FunctionHandler{s}
	router := chi.NewRouter()
	router.Use(middleware.OriginGuard)
	router.Use(auth.Handler)
	router.Use(middleware.PermissionGuard)
	router.Post("/api/function-triggers/{serverID}/{functionID}", h.Trigger)
	router.Route("/api/servers/{serverID}/functions", h.Routes)
	trigger := "/api/function-triggers/a/" + f.ID
	for _, tc := range []struct {
		path, token, origin string
		status              int
	}{{trigger, "bad", "", 401}, {trigger, token, "https://evil.invalid", 403}, {trigger, token, "", 202}, {"/api/servers/a/functions/" + f.ID + "/http-token", token, "", 401}} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"event":true}`))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		if tc.status == 202 && (strings.Contains(w.Body.String(), "logs") || strings.Contains(w.Body.String(), token)) {
			t.Fatal("HTTP trigger leaked details")
		}
	}
}
