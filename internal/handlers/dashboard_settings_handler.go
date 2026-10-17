package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
)

type DashboardSettingsHandler struct {
	Service *services.DashboardDomainService
}

func (h *DashboardSettingsHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !middleware.HasPermission(r, "*") {
				respondError(w, 403, "Master dashboard settings require administrator or full-access permission", nil)
				return
			}
			if agent, _ := r.Context().Value(middleware.AgentContextKey).(bool); agent {
				respondError(w, 403, "Manage the master dashboard domain from a dashboard browser session", nil)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/domain", h.Get)
	r.Put("/domain", h.Save)
	r.Get("/domain/nginx", h.Nginx)
	r.Post("/domain/check", h.Check)
	r.Post("/domain/apply", h.Apply)
}

func (h *DashboardSettingsHandler) Identity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, 200, map[string]string{"instance_id": h.Service.Identity})
}
func (h *DashboardSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Service.Store.Domain()
	if err != nil {
		respondError(w, 500, "Unable to load dashboard domain", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: map[string]interface{}{"settings": settings, "upstream": h.Service.Upstream, "platform": h.Service.OS, "managed_supported": h.Service.OS == "linux", "public_url": h.Service.PublicURL()}})
}
func (h *DashboardSettingsHandler) Save(w http.ResponseWriter, r *http.Request) {
	var input models.DashboardDomainInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		respondError(w, 400, "Invalid domain settings", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send one domain settings object", nil)
		return
	}
	settings, err := h.Service.Save(input)
	if err != nil {
		status := 400
		if strings.Contains(err.Error(), "busy") {
			status = 409
		}
		respondError(w, status, "Unable to save domain settings", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: settings, Message: "Domain settings saved. Apply setup to change host routing."})
}
func (h *DashboardSettingsHandler) Nginx(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Service.Store.Domain()
	if err != nil {
		respondError(w, 500, "Unable to load domain settings", err)
		return
	}
	if _, err := services.ValidateDashboardDomain(settings.DashboardDomainInput); err != nil {
		respondError(w, 400, "Save valid domain settings before previewing Nginx", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: map[string]string{"path": "/etc/nginx/conf.d/gopulse-dashboard.conf", "configuration": services.DashboardNginxConfig(settings.DashboardDomainInput, h.Service.Upstream)}})
}
func (h *DashboardSettingsHandler) Check(w http.ResponseWriter, r *http.Request) {
	check, err := h.Service.Check(r.Context())
	if err != nil {
		respondError(w, 409, "Unable to check dashboard domain", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: check})
}
func (h *DashboardSettingsHandler) Apply(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" || r.Header.Get("X-GoPulse-Human-Approval") != "true" {
		respondError(w, 403, "Apply domain setup from the dashboard", nil)
		return
	}
	if _, err := r.Cookie("jwt_token"); err != nil {
		respondError(w, 403, "Dashboard browser session required", nil)
		return
	}
	op, err := h.Service.Deploy(operationOwner(r))
	if err != nil {
		respondError(w, 409, "Domain setup could not start", err)
		return
	}
	respondJSON(w, 202, models.APIResponse{Success: true, Data: op})
}
