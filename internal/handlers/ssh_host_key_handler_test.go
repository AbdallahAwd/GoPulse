package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"
)

func hostKeyRequest(t *testing.T, method, path string, body any, role string, permissions, servers []string) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	ctx := context.WithValue(r.Context(), middleware.RoleContextKey, role)
	ctx = context.WithValue(ctx, middleware.PermissionsContextKey, permissions)
	ctx = context.WithValue(ctx, middleware.AllowedServersContextKey, servers)
	return r.WithContext(ctx)
}

func hostKeyHandlerFixture(t *testing.T) (*ServerHandler, *store.ServerStore, chi.Router, string) {
	t.Helper()
	servers, err := store.NewServerStore(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if err := servers.AddServer(models.Server{ID: "one", Name: "Test", Host: "127.0.0.1", Port: 2222, User: "fixture", Password: "DO-NOT-RETURN"}); err != nil {
		t.Fatal(err)
	}
	h := NewServerHandler(servers)
	r := chi.NewRouter()
	r.Use(middleware.OriginGuard, middleware.PermissionGuard)
	r.Post("/api/servers", h.AddServer)
	r.Put("/api/servers/{serverID}", h.UpdateServer)
	r.Post("/api/servers/ssh-host-key", h.ProbeSSHHostKey)
	r.Post("/api/servers/{serverID}/ssh-host-key", h.ProbeSSHHostKey)
	r.Put("/api/servers/{serverID}/ssh-host-key", h.TrustSSHHostKey)
	return h, servers, r, key
}

func TestSSHHostKeyTrustRequiresWriteAndServerScope(t *testing.T) {
	_, servers, router, key := hostKeyHandlerFixture(t)
	for _, tc := range []struct {
		role                 string
		permissions, servers []string
		status               int
	}{
		{"viewer", nil, []string{"one"}, 403},
		{"operator", []string{"server:write"}, []string{"other"}, 403},
		{"operator", []string{"server:write"}, []string{"one"}, 200},
	} {
		request := hostKeyRequest(t, "PUT", "/api/servers/one/ssh-host-key", remote.SSHHostKey{Host: "127.0.0.1", Port: 2222, PublicKey: key}, tc.role, tc.permissions, tc.servers)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s status %d: %s", tc.role, response.Code, response.Body.String())
		}
		saved, err := servers.GetServer("one")
		if err != nil {
			t.Fatal(err)
		}
		if tc.status == 200 && saved.TrustedHostKey != key {
			t.Fatal("approved key not stored")
		}
		if tc.status != 200 && saved.TrustedHostKey != "" {
			t.Fatal("unauthorized key stored")
		}
		if strings.Contains(response.Body.String(), "DO-NOT-RETURN") {
			t.Fatal("password leaked")
		}
	}
	// Browser origins are checked even for an authenticated administrator.
	request := hostKeyRequest(t, "PUT", "/api/servers/one/ssh-host-key", remote.SSHHostKey{Host: "127.0.0.1", Port: 2222, PublicKey: key}, "admin", nil, nil)
	request.Header.Set("Origin", "https://evil.invalid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("cross-site trust accepted")
	}
}

func TestSSHHostKeyTrustRejectsChangedAddressAndMalformedKeys(t *testing.T) {
	_, servers, router, key := hostKeyHandlerFixture(t)
	for _, tc := range []struct {
		host   string
		port   int
		key    string
		status int
	}{
		{"127.0.0.2", 2222, key, 409}, {"127.0.0.1", 2223, key, 409}, {"127.0.0.1", 2222, "invalid", 400},
	} {
		request := hostKeyRequest(t, "PUT", "/api/servers/one/ssh-host-key", remote.SSHHostKey{Host: tc.host, Port: tc.port, PublicKey: tc.key}, "admin", nil, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
	}
	saved, _ := servers.GetServer("one")
	if saved.TrustedHostKey != "" {
		t.Fatal("failed review changed trust")
	}
}

func TestAddingAndEditingServerPreservesApprovedKeyOnlyForSameAddress(t *testing.T) {
	_, servers, router, key := hostKeyHandlerFixture(t)
	server := models.Server{ID: "new", Name: "New", Host: "[::1]", Port: 2222, User: "fixture", Password: "DO-NOT-RETURN", TrustedHostKey: key}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, hostKeyRequest(t, "POST", "/api/servers", server, "admin", nil, nil))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	saved, err := servers.GetServer("new")
	if err != nil {
		t.Fatal(err)
	}
	if saved.TrustedHostKey != key || saved.Host != "::1" {
		t.Fatalf("pin/address lost: %+v", saved)
	}
	if strings.Contains(response.Body.String(), "DO-NOT-RETURN") {
		t.Fatal("credentials leaked")
	}
	saved.TrustedHostKey = ""
	saved.Password = ""
	saved.Name = "Renamed"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, hostKeyRequest(t, "PUT", "/api/servers/new", saved, "admin", nil, nil))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	updated, _ := servers.GetServer("new")
	if updated.TrustedHostKey != key || updated.Password != "DO-NOT-RETURN" {
		t.Fatal("metadata edit lost credentials or trust")
	}
	saved.Port = 2223
	response = httptest.NewRecorder()
	router.ServeHTTP(response, hostKeyRequest(t, "PUT", "/api/servers/new", saved, "admin", nil, nil))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	updated, _ = servers.GetServer("new")
	if updated.TrustedHostKey != "" {
		t.Fatal("pin carried over to a different address")
	}
	server.ID = "invalid"
	server.TrustedHostKey = "invalid"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, hostKeyRequest(t, "POST", "/api/servers", server, "admin", nil, nil))
	if response.Code != 400 {
		t.Fatal("malformed key accepted")
	}
	if _, err := servers.GetServer("invalid"); err == nil {
		t.Fatal("invalid server saved")
	}
}

func TestSSHKeyDiscoveryIsReadOnlyAndUsesSavedAddress(t *testing.T) {
	_, servers, router, key := hostKeyHandlerFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				server, _, _, _ := ssh.NewServerConn(conn, config)
				if server != nil {
					server.Close()
				}
			}()
		}
	}()
	saved, _ := servers.GetServer("one")
	saved.Port = listener.Addr().(*net.TCPAddr).Port
	saved.TrustedHostKey = key
	if err := servers.UpdateServer(saved); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/servers/one/ssh-host-key", "/api/servers/ssh-host-key"} {
		body := map[string]any{"host": saved.Host, "port": saved.Port}
		if path == "/api/servers/one/ssh-host-key" {
			body["host"] = "ignored.invalid"
		}
		request := hostKeyRequest(t, "POST", path, body, "admin", nil, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("probe: %d %s", response.Code, response.Body.String())
		}
		var result struct {
			Data struct {
				remote.SSHHostKey
				TrustedFingerprint string `json:"trusted_fingerprint"`
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) || result.Data.Port != saved.Port {
			t.Fatal("wrong fingerprint/endpoint")
		}
		if path == "/api/servers/one/ssh-host-key" && result.Data.TrustedFingerprint == "" {
			t.Fatal("previous pin missing from review")
		}
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "DO-NOT-RETURN") {
			t.Fatal("unsafe discovery response")
		}
	}
	unchanged, _ := servers.GetServer("one")
	if unchanged.TrustedHostKey != key {
		t.Fatal("discovery changed trust")
	}
	for _, path := range []string{"/api/servers/one/ssh-host-key", "/api/servers/ssh-host-key"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, hostKeyRequest(t, "POST", path, nil, "viewer", nil, []string{"one"}))
		if response.Code != 403 {
			t.Fatal("viewer allowed to probe")
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, hostKeyRequest(t, "POST", "/api/servers/ssh-host-key", map[string]any{"host": "host:22"}, "admin", nil, nil))
	if response.Code != 400 {
		t.Fatal("invalid discovery address accepted")
	}
}
