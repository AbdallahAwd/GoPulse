package middleware

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIBudgetNeverBlocksAssetsOrPages(t *testing.T) {
	router := chi.NewRouter()
	router.Use(NewRateLimiter(6, time.Hour).APIOnlyHandler)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	router.Get("/", ok)
	router.Get("/static/*", ok)
	router.Route("/api", func(r chi.Router) { r.Use(NewRateLimiter(2, time.Hour).APIHandler); r.Get("/data", ok) })
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:1000"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 2; i++ {
		if request("/api/data").Code != 200 {
			t.Fatal("budget denied too early")
		}
	}
	limited := request("/api/data")
	if limited.Code != 429 || limited.Header().Get("Retry-After") == "" {
		t.Fatal("missing rate limit or recovery header")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(limited.Body.Bytes(), &body); err != nil || body["success"] != false {
		t.Fatal("rate limit is not a structured API failure")
	}
	for i := 0; i < 150; i++ {
		for _, path := range []string{"/", "/static/js/app/databases.js", "/static/css/console.css"} {
			if request(path).Code != 200 {
				t.Fatalf("API exhaustion blocked %s", path)
			}
		}
	}
}
func TestAPIBudgetsAreIndependentForUsersBehindSameProxy(t *testing.T) {
	handler := NewRateLimiter(1, time.Hour).APIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	request := func(user string) int {
		r := httptest.NewRequest("GET", "/api/data", nil)
		r.RemoteAddr = "127.0.0.1:1000"
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, user))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if request("alice") != 200 || request("alice") != 429 || request("bob") != 200 {
		t.Fatal("users share a token bucket")
	}
}
func TestAuthLimiterDoesNotTrustSpoofedForwardedIP(t *testing.T) {
	handler := NewRateLimiter(1, time.Hour).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for i, ip := range []string{"10.0.0.1", "10.0.0.2"} {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.RemoteAddr = "127.0.0.1:1000"
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 200
		if i == 1 {
			want = 429
		}
		if w.Code != want {
			t.Fatal("spoofed header bypassed login limit")
		}
	}
}

func TestUnauthenticatedAPIFailuresRemainRateLimited(t *testing.T) {
	handler := NewRateLimiter(1, time.Hour).APIOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static/js/app/core.js" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(401)
	}))
	for i, want := range []int{401, 429} {
		r := httptest.NewRequest("GET", "/api/servers", nil)
		r.RemoteAddr = "127.0.0.1:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/static/js/app/core.js", nil)
	r.RemoteAddr = "127.0.0.1:1000"
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("authentication traffic blocked scripts")
	}
}
