package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
)

type RemoteBackupHandler struct{ Service *services.RemoteBackupService }

func (h *RemoteBackupHandler) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "serverID")
			if !middleware.CanAccessServer(r, id) {
				respondError(w, 403, "Source server access denied", nil)
				return
			}
			if _, err := h.Service.Servers.GetServer(id); err != nil {
				respondError(w, 404, "Source server not found", nil)
				return
			}
			if r.Method != "GET" && (!middleware.HasPermission(r, "database:write") || !middleware.HasPermission(r, "server:write")) {
				respondError(w, 403, "database:write and server:write required", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", h.List)
	r.Get("/options", h.Options)
	r.Get("/preview", h.Preview)
	r.Post("/", h.Create)
	r.Put("/{planID}", h.Update)
	r.Delete("/{planID}", h.Delete)
	r.Post("/{planID}/run", h.Run)
	r.Post("/{planID}/cancel", h.Cancel)
	r.Get("/{planID}/runs", h.Runs)
	r.Get("/{planID}/artifacts", h.Artifacts)
	r.Post("/{planID}/artifacts/{artifactID}/test", h.Test)
	r.Get("/{planID}/artifacts/{artifactID}/download", h.Download)
	r.Get("/{planID}/recovery-key", h.Key)
}
func backupResponse(w http.ResponseWriter, code int, data interface{}, err error) {
	if err != nil {
		status, message := 500, "Remote backup operation unavailable"
		var invalid *services.RemoteBackupValidationError
		switch {
		case errors.As(err, &invalid):
			status, message = 400, err.Error()
		case errors.Is(err, services.ErrRemoteBackupAccess):
			status, message = 403, err.Error()
		case errors.Is(err, store.ErrRemoteBackupNotFound):
			status, message = 404, err.Error()
		case errors.Is(err, store.ErrRemoteBackupBusy) || errors.Is(err, services.ErrRemoteBackupCapacity):
			status, message = 409, err.Error()
		}
		respondError(w, status, message, nil)
		return
	}
	respondJSON(w, code, models.APIResponse{Success: true, Data: data})
}
func backupDecode(w http.ResponseWriter, r *http.Request, dest interface{}) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		respondError(w, 400, "Invalid backup plan JSON", nil)
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		respondError(w, 400, "Send exactly one JSON object", nil)
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		respondError(w, 400, "Send a backup plan JSON object", nil)
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		respondError(w, 400, "Invalid backup plan fields", nil)
		return false
	}
	return true
}
func (h *RemoteBackupHandler) plan(w http.ResponseWriter, r *http.Request, write bool) (models.RemoteBackupPlan, bool) {
	plan, err := h.Service.Store.Get(chi.URLParam(r, "serverID"), chi.URLParam(r, "planID"))
	if err != nil {
		backupResponse(w, 404, nil, err)
		return plan, false
	}
	if !middleware.CanAccessServer(r, plan.DestinationID) {
		respondError(w, 403, "Destination server access denied", nil)
		return plan, false
	}
	if write && (!middleware.HasPermission(r, "database:write") || !middleware.HasPermission(r, "server:write")) {
		respondError(w, 403, "database:write and server:write required", nil)
		return plan, false
	}
	return plan, true
}
func (h *RemoteBackupHandler) actor(r *http.Request) string {
	actor, _ := r.Context().Value(middleware.UserContextKey).(string)
	return actor
}
func (h *RemoteBackupHandler) List(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Service.Store.Plans(chi.URLParam(r, "serverID"))
	visible := []models.RemoteBackupPlan{}
	for _, plan := range plans {
		if middleware.CanAccessServer(r, plan.DestinationID) {
			visible = append(visible, plan)
		}
	}
	backupResponse(w, 200, visible, err)
}
func (h *RemoteBackupHandler) Options(w http.ResponseWriter, r *http.Request) {
	databases := []map[string]string{}
	for _, db := range h.Service.Databases.ListDatabases() {
		if db.ServerID == chi.URLParam(r, "serverID") && services.RemoteBackupEngineSupported(db.Type) {
			databases = append(databases, map[string]string{"id": db.ID, "name": db.Name, "engine": db.Type})
		}
	}
	servers := []map[string]string{}
	for _, server := range h.Service.Servers.ListServers() {
		if server.ID != chi.URLParam(r, "serverID") && middleware.CanAccessServer(r, server.ID) {
			servers = append(servers, map[string]string{"id": server.ID, "name": server.Name})
		}
	}
	backupResponse(w, 200, map[string]interface{}{"databases": databases, "destinations": servers}, nil)
}
func (h *RemoteBackupHandler) save(w http.ResponseWriter, r *http.Request, id string, code int) {
	var input services.RemoteBackupInput
	if !backupDecode(w, r, &input) {
		return
	}
	if !middleware.CanAccessServer(r, input.DestinationID) {
		respondError(w, 403, "Destination server access denied", nil)
		return
	}
	if id != "" {
		if _, ok := h.plan(w, r, true); !ok {
			return
		}
	}
	plan, err := h.Service.Save(chi.URLParam(r, "serverID"), id, h.actor(r), input)
	backupResponse(w, code, plan, err)
}
func (h *RemoteBackupHandler) Create(w http.ResponseWriter, r *http.Request) { h.save(w, r, "", 201) }
func (h *RemoteBackupHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "planID"), 200)
}
func (h *RemoteBackupHandler) Preview(w http.ResponseWriter, r *http.Request) {
	dates, err := services.CronPreview(r.URL.Query().Get("schedule"), r.URL.Query().Get("timezone"), time.Now())
	if err != nil {
		respondError(w, 400, "Invalid schedule or timezone", nil)
		return
	}
	backupResponse(w, 200, map[string]interface{}{"next_runs": dates}, nil)
}
func (h *RemoteBackupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	backupResponse(w, 200, nil, h.Service.DeletePlan(plan.ServerID, plan.ID))
}
func (h *RemoteBackupHandler) Run(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	run, err := h.Service.RunNow(plan.ServerID, plan.ID, h.actor(r), "")
	backupResponse(w, 202, run, err)
}
func (h *RemoteBackupHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	backupResponse(w, 200, nil, h.Service.Cancel(plan.ServerID, plan.ID))
}
func (h *RemoteBackupHandler) Runs(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, false)
	if !ok {
		return
	}
	runs, err := h.Service.Store.Runs(plan.ID)
	backupResponse(w, 200, runs, err)
}
func (h *RemoteBackupHandler) Artifacts(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, false)
	if !ok {
		return
	}
	artifacts, err := h.Service.Store.Artifacts(plan.ID, true)
	backupResponse(w, 200, artifacts, err)
}
func (h *RemoteBackupHandler) Test(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	run, err := h.Service.RunNow(plan.ServerID, plan.ID, h.actor(r), chi.URLParam(r, "artifactID"))
	backupResponse(w, 202, run, err)
}
func (h *RemoteBackupHandler) Key(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	if _, err := h.Service.Authorize(h.actor(r), plan.ServerID, plan.DestinationID); err != nil {
		backupResponse(w, 403, nil, err)
		return
	}
	key, err := h.Service.RecoveryKey(plan)
	if err != nil {
		respondError(w, 409, "Recovery key unavailable; restore the exported key file on the dashboard host.", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="gopulse-`+plan.ID+`.agekey"`)
	w.Write([]byte(key))
}
func (h *RemoteBackupHandler) Download(w http.ResponseWriter, r *http.Request) {
	plan, ok := h.plan(w, r, true)
	if !ok {
		return
	}
	if _, err := h.Service.Authorize(h.actor(r), plan.ServerID, plan.DestinationID); err != nil {
		backupResponse(w, 403, nil, err)
		return
	}
	a, err := h.Service.Store.Artifact(plan.ID, chi.URLParam(r, "artifactID"))
	if err != nil || a.Pruned {
		if err == nil {
			err = store.ErrRemoteBackupNotFound
		}
		backupResponse(w, 404, nil, err)
		return
	}
	reader, err := h.Service.OpenArchive(r.Context(), plan, a)
	if err != nil {
		respondError(w, 502, "Encrypted archive could not be opened on the destination server", nil)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(a.Size, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.ID+`.zip.age"`)
	w.Header().Set("X-Checksum-SHA256", a.SHA256)
	w.Header().Set("Cache-Control", "no-store")
	if _, err = io.Copy(w, reader); err != nil {
		panic(http.ErrAbortHandler)
	}
}
