package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

func CanAccessServer(r *http.Request, id string) bool {
	role, _ := r.Context().Value(RoleContextKey).(string)
	if role == "admin" || role == "agent_all_servers" {
		return true
	}
	servers, _ := r.Context().Value(AllowedServersContextKey).([]string)
	for _, allowed := range servers {
		if allowed == id {
			return true
		}
	}
	return false
}

func HasPermission(r *http.Request, permission string) bool {
	role, _ := r.Context().Value(RoleContextKey).(string)
	if role == "admin" {
		return true
	}
	permissions, _ := r.Context().Value(PermissionsContextKey).([]string)
	for _, p := range permissions {
		if p == "*" || p == permission || (p == "dashboard:admin" && permission == "*") {
			return true
		}
	}
	return false
}

// SameOrigin rejects cross-site browser requests, including authenticated WebSockets.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host == r.Host && u.User == nil && (u.Path == "" || u.Path == "/")
}

func OriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !SameOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Cross-site request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PermissionGuard applies the existing explicit permission model to every API write.
func PermissionGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/auth/") || isGitHubWebhookPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/mcp" {
			next.ServeHTTP(w, r)
			return
		} // Tool-specific policy follows authentication.
		permission := "server:write"
		if r.URL.Path == "/api/agent-token" || r.URL.Path == "/api/agents" || strings.HasPrefix(r.URL.Path, "/api/agents/") {
			next.ServeHTTP(w, r)
			return
		}
		areas := map[string]string{"settings": "*", "cron": "cron:write", "databases": "database:write", "load-balancers": "domains:write", "projects": "projects:write", "domains": "domains:write", "firewall": "firewall:write", "services": "services:write", "containers": "docker:write", "images": "docker:write", "volumes": "docker:write", "networks": "docker:write", "compose": "docker:write", "files": "files:write", "processes": "processes:write", "system": "system:write", "users": "user:manage"}
		// Match the resource position, never an arbitrary server/resource ID that happens to equal an area name.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		area := ""
		if len(parts) > 1 {
			area = parts[1]
		}
		if area == "servers" && len(parts) > 3 {
			area = parts[3]
		}
		if value, ok := areas[area]; ok {
			permission = value
		}
		if area == "files" && len(parts) > 4 && parts[4] == "exec" {
			permission = "terminal:write"
		}

		if strings.HasPrefix(r.URL.Path, "/api/operations/") {
			permission = ""
		} // Ownership and approval policy enforced in handler.
		if permission != "" && !HasPermission(r, permission) {
			http.Error(w, "Permission required: "+permission, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
