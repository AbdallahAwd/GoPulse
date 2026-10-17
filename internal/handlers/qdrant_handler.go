package handlers

import (
	"context"
	"encoding/json"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type QdrantHandler struct {
	Service    *services.DatabaseService
	Operations *services.Operations
}

func (h *QdrantHandler) Install(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string              `json:"name"`
		APIKey string              `json:"api_key"`
		Config models.QdrantConfig `json:"config"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		respondError(w, 400, "Invalid Qdrant configuration", nil)
		return
	}
	if err := services.ValidateQdrantConfig(&req.Config); err != nil {
		respondError(w, 400, "Invalid Qdrant configuration", err)
		return
	}
	server := chi.URLParam(r, "serverID")
	op, err := h.Operations.Submit(operationOwner(r), server, "database-install:"+server, "install_qdrant", "database:write", false, func(ctx context.Context, step func(string)) (interface{}, error) {
		return h.Service.InstallQdrant(ctx, server, req.Name, req.APIKey, req.Config, step)
	})
	if err != nil {
		respondError(w, 409, "Installation not started", err)
		return
	}
	respondJSON(w, 202, models.APIResponse{Success: true, Data: op})
}
func (h *QdrantHandler) Collections(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.QdrantCollections(r.Context(), chi.URLParam(r, "dbID"))
	if err != nil {
		respondError(w, 502, "Qdrant unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}
func (h *QdrantHandler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Size     int    `json:"size"`
		Distance string `json:"distance"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		respondError(w, 400, "Invalid collection", nil)
		return
	}
	result, err := h.Service.CreateQdrantCollection(r.Context(), chi.URLParam(r, "dbID"), req.Name, req.Size, req.Distance)
	if err != nil {
		respondError(w, 400, "Collection creation failed", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}
func (h *QdrantHandler) Collection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "collection")
	if !services.ValidCollectionName(name) {
		respondError(w, 400, "Invalid collection name", nil)
		return
	}
	result, err := h.Service.QdrantRequest(r.Context(), chi.URLParam(r, "dbID"), "GET", "/collections/"+name, nil)
	if err != nil {
		respondError(w, 502, "Collection unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}
func (h *QdrantHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "collection")
	if r.URL.Query().Get("confirm") != name {
		respondError(w, 400, "Confirm collection deletion by name", nil)
		return
	}
	result, err := h.Service.DeleteQdrantCollection(r.Context(), chi.URLParam(r, "dbID"), name)
	if err != nil {
		respondError(w, 502, "Collection deletion failed", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}
func (h *QdrantHandler) Diagnostics(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.QdrantDiagnostics(r.Context(), chi.URLParam(r, "dbID"))
	if err != nil {
		respondError(w, 502, "Qdrant diagnostics unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}

func (h *QdrantHandler) Snapshots(w http.ResponseWriter, r *http.Request) {
	create := r.Method == http.MethodPost
	result, err := h.Service.QdrantSnapshots(r.Context(), chi.URLParam(r, "dbID"), chi.URLParam(r, "collection"), create)
	if err != nil {
		respondError(w, 502, "Snapshot operation failed", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: result})
}
func (h *QdrantHandler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "dbID")
	collection := chi.URLParam(r, "collection")
	if r.URL.Query().Get("confirm") != collection {
		respondError(w, 400, "Confirm collection restore by name", nil)
		return
	}
	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		respondError(w, 400, "Invalid snapshot", nil)
		return
	}
	server := chi.URLParam(r, "serverID")
	op, err := h.Operations.SubmitReview(operationOwner(r), server, "database:"+id, "restore_qdrant_snapshot", "database:write", false, map[string]string{"database_id": id, "collection": collection, "filename": req.Filename}, nil, func(ctx context.Context, step func(string)) (interface{}, error) {
		step("Restore confirmed snapshot")
		return h.Service.RestoreQdrantSnapshot(ctx, id, collection, req.Filename)
	})
	if err != nil {
		respondError(w, 409, "Restore not started", err)
		return
	}
	respondJSON(w, 202, models.APIResponse{Success: true, Data: op})
}

func (h *QdrantHandler) Configuration(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Service.QdrantConfiguration(chi.URLParam(r, "dbID"))
	if err != nil {
		respondError(w, 404, "Qdrant configuration unavailable", err)
		return
	}
	respondJSON(w, 200, models.APIResponse{Success: true, Data: cfg})
}
