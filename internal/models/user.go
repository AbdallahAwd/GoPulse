package models

import "time"

// User represents a dashboard user account
type User struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Password       string    `json:"password"` // Bcrypt hashed password
	Role           string    `json:"role"`     // "admin", "operator", "viewer"
	Permissions    []string  `json:"permissions"`
	AllowedServers []string  `json:"allowed_servers"`
	CreatedAt      time.Time `json:"created_at"`
}
