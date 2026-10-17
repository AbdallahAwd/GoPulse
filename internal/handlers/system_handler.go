package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
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

// LinuxUser represents a Linux user on the remote host
type LinuxUser struct {
	Username string `json:"username"`
	UID      string `json:"uid"`
	GID      string `json:"gid"`
	Comment  string `json:"comment"`
	HomeDir  string `json:"home_dir"`
	Shell    string `json:"shell"`
	IsSystem bool   `json:"is_system"`
}

// ListLinuxUsers lists all users on the remote server
func (h *SystemHandler) ListLinuxUsers(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		respondError(w, http.StatusBadRequest, "Server ID is required", nil)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SSH connection failed", err)
		return
	}

	output, err := client.RunCommand("cat /etc/passwd")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read passwd file", err)
		return
	}

	var users []LinuxUser
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 7 {
			username := parts[0]
			uidStr := parts[2]
			gidStr := parts[3]
			comment := parts[4]
			homeDir := parts[5]
			shell := parts[6]

			var uid int
			fmt.Sscanf(uidStr, "%d", &uid)

			// Hide all system accounts (UID 1 to 999) except root (UID 0)
			if uid >= 1 && uid < 1000 {
				continue
			}

			isSystem := uid == 0

			users = append(users, LinuxUser{
				Username: username,
				UID:      uidStr,
				GID:      gidStr,
				Comment:  comment,
				HomeDir:  homeDir,
				Shell:    shell,
				IsSystem: isSystem,
			})
		}
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    users,
	})
}

// CreateLinuxUser adds a new Linux user on the remote server
func (h *SystemHandler) CreateLinuxUser(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		respondError(w, http.StatusBadRequest, "Server ID is required", nil)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Shell    string `json:"shell"`
		IsSudoer bool   `json:"is_sudoer"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Username == "" {
		respondError(w, http.StatusBadRequest, "Username is required", nil)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SSH connection failed", err)
		return
	}

	shell := req.Shell
	if shell == "" {
		shell = "/bin/bash"
	}

	// 1. Create the user
	createUserCmd := fmt.Sprintf("sudo useradd -m -s %s %s", shell, req.Username)
	if client.IsRoot() {
		createUserCmd = fmt.Sprintf("useradd -m -s %s %s", shell, req.Username)
	}

	if _, err := client.RunCommand(createUserCmd); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create remote user: "+err.Error(), err)
		return
	}

	// 2. Set password if provided
	if req.Password != "" {
		passwordCmd := fmt.Sprintf("echo '%s:%s' | sudo chpasswd", req.Username, req.Password)
		if client.IsRoot() {
			passwordCmd = fmt.Sprintf("echo '%s:%s' | chpasswd", req.Username, req.Password)
		}

		if _, err := client.RunCommand(passwordCmd); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to set password: "+err.Error(), err)
			return
		}
	}

	// 3. Add to sudoers if checked
	if req.IsSudoer {
		sudoCmd := fmt.Sprintf("sudo usermod -aG sudo %s || sudo usermod -aG wheel %s", req.Username, req.Username)
		if client.IsRoot() {
			sudoCmd = fmt.Sprintf("usermod -aG sudo %s || usermod -aG wheel %s", req.Username, req.Username)
		}

		client.RunCommand(sudoCmd)
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: fmt.Sprintf("Linux user '%s' successfully created on remote server.", req.Username),
	})
}

// DeleteLinuxUser deletes a Linux user on the remote server
func (h *SystemHandler) DeleteLinuxUser(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	username := chi.URLParam(r, "username")

	if serverID == "" || username == "" {
		respondError(w, http.StatusBadRequest, "Server ID and Username are required", nil)
		return
	}

	if username == "root" {
		respondError(w, http.StatusForbidden, "Deleting the 'root' superuser is strictly prohibited.", nil)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SSH connection failed", err)
		return
	}

	deleteCmd := fmt.Sprintf("sudo userdel -r %s", username)
	if client.IsRoot() {
		deleteCmd = fmt.Sprintf("userdel -r %s", username)
	}

	if _, err := client.RunCommand(deleteCmd); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete remote user: "+err.Error(), err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: fmt.Sprintf("Linux user '%s' and their home directory deleted.", username),
	})
}
