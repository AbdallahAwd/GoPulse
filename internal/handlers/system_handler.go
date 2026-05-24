package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/go-chi/chi/v5"
)

// SystemHandler handles system-level operations
type SystemHandler struct {
	serviceManager *services.ServiceManager
}

// NewSystemHandler creates a new SystemHandler
func NewSystemHandler(serviceManager *services.ServiceManager) *SystemHandler {
	return &SystemHandler{
		serviceManager: serviceManager,
	}
}

// Restart handles POST /api/system/restart (Local)
func (h *SystemHandler) Restart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Server restarting...",
	})

	// Restart in a goroutine to allow the response to complete
	go func() {
		time.Sleep(500 * time.Millisecond)

		executable, err := os.Executable()
		if err != nil {
			// Fallback: just exit and hope a supervisor restarts it
			os.Exit(0)
		}

		// replace current process
		if err := syscall.Exec(executable, os.Args, os.Environ()); err != nil {
			// If exec fails, exit
			os.Exit(1)
		}
	}()
}

// Shutdown handles POST /api/system/shutdown (Local)
func (h *SystemHandler) Shutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Server shutting down...",
	})

	// Shutdown in a goroutine
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

// RestartRemote handles POST /api/servers/{serverID}/system/restart
func (h *SystemHandler) RestartRemote(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "Failed to get SSH client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Run reboot command with sudo if not root
	rebootCmd := "sudo reboot"
	if client.IsRoot() {
		rebootCmd = "reboot"
	}
	go func() {
		client.RunCommand(rebootCmd)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Remote server restarting...",
	})
}

// ShutdownRemote handles POST /api/servers/{serverID}/system/shutdown
func (h *SystemHandler) ShutdownRemote(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "Failed to get SSH client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Run shutdown command with sudo if not root
	shutdownCmd := "sudo shutdown -h now"
	if client.IsRoot() {
		shutdownCmd = "shutdown -h now"
	}
	go func() {
		client.RunCommand(shutdownCmd)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.APIResponse{
		Success: true,
		Message: "Remote server shutting down...",
	})
}

// CheckSoftware handles GET /api/servers/{serverID}/system/check/{software}
// Checks if docker or nginx is installed on the remote server
func (h *SystemHandler) CheckSoftware(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	software := chi.URLParam(r, "software")

	if software != "docker" && software != "nginx" {
		respondError(w, http.StatusBadRequest, "Unsupported software: "+software, nil)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SSH connection failed", err)
		return
	}

	var cmd string
	if software == "docker" {
		cmd = "docker --version 2>/dev/null && echo 'INSTALLED' || echo 'NOT_INSTALLED'"
	} else {
		cmd = "nginx -v 2>&1 && echo 'INSTALLED' || echo 'NOT_INSTALLED'"
	}

	output, err := client.RunCommand(cmd)
	if err != nil {
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: true,
			Data: map[string]interface{}{
				"installed": false,
				"software":  software,
			},
		})
		return
	}

	installed := !contains(output, "NOT_INSTALLED") && contains(output, "INSTALLED")

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"installed": installed,
			"software":  software,
			"version":   strings.TrimSpace(output),
		},
	})
}

// InstallSoftware handles POST /api/servers/{serverID}/system/install/{software}
// Installs docker or nginx on the remote server
func (h *SystemHandler) InstallSoftware(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	software := chi.URLParam(r, "software")

	if software != "docker" && software != "nginx" {
		respondError(w, http.StatusBadRequest, "Unsupported software: "+software, nil)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SSH connection failed", err)
		return
	}

	var installCmd string
	if software == "docker" {
		installCmd = "curl -fsSL https://get.docker.com | sh 2>&1"
	} else {
		// Try apt first, fallback to yum
		installCmd = "if command -v apt-get &>/dev/null; then sudo apt-get update && sudo apt-get install -y nginx 2>&1; elif command -v yum &>/dev/null; then sudo yum install -y nginx 2>&1; elif command -v dnf &>/dev/null; then sudo dnf install -y nginx 2>&1; else echo 'ERROR: No supported package manager found'; fi"
	}

	// Stream the output
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")

	output, err := client.RunCommand(installCmd)
	if err != nil {
		w.Write([]byte(fmt.Sprintf("\n[Error] Installation failed: %v\n", err)))
		return
	}

	w.Write([]byte(output))

	// Post-install steps
	if software == "nginx" {
		// Enable and start nginx
		postCmd := "sudo systemctl enable nginx && sudo systemctl start nginx 2>&1"
		postOutput, _ := client.RunCommand(postCmd)
		w.Write([]byte("\n" + postOutput))
		w.Write([]byte("\n[Success] Nginx installed and started!\n"))
	} else {
		w.Write([]byte("\n[Success] Docker installed!\n"))
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
