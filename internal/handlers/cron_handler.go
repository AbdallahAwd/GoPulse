package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type CronHandler struct {
	Service *services.CronService
	Servers *store.ServerStore
}

func (h *CronHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !middleware.CanAccessServer(r, chi.URLParam(r, "serverID")) {
				respondError(w, 403, "Server access denied", nil)
				return
			}
			if _, err := h.Servers.GetServer(chi.URLParam(r, "serverID")); err != nil {
				respondError(w, 404, "Server not found", nil)
				return
			}
			if r.Method != "GET" && !middleware.HasPermission(r, "cron:write") {
				respondError(w, 403, "Permission required: cron:write", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", h.List)
	r.Get("/preview", h.Preview)
	r.Post("/", h.Create)
	r.Put("/{jobID}", h.Update)
	r.Delete("/{jobID}", h.Delete)
	r.Put("/{jobID}/enabled", h.Enabled)
	r.Post("/{jobID}/run", h.RunNow)
	r.Post("/{jobID}/cancel", h.Cancel)
	r.Get("/{jobID}/runs", h.Runs)
	r.Get("/{jobID}/runs/{runID}", h.Run)
}

func cronResponse(w http.ResponseWriter, code int, value interface{}, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		message := "Unable to access cron jobs."
		var invalid *services.CronValidationError
		switch {
		case errors.As(err, &invalid):
			status = 400
			message = err.Error()
		case errors.Is(err, services.ErrCronNotFound):
			status = 404
			message = err.Error()
		case errors.Is(err, store.ErrCronBusy) || errors.Is(err, services.ErrCronCapacity):
			status = 409
			message = err.Error()
		}
		respondError(w, status, message, nil)
		return
	}
	respondJSON(w, code, models.APIResponse{Success: true, Data: value})
}

// Reject oversized, unknown, or concatenated JSON before any mutation.
func cronDecode(w http.ResponseWriter, r *http.Request, dest interface{}) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		respondError(w, 400, "Invalid job data: "+err.Error(), nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send exactly one JSON object.", nil)
		return false
	}
	return true
}

type cronInput struct {
	Name           string `json:"name"`
	Schedule       string `json:"schedule"`
	Timezone       string `json:"timezone"`
	Command        string `json:"command"`
	WorkingDir     string `json:"working_dir"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Enabled        bool   `json:"enabled"`
}

func (h *CronHandler) save(w http.ResponseWriter, r *http.Request, id string, code int) {
	var input cronInput
	if !cronDecode(w, r, &input) {
		return
	}
	actor, _ := r.Context().Value(middleware.UserContextKey).(string)
	job, err := h.Service.Save(chi.URLParam(r, "serverID"), id, actor, models.CronJob{Name: input.Name, Schedule: input.Schedule, Timezone: input.Timezone, Command: input.Command, WorkingDir: input.WorkingDir, TimeoutSeconds: input.TimeoutSeconds, Enabled: input.Enabled})
	cronResponse(w, code, job, err)
}
func (h *CronHandler) Create(w http.ResponseWriter, r *http.Request) { h.save(w, r, "", 201) }
func (h *CronHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "jobID"), 200)
}
func (h *CronHandler) List(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.Service.List(chi.URLParam(r, "serverID"))
	cronResponse(w, 200, jobs, err)
}
func (h *CronHandler) Preview(w http.ResponseWriter, r *http.Request) {
	next, err := services.CronPreview(r.URL.Query().Get("schedule"), r.URL.Query().Get("timezone"), time.Now())
	cronResponse(w, 200, map[string]interface{}{"next_runs": next}, err)
}
func (h *CronHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.Service.Delete(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"))
	cronResponse(w, 200, map[string]bool{"deleted": err == nil}, err)
}
func (h *CronHandler) Enabled(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !cronDecode(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		respondError(w, 400, "An enabled boolean is required.", nil)
		return
	}
	job, err := h.Service.SetEnabled(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"), *input.Enabled)
	cronResponse(w, 200, job, err)
}
func (h *CronHandler) RunNow(w http.ResponseWriter, r *http.Request) {
	actor, _ := r.Context().Value(middleware.UserContextKey).(string)
	run, err := h.Service.RunNow(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"), actor)
	cronResponse(w, 202, run, err)
}
func (h *CronHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	err := h.Service.Cancel(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"))
	cronResponse(w, 202, map[string]bool{"cancellation_requested": err == nil}, err)
}
func (h *CronHandler) Runs(w http.ResponseWriter, r *http.Request) {
	limit, offset := 25, 0
	for key, dest := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := r.URL.Query().Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 || (key == "limit" && (n < 1 || n > 100)) {
				respondError(w, 400, "Use a limit of 1–100 and a nonnegative offset.", nil)
				return
			}
			*dest = n
		}
	}
	runs, err := h.Service.Runs(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"), limit, offset)
	cronResponse(w, 200, runs, err)
}
func (h *CronHandler) Run(w http.ResponseWriter, r *http.Request) {
	run, err := h.Service.Run(chi.URLParam(r, "serverID"), chi.URLParam(r, "jobID"), chi.URLParam(r, "runID"))
	cronResponse(w, 200, run, err)
}
