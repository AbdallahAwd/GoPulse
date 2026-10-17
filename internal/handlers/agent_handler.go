package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

var agentPermissions = []string{"files:read", "logs:read", "database:read", "secrets:read", "server:write", "projects:write", "database:write", "docker:write", "domains:write", "cron:write", "files:write", "terminal:write", "services:write", "processes:write", "system:write", "firewall:write", "user:manage", "dashboard:admin"}

type AgentHandler struct {
	MCP       *MCPHandler
	Store     *store.AgentStore
	Servers   *store.ServerStore
	PublicURL func() string
}

func (h *AgentHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				respondError(w, 403, "Manage agent access from a dashboard browser session", nil)
				return
			}
			if _, err := r.Cookie("jwt_token"); err != nil {
				respondError(w, 403, "Dashboard browser session required", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{agentID}", h.Revoke)
	r.Get("/configuration", h.Configuration)
	r.Get("/audit", h.Audit)
}
func agentPermissionAllowed(r *http.Request, p string) bool {
	if p == "files:read" || p == "logs:read" || p == "database:read" {
		return true
	}
	if p == "secrets:read" {
		return middleware.HasPermission(r, "database:write") || middleware.HasPermission(r, "projects:write")
	}
	if p == "dashboard:admin" {
		return middleware.HasPermission(r, "*")
	}
	return middleware.HasPermission(r, p)
}
func (h *AgentHandler) Configuration(w http.ResponseWriter, r *http.Request) {
	permissions := []string{}
	for _, p := range agentPermissions {
		if agentPermissionAllowed(r, p) {
			permissions = append(permissions, p)
		}
	}
	servers := []map[string]string{}
	for _, server := range h.Servers.ListServers() {
		if middleware.CanAccessServer(r, server.ID) {
			servers = append(servers, map[string]string{"id": server.ID, "name": server.Name, "host": server.Host})
		}
	}
	role, _ := r.Context().Value(middleware.RoleContextKey).(string)
	categories := map[string]int{}
	if h.MCP != nil {
		for _, tool := range h.MCP.tools() {
			category := "infrastructure"
			if tool.api != nil {
				category = tool.api.category
			}
			categories[category]++
		}
	}
	publicURL := ""
	if h.PublicURL != nil {
		publicURL = h.PublicURL()
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: map[string]interface{}{"public_url": publicURL, "tool_categories": categories, "endpoint": "/api/mcp", "permissions": permissions, "servers": servers, "can_select_all_servers": role == "admin", "modes": []string{"read_only", "supervised", "full_control"}, "max_expiry_hours": 720}})
}
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	credentials, err := h.Store.List(operationOwner(r), agentOwnerID(r))
	if err != nil {
		respondError(w, 500, "Unable to read agent credentials", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, 200, models.APIResponse{Success: true, Data: credentials})
}
func (h *AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string   `json:"name"`
		Mode        string   `json:"mode"`
		Permissions []string `json:"permissions"`
		ServerIDs   []string `json:"server_ids"`
		AllServers  bool     `json:"all_servers"`
		ExpiryHours int      `json:"expiry_hours"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		respondError(w, 400, "Invalid credential settings", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send one settings object", nil)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 80 {
		respondError(w, 400, "Name must contain 1–80 characters", nil)
		return
	}
	if input.Mode != "read_only" && input.Mode != "supervised" && input.Mode != "full_control" {
		respondError(w, 400, "Choose read_only, supervised, or full_control", nil)
		return
	}
	if input.ExpiryHours < 1 || input.ExpiryHours > 720 {
		respondError(w, 400, "Expiry must be 1–720 hours", nil)
		return
	}
	role, _ := r.Context().Value(middleware.RoleContextKey).(string)
	if input.AllServers && role != "admin" {
		respondError(w, 403, "Only administrators may grant access to all current and future servers", nil)
		return
	}
	if !input.AllServers && len(input.ServerIDs) == 0 {
		respondError(w, 400, "Choose at least one server", nil)
		return
	}
	if len(input.ServerIDs) > 1000 || len(input.Permissions) > len(agentPermissions) {
		respondError(w, 400, "Too many scopes", nil)
		return
	}
	permissions := []string{}
	seen := map[string]bool{}
	for _, p := range input.Permissions {
		known := false
		for _, allowed := range agentPermissions {
			if p == allowed {
				known = true
			}
		}
		if !known || !agentPermissionAllowed(r, p) {
			respondError(w, 403, "Scope unavailable: "+p, nil)
			return
		}
		if (p == "dashboard:admin" || p == "user:manage") && !input.AllServers {
			respondError(w, 400, "Global administration scopes require all-server access", nil)
			return
		}
		if !seen[p] {
			permissions = append(permissions, p)
			seen[p] = true
		}
	}
	serverIDs := []string{}
	seen = map[string]bool{}
	for _, id := range input.ServerIDs {
		if !middleware.CanAccessServer(r, id) {
			respondError(w, 403, "Server access denied", nil)
			return
		}
		if _, err := h.Servers.GetServer(id); err != nil {
			respondError(w, 400, "Server not found", nil)
			return
		}
		if !seen[id] {
			serverIDs = append(serverIDs, id)
			seen[id] = true
		}
	}
	credentials, err := h.Store.List(operationOwner(r), agentOwnerID(r))
	if err != nil {
		respondError(w, 500, "Credential store unavailable", nil)
		return
	}
	active := 0
	for _, item := range credentials {
		if item.RevokedAt.IsZero() && time.Now().Before(item.ExpiresAt) {
			active++
		}
	}
	if active >= 25 {
		respondError(w, 409, "Revoke an existing credential before creating another (maximum 25 active)", nil)
		return
	}
	credential, secret, err := h.Store.Create(models.AgentCredential{OwnerID: agentOwnerID(r), Name: input.Name, Owner: operationOwner(r), Mode: input.Mode, Permissions: permissions, ServerIDs: serverIDs, AllServers: input.AllServers, ExpiresAt: time.Now().Add(time.Duration(input.ExpiryHours) * time.Hour).UTC()})
	if err != nil {
		respondError(w, 500, "Unable to create credential", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, 201, models.APIResponse{Success: true, Data: map[string]interface{}{"credential": credential, "token": secret, "endpoint": "/api/mcp"}})
}
func (h *AgentHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.Revoke(operationOwner(r), chi.URLParam(r, "agentID"), agentOwnerID(r)); err != nil {
		respondError(w, 404, "Credential not found", nil)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Message: "Credential revoked"})
}
func (h *AgentHandler) Audit(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.Audit(operationOwner(r), 100)
	if err != nil {
		respondError(w, 500, "Audit history unavailable", nil)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: items})
}

func agentOwnerID(r *http.Request) string {
	id, _ := r.Context().Value(middleware.UserIDContextKey).(string)
	return id
}
