package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// LoadBalancerHandler handles HTTP requests for load balancers
type LoadBalancerHandler struct {
	service *services.LoadBalancerService
}

// NewLoadBalancerHandler creates a new LoadBalancerHandler
func NewLoadBalancerHandler(service *services.LoadBalancerService) *LoadBalancerHandler {
	return &LoadBalancerHandler{service: service}
}

// ListLoadBalancers lists all load balancers for a server
func (h *LoadBalancerHandler) ListLoadBalancers(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	lbs := h.service.ListLoadBalancers(serverID)
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    lbs,
	})
}

// GetLoadBalancer returns details of a single load balancer
func (h *LoadBalancerHandler) GetLoadBalancer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lb, err := h.service.GetLoadBalancer(id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    lb,
	})
}

type addLoadBalancerRequest struct {
	Name                string `json:"name"`
	Domain              string `json:"domain"`
	Port                int    `json:"port"`
	Algorithm           string `json:"algorithm"`
	SSL                 bool   `json:"ssl"`
	HealthCheckPath     string `json:"health_check_path"`
	HealthCheckInterval int    `json:"health_check_interval"`
}

// AddLoadBalancer creates a new load balancer
func (h *LoadBalancerHandler) AddLoadBalancer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req addLoadBalancerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	if req.Name == "" || req.Domain == "" || req.Port <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Name, domain and valid port are required",
		})
		return
	}

	if req.Algorithm == "" {
		req.Algorithm = "round-robin"
	}
	if req.HealthCheckPath == "" {
		req.HealthCheckPath = "/"
	}
	if req.HealthCheckInterval <= 0 {
		req.HealthCheckInterval = 10
	}

	lb := models.LoadBalancer{
		ID:                  uuid.New().String(),
		ServerID:            serverID,
		Name:                req.Name,
		Domain:              req.Domain,
		Port:                req.Port,
		Algorithm:           req.Algorithm,
		SSL:                 req.SSL,
		HealthCheckPath:     req.HealthCheckPath,
		HealthCheckInterval: req.HealthCheckInterval,
		Status:              "configuring",
		CreatedAt:           time.Now(),
	}

	if err := h.service.AddLoadBalancer(lb); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    lb,
		Message: "Load balancer creation started",
	})
}

// RemoveLoadBalancer removes a load balancer
func (h *LoadBalancerHandler) RemoveLoadBalancer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	id := chi.URLParam(r, "id")

	if err := h.service.RemoveLoadBalancer(serverID, id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Load balancer removed successfully",
	})
}

type addBackendRequest struct {
	TargetServerID  string `json:"target_server_id"`
	TargetProjectID string `json:"target_project_id"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Weight          int    `json:"weight"`
	Backup          bool   `json:"backup"`
}

// AddBackend adds a target backend node
func (h *LoadBalancerHandler) AddBackend(w http.ResponseWriter, r *http.Request) {
	lbID := chi.URLParam(r, "id")

	var req addBackendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	if req.Host == "" || req.Port <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   "Valid host and port are required",
		})
		return
	}

	if req.Weight <= 0 {
		req.Weight = 1
	}

	backend := models.LoadBalancerBackend{
		ID:              uuid.New().String(),
		LoadBalancerID:  lbID,
		TargetServerID:  req.TargetServerID,
		TargetProjectID: req.TargetProjectID,
		Host:            req.Host,
		Port:            req.Port,
		Weight:          req.Weight,
		Backup:          req.Backup,
		Status:          "unknown",
		CreatedAt:       time.Now(),
	}

	if err := h.service.AddBackend(backend); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Data:    backend,
		Message: "Backend node added and load balancer config queued for deploy",
	})
}

// RemoveBackend removes a backend node
func (h *LoadBalancerHandler) RemoveBackend(w http.ResponseWriter, r *http.Request) {
	lbID := chi.URLParam(r, "id")
	backendID := chi.URLParam(r, "backendID")

	if err := h.service.RemoveBackend(lbID, backendID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Backend node removed and load balancer config queued for deploy",
	})
}

// GetLogs returns live Nginx and deployment logs
func (h *LoadBalancerHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	logs, err := h.service.GetLogs(r.Context(), id)
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
		Data:    logs,
	})
}

