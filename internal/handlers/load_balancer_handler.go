package handlers

import (
	"context"
	"encoding/json"
	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"time"
)

type LoadBalancerHandler struct {
	service    *services.LoadBalancerService
	operations *services.Operations
}

func NewLoadBalancerHandler(service *services.LoadBalancerService) *LoadBalancerHandler {
	return &LoadBalancerHandler{service: service}
}
func (h *LoadBalancerHandler) SetOperations(ops *services.Operations) { h.operations = ops }
func (h *LoadBalancerHandler) ListLoadBalancers(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, models.APIResponse{Success: true, Data: h.service.ListLoadBalancers(chi.URLParam(r, "serverID"))})
}
func (h *LoadBalancerHandler) GetLoadBalancer(w http.ResponseWriter, r *http.Request) {
	lb, err := h.service.GetLoadBalancer(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, 404, "Load balancer not found", nil)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: lb})
}
func (h *LoadBalancerHandler) authorizeTargets(r *http.Request, lb models.LoadBalancer) bool {
	for _, b := range lb.Backends {
		server, err := h.service.BackendServer(b)
		if err != nil {
			return false
		}
		if server == "" {
			if !middleware.HasPermission(r, "*") {
				return false
			}
		} else if !middleware.CanAccessServer(r, server) {
			return false
		}
	}
	return true
}
func (h *LoadBalancerHandler) readConfiguration(w http.ResponseWriter, r *http.Request) (models.LoadBalancer, bool) {
	var lb models.LoadBalancer
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lb); err != nil {
		respondError(w, 400, "Invalid load balancer configuration", nil)
		return lb, false
	}
	lb.ServerID = chi.URLParam(r, "serverID")
	lb.ID = chi.URLParam(r, "id")
	if lb.ID == "" {
		lb.ID = uuid.NewString()
	}
	lb.CreatedAt = time.Now()
	if lb.Algorithm == "" {
		lb.Algorithm = "round-robin"
	}
	if lb.HealthCheckPath == "" {
		lb.HealthCheckPath = "/"
	}
	if lb.HealthCheckInterval == 0 {
		lb.HealthCheckInterval = 10
	}
	if !h.authorizeTargets(r, lb) {
		respondError(w, 403, "Access to every backend server is required; custom targets require administrator permission", nil)
		return lb, false
	}
	if err := h.service.Prepare(&lb); err != nil {
		respondError(w, 400, "Invalid load balancer configuration", err)
		return lb, false
	}
	return lb, true
}
func (h *LoadBalancerHandler) submit(w http.ResponseWriter, r *http.Request, lb models.LoadBalancer, create bool) {
	kind := "update_load_balancer"
	if create {
		kind = "create_load_balancer"
	}
	op, err := h.operations.Submit(operationOwner(r), lb.ServerID, "loadbalancer:"+lb.ServerID, kind, "domains:write", false, func(ctx context.Context, step func(string)) (interface{}, error) {
		var err error
		if create {
			err = h.service.Create(ctx, lb, step)
		} else {
			err = h.service.Update(ctx, lb, step)
		}
		if err != nil {
			return map[string]string{"load_balancer_id": lb.ID}, err
		}
		return h.service.GetLoadBalancer(lb.ID)
	})
	if err != nil {
		respondError(w, 409, "Deployment not started", err)
		return
	}
	respondJSON(w, 202, models.APIResponse{Success: true, Data: op})
}
func (h *LoadBalancerHandler) AddLoadBalancer(w http.ResponseWriter, r *http.Request) {
	lb, ok := h.readConfiguration(w, r)
	if ok {
		h.submit(w, r, lb, true)
	}
}
func (h *LoadBalancerHandler) UpdateLoadBalancer(w http.ResponseWriter, r *http.Request) {
	lb, ok := h.readConfiguration(w, r)
	if ok {
		h.submit(w, r, lb, false)
	}
}
func (h *LoadBalancerHandler) Retry(w http.ResponseWriter, r *http.Request) {
	lb, err := h.service.GetLoadBalancer(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, 404, "Load balancer not found", nil)
		return
	}
	if !h.authorizeTargets(r, lb) {
		respondError(w, 403, "Backend access denied", nil)
		return
	}
	h.submit(w, r, lb, false)
}
func (h *LoadBalancerHandler) RemoveLoadBalancer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if r.URL.Query().Get("confirm") != id {
		respondError(w, 400, "Confirm load balancer removal by ID", nil)
		return
	}
	if err := h.service.RemoveLoadBalancer(chi.URLParam(r, "serverID"), id); err != nil {
		respondError(w, 502, "Load balancer removal failed", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Message: "Load balancer removed and Nginx verified"})
}
func (h *LoadBalancerHandler) AddBackend(w http.ResponseWriter, r *http.Request) {
	lb, err := h.service.GetLoadBalancer(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, 404, "Load balancer not found", nil)
		return
	}
	var b models.LoadBalancerBackend
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		respondError(w, 400, "Invalid backend", nil)
		return
	}
	b.ID = uuid.NewString()
	b.LoadBalancerID = lb.ID
	lb.Backends = append(lb.Backends, b)
	if !h.authorizeTargets(r, lb) {
		respondError(w, 403, "Backend server access denied", nil)
		return
	}
	if err := h.service.Prepare(&lb); err != nil {
		respondError(w, 400, "Invalid backend", err)
		return
	}
	h.submit(w, r, lb, false)
}
func (h *LoadBalancerHandler) RemoveBackend(w http.ResponseWriter, r *http.Request) {
	lb, err := h.service.GetLoadBalancer(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, 404, "Load balancer not found", nil)
		return
	}
	backends := []models.LoadBalancerBackend{}
	found := false
	for _, b := range lb.Backends {
		if b.ID == chi.URLParam(r, "backendID") {
			found = true
		} else {
			backends = append(backends, b)
		}
	}
	if !found {
		respondError(w, 404, "Backend not found", nil)
		return
	}
	lb.Backends = backends
	if err := h.service.Prepare(&lb); err != nil {
		respondError(w, 400, "Cannot remove backend", err)
		return
	}
	if !h.authorizeTargets(r, lb) {
		respondError(w, 403, "Backend server access denied", nil)
		return
	}
	h.submit(w, r, lb, false)
}
func (h *LoadBalancerHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.service.GetLogs(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, 502, "Nginx logs unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: logs})
}
