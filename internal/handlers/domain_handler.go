package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DomainHandler handles HTTP requests for domain management
type DomainHandler struct {
	service *services.DomainService
}

// NewDomainHandler creates a new DomainHandler
func NewDomainHandler(service *services.DomainService) *DomainHandler {
	return &DomainHandler{service: service}
}

// ListDomains returns all domains for a server
func (h *DomainHandler) ListDomains(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	domains := h.service.ListDomains(serverID)
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    domains,
	})
}

// addDomainRequest is the request body for adding a domain
type addDomainRequest struct {
	Domain string `json:"domain"`
	Port   int    `json:"port"`
	SSL    bool   `json:"ssl"`
}

// AddDomain creates a new domain mapping
func (h *DomainHandler) AddDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req addDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	if req.Domain == "" || req.Port <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Domain name and valid port are required",
		})
		return
	}

	domain := models.Domain{
		ID:       uuid.New().String(),
		ServerID: serverID,
		Domain:   req.Domain,
		Port:     req.Port,
		SSL:      req.SSL,
	}

	if err := h.service.AddDomain(domain); err != nil {
		// Check if partial success (saved but Nginx failed)
		if len(err.Error()) > 0 && err.Error()[:12] == "domain saved" {
			json.NewEncoder(w).Encode(models.APIResponse{
				Success: true,
				Data:    domain,
				Message: err.Error(),
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    domain,
		Message: "Domain added and Nginx configured successfully",
	})
}

// RemoveDomain deletes a domain mapping
func (h *DomainHandler) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	domainID := chi.URLParam(r, "id")

	if err := h.service.RemoveDomain(serverID, domainID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Domain removed successfully",
	})
}

// NginxStatus returns the nginx status for the server
func (h *DomainHandler) NginxStatus(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	status, err := h.service.CheckNginxStatus(serverID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data: map[string]string{
			"status": status,
		},
	})
}
