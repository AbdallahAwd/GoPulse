package handlers

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/amaa/gopulse/internal/middleware"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
)

// DashboardHandler handles the main dashboard page
type DashboardHandler struct {
	assets fs.FS
	store  *store.ServerStore
}

// NewDashboardHandler creates a new DashboardHandler
func NewDashboardHandler(assets fs.FS, store *store.ServerStore) *DashboardHandler {
	return &DashboardHandler{assets: assets, store: store}
}

// Index starts with the servers the current user manages.
func (h *DashboardHandler) Index(w http.ResponseWriter, r *http.Request) {
	h.Servers(w, r)
}

// Servers keeps the existing inventory and onboarding workflows available.
func (h *DashboardHandler) Servers(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{"Title": "GoPulse - Servers", "Page": "servers", "GlobalSection": "servers"}
	if err := h.render(w, r, "server_list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Workspace renders secondary console screens and redirects legacy resource URLs.
func (h *DashboardHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	section := strings.Trim(r.URL.Path, "/")
	if section == "" {
		h.Servers(w, r)
		return
	}
	// Old global-resource bookmarks now start with the appropriate server filter.
	legacyRoles := map[string]string{"applications": "apps", "databases": "data", "networking": "edge"}
	if role, ok := legacyRoles[section]; ok {
		http.Redirect(w, r, "/?role="+role, http.StatusFound)
		return
	}
	titles := map[string]string{"activity": "Activity", "settings": "Settings", "agents": "Agents"}
	title, ok := titles[section]
	if !ok {
		http.NotFound(w, r)
		return
	}
	descriptions := map[string]string{
		"activity": "Inspect deployment attempts and your infrastructure operations.",
		"agents":   "Connect Claude Code, Codex, or another MCP client to your servers.",
		"settings": "Manage team access, integrations, and dashboard configuration.",
	}
	data := map[string]interface{}{"Title": "GoPulse - " + title, "Page": "workspace", "GlobalSection": section, "SectionTitle": title, "SectionDescription": descriptions[section]}
	view := "workspace.html"
	if section == "agents" {
		view = "agents.html"
		data["Page"] = "agents"
	}
	if err := h.render(w, r, view, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServerDashboard renders the dashboard for a specific server
func (h *DashboardHandler) ServerDashboard(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	server, err := h.store.GetServer(serverID)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"Title":      "GoPulse - " + server.Name,
		"Page":       "dashboard",
		"ServerID":   server.ID,
		"ServerName": server.Name,
		"IsDbOnly":   server.IsDbOnly,
		"ServerHost": server.Host,
		"ServerPort": server.Port,
	}

	if err := h.render(w, r, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Login renders the login page
func (h *DashboardHandler) Login(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "GoPulse - Sign In",
		"Page":  "login",
	}

	if err := h.render(w, r, "login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Register renders the registration page
func (h *DashboardHandler) Register(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "GoPulse - Setup",
		"Page":  "register",
	}

	if err := h.render(w, r, "register.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Users renders the user management page
func (h *DashboardHandler) Users(w http.ResponseWriter, r *http.Request) {
	servers := h.store.ListServers()

	data := map[string]interface{}{
		"Title":   "GoPulse - User Management",
		"Page":    "users",
		"Servers": servers,
	}

	if err := h.render(w, r, "users.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *DashboardHandler) render(w http.ResponseWriter, r *http.Request, view string, data map[string]interface{}) error {
	data["CanManageUsers"] = middleware.HasPermission(r, "user:manage")
	data["CanManageServers"] = middleware.HasPermission(r, "server:write")
	data["CanWriteCron"] = middleware.HasPermission(r, "cron:write")
	data["CanWriteProjects"] = middleware.HasPermission(r, "projects:write")
	data["CanWriteDatabases"] = middleware.HasPermission(r, "database:write")
	data["CanTransferConfiguration"] = middleware.HasPermission(r, "*")
	if data["GlobalSection"] == nil {
		data["GlobalSection"] = "settings"
		if data["Page"] == "dashboard" {
			data["GlobalSection"] = "servers"
		}
	}
	data["ServerNavigation"] = []struct {
		Key, Label, Icon, Group string
		AppOnly                 bool
	}{
		{"overview", "Overview", "fa-gauge-high", "Resources", false},
		{"projects", "Applications", "fa-cubes", "Resources", true},
		{"databases", "Databases", "fa-database", "Resources", false},
		{"domains", "Domains and TLS", "fa-globe", "Resources", true},
		{"load-balancers", "Load balancers", "fa-network-wired", "Resources", true},
		{"processes", "Processes", "fa-list", "Server tools", false},
		{"docker", "Docker", "fa-box", "Server tools", false},
		{"cron", "Cron jobs", "fa-clock", "Server tools", false},
		{"files", "Files", "fa-folder", "Server tools", false},
		{"terminal", "Terminal", "fa-terminal", "Server tools", false},
		{"system-users", "Server users", "fa-users", "Server tools", false},
		{"firewall", "Firewall", "fa-shield-halved", "Server tools", false},
		{"settings", "Server settings", "fa-gear", "Server tools", false},
	}
	data["GlobalNavigation"] = []struct{ Key, Label, URL, Icon string }{
		{"servers", "Servers", "/", "fa-server"},
		{"activity", "Activity", "/activity", "fa-clock-rotate-left"},
		{"agents", "Agents", "/agents", "fa-robot"},
		{"settings", "Settings", "/settings", "fa-gear"},
	}
	tmpl, err := template.ParseFS(h.assets, "templates/base.html", "templates/"+view)
	if err != nil {
		return err
	}

	return tmpl.ExecuteTemplate(w, "base.html", data)
}
