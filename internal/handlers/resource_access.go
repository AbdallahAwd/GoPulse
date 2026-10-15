package handlers

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (h *DatabaseHandler) RequireOwnership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "dbID")
		if id != "" {
			db, err := h.service.GetDatabase(id)
			if err != nil || db.ServerID != chi.URLParam(r, "serverID") {
				respondError(w, 404, "Database not found on this server", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (h *LoadBalancerHandler) RequireOwnership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id != "" {
			lb, err := h.service.GetLoadBalancer(id)
			if err != nil || lb.ServerID != chi.URLParam(r, "serverID") {
				respondError(w, 404, "Load balancer not found on this server", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *ProjectHandler) RequireOwnership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id != "" {
			project, err := h.service.GetProject(r.Context(), id)
			if err != nil || project.ServerID != chi.URLParam(r, "serverID") {
				respondError(w, 404, "Application not found on this server", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
