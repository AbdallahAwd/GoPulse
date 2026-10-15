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
