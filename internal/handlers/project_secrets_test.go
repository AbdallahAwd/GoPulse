package handlers

import (
	"context"
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

func TestProjectReadersCannotRetrieveDeploymentSecrets(t *testing.T) {
	projects, err := store.NewProjectStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.AddProject(models.Project{ID: "application", ServerID: "server", Name: "web", WebhookSecret: "PRIVATE-WEBHOOK", EnvVars: map[string]string{"PASSWORD": "PRIVATE-ENV"}}); err != nil {
		t.Fatal(err)
	}
	h := NewProjectHandler(services.NewProjectService(projects, nil, ""))
	for _, permissions := range [][]string{nil, {"projects:write"}} {
		for _, tc := range []struct {
			name string
			call http.HandlerFunc
		}{{"list", h.ListProjects}, {"detail", h.GetProject}, {"automation", h.GetAutomation}, {"environment", h.GetEnvVars}} {
			t.Run(tc.name+"/"+strings.Join(permissions, ","), func(t *testing.T) {
				r := httptest.NewRequest("GET", "http://localhost/", nil)
				route := chi.NewRouteContext()
				route.URLParams.Add("serverID", "server")
				route.URLParams.Add("id", "application")
				ctx := context.WithValue(r.Context(), chi.RouteCtxKey, route)
				ctx = context.WithValue(ctx, middleware.PermissionsContextKey, permissions)
				w := httptest.NewRecorder()
				tc.call(w, r.WithContext(ctx))
				allowed := len(permissions) > 0
				if tc.name == "environment" && !allowed {
					if w.Code != 403 {
						t.Fatalf("environment read status %d, want 403", w.Code)
					}
				} else if w.Code != 200 {
					t.Fatalf("unexpected response %d: %s", w.Code, w.Body.String())
				}
				body := w.Body.String()
				if !allowed && (strings.Contains(body, "PRIVATE-WEBHOOK") || strings.Contains(body, "PRIVATE-ENV")) {
					t.Fatal("read-only response exposed credentials")
				}
				if allowed && tc.name != "environment" && !strings.Contains(body, "PRIVATE-WEBHOOK") {
					t.Fatal("writer lost access to deployment configuration")
				}
			})
		}
	}
}
