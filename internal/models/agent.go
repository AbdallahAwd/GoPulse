package models

import "time"

// AgentCredential stores a hash rather than a reusable bearer secret.
type AgentCredential struct {
	OwnerID     string    `json:"owner_id"`
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Owner       string    `json:"owner"`
	Mode        string    `json:"mode"`
	Permissions []string  `json:"permissions"`
	ServerIDs   []string  `json:"server_ids"`
	AllServers  bool      `json:"all_servers"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	RevokedAt   time.Time `json:"revoked_at"`
	Hash        string    `json:"-"`
}
