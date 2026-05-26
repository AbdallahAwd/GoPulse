package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const (
	UserContextKey           contextKey = "user"
	RoleContextKey           contextKey = "role"
	PermissionsContextKey    contextKey = "permissions"
	AllowedServersContextKey contextKey = "allowed_servers"
)

// AuthMiddleware provides JWT-based authentication
type AuthMiddleware struct {
	jwtSecret string
	userStore *store.UserStore
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(jwtSecret string, userStore *store.UserStore) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret: jwtSecret,
		userStore: userStore,
	}
}

// GetUserStore returns the user store
func (am *AuthMiddleware) GetUserStore() *store.UserStore {
	return am.userStore
}

// Handler returns the authentication middleware handler
func (am *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Always allow static files
		if strings.HasPrefix(path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		// Always allow favicon.ico or logo
		if path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if there are registered users
		hasUsers := am.userStore.HasUsers()

		// Determine auth status
		claims, isAuthenticated := am.authenticate(r)

		var username string
		var role string
		var permissions []string
		var allowedServers []string

		if isAuthenticated {
			if u, ok := claims["username"].(string); ok {
				username = u
			}
			if rl, ok := claims["role"].(string); ok {
				role = rl
			}
			if perms, ok := claims["permissions"].([]interface{}); ok {
				for _, p := range perms {
					if ps, ok := p.(string); ok {
						permissions = append(permissions, ps)
					}
				}
			}
			if srvs, ok := claims["allowed_servers"].([]interface{}); ok {
				for _, s := range srvs {
					if ss, ok := s.(string); ok {
						allowedServers = append(allowedServers, ss)
					}
				}
			}
		}

		// API routes authentication
		if strings.HasPrefix(path, "/api/") {
			// Exclude the registration, login and check endpoints from block checks
			if path == "/api/auth/register" || path == "/api/auth/login" || path == "/api/auth/has-users" {
				next.ServeHTTP(w, r)
				return
			}

			if !isAuthenticated {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "Unauthorized",
					"message": "Please log in to continue",
				})
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, username)
			ctx = context.WithValue(ctx, RoleContextKey, role)
			ctx = context.WithValue(ctx, PermissionsContextKey, permissions)
			ctx = context.WithValue(ctx, AllowedServersContextKey, allowedServers)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Web routes authentication & routing
		if !hasUsers {
			// Bootstrap mode: No users registered, redirect all non-register pages to /register
			if path == "/register" {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/register", http.StatusFound)
			return
		}

		// Users exist
		if !isAuthenticated {
			// Not logged in: allow access ONLY to /login
			if path == "/login" {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Logged in
		if path == "/login" || path == "/register" {
			// Already logged in, redirect away from auth pages to home dashboard
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, username)
		ctx = context.WithValue(ctx, RoleContextKey, role)
		ctx = context.WithValue(ctx, PermissionsContextKey, permissions)
		ctx = context.WithValue(ctx, AllowedServersContextKey, allowedServers)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate checks both Authorization Header (for API/Bearer) and JWT Cookie (for browser requests)
func (am *AuthMiddleware) authenticate(r *http.Request) (jwt.MapClaims, bool) {
	var tokenString string

	// 1. Try to read from Cookie first (seamless for browser pages)
	cookie, err := r.Cookie("jwt_token")
	if err == nil {
		tokenString = cookie.Value
	}

	// 2. Try Bearer header if no cookie found (fallback for API calls)
	if tokenString == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if tokenString == "" {
		return nil, false
	}

	// Parse and validate the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(am.jwtSecret), nil
	})

	if err == nil && token.Valid {
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			return claims, true
		}
	}

	return nil, false
}

// GenerateToken generates a JWT token for a user with extended roles and allowed servers scoping
func (am *AuthMiddleware) GenerateToken(username string, duration time.Duration) (string, error) {
	user, err := am.userStore.GetUser(username)
	role := "viewer"
	permissions := []string{}
	allowedServers := []string{}

	if err == nil {
		role = user.Role
		permissions = user.Permissions
		allowedServers = user.AllowedServers
	}

	claims := jwt.MapClaims{
		"username":        username,
		"role":            role,
		"permissions":     permissions,
		"allowed_servers": allowedServers,
		"exp":             time.Now().Add(duration).Unix(),
		"iat":             time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(am.jwtSecret))
}

// LoginHandler handles login requests, returns JWT in body AND sets a secure browser Cookie
func (am *AuthMiddleware) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	// Support both JSON body and standard Form body
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
	}

	user, err := am.userStore.GetUser(req.Username)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Compare bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := am.GenerateToken(req.Username, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Set browser cookie (seamless authentication for standard HTML page renders)
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in HTTPS production
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"token":    token,
		"username": req.Username,
	})
}

// RegisterHandler registers the first-time admin user
func (am *AuthMiddleware) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Registration is ONLY allowed if no user exists yet
	if am.userStore.HasUsers() {
		http.Error(w, "Registration is locked. Admin account already exists.", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	// Support both JSON body and standard Form body
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Hash password using bcrypt
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to secure password", http.StatusInternalServerError)
		return
	}

	user := models.User{
		Username: req.Username,
		Password: string(hashedBytes),
	}

	if err := am.userStore.AddUser(user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate and auto-login user immediately
	token, err := am.GenerateToken(req.Username, 24*time.Hour)
	if err != nil {
		http.Error(w, "Account created, but automatic login failed", http.StatusInternalServerError)
		return
	}

	// Set browser cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  "Admin account successfully registered",
		"token":    token,
		"username": req.Username,
	})
}

// LogoutHandler logs out the user by clearing the JWT cookie
func (am *AuthMiddleware) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0), // Expire instantly
		MaxAge:   -1,
	})

	// Redirect to login page
	http.Redirect(w, r, "/login", http.StatusFound)
}

// CheckUsersHandler returns if the system has users
func (am *AuthMiddleware) CheckUsersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"has_users": am.userStore.HasUsers(),
	})
}

// RequirePermission is a mid-tier middleware check for granular actions
func (am *AuthMiddleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(RoleContextKey).(string)
			permissions, _ := r.Context().Value(PermissionsContextKey).([]string)

			// Admins bypass all permissions checks
			if role == "admin" {
				next.ServeHTTP(w, r)
				return
			}

			// Check permission list
			hasPerm := false
			for _, p := range permissions {
				if p == "*" || p == permission {
					hasPerm = true
					break
				}
			}

			if !hasPerm {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   "Forbidden",
						"message": fmt.Sprintf("You do not have the required permission: %s", permission),
					})
					return
				}
				http.Error(w, "Forbidden - You do not have access to this resource.", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireServerAccess checks if a non-admin has access to the requested {serverID}
func (am *AuthMiddleware) RequireServerAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(RoleContextKey).(string)
		allowedServers, _ := r.Context().Value(AllowedServersContextKey).([]string)

		// Admins bypass server restriction checks
		if role == "admin" {
			next.ServeHTTP(w, r)
			return
		}

		serverID := chi.URLParam(r, "serverID")
		if serverID == "" {
			serverID = r.URL.Query().Get("server_id")
		}

		if serverID != "" {
			allowed := false
			for _, id := range allowedServers {
				if id == serverID {
					allowed = true
					break
				}
			}

			if !allowed {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   "Forbidden",
						"message": "You are not authorized to access this server.",
					})
					return
				}
				http.Error(w, "Forbidden - You are not authorized to access this server.", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// ListUsersHandler lists all users in the system (admin-only)
func (am *AuthMiddleware) ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	users := am.userStore.ListUsers()
	// Strip passwords for safety
	for i := range users {
		users[i].Password = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    users,
	})
}

// CreateUserHandler adds a new user with bcrypt encryption (admin-only)
func (am *AuthMiddleware) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username       string   `json:"username"`
		Password       string   `json:"password"`
		Role           string   `json:"role"`
		Permissions    []string `json:"permissions"`
		AllowedServers []string `json:"allowed_servers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to secure password", http.StatusInternalServerError)
		return
	}

	user := models.User{
		Username:       req.Username,
		Password:       string(hashedBytes),
		Role:           req.Role,
		Permissions:    req.Permissions,
		AllowedServers: req.AllowedServers,
	}

	if err := am.userStore.AddUser(user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "User successfully created",
	})
}

// DeleteUserHandler deletes a user by ID (admin-only)
func (am *AuthMiddleware) DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	// Prevent self-deletion
	currentUsername, _ := r.Context().Value(UserContextKey).(string)
	users := am.userStore.ListUsers()
	for _, u := range users {
		if u.ID == id {
			if u.Username == currentUsername {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "Forbidden",
					"message": "You cannot delete your own active administrator session.",
				})
				return
			}
			break
		}
	}

	if err := am.userStore.DeleteUserByID(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "User successfully deleted",
	})
}
