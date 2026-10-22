package handlers

import (
	"context"
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

type qdrantSecretHost struct {
	calls int
	key   string
}

func (e *qdrantSecretHost) Run(context.Context, string, string) (string, error) {
	e.calls++
	return e.key + "\n", nil
}

func TestQdrantNetworkAndCredentialsAccess(t *testing.T) {
	st, err := store.NewDatabaseStore(filepath.Join(t.TempDir(), "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddDatabase(models.Database{ID: "db-vectors", ServerID: "server", Type: "qdrant"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetQdrantConfig("db-vectors", models.QdrantConfig{Version: "v1.19.0", BindAddress: "127.0.0.1", HostPort: 6333, StoragePath: "/var/lib/gopulse/databases/db-vectors"}); err != nil {
		t.Fatal(err)
	}
	host := &qdrantSecretHost{key: strings.Repeat("k", 40)}
	service := services.NewDatabaseService(st, services.NewServiceManagerWithExecutor(nil, host))
	db := NewDatabaseHandler(service, nil)
	q := &QdrantHandler{Service: service}
	router := chi.NewRouter()
	router.Route("/servers/{serverID}/databases", func(r chi.Router) {
		owned := r.With(db.RequireOwnership)
		owned.Get("/{dbID}", db.GetDatabase)
		owned.Get("/{dbID}/credentials", db.Credentials)
		owned.Put("/{dbID}/qdrant/network", q.ConfigureNetwork)
	})
	for _, tc := range []struct {
		method, path, role, body string
		code                     int
	}{
		{"GET", "/servers/server/databases/db-vectors", "admin", "", 200},
		{"GET", "/servers/server/databases/db-vectors/credentials", "viewer", "", 403},
		{"GET", "/servers/other/databases/db-vectors/credentials", "admin", "", 404},
		{"PUT", "/servers/server/databases/db-vectors/qdrant/network", "viewer", `{"bind_address":"0.0.0.0","host_port":6333}`, 403},
		{"PUT", "/servers/other/databases/db-vectors/qdrant/network", "admin", `{}`, 404},
		{"PUT", "/servers/server/databases/db-vectors/qdrant/network", "admin", `{"bind_address":"0.0.0.0; reboot","host_port":6333}`, 400},
		{"PUT", "/servers/server/databases/db-vectors/qdrant/network", "admin", `{"bind_address":"0.0.0.0","host_port":6333,"grpc_port":6333}`, 400},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req = req.WithContext(context.WithValue(req.Context(), middleware.RoleContextKey, tc.role))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s %s: got %d want %d: %s", tc.method, tc.path, w.Code, tc.code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), host.key) || host.calls != 0 {
			t.Fatal("unexpected credential read")
		}
	}
	req := httptest.NewRequest("GET", "/servers/server/databases/db-vectors/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RoleContextKey, "admin"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"api_key":"`+host.key+`"`) || w.Header().Get("Cache-Control") != "no-store" || host.calls != 1 {
		t.Fatal("authorized key read failed")
	}
}
