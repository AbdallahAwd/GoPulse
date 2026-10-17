package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/AbdallahAwd/GoPulse/web"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestDashboardSettingsAuthorizationAndPublicIdentity(t *testing.T) {
	dir := t.TempDir()
	users, err := store.NewUserStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = users.AddUser(models.User{Username: "admin", Password: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if err = users.AddUser(models.User{Username: "viewer", Password: "fixture", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	settings, err := store.NewDashboardSettingsStore(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	opsStore, err := store.NewOperationStore(filepath.Join(dir, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := services.NewOperations(opsStore)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	s, err := services.NewDashboardDomainService(settings, ops, "0.0.0.0", 2886)
	if err != nil {
		t.Fatal(err)
	}
	s.OS = "darwin"
	auth := middleware.NewAuthMiddleware(testAuthSecret, users)
	handler := &DashboardSettingsHandler{Service: s}
	router := chi.NewRouter()
	router.Use(middleware.OriginGuard, auth.Handler, middleware.PermissionGuard)
	router.Get(services.DashboardIdentityPath, handler.Identity)
	router.Route("/api/settings/dashboard", handler.Routes)
	request := func(owner, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if owner != "" {
			token, err := auth.GenerateToken(owner, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			r.AddCookie(&http.Cookie{Name: "jwt_token", Value: token})
		}
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "PUT", "POST"} {
		path := "/api/settings/dashboard/domain"
		if method == "POST" {
			path += "/apply"
		}
		w := request("viewer", method, path, `{"domain":"dashboard.example.com"}`, nil)
		if w.Code != 403 {
			t.Fatal("viewer reached master settings", w.Code)
		}
	}
	w := request("", "GET", services.DashboardIdentityPath, "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), s.Identity) || strings.Contains(w.Body.String(), "2886") {
		t.Fatal("public probe failed or exposed host configuration", w.Body.String())
	}
	w = request("admin", "PUT", "/api/settings/dashboard/domain", `{"domain":"dashboard.example.com","https":true,"email":"admin@example.com","agree_terms":true}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("admin", "GET", "/api/settings/dashboard/domain/nginx", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "127.0.0.1:2886") {
		t.Fatal("preview ignored listening port", w.Body.String())
	}
	w = request("admin", "PUT", "/api/settings/dashboard/domain", `{"domain":"dashboard.example.com","unknown":"not accepted"}`, nil)
	if w.Code != 400 {
		t.Fatal("unknown settings accepted")
	}
	w = request("admin", "PUT", "/api/settings/dashboard/domain", `{"domain":"other.example.com","https":false}`, map[string]string{"Origin": "https://attacker.example"})
	if w.Code != 403 {
		t.Fatal("cross-origin settings write accepted")
	}
	w = request("admin", "POST", "/api/settings/dashboard/domain/apply", "", nil)
	if w.Code != 403 {
		t.Fatal("host mutation did not require explicit browser approval")
	}
	w = request("admin", "POST", "/api/settings/dashboard/domain/apply", "", map[string]string{"X-GoPulse-Human-Approval": "true"})
	if w.Code != 409 {
		t.Fatal("non-Linux host setup started")
	}
	loaded, _ := settings.Domain()
	if loaded.Domain != "dashboard.example.com" || loaded.AppliedDomain != "" {
		t.Fatal("saving settings deployed routing", loaded)
	}
}

func TestDashboardHTTPSCookiesDoNotTrustForwardedHeader(t *testing.T) {
	users, err := store.NewUserStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.AddUser(models.User{Username: "admin", Password: string(hash)}); err != nil {
		t.Fatal(err)
	}
	auth := middleware.NewAuthMiddleware(testAuthSecret, users)
	auth.SetSecureCookieHost(func(host string) bool { return host == "dashboard.example.com" })
	for _, host := range []string{"dashboard.example.com", "127.0.0.1:2886"} {
		r := httptest.NewRequest("POST", "http://"+host+"/api/auth/login", strings.NewReader(`{"username":"admin","password":"fixture"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		auth.LoginHandler(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Secure != (host == "dashboard.example.com") {
			t.Fatal("incorrect HTTPS proxy cookie", host, cookies)
		}
	}
}

func TestSettingsDomainPanelUsesMasterContext(t *testing.T) {
	h := NewDashboardHandler(web.Assets, nil)
	request := httptest.NewRequest("GET", "/settings", nil)
	request = request.WithContext(context.WithValue(request.Context(), middleware.RoleContextKey, "admin"))
	w := httptest.NewRecorder()
	h.Workspace(w, request)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, id := range []string{"master-domain-panel", "master-domain-form", "master-domain-hostname", "master-domain-apply", "master-domain-nginx"} {
		if !strings.Contains(w.Body.String(), `id="`+id+`"`) {
			t.Fatal("master settings panel missing", id)
		}
	}
}
