package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

const testAuthSecret = "0123456789abcdef0123456789abcdef"

type mcpExecutor struct{ writes atomic.Int32 }

func (e *mcpExecutor) Run(_ context.Context, _ string, command string) (string, error) {
	if strings.Contains(command, "restart") {
		e.writes.Add(1)
		return "", nil
	}
	return "active", nil
}

type mcpFixture struct {
	router   http.Handler
	auth     *middleware.AuthMiddleware
	ops      *services.Operations
	users    *store.UserStore
	executor *mcpExecutor
	dbs      *store.DatabaseStore
}

func setupMCP(t *testing.T) *mcpFixture {
	t.Helper()
	dir := t.TempDir()
	users, err := store.NewUserStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	users.AddUser(models.User{Username: "admin", Password: "test"})
	users.AddUser(models.User{Username: "operator", Role: "operator", Permissions: []string{"services:write", "database:write", "domains:write"}, AllowedServers: []string{"server-a"}})
	users.AddUser(models.User{Username: "viewer", Role: "viewer", AllowedServers: []string{"server-a"}})
	servers, _ := store.NewServerStore(filepath.Join(dir, "servers.json"))
	servers.AddServer(models.Server{ID: "server-a", Host: "10.0.0.1", Password: "TOP-SECRET"})
	servers.AddServer(models.Server{ID: "server-b", Host: "10.0.0.2"})
	dbs, _ := store.NewDatabaseStore(filepath.Join(dir, "databases.json"))
	dbs.AddDatabase(models.Database{ID: "db-b", ServerID: "server-b", Type: "qdrant", Password: "DB-SECRET"})
	dbSvc := services.NewDatabaseService(dbs, nil)
	lbs, _ := store.NewLoadBalancerStore(filepath.Join(dir, "lbs.json"))
	projects, _ := store.NewProjectStore(filepath.Join(dir, "projects.json"))
	executor := &mcpExecutor{}
	manager := services.NewServiceManagerWithExecutor(servers, executor)
	lbSvc := services.NewLoadBalancerService(lbs, servers, projects, manager)
	st, _ := store.NewOperationStore(filepath.Join(dir, "operations.json"))
	ops, err := services.NewOperations(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	auth := middleware.NewAuthMiddleware(testAuthSecret, users)
	mcp := &MCPHandler{Servers: servers, Databases: dbSvc, LoadBalancers: lbSvc, Manager: manager, Operations: ops}
	router := chi.NewRouter()
	router.Use(middleware.OriginGuard)
	router.Use(auth.Handler)
	router.Use(middleware.PermissionGuard)
	router.Handle("/api/mcp", mcp)
	oh := &OperationHandler{Operations: ops}
	router.Post("/api/operations/{operationID}/approve", oh.Approve)
	router.Post("/api/agent-token", auth.AgentTokenHandler)
	router.Post("/api/servers/{serverID}/services/nginx/restart", func(w http.ResponseWriter, r *http.Request) { executor.writes.Add(1) })
	return &mcpFixture{router, auth, ops, users, executor, dbs}
}
func agentToken(t *testing.T, user string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"username": user, "agent": true, "aud": "gopulse-mcp", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(testAuthSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func (f *mcpFixture) request(method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}
func (f *mcpFixture) call(t *testing.T, user, tool string, args interface{}) map[string]interface{} {
	t.Helper()
	params, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": tool, "arguments": args}})
	w := f.request("POST", "/api/mcp", string(params), agentToken(t, user))
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid result: %v", response)
	}
	return result
}
func TestMCPDiscoveryReadAuthorizationAndSecrets(t *testing.T) {
	f := setupMCP(t)
	init := f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, agentToken(t, "operator"))
	if init.Code != 200 || !strings.Contains(init.Body.String(), "protocolVersion") {
		t.Fatal(init.Body.String())
	}
	list := f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, agentToken(t, "viewer"))
	if strings.Contains(list.Body.String(), "restart_service") || !strings.Contains(list.Body.String(), "list_servers") {
		t.Fatal("viewer tool discovery permissions")
	}
	result := f.call(t, "operator", "list_servers", map[string]interface{}{})
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "TOP-SECRET") || strings.Contains(string(raw), "server-b") || !strings.Contains(string(raw), "server-a") {
		t.Fatalf("unscoped/secret response %s", raw)
	}
	for _, request := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"list_databases", map[string]interface{}{"server_id": "server-b"}},
		{"get_database_status", map[string]interface{}{"server_id": "server-a", "database_id": "db-b"}},

		{"restart_service", map[string]interface{}{"server_id": "server-a", "service": "nginx;reboot"}},
		{"list_servers", map[string]interface{}{"unexpected": "value"}},
	} {
		if f.call(t, "operator", request.tool, request.args)["isError"] != true {
			t.Fatalf("accepted %+v", request)
		}
	}
	unknown := f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_shell","arguments":{}}}`, agentToken(t, "operator"))
	if !strings.Contains(unknown.Body.String(), `"error"`) {
		t.Fatal("arbitrary shell tool exposed")
	}
	if f.executor.writes.Load() != 0 {
		t.Fatal("denied operation executed")
	}
}
func TestMCPWriteRequiresHumanApprovalAndCannotBypassREST(t *testing.T) {
	f := setupMCP(t)
	result := f.call(t, "operator", "restart_service", map[string]interface{}{"server_id": "server-a", "service": "nginx"})
	data := result["structuredContent"].(map[string]interface{})["data"].(map[string]interface{})
	id := data["operation_id"].(string)
	if data["status"] != "awaiting_approval" || f.executor.writes.Load() != 0 {
		t.Fatal("write executed before approval")
	}
	denied := f.request("POST", "/api/operations/"+id+"/approve", "", agentToken(t, "operator"))
	if denied.Code != 403 {
		t.Fatal("agent approved itself")
	}
	denied = f.request("POST", "/api/servers/server-a/services/nginx/restart", "", agentToken(t, "operator"))
	if denied.Code != 403 {
		t.Fatal("agent bypassed MCP through REST")
	}
	browser, err := f.auth.GenerateToken("operator", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/operations/"+id+"/approve", nil)
	r.AddCookie(&http.Cookie{Name: "jwt_token", Value: browser})
	r.Header.Set("X-GoPulse-Human-Approval", "true")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("approval: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for {
		op, _ := f.ops.Get(id)
		if op.Status == "completed" {
			break
		}
		if op.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("operation: %+v", op)
		}
		time.Sleep(time.Millisecond)
	}
	if f.executor.writes.Load() != 1 {
		t.Fatal("write not executed exactly once")
	}
	replay := httptest.NewRecorder()
	f.router.ServeHTTP(replay, r)
	if replay.Code != 409 || f.executor.writes.Load() != 1 {
		t.Fatal("approval replay accepted")
	}
	// Durable audit storage is exercised by every request and approval; direct store tests verify rows.
}
func TestMCPRejectsMissingAuthCrossOriginAndRevokedUser(t *testing.T) {
	f := setupMCP(t)
	for _, token := range []string{"invalid", ""} {
		w := f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Authorization", "Bearer "+agentToken(t, "operator"))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
	token := agentToken(t, "viewer")
	u, _ := f.users.GetUser("viewer")
	f.users.DeleteUserByID(u.ID)
	w = f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token)
	if w.Code != 401 {
		t.Fatal("revoked user token accepted")
	}
}
func TestMCPRateLimitAndTransport(t *testing.T) {
	f := setupMCP(t)
	token := agentToken(t, "viewer")
	w := f.request("GET", "/api/mcp", "", token)
	if w.Code != 405 {
		t.Fatalf("GET %d", w.Code)
	}
	for i := 0; i < 60; i++ {
		w = f.request("POST", "/api/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, token)
	}
	if w.Code != 429 {
		t.Fatalf("rate limit %d", w.Code)
	}
}
func TestMCPSchemaValidation(t *testing.T) {
	f := setupMCP(t)
	for _, tool := range (&MCPHandler{}).tools() {
		if tool.Name == "create_qdrant_collection" {
			for _, size := range []interface{}{0, 65537, 3.5, "384"} {
				args := map[string]interface{}{"server_id": "server-a", "database_id": "db-a", "name": "test", "size": size, "distance": "Cosine"}
				if f.call(t, "operator", tool.Name, args)["isError"] != true {
					t.Fatal(fmt.Sprint(size))
				}
			}
		}
	}
}
