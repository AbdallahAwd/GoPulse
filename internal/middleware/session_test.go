package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginHonorsConfiguredSessionDurationAndSecureCookie(t *testing.T) {
	users, err := store.NewUserStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.AddUser(models.User{Username: "admin", Password: string(hash)}); err != nil {
		t.Fatal(err)
	}
	am := NewAuthMiddleware("test-jwt-signing-secret-with-32-characters", users)
	am.SetSessionOptions(2*time.Hour, true)
	r := httptest.NewRequest("POST", "http://localhost/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	am.LoginHandler(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || time.Until(cookies[0].Expires) > 2*time.Hour || time.Until(cookies[0].Expires) < 119*time.Minute {
		t.Fatalf("invalid configured session cookie: %+v", cookies)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Parse(body.Token, func(*jwt.Token) (interface{}, error) { return []byte(am.jwtSecret), nil })
	if err != nil {
		t.Fatal(err)
	}
	expiry, err := token.Claims.GetExpirationTime()
	if err != nil || expiry == nil || time.Until(expiry.Time) < 119*time.Minute || time.Until(expiry.Time) > 2*time.Hour {
		t.Fatal("token did not honor configured duration")
	}
}
