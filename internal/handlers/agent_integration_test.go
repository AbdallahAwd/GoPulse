package handlers

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type agentIntegrationFixture struct {
	router http.Handler
	auth   *middleware.AuthMiddleware
	users  *store.UserStore
	agents *store.AgentStore
	ops    *services.Operations
	mcp    *MCPHandler
	writes atomic.Int32
}

func agentIntegration(t *testing.T) *agentIntegrationFixture {
	t.Helper()
	dir := t.TempDir()
	users, err := store.NewUserStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := users.AddUser(models.User{Username: "admin", Password: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := users.AddUser(models.User{Username: "operator", Password: "fixture", Role: "operator", Permissions: []string{"docker:write", "cron:write"}, AllowedServers: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := users.AddUser(models.User{Username: "viewer", Password: "fixture", Role: "viewer", AllowedServers: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	servers, err := store.NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := servers.AddServer(models.Server{ID: id, Name: "Server " + id, Host: "192.0.2.1", Port: 22}); err != nil {
			t.Fatal(err)
		}
	}
	agents, err := store.NewAgentStore(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.NewOperationStore(filepath.Join(dir, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := services.NewOperations(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	auth := middleware.NewAuthMiddleware(testAuthSecret, users)
	auth.SetAgentStore(agents)
	f := &agentIntegrationFixture{auth: auth, users: users, agents: agents, ops: ops}
	mcpHandler := &MCPHandler{Servers: servers, Operations: ops, Auth: auth}
	f.mcp = mcpHandler
	router := chi.NewRouter()
	router.Use(middleware.OriginGuard)
	router.Use(auth.Handler)
	router.Use(middleware.PermissionGuard)
	router.Handle("/api/mcp", mcpHandler)
	router.Route("/api/agents", (&AgentHandler{Store: agents, Servers: servers, MCP: mcpHandler}).Routes)
	oh := &OperationHandler{Operations: ops}
	router.Post("/api/operations/{operationID}/approve", oh.Approve)
	router.Post("/api/operations/{operationID}/reject", oh.Reject)
	router.Route("/api/servers/{serverID}", func(r chi.Router) {
		r.Use(auth.RequireServerAccess)
		r.Get("/containers", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(models.APIResponse{Success: true, Data: []map[string]interface{}{{"id": "container", "name": "fixture", "password": "DO-NOT-LEAK", "env_vars": map[string]string{"TOKEN": "DO-NOT-LEAK"}}}})
		})
		r.Post("/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
			f.writes.Add(1)
			json.NewEncoder(w).Encode(models.APIResponse{Success: true, Data: map[string]string{"status": "running"}})
		})
		r.Post("/containers/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
			f.writes.Add(1)
			json.NewEncoder(w).Encode(models.APIResponse{Success: true})
		})
		r.Get("/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, strings.Repeat("bounded log\n", 20000))
		})
		r.Post("/cron", func(w http.ResponseWriter, r *http.Request) {
			var input map[string]interface{}
			json.NewDecoder(r.Body).Decode(&input)
			f.writes.Add(1)
			json.NewEncoder(w).Encode(models.APIResponse{Success: true, Data: input})
		})
	})
	mcpHandler.API = router
	f.router = router
	return f
}
func (f *agentIntegrationFixture) browser(t *testing.T, owner, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := f.auth.GenerateToken(owner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "jwt_token", Value: token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GoPulse-Human-Approval", "true")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}
func (f *agentIntegrationFixture) credential(t *testing.T, owner, mode string, permissions []string, all bool) (models.AgentCredential, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]interface{}{"name": "Fixture agent", "mode": mode, "permissions": permissions, "server_ids": []string{"a"}, "all_servers": all, "expiry_hours": 24})
	w := f.browser(t, owner, "POST", "/api/agents", string(raw))
	if w.Code != 201 {
		t.Fatalf("credential %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Credential models.AgentCredential `json:"credential"`
			Token      string                 `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Credential, response.Data.Token
}
func (f *agentIntegrationFixture) rpc(token, method string, params interface{}) map[string]interface{} {
	raw, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	r := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	response["http_status"] = w.Code
	return response
}
func (f *agentIntegrationFixture) call(t *testing.T, token, name string, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	response := f.rpc(token, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("call %s: %+v", name, response)
	}
	return result
}
func operationFrom(t *testing.T, result map[string]interface{}) models.Operation {
	t.Helper()
	data := result["structuredContent"].(map[string]interface{})["data"]
	raw, _ := json.Marshal(data)
	var op models.Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		t.Fatal(err)
	}
	if op.ID == "" {
		t.Fatal("missing operation", result)
	}
	return op
}
func agentOperationDone(t *testing.T, f *agentIntegrationFixture, id string) models.Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, err := f.ops.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status != "running" && op.Status != "awaiting_approval" {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return models.Operation{}
}
func TestAgentReadOnlyScopeSecretsAndRESTBoundary(t *testing.T) {
	f := agentIntegration(t)
	_, token := f.credential(t, "viewer", "read_only", nil, false)
	list := f.rpc(token, "tools/list", map[string]interface{}{})
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "start_container") || strings.Contains(string(raw), "execute_server_command") || strings.Contains(string(raw), "read_file") {
		t.Fatal("write/sensitive tool discovered")
	}
	result := f.call(t, token, "list_containers", map[string]interface{}{"server_id": "a"})
	raw, _ = json.Marshal(result)
	if strings.Contains(string(raw), "DO-NOT-LEAK") {
		t.Fatal("structured credential leak")
	}
	if result := f.call(t, token, "list_containers", map[string]interface{}{"server_id": "b"}); result["isError"] != true {
		t.Fatal("cross-server read allowed")
	}
	r := httptest.NewRequest("POST", "/api/servers/a/containers/container/start", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-GoPulse-Agent-Dispatch", "true")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 || f.writes.Load() != 0 {
		t.Fatal("REST/marker bypass allowed")
	}
}
func TestAgentFullControlHasBoundedPermissionsAndDurableDeduplication(t *testing.T) {
	f := agentIntegration(t)
	credential, token := f.credential(t, "operator", "full_control", []string{"docker:write"}, false)
	args := map[string]interface{}{"server_id": "a", "container_id": "container", "request_id": "same-request"}
	first := operationFrom(t, f.call(t, token, "start_container", args))
	done := agentOperationDone(t, f, first.ID)
	if done.Status != "completed" || done.AgentCredentialID != credential.ID || f.writes.Load() != 1 {
		t.Fatal("full operation did not run once", done)
	}
	replay := operationFrom(t, f.call(t, token, "start_container", args))
	if replay.ID != first.ID || f.writes.Load() != 1 {
		t.Fatal("retry executed twice")
	}
	if result := f.call(t, token, "stop_container", args); result["isError"] != true || f.writes.Load() != 1 {
		t.Fatal("different operation reused request_id")
	}
	if result := f.call(t, token, "start_container", map[string]interface{}{"server_id": "b", "container_id": "container"}); result["isError"] != true {
		t.Fatal("full mode escaped server scope")
	}
	if response := f.rpc(token, "tools/call", map[string]interface{}{"name": "execute_server_command", "arguments": map[string]interface{}{"server_id": "a", "configuration": map[string]string{"command": "never"}}}); response["error"] == nil {
		t.Fatal("full mode invented terminal permission")
	}
}
func TestAgentSupervisedApprovalRejectAndRevocation(t *testing.T) {
	f := agentIntegration(t)
	credential, token := f.credential(t, "operator", "supervised", []string{"docker:write"}, false)
	op := operationFrom(t, f.call(t, token, "start_container", map[string]interface{}{"server_id": "a", "container_id": "container"}))
	if op.Status != "awaiting_approval" || f.writes.Load() != 0 {
		t.Fatal("supervised write executed")
	}
	w := f.browser(t, "operator", "POST", "/api/operations/"+op.ID+"/reject", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	done, _ := f.ops.Get(op.ID)
	if done.Status != "rejected" || f.writes.Load() != 0 {
		t.Fatal("rejected write executed")
	}
	op = operationFrom(t, f.call(t, token, "start_container", map[string]interface{}{"server_id": "a", "container_id": "container"}))
	w = f.browser(t, "operator", "DELETE", "/api/agents/"+credential.ID, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if response := f.rpc(token, "ping", nil); response["http_status"] != 401 {
		t.Fatal("revoked credential accepted", response)
	}
	w = f.browser(t, "operator", "POST", "/api/operations/"+op.ID+"/approve", "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	done = agentOperationDone(t, f, op.ID)
	if done.Status != "failed" || f.writes.Load() != 0 {
		t.Fatal("revoked queued write executed", done)
	}
	_, token = f.credential(t, "operator", "supervised", []string{"docker:write"}, false)
	op = operationFrom(t, f.call(t, token, "start_container", map[string]interface{}{"server_id": "a", "container_id": "container"}))
	w = f.browser(t, "operator", "POST", "/api/operations/"+op.ID+"/approve", "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if done := agentOperationDone(t, f, op.ID); done.Status != "completed" || f.writes.Load() != 1 {
		t.Fatal("approved write did not execute", done)
	}
}
func TestAgentScopeCannotExpandAndOwnerIdentityCannotBeReused(t *testing.T) {
	f := agentIntegration(t)
	for _, body := range []string{`{"name":"bad","mode":"full_control","permissions":["terminal:write"],"server_ids":["a"],"expiry_hours":24}`, `{"name":"bad","mode":"read_only","permissions":[],"server_ids":["b"],"expiry_hours":24}`, `{"name":"bad","mode":"read_only","permissions":[],"all_servers":true,"expiry_hours":24}`} {
		w := f.browser(t, "operator", "POST", "/api/agents", body)
		if w.Code != 403 {
			t.Fatal("scope expanded", w.Code, w.Body.String())
		}
	}
	_, token := f.credential(t, "operator", "full_control", []string{"docker:write"}, false)
	user, _ := f.users.GetUser("operator")
	if err := f.users.DeleteUserByID(user.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.users.AddUser(models.User{Username: "operator", Password: "fixture", Role: "admin", Permissions: []string{"*"}}); err != nil {
		t.Fatal(err)
	}
	if response := f.rpc(token, "ping", nil); response["http_status"] != 401 {
		t.Fatal("recreated username inherited old token")
	}
}

func TestAgentCurrentParentRightsConstrainExistingCredentials(t *testing.T) {
	f := agentIntegration(t)
	_, token := f.credential(t, "operator", "supervised", []string{"docker:write"}, false)
	op := operationFrom(t, f.call(t, token, "start_container", map[string]interface{}{"server_id": "a", "container_id": "container"}))
	user, _ := f.users.GetUser("operator")
	user.Permissions = nil
	if err := f.users.UpdateUser(user); err != nil {
		t.Fatal(err)
	}
	list, _ := json.Marshal(f.rpc(token, "tools/list", map[string]interface{}{}))
	if strings.Contains(string(list), "start_container") {
		t.Fatal("existing token retained revoked parent permission")
	}
	if w := f.browser(t, "operator", "POST", "/api/operations/"+op.ID+"/approve", ""); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if done, err := f.ops.Get(op.ID); err != nil || done.Status != "awaiting_approval" || f.writes.Load() != 0 {
		t.Fatal("queued work escaped updated parent permissions", done)
	}
	user.AllowedServers = []string{"b"}
	if err := f.users.UpdateUser(user); err != nil {
		t.Fatal(err)
	}
	if result := f.call(t, token, "list_containers", map[string]interface{}{"server_id": "a"}); result["isError"] != true {
		t.Fatal("existing token retained removed server access")
	}
}

func TestAgentAllServerAccessDoesNotGrantUnselectedPermissions(t *testing.T) {
	f := agentIntegration(t)
	_, token := f.credential(t, "admin", "full_control", nil, true)
	if result := f.call(t, token, "list_containers", map[string]interface{}{"server_id": "b"}); result["isError"] == true {
		t.Fatal("global server scope denied a permitted inventory read")
	}
	if response := f.rpc(token, "tools/call", map[string]interface{}{"name": "start_container", "arguments": map[string]string{"server_id": "b", "container_id": "container"}}); response["error"] == nil || f.writes.Load() != 0 {
		t.Fatal("global server scope granted an unselected write permission")
	}
}
func TestAgentLogsAreBoundedAndCredentialCreationIsHumanOnly(t *testing.T) {
	f := agentIntegration(t)
	_, token := f.credential(t, "viewer", "read_only", []string{"logs:read"}, false)
	result := f.call(t, token, "get_container_logs", map[string]interface{}{"server_id": "a", "container_id": "container"})
	raw, _ := json.Marshal(result)
	if len(raw) > 4*mcpOutputLimit || !strings.Contains(string(raw), `"truncated":true`) {
		t.Fatal("output is not bounded")
	}
	r := httptest.NewRequest("POST", "/api/agents", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("agent minted another credential")
	}
	w = f.browser(t, "viewer", "GET", "/api/agents", "")
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("secret stored in listing")
	}
}

type agentBearerTransport struct{ token string }

func (t agentBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}
func TestAgentOfficialSDKClientConnectsDiscoversAndCalls(t *testing.T) {
	f := agentIntegration(t)
	_, token := f.credential(t, "viewer", "read_only", nil, false)
	server := httptest.NewServer(f.router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/api/mcp", HTTPClient: &http.Client{Transport: agentBearerTransport{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) < 10 {
		t.Fatal("discovery failed", err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_containers", Arguments: map[string]string{"server_id": "a"}})
	if err != nil || result.IsError {
		t.Fatal("SDK tool call failed", err, result)
	}
}
func TestAgentCatalogCoversEveryResourceFamilyAndValidSchemas(t *testing.T) {
	f := agentIntegration(t)
	seen := map[string]bool{}
	for _, tool := range f.mcp.tools() {
		if seen[tool.Name] {
			t.Fatal("duplicate tool", tool.Name)
		}
		seen[tool.Name] = true
		// Resolving all schemas also catches nullable required fields and malformed definitions.
		if err := validateToolArgs(map[string]interface{}{}, tool.InputSchema); err != nil && strings.Contains(err.Error(), "resolve") {
			t.Fatal(tool.Name, err)
		}
	}
	for _, name := range []string{"list_servers", "list_applications", "start_application", "list_containers", "run_image", "execute_database_query", "create_database_backup", "browse_database_rows", "create_domain", "update_load_balancer", "write_file", "execute_server_command", "create_cron_job", "create_firewall_rule", "create_server_user", "create_dashboard_user"} {
		if !seen[name] {
			t.Fatal("missing capability", name)
		}
	}
	t.Logf("MCP tool catalog: %d tools", len(seen))
	if len(seen) < 100 {
		t.Fatal("incomplete catalog", len(seen))
	}
}
