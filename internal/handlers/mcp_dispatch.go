package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
)

type mcpIdempotencyKey struct{}

type mcpExecutionContext struct {
	context.Context
	values context.Context
}

func (c mcpExecutionContext) Value(key interface{}) interface{} {
	if value := c.Context.Value(key); value != nil {
		return value
	}
	return c.values.Value(key)
}

func mcpDepthOK(value interface{}, depth int) bool {
	if depth > 20 {
		return false
	}
	switch value := value.(type) {
	case map[string]interface{}:
		for _, item := range value {
			if !mcpDepthOK(item, depth+1) {
				return false
			}
		}
	case []interface{}:
		for _, item := range value {
			if !mcpDepthOK(item, depth+1) {
				return false
			}
		}
	}
	return true
}

// Redact structured credentials and environment maps. File/log contents are
// data granted by their explicit read scopes; no claim of content-level DLP is
// made. File content is omitted from persisted approval previews.
func mcpRedact(value interface{}, preview bool) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range value {
			lower := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			secret := lower == "env_vars" || lower == "environment" || lower == "ssh_key_content" || lower == "key_content" || lower == "private_key" || lower == "password" || lower == "token" || lower == "api_key" || strings.Contains(lower, "secret") || strings.HasSuffix(lower, "_password") || strings.HasSuffix(lower, "_token")
			if secret {
				out[key] = "[redacted]"
				continue
			}
			if preview && (lower == "content" || lower == "data") {
				raw, _ := json.Marshal(item)
				hash := sha256.Sum256(raw)
				out[key] = map[string]interface{}{"omitted": true, "bytes": len(raw), "sha256": hex.EncodeToString(hash[:])}
				continue
			}
			if text, ok := item.(string); ok && strings.Contains(lower, "url") {
				if parsed, err := url.Parse(text); err == nil && parsed.User != nil {
					parsed.User = nil
					item = parsed.String()
				}
			}
			out[key] = mcpRedact(item, preview)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, item := range value {
			out[i] = mcpRedact(item, preview)
		}
		return out
	case nil, string, bool, float64, int, int64, json.Number:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return "[unavailable]"
		}
		var decoded interface{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&decoded) != nil {
			return "[unavailable]"
		}
		return mcpRedact(decoded, preview)
	}
}

func mcpTargets(args map[string]interface{}) []string {
	seen := map[string]bool{}
	var visit func(interface{})
	visit = func(value interface{}) {
		switch value := value.(type) {
		case map[string]interface{}:
			for key, item := range value {
				if key == "target_server_id" {
					if id, ok := item.(string); ok && id != "" {
						seen[id] = true
					}
				}
				visit(item)
			}
		case []interface{}:
			for _, item := range value {
				visit(item)
			}
		}
	}
	visit(args)
	targets := []string{}
	for id := range seen {
		targets = append(targets, id)
	}
	sort.Strings(targets)
	return targets
}

func (h *MCPHandler) submit(r *http.Request, t mcpTool, owner, server, resource, kind, permission string, _ bool, preview interface{}, targets []string, work services.OperationWork) (models.Operation, error) {
	approval := middleware.AgentMode(r) != "full_control"
	credentialID, label := "", ""
	if credential := middleware.AgentCredential(r); credential != nil {
		credentialID, label = credential.ID, credential.Name
	}
	requestID, _ := r.Context().Value(mcpIdempotencyKey{}).(string)
	requestKey := ""
	if requestID != "" {
		hash := sha256.Sum256([]byte(credentialID + ":" + requestID))
		requestKey = hex.EncodeToString(hash[:])
	}
	raw, _ := json.Marshal(map[string]interface{}{"server": server, "tool": kind, "payload": preview, "targets": targets})
	fingerprint := sha256.Sum256(raw)
	wrapped := func(ctx context.Context, step func(string)) (interface{}, error) {
		snapshot := r.Clone(mcpExecutionContext{Context: ctx, values: r.Context()})
		if h.Auth != nil {
			var err error
			snapshot, err = h.Auth.RefreshAgentRequest(snapshot)
			if err != nil {
				return nil, err
			}
		}
		if !mcpToolVisible(snapshot, t) {
			return nil, fmt.Errorf("agent permission revoked before execution")
		}
		if server != "" && !middleware.CanAccessServer(snapshot, server) {
			return nil, fmt.Errorf("server access revoked before execution")
		}
		for _, target := range targets {
			if !middleware.CanAccessServer(snapshot, target) {
				return nil, fmt.Errorf("backend server access revoked before execution")
			}
		}
		// Use the refreshed permission context while retaining the worker deadline.
		result, err := work(snapshot.Context(), step)
		return mcpRedact(result, false), err
	}
	return h.Operations.SubmitReview(owner, server, resource, kind, permission, approval, mcpRedact(preview, true), targets, wrapped, credentialID, label, requestKey, hex.EncodeToString(fingerprint[:]))
}

func (h *MCPHandler) executeAPI(ctx context.Context, r *http.Request, t mcpTool, args map[string]interface{}) (interface{}, error) {
	if h.API == nil {
		return nil, fmt.Errorf("dashboard API integration unavailable")
	}
	targets := mcpTargets(args)
	for _, target := range targets {
		if !middleware.CanAccessServer(r, target) {
			return nil, fmt.Errorf("access to every linked server is required")
		}
	}
	if t.api.method == "GET" {
		return h.dispatchAPI(ctx, r, t, args)
	}
	server, _ := args["server_id"].(string)
	ids := []string{}
	for key, value := range args {
		if strings.HasSuffix(key, "_id") && key != "server_id" && key != "request_id" {
			ids = append(ids, key+":"+fmt.Sprint(value))
		}
	}
	sort.Strings(ids)
	resource := "mcp:" + server + ":" + t.api.category + ":" + strings.Join(ids, ":")
	preview := map[string]interface{}{"tool": t.Name, "server_id": server, "arguments": args}
	return h.submit(r, t, operationOwner(r), server, resource, t.Name, t.permission, true, preview, targets, func(ctx context.Context, step func(string)) (interface{}, error) {
		step("Execute authorized dashboard action")
		workerRequest := r.Clone(ctx)
		if h.Auth != nil {
			var err error
			workerRequest, err = h.Auth.RefreshAgentRequest(workerRequest)
			if err != nil {
				return nil, err
			}
		}
		result, err := h.dispatchAPI(ctx, workerRequest, t, args)
		if err != nil {
			return result, err
		}
		// Existing deployment handlers sometimes return a second async operation.
		// Wait for it so the MCP operation reflects completion, not just acceptance.
		if data, ok := result.(map[string]interface{}); ok && (t.Name == "update_load_balancer" || t.Name == "restore_qdrant_snapshot") {
			if payload, ok := data["data"].(map[string]interface{}); ok {
				if id, ok := payload["operation_id"].(string); ok && id != "" {
					step("Wait for dashboard deployment verification")
					ticker := time.NewTicker(500 * time.Millisecond)
					defer ticker.Stop()
					for {
						op, err := h.Operations.Get(id)
						if err != nil {
							return nil, err
						}
						switch op.Status {
						case "completed":
							return op.Result, nil
						case "failed", "interrupted", "expired", "rejected":
							return nil, fmt.Errorf("dashboard operation %s: %s", id, op.Error)
						}
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-ticker.C:
						}
					}
				}
			}
		}
		return result, nil
	})
}

const mcpOutputLimit = 128 * 1024

type mcpCapture struct {
	mu        sync.Mutex
	header    http.Header
	body      bytes.Buffer
	status    int
	truncated bool
}

func (w *mcpCapture) Header() http.Header { return w.header }
func (w *mcpCapture) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}
func (w *mcpCapture) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = 200
	}
	n := len(data)
	remaining := mcpOutputLimit - w.body.Len()
	if n > remaining {
		data = data[:remaining]
		w.truncated = true
	}
	w.body.Write(data)
	return n, nil
}
func (w *mcpCapture) Flush() {}

func (h *MCPHandler) dispatchAPI(ctx context.Context, r *http.Request, t mcpTool, args map[string]interface{}) (interface{}, error) {
	path := t.api.path
	for key, value := range args {
		path = strings.ReplaceAll(path, "{"+key+"}", url.PathEscape(fmt.Sprint(value)))
	}
	query := url.Values{}
	for _, key := range t.api.query {
		if value, ok := args[key]; ok {
			query.Set(key, fmt.Sprint(value))
		}
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	body := []byte("{}")
	if configuration, ok := args["configuration"]; ok {
		body, _ = json.Marshal(configuration)
	}
	// Chi's outer routing context is pooled and belongs to the MCP HTTP request.
	// Reusing it can reroute this call to /mcp or race after the request finishes.
	dispatchContext := context.WithValue(middleware.AgentDispatch(ctx), chi.RouteCtxKey, chi.NewRouteContext())
	request, err := http.NewRequestWithContext(dispatchContext, t.api.method, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", r.Header.Get("Authorization"))
	request.Header.Set("Content-Type", "application/json")
	request.Host = r.Host
	request.RemoteAddr = "127.0.0.1:0"
	capture := &mcpCapture{header: http.Header{}}
	// No network loopback, cookies, redirects, or authentication downgrade. Normal
	// middleware validates this bearer again, followed by resource ownership.
	h.API.ServeHTTP(capture, request)
	status := capture.status
	if status == 0 {
		status = 200
	}
	var value interface{}
	if capture.truncated || json.Unmarshal(capture.body.Bytes(), &value) != nil {
		value = map[string]interface{}{"output": strings.ToValidUTF8(capture.body.String(), "�"), "truncated": capture.truncated, "http_status": status}
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("dashboard action rejected (HTTP %d): %s", status, strings.ToValidUTF8(capture.body.String(), "�"))
	}
	if object, ok := value.(map[string]interface{}); ok {
		if success, ok := object["success"].(bool); ok && !success {
			return value, fmt.Errorf("dashboard action failed: %v", object["error"])
		}
	}
	if !t.api.allowSecrets {
		value = mcpRedact(value, false)
	}
	return value, nil
}
