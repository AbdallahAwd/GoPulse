package handlers

import (
	"html/template"
	"io/fs"
	"net/http"

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

// Index renders the server list page
func (h *DashboardHandler) Index(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "GoPulse - Servers",
		"Page":  "servers",
	}

	// We need to define which template to render as "content"
	// For base.html to include server_list.html, we likely need to parse them together or associate them.
	// Since we parsed "templates/*.html", we have access to all named templates.
	// In Go templates, {{block "content" .}}{{end}} works if the child template defines it.
	// But usually we execute the specific child template which includes the base, or execute base with data.
	// Assuming base.html has {{template "content" .}}, and server_list.html defines "content".

	// However, if both server_list.html and dashboard.html define "content", the last parsed one wins in a single template set.
	// We might need separate template instances or use explicit template names.
	// For simplicity with shared ParseFS, let's assume we can target the template name if defined distinctly,
	// but standard "content" block overriding is tricky with a single set.

	// A common pattern is:
	// t.Clone() -> t.ParseFiles("server_list.html") -> t.Execute("base.html", data)
	// But we already pre-parsed everything into `h.templates`.

	// Let's assume we update the templates to define unique names like "content-server-list" and "content-dashboard"
	// and base.html dynamically picks one, or we just Clone and re-parse.

	// Actually, if we use block definitions, we need to be careful.
	// Let's try to clone and execute specific "server_list.html" if it's a standalone file that extends base.
	// If the files just define {{judge "content"}}, they fight.

	// Let's just execute "server_list.html" directly if it contains the full page or extends base?
	// Usually:
	// {{define "content"}} ... {{end}} in server_list.html
	// To render it, we simply execute "base.html".
	// But if dashboard.html ALSO defines "content", they conflict in the same `*template.Template`.

	// Strategy: Parse templates on the fly or group them?
	// Or give them distinct names and have base.html accept a "ContentTemplate" variable?
	// Let's try the variable approach.
	// base.html: {{template (or .ContentTemplate "content") .}} -> dynamic template execution is not direct in standard text/template without a func map.

	// Better: Clone the common layout and parse the specific page.
	// But `h.templates` has all of them.

	// Let's look at `base.html` again. It has `{{template "content" .}}`.
	// If I modify `server_list.html` to `{{define "server_list_content"}}` and `dashboard.html` to `{{define "dashboard_content"}}`
	// Then pass "server_list_content" as a variable to base?
	// Base would need `{{template (print .Page "_content") .}}`? No, `template` action needs a string literal or we need a func.

	// Let's assume for this step I will rename the definitions in the HTML files and use a custom func or just distinct base templates?
	// Or simpler: clone the base and parse the specific file.
	// `t := template.Must(template.ParseFS(web.Assets, "templates/base.html", "templates/components/*.html", "templates/server_list.html"))`

	// Re-parsing per request is fine for low traffic MVP.
	// Let's use `h.render(w, "server_list.html", data)` helper.

	if err := h.render(w, "server_list.html", data); err != nil {
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
	}

	if err := h.render(w, "dashboard.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Login renders the login page
func (h *DashboardHandler) Login(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "GoPulse - Sign In",
		"Page":  "login",
	}

	if err := h.render(w, "login.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Register renders the registration page
func (h *DashboardHandler) Register(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "GoPulse - Setup",
		"Page":  "register",
	}

	if err := h.render(w, "register.html", data); err != nil {
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

	if err := h.render(w, "users.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *DashboardHandler) render(w http.ResponseWriter, view string, data interface{}) error {
	tmpl, err := template.ParseFS(h.assets, "templates/base.html", "templates/"+view)
	if err != nil {
		return err
	}

	return tmpl.ExecuteTemplate(w, "base.html", data)
}
