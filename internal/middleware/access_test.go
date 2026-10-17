package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSameOrigin(t *testing.T) {
	for _, test := range []struct {
		origin string
		allow  bool
	}{{"", true}, {"https://dashboard.example", true}, {"https://evil.example", false}, {"null", false}, {"https://dashboard.example@evil.example", false}} {
		r := httptest.NewRequest("POST", "https://dashboard.example/api/mcp", nil)
		r.Header.Set("Origin", test.origin)
		if SameOrigin(r) != test.allow {
			t.Errorf("%s", test.origin)
		}
	}
}

func TestOriginGuardAllowsOAuthNavigationButRejectsCrossSiteAPIRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, mode, dest, origin string
		status                           int
	}{
		{"OAuth callback", "GET", "navigate", "document", "", 204},
		{"API fetch", "GET", "cors", "empty", "", 403},
		{"iframe", "GET", "navigate", "iframe", "", 403},
		{"WebSocket", "GET", "websocket", "empty", "https://evil.example", 403},
		{"cross-site form", "POST", "navigate", "document", "https://evil.example", 403},
		{"navigation with foreign Origin", "GET", "navigate", "document", "https://evil.example", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "https://panel.example/auth/github/callback?code=test", nil)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("Sec-Fetch-Mode", tc.mode)
			r.Header.Set("Sec-Fetch-Dest", tc.dest)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			OriginGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
		})
	}
}
func TestPermissionGuardDoesNotInterpretResourceIDsAsPermissions(t *testing.T) {
	for _, path := range []string{"/api/servers/firewall/databases/db/start", "/api/servers/databases/services/nginx/restart"} {
		request := httptest.NewRequest("POST", path, nil)
		ctx := context.WithValue(request.Context(), RoleContextKey, "viewer")
		ctx = context.WithValue(ctx, PermissionsContextKey, []string{"firewall:write"})
		response := httptest.NewRecorder()
		PermissionGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(response, request.WithContext(ctx))
		if response.Code != 403 {
			t.Fatalf("resource ID changed permission: %s %d", path, response.Code)
		}
	}
}
func TestRateLimiterFractionalRefillAndUntrustedForwarding(t *testing.T) {
	rl := &RateLimiter{rate: 100, interval: time.Minute, visitors: map[string]*visitor{"ip": {tokens: 0, lastUpdate: time.Now().Add(-time.Second)}}}
	if !rl.allow("ip") {
		t.Fatal("fractional time refill truncated")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:40000"
	r.Header.Set("X-Forwarded-For", "evil")
	if getIP(r) != "10.1.2.3" {
		t.Fatal("spoofed client identity")
	}
	rl.visitors["partial"] = &visitor{tokens: 0.1, lastUpdate: time.Now()}
	if rl.allow("partial") {
		t.Fatal("request allowed without a full token")
	}
}
