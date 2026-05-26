package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ProjectHandler handles project-related endpoints
type ProjectHandler struct {
	service *services.ProjectService
}

// NewProjectHandler creates a new ProjectHandler
func NewProjectHandler(service *services.ProjectService) *ProjectHandler {
	return &ProjectHandler{service: service}
}

// ListProjects handles GET /api/servers/{serverID}/projects
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	// Filter by serverID? stored projects are flattened but have serverID.
	// We should probably filter in service/store.
	// For now, listing all might be fine or we filter here.
	projects, err := h.service.ListProjects(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list projects", err)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	filtered := []models.Project{}
	for _, p := range projects {
		if p.ServerID == serverID {
			filtered = append(filtered, p)
		}
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    filtered,
	})
}

// CreateProject handles POST /api/servers/{serverID}/projects
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var project models.Project
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	project.ServerID = serverID

	if project.ID == "" {
		project.ID = uuid.New().String()
	}

	if err := h.service.CreateProject(r.Context(), project); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create project", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    project,
	})
}

// GetProject handles GET /api/servers/{serverID}/projects/{id}
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	project, err := h.service.GetProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Project not found", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    project,
	})
}

// SetupProject handles POST /api/servers/{serverID}/projects/{id}/setup
func (h *ProjectHandler) SetupProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if err := h.service.SetupProject(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to setup project", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project setup initiated/completed",
	})
}

// DetectStrategy handles GET /api/servers/{serverID}/projects/{id}/detect
func (h *ProjectHandler) DetectStrategy(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	strategy, err := h.service.DetectBuildStrategy(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to detect strategy", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    map[string]string{"strategy": strategy},
	})
}

// BuildProject handles POST /api/servers/{serverID}/projects/{id}/build
func (h *ProjectHandler) BuildProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	// Maybe update project with build override from body first?
	// For now simple trigger.
	if err := h.service.BuildProject(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Build failed", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Build completed",
	})
}

// StartProject handles POST /api/servers/{serverID}/projects/{id}/start
func (h *ProjectHandler) StartProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if err := h.service.StartProject(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Start failed", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project started",
	})
}

// StopProject handles POST /api/servers/{serverID}/projects/{id}/stop
func (h *ProjectHandler) StopProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if err := h.service.StopProject(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Stop failed", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project stopped",
	})
}

// DeleteProject handles DELETE /api/servers/{serverID}/projects/{id}
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if err := h.service.DeleteProject(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Delete failed", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project deleted",
	})
}

// UploadUpload handles project zip upload
// This one is tricky as it needs to move file to the server using SSH?
// Process: 1. Upload to dashboard server temp. 2. SCP to target server.
// Using existing FileService?
// UploadProject handles project zip upload with streaming
func (h *ProjectHandler) UploadProject(w http.ResponseWriter, r *http.Request) {
	// Check for project_id in query params first
	projectID := r.URL.Query().Get("project_id")

	// Use MultipartReader for streaming to avoid buffering 50MB+ in memory
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid multipart request", err)
		return
	}

	fileFound := false

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			respondError(w, http.StatusBadRequest, "Failed to read multipart", err)
			return
		}

		partName := part.FormName()
		if partName == "project_id" {
			// Read project_id from body if not already set
			if projectID == "" {
				// Limit project ID size (e.g., 64 bytes for UUID)
				buf := make([]byte, 64)
				n, _ := part.Read(buf)
				projectID = string(buf[:n])
			}
			continue
		}

		if partName == "file" {
			if projectID == "" {
				respondError(w, http.StatusBadRequest, "project_id must replace file field or be in query params", nil)
				return
			}

			filename := part.FileName()
			if filename == "" {
				continue
			}

			// Call service with the part stream directly
			if err := h.service.UploadProjectFile(r.Context(), projectID, part, filename); err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to upload project", err)
				return
			}
			fileFound = true
			// We only handle one file per request
			break
		}
	}

	if !fileFound {
		respondError(w, http.StatusBadRequest, "No file found in request", nil)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project uploaded successfully",
	})
}

// UpdateProject handles PUT /api/servers/{serverID}/projects/{id}
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var project models.Project
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	project.ID = projectID
	if err := h.service.UpdateProject(r.Context(), project); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update project", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Project updated successfully",
	})
}

// EnableWebhook handles POST /api/servers/{serverID}/projects/{id}/webhook/enable
func (h *ProjectHandler) EnableWebhook(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	secret, err := h.service.EnableWebhook(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to enable webhook", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]string{
			"webhook_url":    fmt.Sprintf("/api/webhooks/github/%s", projectID),
			"webhook_secret": secret,
		},
		Message: "Webhook enabled",
	})
}

// DisableWebhook handles POST /api/servers/{serverID}/projects/{id}/webhook/disable
func (h *ProjectHandler) DisableWebhook(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if err := h.service.DisableWebhook(r.Context(), projectID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to disable webhook", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Webhook disabled",
	})
}

// GetEnvVars handles GET /api/servers/{serverID}/projects/{id}/env
func (h *ProjectHandler) GetEnvVars(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	vars, err := h.service.GetEnvVars(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get env vars", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    vars,
	})
}

// UpdateEnvVars handles PUT /api/servers/{serverID}/projects/{id}/env
func (h *ProjectHandler) UpdateEnvVars(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var vars map[string]string
	if err := json.NewDecoder(r.Body).Decode(&vars); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if err := h.service.UpdateEnvVars(r.Context(), projectID, vars); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update env vars", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Environment variables updated and synced",
	})
}
