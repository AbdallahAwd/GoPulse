package handlers

import (
	"context"
	"encoding/json"
	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFleetInventoryCountsScopedResourcesWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	servers, err := store.NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	projects, _ := store.NewProjectStore(filepath.Join(dir, "projects.json"))
	databases, _ := store.NewDatabaseStore(filepath.Join(dir, "databases.json"))
	balancers, _ := store.NewLoadBalancerStore(filepath.Join(dir, "load_balancers.json"))
	for _, id := range []string{"allowed", "other"} {
		if err := servers.AddServer(models.Server{ID: id, Name: id, Password: "SSH-SECRET", KeyPath: "PRIVATE-PATH"}); err != nil {
			t.Fatal(err)
		}
		if err := projects.AddProject(models.Project{ID: "p-" + id, ServerID: id, EnvVars: map[string]string{"TOKEN": "ENV-SECRET"}}); err != nil {
			t.Fatal(err)
		}
		if err := databases.AddDatabase(models.Database{ID: "d-" + id, ServerID: id, Password: "DB-SECRET"}); err != nil {
			t.Fatal(err)
		}
		if err := balancers.AddLoadBalancer(models.LoadBalancer{ID: "lb-" + id, ServerID: id}); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest("GET", "/api/servers/inventory", nil)
	request = request.WithContext(context.WithValue(request.Context(), middleware.AllowedServersContextKey, []string{"allowed"}))
	response := httptest.NewRecorder()
	NewServerHandler(servers).Inventory(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Data []struct {
			ID     string
			Counts store.ServerResourceCounts
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0].ID != "allowed" || result.Data[0].Counts != (store.ServerResourceCounts{Apps: 1, Data: 1, Edge: 1}) {
		t.Fatalf("unexpected scoped snapshot: %+v", result)
	}
	for _, value := range []string{"other", "SSH-SECRET", "DB-SECRET", "ENV-SECRET", "PRIVATE-PATH"} {
		if strings.Contains(response.Body.String(), value) {
			t.Fatalf("inventory exposed %s", value)
		}
	}
}
