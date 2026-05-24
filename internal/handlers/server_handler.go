package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
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

// ListServers returns all configured servers
func (h *ServerHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	servers := h.store.ListServers()

	// Sanitize passwords before returning
	safeServers := make([]models.Server, len(servers))
	for i, s := range servers {
		s.Password = "" // Do not return password
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

	server.Password = "" // Sanitize

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
	keysDir := filepath.Join(os.Getenv("HOME"), ".gopulse", "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		http.Error(w, "Failed to create keys directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%s_%s", uuid.New().String()[:8], header.Filename)
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
