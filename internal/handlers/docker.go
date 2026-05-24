package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DockerHandler handles Docker-related endpoints
type DockerHandler struct {
	manager *services.ServiceManager
}

// NewDockerHandler creates a new DockerHandler
func NewDockerHandler(manager *services.ServiceManager) *DockerHandler {
	return &DockerHandler{manager: manager}
}

// ListContainers handles GET /api/servers/{serverID}/containers
func (h *DockerHandler) ListContainers(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	// all is deprecated/ignored as we default to showing all or what implementation provides

	containers, err := service.ListContainers(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list containers", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    containers,
	})
}

// GetContainer handles GET /api/servers/{serverID}/containers/{id}
func (h *DockerHandler) GetContainer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	container, err := service.GetContainer(r.Context(), containerID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Container not found", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    container,
	})
}

// GetContainerStats handles GET /api/servers/{serverID}/containers/{id}/stats
func (h *DockerHandler) GetContainerStats(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	stats, err := service.GetContainerStats(r.Context(), containerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get container stats", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    stats,
	})
}

// StartContainer handles POST /api/servers/{serverID}/containers/{id}/start
func (h *DockerHandler) StartContainer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.StartContainer(r.Context(), containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to start container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Container started successfully",
	})
}

// StopContainer handles POST /api/servers/{serverID}/containers/{id}/stop
func (h *DockerHandler) StopContainer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.StopContainer(r.Context(), containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to stop container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Container stopped successfully",
	})
}

// RestartContainer handles POST /api/servers/{serverID}/containers/{id}/restart
func (h *DockerHandler) RestartContainer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.RestartContainer(r.Context(), containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to restart container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Container restarted successfully",
	})
}

// GetContainerLogs handles GET /api/servers/{serverID}/containers/{id}/logs
func (h *DockerHandler) GetContainerLogs(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	// Tail param is not supported in interface currently

	logs, err := service.GetContainerLogs(r.Context(), containerID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get container logs", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"logs": logs,
		},
	})
}

// RemoveContainer handles DELETE /api/servers/{serverID}/containers/{id}
func (h *DockerHandler) RemoveContainer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	containerID := chi.URLParam(r, "id")
	// force param not supported in interface (needs update if we want force)
	// Interface: RemoveContainer(ctx, id)

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.RemoveContainer(r.Context(), containerID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to remove container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Container removed successfully",
	})
}

// ListImages handles GET /api/servers/{serverID}/images
func (h *DockerHandler) ListImages(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	images, err := service.ListImages(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list images", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    images,
	})
}

// PullImage handles POST /api/servers/{serverID}/images/pull
func (h *DockerHandler) PullImage(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var req struct {
		Image string `json:"image"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	reader, err := service.PullImage(r.Context(), req.Image)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to pull image", err)
		return
	}
	defer reader.Close()

	// Stream output
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")

	if _, err := io.Copy(w, reader); err != nil {
		// Log error?
	}
}

// RemoveImage handles DELETE /api/servers/{serverID}/images/{id}
func (h *DockerHandler) RemoveImage(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	imageID := chi.URLParam(r, "id")

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.RemoveImage(r.Context(), imageID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to remove image", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Image removed successfully",
	})
}

// RunImage handles POST /api/servers/{serverID}/images/run
func (h *DockerHandler) RunImage(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var req struct {
		Image string `json:"image"`
		Name  string `json:"name"`
		Ports string `json:"ports"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	containerID, err := service.RunContainer(r.Context(), req.Image, req.Name, req.Ports)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to run container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Container started successfully",
		Data: map[string]string{
			"id": containerID,
		},
	})
}

// ListVolumes handles GET /api/servers/{serverID}/volumes
func (h *DockerHandler) ListVolumes(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	volumes, err := service.ListVolumes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list volumes", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    volumes,
	})
}

// RemoveVolume handles DELETE /api/servers/{serverID}/volumes/{name}
func (h *DockerHandler) RemoveVolume(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	name := chi.URLParam(r, "name")

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.RemoveVolume(r.Context(), name); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to remove volume", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Volume removed successfully",
	})
}

// ListNetworks handles GET /api/servers/{serverID}/networks
func (h *DockerHandler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	networks, err := service.ListNetworks(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list networks", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    networks,
	})
}

// RemoveNetwork handles DELETE /api/servers/{serverID}/networks/{id}
func (h *DockerHandler) RemoveNetwork(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	id := chi.URLParam(r, "id")

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	if err := service.RemoveNetwork(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to remove network", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Network removed successfully",
	})
}

// BuildImage handles POST /api/servers/{serverID}/images/build
func (h *DockerHandler) BuildImage(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var req struct {
		Path    string `json:"path"`
		Tag     string `json:"tag"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	buildPath := req.Path

	if req.Content != "" {
		fileService, err := h.manager.GetFileProvider(serverID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
			return
		}

		tempID, err := uuid.NewRandom()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to generate uuid", err)
			return
		}
		tempDir := fmt.Sprintf("/tmp/gopulse-build-%s", tempID.String())

		if err := fileService.Mkdir(r.Context(), tempDir); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to create temp directory", err)
			return
		}

		// Cleanup in background
		defer func() {
			go fileService.Delete(context.Background(), tempDir)
		}()

		dockerfilePath := fmt.Sprintf("%s/Dockerfile", tempDir)
		if err := fileService.WriteFile(r.Context(), dockerfilePath, bytes.NewBufferString(req.Content), 0644); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to write Dockerfile", err)
			return
		}
		buildPath = tempDir
	}

	reader, err := service.BuildImage(r.Context(), buildPath, req.Tag)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to start build", err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	if _, err := io.Copy(w, reader); err != nil {
		// client disconnected
	}
}

// BuildCompose handles POST /api/servers/{serverID}/compose/build
func (h *DockerHandler) BuildCompose(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	service, err := h.manager.GetDockerProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get docker service", err)
		return
	}

	// Determine project directory
	projectDir := req.File

	if req.Content != "" {
		fileService, err := h.manager.GetFileProvider(serverID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
			return
		}

		tempID, err := uuid.NewRandom()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to generate uuid", err)
			return
		}
		tempDir := fmt.Sprintf("/tmp/gopulse-compose-%s", tempID.String())

		if err := fileService.Mkdir(r.Context(), tempDir); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to create temp directory", err)
			return
		}

		defer func() {
			go fileService.Delete(context.Background(), tempDir)
		}()

		composePath := fmt.Sprintf("%s/docker-compose.yml", tempDir)
		if err := fileService.WriteFile(r.Context(), composePath, bytes.NewBufferString(req.Content), 0644); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to write compose file", err)
			return
		}
		projectDir = tempDir
	} else {
		// If path is provided, check if it's a file or directory
		// We can't easily check remote file type here without extra calls,
		// but standard practice for "File" input in this context might be full path to docker-compose.yml
		// So we take Dir() if it looks like a file, or assume it's a dir.
		if filepath.Ext(projectDir) == ".yml" || filepath.Ext(projectDir) == ".yaml" {
			projectDir = filepath.Dir(projectDir)
		}
	}

	reader, err := service.BuildCompose(r.Context(), projectDir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to start compose build", err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	if _, err := io.Copy(w, reader); err != nil {
		// client disconnected
	}
}

// decodeJSON helper
func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
