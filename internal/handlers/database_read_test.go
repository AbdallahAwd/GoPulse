package handlers

import (
	"context"
	"errors"
	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type unavailableDatabaseHost struct{ calls int }

func (e *unavailableDatabaseHost) Run(context.Context, string, string) (string, error) {
	e.calls++
	return "", errors.New("SSH unavailable")
}
func TestDatabaseMetadataWorksWhenRemoteHostIsUnavailable(t *testing.T) {
	st, _ := store.NewDatabaseStore(filepath.Join(t.TempDir(), "databases.json"))
	if err := st.AddDatabase(models.Database{ID: "db-test", ServerID: "server", Type: "postgres", Status: models.DatabaseStatusRunning, Password: "DATABASE-SECRET"}); err != nil {
		t.Fatal(err)
	}
	executor := &unavailableDatabaseHost{}
	handler := NewDatabaseHandler(services.NewDatabaseService(st, services.NewServiceManagerWithExecutor(nil, executor)), nil)
	router := chi.NewRouter()
	router.Route("/servers/{serverID}/databases", func(r chi.Router) {
		owned := r.With(handler.RequireOwnership)
		owned.Get("/", handler.ListDatabases)
		owned.Get("/{dbID}", handler.GetDatabase)
		owned.Get("/{dbID}/credentials", handler.Credentials)
	})
	for _, path := range []string{"/servers/server/databases", "/servers/server/databases/db-test"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "DATABASE-SECRET") || executor.calls != 0 {
			t.Fatalf("metadata contacted a remote host or leaked secrets: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/servers/server/databases/db-test?refresh=true", nil))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "SSH unavailable") {
		t.Fatal("health failure was hidden")
	}
	db, _ := st.GetDatabase("db-test")
	if db.Status != models.DatabaseStatusRunning {
		t.Fatal("failed verification destroyed last known state")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/servers/server/databases/db-test/credentials", nil))
	if w.Code != 403 || strings.Contains(w.Body.String(), "DATABASE-SECRET") {
		t.Fatal("viewer can read credentials")
	}
	req := httptest.NewRequest("GET", "/servers/server/databases/db-test/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RoleContextKey, "admin"))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "DATABASE-SECRET") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authorized explicit credential read failed")
	}
	req = httptest.NewRequest("GET", "/servers/other/databases/db-test/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RoleContextKey, "admin"))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatal("credential read crossed server ownership")
	}
}
