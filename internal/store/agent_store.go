package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

type AgentStore struct{ db *sql.DB }

func NewAgentStore(path string) (*AgentStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &AgentStore{db}, nil
}
func agentHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
func (s *AgentStore) Create(credential models.AgentCredential) (models.AgentCredential, string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return credential, "", err
	}
	secret := "gp_agent_" + hex.EncodeToString(random[:])
	credential.ID = uuid.NewString()
	credential.CreatedAt = time.Now().UTC()
	credential.Hash = agentHash(secret)
	tx, err := s.db.Begin()
	if err != nil {
		return credential, "", err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM agent_credentials WHERE owner=? AND json_extract(data,'$.owner_id')=? AND json_extract(data,'$.revoked_at') LIKE '0001-%' AND julianday(json_extract(data,'$.expires_at'))>julianday('now')`, credential.Owner, credential.OwnerID).Scan(&active); err != nil {
		return credential, "", err
	}
	if active >= 25 {
		return credential, "", fmt.Errorf("maximum of 25 active agent credentials reached")
	}
	_, err = tx.Exec(`INSERT INTO agent_credentials(id,owner,token_hash,data) VALUES(?,?,?,?)`, credential.ID, credential.Owner, credential.Hash, encodeJSON(credential, "{}"))
	if err != nil {
		return credential, "", err
	}
	if err := tx.Commit(); err != nil {
		return credential, "", err
	}
	return credential, secret, nil
}
func (s *AgentStore) Get(id string) (models.AgentCredential, error) {
	var raw string
	var credential models.AgentCredential
	err := s.db.QueryRow(`SELECT data FROM agent_credentials WHERE id=?`, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &credential)
	}
	return credential, err
}
func (s *AgentStore) Authenticate(secret string) (models.AgentCredential, error) {
	var raw string
	var credential models.AgentCredential
	err := s.db.QueryRow(`SELECT data FROM agent_credentials WHERE token_hash=?`, agentHash(secret)).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &credential)
	}
	if err != nil || !credential.RevokedAt.IsZero() || !time.Now().Before(credential.ExpiresAt) {
		return models.AgentCredential{}, fmt.Errorf("agent credential invalid, expired, or revoked")
	}
	// At most one metadata write per minute per credential.
	if time.Since(credential.LastUsedAt) >= time.Minute {
		_, err = s.db.Exec(`UPDATE agent_credentials SET data=json_set(data,'$.last_used_at',?) WHERE id=?`, timeToText(time.Now()), credential.ID)
		if err != nil {
			return models.AgentCredential{}, err
		}
	}
	return credential, nil
}
func (s *AgentStore) List(owner string, ownerIDs ...string) ([]models.AgentCredential, error) {
	ownerID := ""
	if len(ownerIDs) > 0 {
		ownerID = ownerIDs[0]
	}
	rows, err := s.db.Query(`SELECT data FROM agent_credentials WHERE owner=? AND (?='' OR json_extract(data,'$.owner_id')=?) ORDER BY (json_extract(data,'$.revoked_at') LIKE '0001-%' AND julianday(json_extract(data,'$.expires_at'))>julianday('now')) DESC,rowid DESC LIMIT 100`, owner, ownerID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := []models.AgentCredential{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item models.AgentCredential
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		credentials = append(credentials, item)
	}
	return credentials, rows.Err()
}
func (s *AgentStore) Revoke(owner, id string, ownerIDs ...string) error {
	ownerID := ""
	if len(ownerIDs) > 0 {
		ownerID = ownerIDs[0]
	}
	result, err := s.db.Exec(`UPDATE agent_credentials SET data=json_set(data,'$.revoked_at',?) WHERE id=? AND owner=? AND (?='' OR json_extract(data,'$.owner_id')=?)`, timeToText(time.Now()), id, owner, ownerID, ownerID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
func (s *AgentStore) Audit(owner string, limit int) ([]map[string]interface{}, error) {
	// Match explicit ownership, or exact legacy actor identity. Never match
	// username prefixes; raw credentials and request arguments are not audited.
	rows, err := s.db.Query(`SELECT actor,tool,server_id,outcome,operation_id,created_at FROM tool_audit WHERE owner=? OR (owner='' AND actor=?) ORDER BY id DESC LIMIT ?`, owner, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var actor, tool, server, outcome, operation, at string
		if err := rows.Scan(&actor, &tool, &server, &outcome, &operation, &at); err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{"actor": actor, "tool": tool, "server_id": server, "outcome": outcome, "operation_id": operation, "created_at": at})
	}
	return items, rows.Err()
}
