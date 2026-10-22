package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"
)

func canonicalHostKey(raw string) (string, error) {
	key, err := remote.ParseHostKey(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), nil
}

// ProbeSSHHostKey performs key exchange only. Discovery never grants trust and
// never reads or sends a saved password or private key.
func (h *ServerHandler) ProbeSSHHostKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !middleware.HasPermission(r, "server:write") {
		respondError(w, http.StatusForbidden, "Permission required: server:write", nil)
		return
	}
	var request struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	var trusted string
	if id := chi.URLParam(r, "serverID"); id != "" {
		if !middleware.CanAccessServer(r, id) {
			respondError(w, http.StatusForbidden, "Server access denied", nil)
			return
		}
		server, err := h.store.GetServer(id)
		if err != nil {
			respondError(w, http.StatusNotFound, "Server not found", nil)
			return
		}
		request.Host, request.Port, trusted = server.Host, server.Port, server.TrustedHostKey
	} else if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid SSH address", nil)
		return
	}
	host, port, err := remote.NormalizeSSHEndpoint(request.Host, request.Port)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	observed, err := remote.ProbeSSHHostKey(r.Context(), host, port)
	if err != nil {
		respondError(w, http.StatusBadGateway, "Unable to fetch SSH host key: "+err.Error(), nil)
		return
	}
	previousFingerprint := ""
	if key, err := remote.ParseHostKey(trusted); err == nil {
		previousFingerprint = ssh.FingerprintSHA256(key)
	}
	respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Data: struct {
		remote.SSHHostKey
		TrustedFingerprint string `json:"trusted_fingerprint,omitempty"`
	}{observed, previousFingerprint}})
}

// TrustSSHHostKey records exactly the public key approved by the operator. It
// does not fetch a replacement during saving (the network key could change).
func (h *ServerHandler) TrustSSHHostKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := chi.URLParam(r, "serverID")
	if !middleware.HasPermission(r, "server:write") || !middleware.CanAccessServer(r, id) {
		respondError(w, http.StatusForbidden, "Server write access required", nil)
		return
	}
	var request remote.SSHHostKey
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24576)).Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid SSH host key", nil)
		return
	}
	key, err := canonicalHostKey(request.PublicKey)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	// Compare the exact endpoint returned by discovery, not a newly resolved IP.
	if err := h.store.TrustHostKey(id, request.Host, request.Port, key); err != nil {
		respondError(w, http.StatusConflict, err.Error(), nil)
		return
	}
	respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Message: "SSH host key trusted"})
}
