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

func backupRouter(t *testing.T) (http.Handler, *services.RemoteBackupService) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "state.json")
	servers, e := store.NewServerStore(file)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b", "c"} {
		if e = servers.AddServer(models.Server{ID: id, Name: id, Host: id + ".invalid", Password: "server-secret"}); e != nil {
			t.Fatal(e)
		}
	}
	dbs, e := store.NewDatabaseStore(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = dbs.AddDatabase(models.Database{ID: "db", ServerID: "a", Name: "App", Type: "sqlite", Password: "database-secret"}); e != nil {
		t.Fatal(e)
	}
	users, e := store.NewUserStore(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = users.AddUserRaw(models.User{ID: "admin", Username: "admin", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	st, e := store.NewRemoteBackupStore(file)
	if e != nil {
		t.Fatal(e)
	}
	service, e := services.NewRemoteBackupService(st, servers, dbs, users, nil, filepath.Join(t.TempDir(), "keys"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(service.Close)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			if role == "" {
				role = "admin"
			}
			ctx := context.WithValue(r.Context(), middleware.UserContextKey, "admin")
			ctx = context.WithValue(ctx, middleware.RoleContextKey, role)
			ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, strings.Fields(r.Header.Get("X-Test-Scope")))
			ctx = context.WithValue(ctx, middleware.PermissionsContextKey, strings.Fields(r.Header.Get("X-Test-Permissions")))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.PermissionGuard)
	h := &RemoteBackupHandler{service}
	r.Route("/api/servers/{serverID}/remote-backups", h.Routes)
	return r, service
}

const backupInputJSON = `{"name":"Daily","database_id":"db","destination_id":"b","directory":"/srv/backups","schedule":"0 2 * * *","timezone":"UTC","retention_days":7,"keep_last":2,"enabled":false,"auto_test":true,"timeout_seconds":120,"max_size_gb":1}`

func backupHTTP(router http.Handler, method, path, body, role, scope, permissions string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Test-Role", role)
	r.Header.Set("X-Test-Scope", scope)
	r.Header.Set("X-Test-Permissions", permissions)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}
func TestRemoteBackupAPIIsolationKeysAndValidation(t *testing.T) {
	r, s := backupRouter(t)
	base := "/api/servers/a/remote-backups"
	created := backupHTTP(r, "POST", base, backupInputJSON, "admin", "", "")
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var envelope struct {
		Data models.RemoteBackupPlan `json:"data"`
	}
	if e := json.Unmarshal(created.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	id := envelope.Data.ID
	for _, tc := range []struct {
		method, path, body, role, scope, permissions string
		status                                       int
	}{
		{"GET", base, "", "viewer", "a", "", 200},
		{"GET", base + "/" + id + "/runs", "", "viewer", "a", "", 403},
		{"GET", base + "/" + id + "/artifacts", "", "viewer", "a b", "", 200},
		{"GET", base + "/" + id + "/recovery-key", "", "viewer", "a b", "", 403},
		{"GET", base + "/" + id + "/artifacts/unknown/download", "", "viewer", "a b", "", 403},
		{"POST", base + "/" + id + "/run", "{}", "operator", "a b", "database:write", 403},
		{"POST", base + "/" + id + "/cancel", "{}", "operator", "a b", "server:write", 403},
		{"POST", base + "/" + id + "/artifacts/x/test", "{}", "operator", "a", "server:write database:write", 403},
		{"DELETE", base + "/" + id, "", "operator", "a", "server:write database:write", 403},
		{"GET", "/api/servers/c/remote-backups/" + id + "/runs", "", "admin", "", "", 404},
		{"GET", base + "/preview?schedule=invalid&timezone=UTC", "", "admin", "", "", 400},
		{"POST", base, "null", "admin", "", "", 400},
		{"POST", base, backupInputJSON + "{}", "admin", "", "", 400},
		{"POST", base, strings.Replace(backupInputJSON, `"name":`, `"unknown":true,"name":`, 1), "admin", "", "", 400},
	} {
		w := backupHTTP(r, tc.method, tc.path, tc.body, tc.role, tc.scope, tc.permissions)
		if w.Code != tc.status {
			t.Errorf("%s %s got %d: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	hidden := backupHTTP(r, "GET", base, "", "viewer", "a", "")
	if strings.Contains(hidden.Body.String(), id) {
		t.Fatal("destination scope leak", hidden.Body.String())
	}
	options := backupHTTP(r, "GET", base+"/options", "", "admin", "", "")
	if strings.Contains(options.Body.String(), "secret") {
		t.Fatal("credentials in options", options.Body.String())
	}
	key := backupHTTP(r, "GET", base+"/"+id+"/recovery-key", "", "admin", "", "")
	if key.Code != 200 || key.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(key.Body.String(), "AGE-SECRET-KEY-") {
		t.Fatal(key.Code, key.Body.String())
	}
	list := backupHTTP(r, "GET", base, "", "admin", "", "")
	if strings.Contains(list.Body.String(), "AGE-SECRET") || strings.Contains(list.Body.String(), "database-secret") {
		t.Fatal("secret in plan list")
	}
	if e := s.Databases.RemoveDatabase("db"); e == nil {
		t.Fatal("referenced database deleted")
	}
	if e := s.Servers.RemoveServer("b"); e == nil {
		t.Fatal("referenced destination deleted")
	}
	deleted := backupHTTP(r, "DELETE", base+"/"+id, "", "admin", "", "")
	if deleted.Code != 200 {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
}
