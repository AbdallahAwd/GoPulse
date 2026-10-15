package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

const AgentCredentialContextKey contextKey = "agent_credential"

type agentDispatchKey struct{}

// AgentDispatch is an in-process capability, never accepted from HTTP headers.
// MCP chooses an allowlisted route and re-enters normal auth/permission checks.
func AgentDispatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentDispatchKey{}, true)
}
func isAgentDispatch(ctx context.Context) bool {
	value, _ := ctx.Value(agentDispatchKey{}).(bool)
	return value
}
func AgentCredential(r *http.Request) *models.AgentCredential {
	value, _ := r.Context().Value(AgentCredentialContextKey).(*models.AgentCredential)
	return value
}
func AgentMode(r *http.Request) string {
	if credential := AgentCredential(r); credential != nil {
		return credential.Mode
	}
	return "supervised"
}
func AgentAuditActor(r *http.Request) string {
	owner, _ := r.Context().Value(UserContextKey).(string)
	if credential := AgentCredential(r); credential != nil {
		return owner + "|agent:" + credential.ID
	}
	return owner
}

func (am *AuthMiddleware) SetAgentStore(st *store.AgentStore) { am.agentStore = st }

func (am *AuthMiddleware) effectiveAgent(secret string) (models.AgentCredential, []string, []string, error) {
	if am.agentStore == nil {
		return models.AgentCredential{}, nil, nil, fmt.Errorf("agent credentials unavailable")
	}
	credential, err := am.agentStore.Authenticate(secret)
	if err != nil {
		return credential, nil, nil, err
	}
	user, err := am.userStore.GetUser(credential.Owner)
	if err != nil || credential.OwnerID != user.ID {
		return credential, nil, nil, fmt.Errorf("credential owner no longer exists")
	}
	permissions := []string{}
	for _, p := range credential.Permissions {
		if agentOwnerPermission(user, p) {
			permissions = append(permissions, p)
		}
	}
	servers := []string{}
	if credential.AllServers && user.Role == "admin" {
		servers = nil
	} else if credential.AllServers {
		servers = append(servers, user.AllowedServers...)
	} else {
		for _, id := range credential.ServerIDs {
			if user.Role == "admin" || containsAgentString(user.AllowedServers, id) {
				servers = append(servers, id)
			}
		}
	}
	return credential, permissions, servers, nil
}
func containsAgentString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// Read scopes add restrictions to existing server-readable API routes. Secret
// access requires existing database/project write authority or an administrator.
func agentOwnerPermission(user models.User, permission string) bool {
	if user.Role == "admin" || containsAgentString(user.Permissions, "*") {
		return true
	}
	if permission == "dashboard:admin" {
		return false
	}
	if permission == "files:read" || permission == "logs:read" || permission == "database:read" {
		return true
	}
	if permission == "secrets:read" {
		return containsAgentString(user.Permissions, "database:write") || containsAgentString(user.Permissions, "projects:write")
	}
	return containsAgentString(user.Permissions, permission)
}

func (am *AuthMiddleware) opaqueAgentClaims(r *http.Request) (jwt.MapClaims, bool) {
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	credential, permissions, servers, err := am.effectiveAgent(secret)
	if err != nil {
		return nil, false
	}
	return jwt.MapClaims{"username": credential.Owner, "agent": true, "aud": "gopulse-mcp", "exp": credential.ExpiresAt.Unix(), "agent_credential": credential, "agent_permissions": permissions, "agent_servers": servers}, true
}

// RefreshAgentRequest revalidates revocation and current ownership/scopes before
// an asynchronous operation dispatches. Nothing is authorized from cached JWT
// claims alone, and revoked tokens cannot leave approval work executable.
func (am *AuthMiddleware) RefreshAgentRequest(r *http.Request) (*http.Request, error) {
	var refreshed *http.Request
	recorder := &agentAuthRecorder{header: http.Header{}}
	am.Handler(http.HandlerFunc(func(_ http.ResponseWriter, next *http.Request) { refreshed = next })).ServeHTTP(recorder, r)
	if refreshed == nil {
		return nil, fmt.Errorf("agent authorization expired or was revoked")
	}
	return refreshed, nil
}

type agentAuthRecorder struct{ header http.Header }

func (w *agentAuthRecorder) Header() http.Header            { return w.header }
func (w *agentAuthRecorder) WriteHeader(int)                {}
func (w *agentAuthRecorder) Write(data []byte) (int, error) { return len(data), nil }
