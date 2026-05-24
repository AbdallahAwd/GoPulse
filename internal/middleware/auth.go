package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// AuthMiddleware provides JWT-based authentication
type AuthMiddleware struct {
	jwtSecret string
	adminUser string
	adminPass string
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(jwtSecret, adminUser, adminPass string) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret: jwtSecret,
		adminUser: adminUser,
		adminPass: adminPass,
	}
}

// Handler returns the authentication middleware handler
func (am *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try JWT first
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(am.jwtSecret), nil
			})

			if err == nil && token.Valid {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					ctx := context.WithValue(r.Context(), UserContextKey, claims["username"])
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		// Fall back to Basic Auth
		if strings.HasPrefix(authHeader, "Basic ") {
			encoded := strings.TrimPrefix(authHeader, "Basic ")
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 && parts[0] == am.adminUser && parts[1] == am.adminPass {
					ctx := context.WithValue(r.Context(), UserContextKey, parts[0])
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="GoPulse"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// GenerateToken generates a JWT token for a user
func (am *AuthMiddleware) GenerateToken(username string, duration time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(duration).Unix(),
		"iat":      time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(am.jwtSecret))
}

// LoginHandler handles login requests and returns a JWT token
func (am *AuthMiddleware) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username != am.adminUser || password != am.adminPass {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := am.GenerateToken(username, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"token":"%s","username":"%s"}`, token, username)
}
