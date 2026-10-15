package store

import (
	"database/sql"
	"encoding/json"
	"github.com/amaa/gopulse/internal/models"
	"time"
)

type OperationStore struct{ db *sql.DB }

func NewOperationStore(path string) (*OperationStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &OperationStore{db}, nil
}
func (s *OperationStore) Create(op models.Operation) error {
	_, err := s.db.Exec(`INSERT INTO operations(id,owner,resource,status,data) VALUES(?,?,?,?,?)`, op.ID, op.Owner, op.Resource, op.Status, encodeJSON(op, "{}"))
	return err
}
func (s *OperationStore) Save(op models.Operation) error {
	_, err := s.db.Exec(`UPDATE operations SET status=?,data=? WHERE id=?`, op.Status, encodeJSON(op, "{}"), op.ID)
	return err
}
func (s *OperationStore) Get(id string) (models.Operation, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM operations WHERE id=?`, id).Scan(&raw)
	var op models.Operation
	if err == nil {
		err = json.Unmarshal([]byte(raw), &op)
	}
	return op, err
}
func (s *OperationStore) List() ([]models.Operation, error) {
	rows, err := s.db.Query(`SELECT data FROM operations ORDER BY rowid DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ops := []models.Operation{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var op models.Operation
		if err := json.Unmarshal([]byte(raw), &op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}
func (s *OperationStore) Recover() error {
	rows, err := s.db.Query(`SELECT data FROM operations WHERE status IN ('running','awaiting_approval')`)
	if err != nil {
		return err
	}
	ops := []models.Operation{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var op models.Operation
		if err := json.Unmarshal([]byte(raw), &op); err != nil {
			rows.Close()
			return err
		}
		ops = append(ops, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, op := range ops {
		if op.Status == "running" || op.Status == "awaiting_approval" {
			op.Status = "interrupted"
			op.Error = "Application restarted; inspect remote state before retrying"
			op.UpdatedAt = time.Now()
			if err := s.Save(op); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *OperationStore) Audit(actor, tool, server, outcome, operation string) error {
	return s.AuditIdentity(actor, "", tool, server, outcome, operation)
}
func (s *OperationStore) AuditIdentity(owner, agentID, tool, server, outcome, operation string) error {
	actor := owner
	if agentID != "" {
		actor = owner + "|agent:" + agentID
	}
	_, err := s.db.Exec(`INSERT INTO tool_audit(actor,owner,agent_id,tool,server_id,outcome,operation_id,created_at) VALUES(?,?,?,?,?,?,?,?)`, actor, owner, agentID, tool, server, outcome, operation, timeToText(time.Now()))
	return err
}

func (s *DatabaseStore) SetQdrantConfig(id string, cfg models.QdrantConfig) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO resource_settings(id,data) VALUES(?,?)`, id, encodeJSON(cfg, "{}"))
	return err
}
func (s *DatabaseStore) GetQdrantConfig(id string) (models.QdrantConfig, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM resource_settings WHERE id=?`, id).Scan(&raw)
	var cfg models.QdrantConfig
	if err == nil {
		err = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg, err
}

func (s *OperationStore) GetByRequest(owner, key string) (models.Operation, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM operations WHERE owner=? AND json_extract(data,'$.request_key')=?`, owner, key).Scan(&raw)
	var op models.Operation
	if err == nil {
		err = json.Unmarshal([]byte(raw), &op)
	}
	return op, err
}
