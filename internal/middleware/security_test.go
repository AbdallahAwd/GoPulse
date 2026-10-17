package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogsOmitOAuthCodesAndQuerySecrets(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	r := httptest.NewRequest("GET", "/auth/github/callback?code=PRIVATE-CODE&state=PRIVATE-STATE", nil)
	LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), r)
	got := output.String()
	if strings.Contains(got, "PRIVATE-") || !strings.Contains(got, "/auth/github/callback") {
		t.Fatalf("unexpected request log: %s", got)
	}
}

func TestSecurityHeadersPreventCachingSensitiveResponses(t *testing.T) {
	for _, path := range []string{"/login", "/api/auth/login", "/api/export-import/export", "/static/js/app/main.js"} {
		w := httptest.NewRecorder()
		SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("missing security headers for %s", path)
		}
		if !strings.HasPrefix(path, "/static/") && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("sensitive response %s can be cached", path)
		}
	}
}
