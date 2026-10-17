package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/monitor"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

func monitoringRouter(t *testing.T) (http.Handler, *services.MonitoringService) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servers.json")
	servers, err := store.NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err = servers.AddServer(models.Server{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := services.NewMonitoringService(st, servers, 5*time.Second)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			if role == "" {
				role = "admin"
			}
			ctx := context.WithValue(r.Context(), middleware.RoleContextKey, role)
			ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, []string{"a"})
			ctx = context.WithValue(ctx, middleware.PermissionsContextKey, strings.Fields(r.Header.Get("X-Test-Permissions")))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	router.Use(middleware.PermissionGuard)
	h := &MonitoringHandler{Service: service}
	router.Route("/api/servers/{serverID}/alerts", h.Routes)
	collector := monitor.NewCollector(servers, nil, time.Second, monitor.NewPersistentStatsHistory(st))
	stats := NewStatsHandler(collector)
	router.Get("/api/servers/{serverID}/stats/history", stats.GetStatsHistory)
	return router, service
}

const alertInputJSON = `{"name":"Disk capacity","metric":"disk","threshold":85,"duration_seconds":300,"enabled":true,"webhook_url":"https://hooks.example/events?token=private-example"}`

func TestMonitoringAPIManagementAndScope(t *testing.T) {
	router, _ := monitoringRouter(t)
	created := cronHTTP(router, "POST", "/api/servers/a/alerts", alertInputJSON, "admin", "")
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var envelope struct {
		Data models.AlertRule `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	id := envelope.Data.ID
	if len(envelope.Data.WebhookSecret) != 64 || !envelope.Data.HasWebhook || envelope.Data.State != "unknown" {
		t.Fatal(envelope.Data)
	}
	for _, tc := range []struct {
		method, path, body, role, permissions string
		status                                int
	}{
		{"GET", "/api/servers/a/alerts", "", "viewer", "", 200},
		{"GET", "/api/servers/a/alerts/events", "", "viewer", "", 200},
		{"GET", "/api/servers/b/alerts", "", "viewer", "", 403},
		{"POST", "/api/servers/a/alerts", alertInputJSON, "viewer", "", 403},
		{"PUT", "/api/servers/a/alerts/" + id, alertInputJSON, "viewer", "", 403},
		{"DELETE", "/api/servers/a/alerts/" + id, "", "viewer", "", 403},
		{"POST", "/api/servers/a/alerts/" + id + "/test", "{}", "viewer", "", 403},
		{"PUT", "/api/servers/a/alerts/" + id + "/silence", `{"seconds":3600}`, "viewer", "", 403},
		{"PUT", "/api/servers/b/alerts/" + id, alertInputJSON, "admin", "", 404},
		{"DELETE", "/api/servers/b/alerts/" + id, "", "admin", "", 404},
		{"POST", "/api/servers/b/alerts/" + id + "/test", "{}", "admin", "", 404},
		{"GET", "/api/servers/missing/alerts", "", "admin", "", 404},
		{"PUT", "/api/servers/a/alerts/" + id, alertInputJSON, "viewer", "server:write", 200},
		{"PUT", "/api/servers/a/alerts/" + id + "/silence", `{"seconds":3600}`, "admin", "", 200},
		{"PUT", "/api/servers/a/alerts/" + id + "/silence", `{"seconds":0}`, "admin", "", 200},
		{"POST", "/api/servers/a/alerts/" + id + "/test", "{}", "admin", "", 202},
	} {
		response := cronHTTP(router, tc.method, tc.path, tc.body, tc.role, tc.permissions)
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
		if tc.method == "GET" && tc.role == "viewer" {
			if strings.Contains(response.Body.String(), "private-example") || strings.Contains(response.Body.String(), envelope.Data.WebhookSecret) {
				t.Fatal("viewer received webhook credentials")
			}
		}
	}
	response := cronHTTP(router, "GET", "/api/servers/a/alerts/events", "", "admin", "")
	if strings.Contains(response.Body.String(), "private-example") || strings.Contains(response.Body.String(), envelope.Data.WebhookSecret) {
		t.Fatal("event API leaked destination or secret")
	}
	if response = cronHTTP(router, "DELETE", "/api/servers/a/alerts/"+id, "", "admin", ""); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
}
func TestMonitoringAPIRejectsBadInput(t *testing.T) {
	router, _ := monitoringRouter(t)
	for _, raw := range []string{"", `null`, `[]`, alertInputJSON + ` {}`, `{"name":"x","extra":true}`, strings.Repeat("x", 9000), `{"name":"x","metric":"cpu","threshold":-1}`, `{"name":"x","metric":"cpu","webhook_url":"http://example.com"}`} {
		response := cronHTTP(router, "POST", "/api/servers/a/alerts", raw, "admin", "")
		if response.Code != 400 {
			t.Fatalf("accepted bad input (%d): %s", response.Code, response.Body.String())
		}
	}
}
func TestPersistentHistoryAPIAndValidation(t *testing.T) {
	router, service := monitoringRouter(t)
	now := time.Now().UTC()
	if err := service.Observe("a", &models.SystemStats{CPU: models.CPUStats{Percent: 21}, Disk: []models.DiskStats{{UsedPercent: 71}}}, now); err != nil {
		t.Fatal(err)
	}
	for _, rangeValue := range []string{"1h", "6h", "24h", "7d", "30d"} {
		response := cronHTTP(router, "GET", "/api/servers/a/stats/history?range="+rangeValue, "", "viewer", "")
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"cpu_percent":21`) {
			t.Fatal(rangeValue, response.Code, response.Body.String())
		}
	}
	for _, rangeValue := range []string{"-1h", "999999h", "bad", "0"} {
		response := cronHTTP(router, "GET", "/api/servers/a/stats/history?range="+rangeValue, "", "viewer", "")
		if response.Code != 400 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response := cronHTTP(router, "GET", "/api/servers/b/stats/history?range=1h", "", "viewer", "")
	if response.Code != 403 {
		t.Fatal("history scope bypass", response.Code)
	}
}
