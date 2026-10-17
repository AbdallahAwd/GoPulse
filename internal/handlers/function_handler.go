package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type FunctionHandler struct{ Service *services.FunctionService }

func (h *FunctionHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			server := chi.URLParam(r, "serverID")
			if !middleware.CanAccessServer(r, server) {
				respondError(w, 403, "Server access denied", nil)
				return
			}
			if _, e := h.Service.Servers.GetServer(server); e != nil {
				respondError(w, 404, "Server not found", nil)
				return
			}
			if r.Method != http.MethodGet && (!middleware.HasPermission(r, "server:write") || !middleware.HasPermission(r, "docker:write")) {
				respondError(w, 403, "server:write and docker:write required", nil)
				return
			}
			if r.Method != http.MethodGet {
				if _, e := h.Service.Authorize(functionActor(r), server); e != nil {
					functionResponse(w, 0, nil, e)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Put("/{functionID}", h.Update)
	r.Delete("/{functionID}", h.Delete)
	r.Post("/{functionID}/deploy", h.Deploy)
	r.Post("/{functionID}/run", h.Run)
	r.Post("/{functionID}/pause", h.Pause)
	r.Post("/{functionID}/http-token", h.Token)
	r.Post("/{functionID}/mcp", h.MCP)
	r.Get("/{functionID}/versions", h.Versions)
	r.Get("/{functionID}/runs", h.Runs)
	r.Post("/{functionID}/runs/{runID}/cancel", h.Cancel)
}
func functionActor(r *http.Request) string {
	actor, _ := r.Context().Value(middleware.UserContextKey).(string)
	return actor
}
func functionResponse(w http.ResponseWriter, code int, data interface{}, err error) {
	if err != nil {
		status, message := 500, "Function operation unavailable"
		var invalid *services.FunctionValidationError
		switch {
		case errors.As(err, &invalid):
			status, message = 400, err.Error()
		case errors.Is(err, services.ErrFunctionAccess):
			status, message = 403, err.Error()
		case errors.Is(err, store.ErrFunctionNotFound):
			status, message = 404, err.Error()
		case errors.Is(err, store.ErrFunctionBusy):
			status, message = 409, err.Error()
		case errors.Is(err, services.ErrFunctionCapacity):
			status, message = 429, err.Error()
			w.Header().Set("Retry-After", "5")
		}
		respondError(w, status, message, nil)
		return
	}
	respondJSON(w, code, models.APIResponse{Success: true, Data: data})
}
func functionDecode(w http.ResponseWriter, r *http.Request, dest interface{}) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	raw = bytes.TrimSpace(raw)
	if err != nil || len(raw) == 0 || raw[0] != '{' {
		respondError(w, 400, "Send one function JSON object (maximum 256 KiB)", nil)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(dest); err != nil {
		respondError(w, 400, "Invalid function fields", nil)
		return false
	}
	if err = d.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send exactly one JSON object", nil)
		return false
	}
	return true
}
func (h *FunctionHandler) scoped(w http.ResponseWriter, r *http.Request) (models.Function, bool) {
	f, e := h.Service.Store.Get(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"))
	if e != nil {
		functionResponse(w, 0, nil, e)
		return f, false
	}
	return f, true
}
func (h *FunctionHandler) List(w http.ResponseWriter, r *http.Request) {
	f, e := h.Service.Store.List(chi.URLParam(r, "serverID"))
	functionResponse(w, 200, f, e)
}
func (h *FunctionHandler) save(w http.ResponseWriter, r *http.Request, id string, code int) {
	var spec models.FunctionSpec
	if !functionDecode(w, r, &spec) {
		return
	}
	f, e := h.Service.Save(chi.URLParam(r, "serverID"), id, functionActor(r), spec)
	functionResponse(w, code, f, e)
}
func (h *FunctionHandler) Create(w http.ResponseWriter, r *http.Request) { h.save(w, r, "", 201) }
func (h *FunctionHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "functionID"), 200)
}
func (h *FunctionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.scoped(w, r); !ok {
		return
	}
	functionResponse(w, 200, nil, h.Service.Delete(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID")))
}
func (h *FunctionHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	run, e := h.Service.Begin(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), functionActor(r), "deploy", "manual", nil)
	functionResponse(w, 202, run, e)
}
func functionEvent(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if e != nil || !json.Valid(raw) {
		respondError(w, 400, "Send a JSON event up to 32 KiB", nil)
		return nil, false
	}
	return json.RawMessage(raw), true
}
func (h *FunctionHandler) Run(w http.ResponseWriter, r *http.Request) {
	event, ok := functionEvent(w, r)
	if !ok {
		return
	}
	run, e := h.Service.Begin(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), functionActor(r), "invoke", "manual", event)
	functionResponse(w, 202, run, e)
}
func (h *FunctionHandler) Pause(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Paused bool `json:"paused"`
	}
	if !functionDecode(w, r, &input) {
		return
	}
	f, e := h.Service.Pause(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), input.Paused)
	functionResponse(w, 200, f, e)
}
func (h *FunctionHandler) Token(w http.ResponseWriter, r *http.Request) {
	token, e := h.Service.RotateToken(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"))
	functionResponse(w, 200, map[string]string{"token": token, "path": "/api/function-triggers/" + chi.URLParam(r, "serverID") + "/" + chi.URLParam(r, "functionID")}, e)
}
func (h *FunctionHandler) MCP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !functionDecode(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		respondError(w, 400, "enabled must be a boolean", nil)
		return
	}
	f, e := h.Service.SetMCP(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), functionActor(r), *input.Enabled)
	functionResponse(w, 200, f, e)
}
func (h *FunctionHandler) Runs(w http.ResponseWriter, r *http.Request) {
	f, ok := h.scoped(w, r)
	if !ok {
		return
	}
	runs, e := h.Service.Store.Runs(f.ID)
	functionResponse(w, 200, runs, e)
}
func (h *FunctionHandler) Versions(w http.ResponseWriter, r *http.Request) {
	f, ok := h.scoped(w, r)
	if !ok {
		return
	}
	versions, e := h.Service.Store.Versions(f.ID)
	functionResponse(w, 200, versions, e)
}
func (h *FunctionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	functionResponse(w, 200, nil, h.Service.Cancel(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), chi.URLParam(r, "runID")))
}

// Trigger authenticates with a function-specific bearer token, independently of
// dashboard cookies. Tokens are hash-only at rest and never enter invocation logs.
func (h *FunctionHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 128 {
		respondError(w, 401, "Invalid function trigger credentials", nil)
		return
	}
	event, ok := functionEvent(w, r)
	if !ok {
		return
	}
	run, e := h.Service.InvokeHTTP(chi.URLParam(r, "serverID"), chi.URLParam(r, "functionID"), strings.TrimPrefix(auth, "Bearer "), event)
	if errors.Is(e, services.ErrFunctionAccess) {
		respondError(w, 401, "Invalid function trigger credentials", nil)
		return
	}
	// HTTP callers get only the receipt, never logs, source or arbitrary output.
	if e != nil {
		functionResponse(w, 0, nil, e)
		return
	}
	functionResponse(w, 202, map[string]string{"invocation_id": run.ID, "status": run.Status}, nil)
}
