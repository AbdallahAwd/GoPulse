package handlers

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
	"github.com/amaa/gopulse/web"
	"github.com/go-chi/chi/v5"
)

func TestHomeStartsWithServerInventory(t *testing.T) {
	handler := NewDashboardHandler(web.Assets, nil)
	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	handler.Index(response, request)
	body := response.Body.String()
	if response.Code != 200 {
		t.Fatalf("render failed: %s", body)
	}
	for _, fragment := range []string{`id="fleet-page"`, `data-section="servers" aria-current="page"`, `id="fleet-role-view"`, `aria-controls="console-rail"`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing server-first element: %s", fragment)
		}
	}
	if strings.Contains(body, `data-section="applications"`) || strings.Contains(body, `id="workspace-page"`) {
		t.Fatal("home restored resource-first navigation")
	}
	if strings.Contains(body, `onclick="openAddServerModal()" class="console-button"`) || strings.Contains(body, `href="/users"`) {
		t.Fatal("viewer received write or team entry point")
	}
}

func TestServerWorkspaceKeepsExplicitHostContext(t *testing.T) {
	servers, err := store.NewServerStore(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.AddServer(models.Server{ID: "server-a", Name: "Applications EU", Host: "10.0.0.10", Port: 22}); err != nil {
		t.Fatal(err)
	}
	handler := NewDashboardHandler(web.Assets, servers)
	request := httptest.NewRequest("GET", "/servers/server-a/dashboard?view=databases", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("serverID", "server-a")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handler.ServerDashboard(response, request)
	body := response.Body.String()
	if response.Code != 200 {
		t.Fatalf("render failed: %s", body)
	}
	for _, fragment := range []string{`All servers`, `id="dashboard-sidebar-nav"`, `id="console-server-switcher"`, `10.0.0.10`, `?view=load-balancers`, `id="current-server-id" value="server-a"`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing server context: %s", fragment)
		}
	}
	if strings.Contains(body, `data-section="applications"`) {
		t.Fatal("server resources escaped the server workspace")
	}
}

func TestCompatibleWorkspacePagesStillRender(t *testing.T) {
	handler := NewDashboardHandler(web.Assets, nil)
	for _, section := range []string{"activity", "settings"} {
		t.Run(section, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.Workspace(response, httptest.NewRequest("GET", "/"+section, nil))
			body := response.Body.String()
			if response.Code != 200 {
				t.Fatalf("render failed: %s", body)
			}
			for _, destination := range []string{`href="/" data-section="servers"`, `href="/activity" data-section="activity"`, `href="/settings" data-section="settings"`} {
				if !strings.Contains(body, destination) {
					t.Fatalf("secondary navigation missing: %s", destination)
				}
			}
			if strings.Contains(body, `id="workspace-create"`) || strings.Contains(body, `href="/users"`) || strings.Contains(body, `href="/servers?settings=backup"`) {
				t.Fatal("viewer received write/admin entry point")
			}
		})
	}
}

func TestGlobalResourceBookmarksReturnToServerSelection(t *testing.T) {
	handler := NewDashboardHandler(web.Assets, nil)
	for path, target := range map[string]string{"/applications": "/?role=apps", "/databases": "/?role=data", "/networking": "/?role=edge"} {
		response := httptest.NewRecorder()
		handler.Workspace(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 302 || response.Header().Get("Location") != target {
			t.Fatalf("%s did not return to server selection: %d %s", path, response.Code, response.Header().Get("Location"))
		}
	}
}
