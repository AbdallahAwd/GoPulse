package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
)

// FileHandler handles file management endpoints
type FileHandler struct {
	manager *services.ServiceManager
}

// NewFileHandler creates a new FileHandler
func NewFileHandler(manager *services.ServiceManager) *FileHandler {
	return &FileHandler{manager: manager}
}

// ListFiles handles GET /api/servers/{serverID}/files/list
func (h *FileHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/" // Default to root
	}

	// Security: Sanitize path
	// In remote context, path is absolute on remote system.
	// We can trust the input path is intended, but we should clean it.
	cleanPath := filepath.Clean(path)
	// Additional checks can be added here (e.g. Jail)

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	files, err := service.ListDir(r.Context(), cleanPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list files", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"path":  cleanPath,
			"files": files,
		},
	})
}

// DownloadFile handles GET /api/servers/{serverID}/files/content
func (h *FileHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, http.StatusBadRequest, "Path required", nil)
		return
	}

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	reader, err := service.ReadFile(r.Context(), path)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read file", err)
		return
	}
	defer reader.Close()

	// Determine if download or view
	isDownload := r.URL.Query().Get("download") != "false"

	// Set headers
	filename := filepath.Base(path)
	if isDownload {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Type", "application/octet-stream")
	} else {
		// View mode - try to guess type or default to text/plain
		// For editor we want text usually.
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}

	// Stream content
	if _, err := io.Copy(w, reader); err != nil {
		// Can't write JSON error if streaming started
		return
	}
}

// SaveFile handles POST /api/servers/{serverID}/files/save
func (h *FileHandler) SaveFile(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "Path required", nil)
		return
	}

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	// Write content
	// Using a generic 0644 permission or preserving existing?
	// RemoteFileService uses O_TRUNC | O_CREATE. checking perm.
	// We'll use 0644 as default for new, existing files usually keep perms if we don't chmod explicitly?
	// RemoteFileService.WriteFile does call Chmod.
	// Maybe we should just use 0644.
	if err := service.WriteFile(r.Context(), req.Path, strings.NewReader(req.Content), 0644); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save file", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "File saved successfully",
	})
}

// UploadFile handles POST /api/servers/{serverID}/files/upload
func (h *FileHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	// Stream upload directly to destination without buffering entire file in memory/temp
	serverID := chi.URLParam(r, "serverID")
	targetPath := r.URL.Query().Get("path")
	if targetPath == "" {
		respondError(w, http.StatusBadRequest, "Target path required", nil)
		return
	}

	// Use MultipartReader for streaming
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid multipart request", err)
		return
	}

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	var uploadedPath string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			respondError(w, http.StatusBadRequest, "Failed to read multipart", err)
			return
		}

		if part.FormName() == "file" {
			filename := part.FileName()
			if filename == "" {
				continue
			}

			// Full destination path
			destPath := filepath.Join(targetPath, filename)
			uploadedPath = destPath

			// Write stream directly to service
			// Default permissions 0644
			if err := service.WriteFile(r.Context(), destPath, part, 0644); err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to upload file", err)
				return
			}
			// We only handle one file per request for now
			break
		}
	}

	if uploadedPath == "" {
		respondError(w, http.StatusBadRequest, "No file found in request", nil)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "File uploaded successfully",
		Data: map[string]string{
			"path": uploadedPath,
		},
	})
}

// PerformAction handles POST /api/servers/{serverID}/files/action
func (h *FileHandler) PerformAction(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Action  string `json:"action"` // delete, chmod, mkdir, rename
		Path    string `json:"path"`
		NewPath string `json:"new_path,omitempty"` // for rename
		Mode    string `json:"mode,omitempty"`     // for chmod (e.g. "755", octal string)
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	switch req.Action {
	case "delete":
		if err := service.Delete(r.Context(), req.Path); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to delete item", err)
			return
		}
	case "mkdir":
		if err := service.Mkdir(r.Context(), req.Path); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to ensure directory", err)
			return
		}
	case "rename":
		if req.NewPath == "" {
			respondError(w, http.StatusBadRequest, "New path required for rename", nil)
			return
		}
		if err := service.Rename(r.Context(), req.Path, req.NewPath); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to rename item", err)
			return
		}
	case "chmod":
		// Parse mode (octal)
		modeVal, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid mode format (octal required)", err)
			return
		}
		if err := service.Chmod(r.Context(), req.Path, os.FileMode(modeVal)); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to chmod item", err)
			return
		}
	case "compress":
		// New path is destination zip file
		// Defaults to current path + .zip if not provided?
		// But let's assume UI provides a name or we generate one.
		destPath := req.NewPath
		if destPath == "" {
			destPath = req.Path + ".zip"
		}

		if err := service.Compress(r.Context(), req.Path, destPath); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to compress item", err)
			return
		}
	case "decompress":
		// New path is destination directory
		// Defaults to current directory (parent of zip) if not provided?
		// Or maybe a folder with same name as zip (without .zip)?
		destPath := req.NewPath
		if destPath == "" {
			// Default to same directory as zip file
			destPath = filepath.Dir(req.Path)
		}

		if err := service.Decompress(r.Context(), req.Path, destPath); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to decompress item", err)
			return
		}
	default:
		respondError(w, http.StatusBadRequest, "Unknown action", nil)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: fmt.Sprintf("Action %s completed", req.Action),
	})
}

// ExecuteCommand handles POST /api/servers/{serverID}/files/exec
func (h *FileHandler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Path == "" {
		req.Path = "/" // Default to root
	}
	if req.Command == "" {
		respondError(w, http.StatusBadRequest, "Command required", nil)
		return
	}

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer service.Close()

	output, err := service.ExecuteCommand(r.Context(), req.Path, req.Command)
	if err != nil {
		// If command fails, we might still want to show output (stderr)
		// But RunCommand usually returns combined output and error.
		// If error is exit code, we return output but maybe status 200 or 500?
		// User wants "view result". Even if it fails (exit code 1), it's a result.
		// So we return success=false or just return output with error message?
		// Let's return success=true for the API call, but include error in data?
		// Or if execution completely failed (like SSH error).

		// If it's a command error (exit status), err is usually *ExitError.
		// Let's just return whatever output we got, maybe wrap error.
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: false,
			Message: err.Error(),
			Data: map[string]string{
				"output": output,
			},
		})
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]string{
			"output": output,
		},
	})
}
