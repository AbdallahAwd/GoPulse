package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type MonitoringHandler struct{ Service *services.MonitoringService }

func (h *MonitoringHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverID := chi.URLParam(r, "serverID")
			if !middleware.CanAccessServer(r, serverID) {
				respondError(w, 403, "Server access denied", nil)
				return
			}
			if _, err := h.Service.Servers.GetServer(serverID); err != nil {
				respondError(w, 404, "Server not found", nil)
				return
			}
			if r.Method != "GET" && !middleware.HasPermission(r, "server:write") {
				respondError(w, 403, "Permission required: server:write", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/events", h.Events)
	r.Put("/{ruleID}", h.Update)
	r.Delete("/{ruleID}", h.Delete)
	r.Put("/{ruleID}/silence", h.Silence)
	r.Post("/{ruleID}/test", h.Test)
}
func monitoringResponse(w http.ResponseWriter, code int, data interface{}, err error) {
	if err != nil {
		status, message := 500, "Monitoring storage unavailable"
		var invalid *services.AlertValidationError
		if errors.As(err, &invalid) {
			status, message = 400, err.Error()
		} else if errors.Is(err, store.ErrAlertNotFound) {
			status, message = 404, err.Error()
		} else if errors.Is(err, store.ErrAlertTestBusy) {
			status, message = 409, err.Error()
		} else if errors.Is(err, store.ErrAlertLimit) {
			status, message = 409, err.Error()
		}
		respondError(w, status, message, nil)
		return
	}
	respondJSON(w, code, models.APIResponse{Success: true, Data: data})
}
func monitoringDecode(w http.ResponseWriter, r *http.Request, value interface{}) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		respondError(w, 400, "Invalid alert data", nil)
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send exactly one JSON object", nil)
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		respondError(w, 400, "Send an alert JSON object", nil)
		return false
	}
	fields := json.NewDecoder(bytes.NewReader(raw))
	fields.DisallowUnknownFields()
	if err := fields.Decode(value); err != nil {
		respondError(w, 400, "Invalid alert data", nil)
		return false
	}

	return true
}
func (h *MonitoringHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.Service.Store.Rules(chi.URLParam(r, "serverID"))
	if !middleware.HasPermission(r, "server:write") {
		for i := range rules {
			rules[i].WebhookURL = ""
			rules[i].WebhookSecret = ""
		}
	}
	monitoringResponse(w, 200, rules, err)
}
func (h *MonitoringHandler) save(w http.ResponseWriter, r *http.Request, id string, code int) {
	var input services.AlertInput
	if !monitoringDecode(w, r, &input) {
		return
	}
	rule, err := h.Service.Save(chi.URLParam(r, "serverID"), id, input)
	monitoringResponse(w, code, rule, err)
}
func (h *MonitoringHandler) Create(w http.ResponseWriter, r *http.Request) { h.save(w, r, "", 201) }
func (h *MonitoringHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "ruleID"), 200)
}
func (h *MonitoringHandler) Delete(w http.ResponseWriter, r *http.Request) {
	monitoringResponse(w, 200, nil, h.Service.Store.DeleteRule(chi.URLParam(r, "serverID"), chi.URLParam(r, "ruleID")))
}
func (h *MonitoringHandler) Events(w http.ResponseWriter, r *http.Request) {
	events, err := h.Service.Store.Events(chi.URLParam(r, "serverID"))
	monitoringResponse(w, 200, events, err)
}
func (h *MonitoringHandler) Silence(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Seconds int `json:"seconds"`
	}
	if !monitoringDecode(w, r, &input) {
		return
	}
	rule, err := h.Service.Silence(chi.URLParam(r, "serverID"), chi.URLParam(r, "ruleID"), input.Seconds, time.Now().UTC())
	monitoringResponse(w, 200, rule, err)
}
func (h *MonitoringHandler) Test(w http.ResponseWriter, r *http.Request) {
	serverID, id := chi.URLParam(r, "serverID"), chi.URLParam(r, "ruleID")
	rule, err := h.Service.Store.Rule(serverID, id)
	if err == nil && rule.WebhookURL == "" {
		err = &services.AlertValidationError{Message: "Configure a webhook before testing delivery."}
	}
	if err == nil {
		err = h.Service.Store.QueueTest(serverID, id, time.Now().UTC())
	}
	monitoringResponse(w, 202, map[string]string{"message": "Test notification queued"}, err)
}
