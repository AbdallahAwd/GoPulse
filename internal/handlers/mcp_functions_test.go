package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type gatewayBackend struct {
	calls atomic.Int32
	block atomic.Bool
	fail  atomic.Bool
}

func (b *gatewayBackend) Prepare(context.Context, models.Function, models.FunctionRun, io.Writer) (string, error) {
	return "sha256:" + strings.Repeat("a", 64), nil
}
func (b *gatewayBackend) Cleanup(context.Context, models.FunctionRun) error { return nil }
func (b *gatewayBackend) Execute(ctx context.Context, _ models.FunctionVersion, _ models.FunctionRun, event json.RawMessage, w io.Writer) (int, error) {
	b.calls.Add(1)
	if b.block.Load() {
		<-ctx.Done()
		return -1, ctx.Err()
	}
	io.WriteString(w, "fixture console\n")
	if b.fail.Load() {
		return 1, fmt.Errorf("fixture failure")
	}
	w.(interface{ SetResult(json.RawMessage) }).SetResult(event)
	return 0, nil
}

type functionGatewayFixture struct {
	router     http.Handler
	service    *services.FunctionService
	backend    *gatewayBackend
	auth       *middleware.AuthMiddleware
	agents     *store.AgentStore
	ops        *services.Operations
	owner      models.User
	token      string
	credential models.AgentCredential
}

func newFunctionGateway(t *testing.T, mode string) *functionGatewayFixture {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "state.json")
	servers, err := store.NewServerStore(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err = servers.AddServer(models.Server{ID: id, Host: id + ".invalid"}); err != nil {
			t.Fatal(err)
		}
	}
	users, err := store.NewUserStore(file)
	if err != nil {
		t.Fatal(err)
	}
	owner := models.User{ID: "owner", Username: "owner", Role: "admin"}
	if err = users.AddUserRaw(owner); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewFunctionStore(file)
	if err != nil {
		t.Fatal(err)
	}
	backend := &gatewayBackend{}
	svc, err := services.NewFunctionService(st, servers, users, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Close)
	agents, err := store.NewAgentStore(file)
	if err != nil {
		t.Fatal(err)
	}
	credential, token, err := agents.Create(models.AgentCredential{Owner: owner.Username, OwnerID: owner.ID, Name: "test assistant", Mode: mode, Permissions: []string{"server:write", "docker:write"}, ServerIDs: []string{"a"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	auth := middleware.NewAuthMiddleware(testAuthSecret, users)
	auth.SetAgentStore(agents)
	opStore, err := store.NewOperationStore(file)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := services.NewOperations(opStore)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	handler := &MCPHandler{Auth: auth, Servers: servers, Functions: svc, Operations: ops}
	router := chi.NewRouter()
	router.Use(middleware.OriginGuard)
	router.Use(auth.Handler)
	router.Use(middleware.PermissionGuard)
	router.Handle("/api/mcp", handler)
	router.Route("/api/servers/{serverID}/functions", (&FunctionHandler{Service: svc}).Routes)
	return &functionGatewayFixture{router, svc, backend, auth, agents, ops, owner, token, credential}
}
func (f *functionGatewayFixture) function(t *testing.T, server string, enabled, deployed bool) models.Function {
	t.Helper()
	spec := models.FunctionSpec{Name: "Echo", Runtime: "python", Code: "def handler(event, context): return event", TimeoutSeconds: 2, MemoryMB: 256, CPUMillis: 500, Concurrency: 1, Timezone: "UTC"}
	fn, err := f.service.Save(server, "", "owner", spec)
	if err != nil {
		t.Fatal(err)
	}
	if deployed {
		run, err := f.service.Begin(server, fn.ID, "owner", "deploy", "manual", nil)
		if err != nil {
			t.Fatal(err)
		}
		f.waitRun(t, fn.ID, run.ID)
		fn, err = f.service.Store.Get(server, fn.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if enabled {
		fn, err = f.service.SetMCP(server, fn.ID, "owner", true)
		if err != nil {
			t.Fatal(err)
		}
	}
	return fn
}
func (f *functionGatewayFixture) waitRun(t *testing.T, id, run string) models.FunctionRun {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.service.Store.Run(id, run)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "running" {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return models.FunctionRun{}
}
func (f *functionGatewayFixture) request(token, method string, params any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func (f *functionGatewayFixture) call(t *testing.T, token, name string, args any) map[string]any {
	t.Helper()
	w := f.request(token, "tools/call", map[string]any{"name": name, "arguments": args})
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var rpc struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Result == nil {
		t.Fatalf("RPC error: %s", w.Body.String())
	}
	return rpc.Result
}
func gatewayOperation(t *testing.T, result map[string]any) models.Operation {
	t.Helper()
	raw, _ := json.Marshal(result["structuredContent"])
	var out struct {
		Data models.Operation `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Data.ID == "" {
		t.Fatalf("invalid operation %s", raw)
	}
	return out.Data
}
func (f *functionGatewayFixture) waitOperation(t *testing.T, id string) models.Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op, err := f.ops.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status != "running" {
			return op
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return models.Operation{}
}
func TestFunctionMCPSelectionAndAuthorization(t *testing.T) {
	f := newFunctionGateway(t, "full_control")
	selected := f.function(t, "a", true, true)
	hidden := f.function(t, "a", false, true)
	other := f.function(t, "b", true, true)
	draft := f.function(t, "a", false, false)
	list := f.request(f.token, "tools/list", map[string]any{})
	if list.Code != 200 || !strings.Contains(list.Body.String(), functionToolName(selected.ID)) {
		t.Fatal(list.Body.String())
	}
	for _, fn := range []models.Function{hidden, other, draft} {
		if strings.Contains(list.Body.String(), functionToolName(fn.ID)) {
			t.Fatal("unselected or unauthorized function discovered")
		}
	}
	if _, err := f.service.SetMCP("a", draft.ID, "owner", true); err == nil {
		t.Fatal("draft exposed")
	}
	for _, mode := range []string{"read_only", "supervised"} {
		permissions := []string{"server:write"}
		if mode == "read_only" {
			permissions = append(permissions, "docker:write")
		}
		_, token, err := f.agents.Create(models.AgentCredential{Owner: "owner", OwnerID: "owner", Name: mode, Mode: mode, Permissions: permissions, ServerIDs: []string{"a"}, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f.request(token, "tools/list", map[string]any{}).Body.String(), functionToolName(selected.ID)) {
			t.Fatal("insufficient permissions discover function")
		}
	}
	if strings.Contains(f.request(agentToken(t, "owner"), "tools/list", map[string]any{}).Body.String(), functionToolName(selected.ID)) {
		t.Fatal("legacy JWT exposed function")
	}
	f.service.Pause("a", selected.ID, true)
	if strings.Contains(f.request(f.token, "tools/list", map[string]any{}).Body.String(), functionToolName(selected.ID)) {
		t.Fatal("paused function discoverable")
	}
	req := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("origin not checked")
	}
	req = httptest.NewRequest("POST", "/api/servers/a/functions/"+selected.ID+"/mcp", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Authorization", "Bearer "+f.token)
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("agent can expose functions")
	}
}
func TestFunctionMCPResultIdempotencyAndCredentialIsolation(t *testing.T) {
	f := newFunctionGateway(t, "full_control")
	fn := f.function(t, "a", true, true)
	args := map[string]any{"event": map[string]any{"answer": 42, "exact_integer": json.Number("9007199254740993"), "password": "private"}, "request_id": "echo-once"}
	op := gatewayOperation(t, f.call(t, f.token, functionToolName(fn.ID), args))
	done := f.waitOperation(t, op.ID)
	raw, _ := json.Marshal(done.Result)
	if done.Status != "completed" || !strings.Contains(string(raw), `"answer":42`) || !strings.Contains(string(raw), `"exact_integer":9007199254740993`) || strings.Contains(string(raw), `"password":"private"`) {
		t.Fatal(done.Status, string(raw), done.Error)
	}
	if got := gatewayOperation(t, f.call(t, f.token, functionToolName(fn.ID), args)); got.ID != op.ID || f.backend.calls.Load() != 1 {
		t.Fatal("retry executed again")
	}
	if result := f.call(t, f.token, "get_operation_status", map[string]any{"operation_id": op.ID}); result["isError"] == true {
		t.Fatal(result)
	}
	_, other, err := f.agents.Create(models.AgentCredential{Owner: "owner", OwnerID: "owner", Name: "other", Mode: "full_control", Permissions: []string{"server:write", "docker:write"}, ServerIDs: []string{"a"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if f.call(t, other, "get_operation_status", map[string]any{"operation_id": op.ID})["isError"] != true {
		t.Fatal("another credential read function result")
	}
	args["event"] = map[string]any{"answer": 43}
	if f.call(t, f.token, functionToolName(fn.ID), args)["isError"] != true {
		t.Fatal("request ID reused with different event")
	}
	f.service.SetMCP("a", fn.ID, "owner", false)
	if f.call(t, f.token, "get_operation_status", map[string]any{"operation_id": op.ID})["isError"] != true {
		t.Fatal("disabled function results still exposed")
	}
}
func TestFunctionMCPSupervisedRechecksBeforeExecution(t *testing.T) {
	for _, change := range []string{"disable", "redeploy", "revoke", "permissions"} {
		t.Run(change, func(t *testing.T) {
			f := newFunctionGateway(t, "supervised")
			fn := f.function(t, "a", true, true)
			op := gatewayOperation(t, f.call(t, f.token, functionToolName(fn.ID), map[string]any{"event": map[string]any{}, "request_id": "pending"}))
			if op.Status != "awaiting_approval" || f.backend.calls.Load() != 0 {
				t.Fatal("supervised call ran before approval")
			}
			switch change {
			case "disable":
				f.service.SetMCP("a", fn.ID, "owner", false)
			case "redeploy":
				run, err := f.service.Begin("a", fn.ID, "owner", "deploy", "manual", nil)
				if err != nil {
					t.Fatal(err)
				}
				f.waitRun(t, fn.ID, run.ID)
			case "revoke":
				if err := f.agents.Revoke("owner", f.credential.ID, "owner"); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				u := f.owner
				u.Role = "operator"
				u.Permissions = []string{"server:write"}
				u.AllowedServers = []string{"a"}
				if err := f.service.Users.UpdateUser(u); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.ops.Approve(op.ID, "owner", func(string) bool { return true }); err != nil {
				t.Fatal(err)
			}
			if done := f.waitOperation(t, op.ID); done.Status != "failed" || f.backend.calls.Load() != 0 {
				t.Fatal("changed authorization/version executed", done)
			}
		})
	}
}
func TestFunctionMCPRevocationCancelsRunningInvocation(t *testing.T) {
	f := newFunctionGateway(t, "full_control")
	fn := f.function(t, "a", true, true)
	f.backend.block.Store(true)
	op := gatewayOperation(t, f.call(t, f.token, functionToolName(fn.ID), map[string]any{"event": map[string]any{}, "request_id": "cancel-on-revoke"}))
	deadline := time.Now().Add(2 * time.Second)
	for f.backend.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.backend.calls.Load() != 1 {
		t.Fatal("function did not start")
	}
	if err := f.agents.Revoke("owner", f.credential.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if done := f.waitOperation(t, op.ID); done.Status != "failed" {
		t.Fatal(done)
	}
	runs, err := f.service.Store.Runs(fn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.Trigger == "mcp" {
			if final := f.waitRun(t, fn.ID, run.ID); final.Status != "cancelled" {
				t.Fatal(final)
			}
			return
		}
	}
	t.Fatal("MCP invocation missing")
}

type gatewayBearerTransport struct{ token string }

func (tr gatewayBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+tr.token)
	return http.DefaultTransport.RoundTrip(copy)
}
func TestFunctionMCPRealSDKClient(t *testing.T) {
	f := newFunctionGateway(t, "full_control")
	fn := f.function(t, "a", true, true)
	server := httptest.NewServer(f.router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/api/mcp", HTTPClient: &http.Client{Transport: gatewayBearerTransport{f.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range list.Tools {
		if tool.Name == functionToolName(fn.ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("SDK could not discover selected function")
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: functionToolName(fn.ID), Arguments: map[string]any{"event": map[string]any{"sdk": true}, "request_id": "sdk-call"}})
	if err != nil || result.IsError {
		t.Fatal(err, result)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var accepted struct {
		Data models.Operation `json:"data"`
	}
	json.Unmarshal(raw, &accepted)
	done := f.waitOperation(t, accepted.Data.ID)
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_operation_status", Arguments: map[string]any{"operation_id": done.ID}})
	if err != nil || result.IsError {
		t.Fatal(err, result)
	}
	raw, _ = json.Marshal(result.StructuredContent)
	if !strings.Contains(string(raw), `"sdk":true`) {
		t.Fatal(string(raw))
	}
}
func TestFunctionMCPSuccessfulApprovalAndExecutionFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		f := newFunctionGateway(t, "supervised")
		fn := f.function(t, "a", true, true)
		f.backend.fail.Store(failed)
		op := gatewayOperation(t, f.call(t, f.token, functionToolName(fn.ID), map[string]any{"event": map[string]any{"approved": true}, "request_id": "approved-call"}))
		if op.Status != "awaiting_approval" {
			t.Fatal(op)
		}
		if err := f.ops.Approve(op.ID, "owner", func(string) bool { return true }); err != nil {
			t.Fatal(err)
		}
		done := f.waitOperation(t, op.ID)
		if failed && done.Status != "failed" || !failed && done.Status != "completed" || f.backend.calls.Load() != 1 {
			t.Fatal(done)
		}
		raw, _ := json.Marshal(done.Result)
		if !strings.Contains(string(raw), "fixture console") || !strings.Contains(string(raw), "invocation_id") {
			t.Fatal("missing result/log metadata", string(raw))
		}
	}
}
func TestFunctionMCPInputValidationAndExposureAPI(t *testing.T) {
	f := newFunctionGateway(t, "full_control")
	fn := f.function(t, "a", true, true)
	for _, args := range []map[string]any{
		{"event": map[string]any{}},
		{"event": map[string]any{}, "request_id": ""},
		{"event": map[string]any{}, "request_id": "unknown", "server_id": "b"},
		{"event": map[string]any{"large": strings.Repeat("x", 32769)}, "request_id": "large"},
	} {
		w := f.request(f.token, "tools/call", map[string]any{"name": functionToolName(fn.ID), "arguments": args})
		if !strings.Contains(w.Body.String(), `"isError":true`) && !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatal("invalid arguments accepted", w.Body.String())
		}
	}
	if f.backend.calls.Load() != 0 {
		t.Fatal("invalid input executed")
	}
	token, err := f.auth.GenerateToken("owner", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"yes"}`, `{"enabled":true,"unknown":true}`} {
		req := httptest.NewRequest("POST", "/api/servers/a/functions/"+fn.ID+"/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/servers/a/functions/"+fn.ID+"/mcp", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := f.service.Store.Get("a", fn.ID)
	if got.MCPEnabled {
		t.Fatal("exposure API did not disable")
	}
}
