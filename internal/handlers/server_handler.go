package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ServerHandler handles server management operations
type ServerHandler struct {
	store *store.ServerStore
}

// NewServerHandler creates a new ServerHandler
func NewServerHandler(store *store.ServerStore) *ServerHandler {
	return &ServerHandler{store: store}
}

// ListServers returns all configured servers (filtered for restricted users)
func (h *ServerHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	servers := h.store.ListServers()

	role, _ := r.Context().Value(middleware.RoleContextKey).(string)
	allowedServers, _ := r.Context().Value(middleware.AllowedServersContextKey).([]string)

	var filteredServers []models.Server
	for _, s := range servers {
		if role == "admin" {
			filteredServers = append(filteredServers, s)
		} else {
			for _, allowedID := range allowedServers {
				if s.ID == allowedID {
					filteredServers = append(filteredServers, s)
					break
				}
			}
		}
	}

	// Sanitize passwords before returning
	safeServers := make([]models.Server, len(filteredServers))
	for i, s := range filteredServers {
		s.Password = "" // Do not return password
		s.SSHKeyContent = ""
		safeServers[i] = s
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    safeServers,
	})
}

// AddServer adds a new server
func (h *ServerHandler) AddServer(w http.ResponseWriter, r *http.Request) {
	var server models.Server
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if server.Name == "" || server.Host == "" {
		http.Error(w, "Name and Host are required", http.StatusBadRequest)
		return
	}

	// Generate ID if not present
	if server.ID == "" {
		server.ID = uuid.New().String()
	}

	// Set default port if 0
	if server.Port == 0 {
		server.Port = 22
	}

	if err := h.store.AddServer(server); err != nil {
		http.Error(w, "Failed to add server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.Password = ""
	server.SSHKeyContent = ""
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    server,
	})
}

// RemoveServer removes a server
func (h *ServerHandler) RemoveServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serverID")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := h.store.CheckFunctionDeletion(id); err != nil {
		respondError(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err := h.store.CheckRemoteBackupDeletion(id); err != nil {
		respondError(w, http.StatusConflict, "Remote backup plans reference this server; pause and remove them before removing the server.", nil)
		return
	}
	if err := h.store.RemoveServer(id); err != nil {
		http.Error(w, "Failed to remove server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Server removed",
	})
}

// GetServer returns a single server
func (h *ServerHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serverID")
	server, err := h.store.GetServer(id)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	server.Password = ""
	server.SSHKeyContent = ""
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    server,
	})
}

// UploadKey handles SSH private key file upload
func (h *ServerHandler) UploadKey(w http.ResponseWriter, r *http.Request) {
	// Limit upload size to 1MB (SSH keys are small)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	file, header, err := r.FormFile("key")
	if err != nil {
		http.Error(w, "Failed to read key file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create keys directory
	keysDir := filepath.Join(store.DataDirectory(), "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		http.Error(w, "Failed to create keys directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%s_%s", uuid.New().String()[:8], filepath.Base(header.Filename))
	keyPath := filepath.Join(keysDir, filename)

	// Write key file with restricted permissions
	dst, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		http.Error(w, "Failed to save key file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(keyPath) // Clean up on failure
		http.Error(w, "Failed to write key file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]string{
			"key_path": keyPath,
		},
	})
}

// UpdateServer updates an existing server configuration
func (h *ServerHandler) UpdateServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serverID")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	var server models.Server
	if err := json.NewDecoder(r.Body).Decode(&server); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	server.ID = id

	// Retain existing password if blank in request
	original, err := h.store.GetServer(id)
	if err == nil && server.Password == "" {
		server.Password = original.Password
	}

	if err := h.store.UpdateServer(server); err != nil {
		http.Error(w, "Failed to update server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.Password = ""
	server.SSHKeyContent = ""
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    server,
	})
}

// Inventory is a permission-scoped fleet snapshot. The DTO deliberately omits
// passwords, private keys, project environment variables, and database secrets.
func (h *ServerHandler) Inventory(w http.ResponseWriter, r *http.Request) {
	counts, err := h.store.ResourceCounts()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Unable to read server inventory", nil)
		return
	}
	type item struct {
		ID       string                     `json:"id"`
		Name     string                     `json:"name"`
		Host     string                     `json:"host"`
		Port     int                        `json:"port"`
		User     string                     `json:"user"`
		AuthType string                     `json:"auth_type"`
		Tags     []string                   `json:"tags"`
		IsDbOnly bool                       `json:"is_db_only"`
		Counts   store.ServerResourceCounts `json:"counts"`
	}
	items := []item{}
	for _, server := range h.store.ListServers() {
		if !middleware.CanAccessServer(r, server.ID) {
			continue
		}
		items = append(items, item{server.ID, server.Name, server.Host, server.Port, server.User, server.AuthType, server.Tags, server.IsDbOnly, counts[server.ID]})
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Data: items})
}
