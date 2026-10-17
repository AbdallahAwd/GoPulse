package handlers

import (
	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type OperationHandler struct{ Operations *services.Operations }

func operationOwner(r *http.Request) string {
	owner, _ := r.Context().Value(middleware.UserContextKey).(string)
	return owner
}
func canAccessOperation(r *http.Request, op models.Operation) bool {
	if op.Owner != operationOwner(r) {
		return false
	}
	if op.ServerID == "" {
		return middleware.HasPermission(r, "*")
	}
	return middleware.CanAccessServer(r, op.ServerID)
}
func (h *OperationHandler) List(w http.ResponseWriter, r *http.Request) {
	ops, err := h.Operations.List()
	if err != nil {
		respondError(w, 500, "Operations unavailable", err)
		return
	}
	visible := []models.Operation{}
	for _, op := range ops {
		if canAccessOperation(r, op) {
			visible = append(visible, op)
		}
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: visible})
}
func (h *OperationHandler) Get(w http.ResponseWriter, r *http.Request) {
	op, err := h.Operations.Get(chi.URLParam(r, "operationID"))
	if err != nil || !canAccessOperation(r, op) {
		respondError(w, 404, "Operation not found", nil)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: op})
}
func (h *OperationHandler) Approve(w http.ResponseWriter, r *http.Request) {
	// Only a dashboard browser session can approve. Agents' bearer tokens cannot approve their own actions.
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-GoPulse-Human-Approval") != "true" {
		respondError(w, 403, "Approve this operation from the dashboard", nil)
		return
	}
	if _, err := r.Cookie("jwt_token"); err != nil {
		respondError(w, 403, "Dashboard session required", nil)
		return
	}
	op, err := h.Operations.Get(chi.URLParam(r, "operationID"))
	if err != nil || !canAccessOperation(r, op) {
		respondError(w, 404, "Operation not found", nil)
		return
	}
	for _, target := range op.TargetServers {
		if !middleware.CanAccessServer(r, target) {
			respondError(w, 403, "Backend server authorization revoked", nil)
			return
		}
	}
	if err := h.Operations.Approve(op.ID, operationOwner(r), func(permission string) bool { return middleware.HasPermission(r, permission) }); err != nil {
		respondError(w, 409, "Approval rejected", err)
		return
	}
	respondJSON(w, 202, models.APIResponse{Success: true, Message: "Approved; operation started"})
}

func (h *OperationHandler) Reject(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-GoPulse-Human-Approval") != "true" {
		respondError(w, 403, "Reject this operation from the dashboard", nil)
		return
	}
	if _, err := r.Cookie("jwt_token"); err != nil {
		respondError(w, 403, "Dashboard session required", nil)
		return
	}
	op, err := h.Operations.Get(chi.URLParam(r, "operationID"))
	if err != nil || !canAccessOperation(r, op) {
		respondError(w, 404, "Operation not found", nil)
		return
	}
	if err := h.Operations.Reject(op.ID, operationOwner(r)); err != nil {
		respondError(w, 409, "Rejection unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Message: "Operation rejected; no action executed"})
}
