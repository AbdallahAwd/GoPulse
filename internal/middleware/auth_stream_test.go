package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

func TestStreamAuthorizationReflectsPermissionRevocation(t *testing.T) {
	users, err := store.NewUserStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: "alice-id", Username: "alice", Role: "viewer", AllowedServers: []string{"a"}}
	if err := users.AddUserRaw(user); err != nil {
		t.Fatal(err)
	}
	auth := NewAuthMiddleware("isolated-test-secret-of-at-least-32-characters", users)
	token, err := auth.GenerateToken("alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var authorize func(string) bool
	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorize, _ = r.Context().Value(ServerAuthorizerContextKey).(func(string) bool)
	}))
	request := httptest.NewRequest("GET", "/api/stats/ws", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if authorize == nil || !authorize("a") || authorize("b") {
		t.Fatal("initial scope incorrect")
	}
	user.AllowedServers = nil
	if err := users.UpdateUser(user); err != nil {
		t.Fatal(err)
	}
	if authorize("a") {
		t.Fatal("stream retained revoked server access")
	}
}
