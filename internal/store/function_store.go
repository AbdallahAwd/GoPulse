package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

var ErrFunctionNotFound = errors.New("function, version or invocation not found")
var ErrFunctionBusy = errors.New("function is busy, has pending cleanup, or reached its concurrency limit")

type FunctionStore struct{ db *sql.DB }

func NewFunctionStore(path string) (*FunctionStore, error) {
	db, err := openSQLiteStore(path)
	return &FunctionStore{db}, err
}
func (s *FunctionStore) Save(f models.Function, create bool) error {
	if create {
		_, err := s.db.Exec(`INSERT INTO functions(id,server_id,data) VALUES(?,?,?)`, f.ID, f.ServerID, encodeJSON(f, "{}"))
		return err
	}
	r, err := s.db.Exec(`UPDATE functions SET data=? WHERE id=? AND server_id=?`, encodeJSON(f, "{}"), f.ID, f.ServerID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrFunctionNotFound
	}
	return nil
}
func (s *FunctionStore) Get(server, id string) (models.Function, error) {
	var f models.Function
	var raw string
	err := s.db.QueryRow(`SELECT data FROM functions WHERE id=? AND server_id=?`, id, server).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrFunctionNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &f)
	}
	return f, err
}
func (s *FunctionStore) List(server string) ([]models.Function, error) {
	rows, err := s.db.Query(`SELECT data FROM functions WHERE (?='' OR server_id=?) ORDER BY rowid DESC`, server, server)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Function{}
	for rows.Next() {
		var raw string
		var f models.Function
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Claim enforces overlap and cleanup protection in the same SQLite transaction.
func (s *FunctionStore) Claim(f models.Function, r models.FunctionRun) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count, deploys, pending int
	if err = tx.QueryRow(`SELECT COUNT(*),COALESCE(SUM(kind='deploy'),0),COALESCE(SUM(needs_cleanup=1 AND status!='running'),0) FROM function_runs WHERE function_id=? AND (status='running' OR needs_cleanup=1)`, f.ID).Scan(&count, &deploys, &pending); err != nil {
		return err
	}
	if pending > 0 || (r.Kind == "deploy" && deploys > 0) || (r.Kind == "invoke" && count-deploys >= f.Deployed.Spec.Concurrency) {
		return ErrFunctionBusy
	}
	_, err = tx.Exec(`INSERT INTO function_runs(id,function_id,server_id,kind,status,needs_cleanup,data) VALUES(?,?,?,?,?,?,?)`, r.ID, f.ID, f.ServerID, r.Kind, r.Status, boolToInt(r.NeedsCleanup), encodeJSON(r, "{}"))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *FunctionStore) UpdateRun(r models.FunctionRun) error {
	_, err := s.db.Exec(`UPDATE function_runs SET status=?,needs_cleanup=?,data=? WHERE id=? AND function_id=?`, r.Status, boolToInt(r.NeedsCleanup), encodeJSON(r, "{}"), r.ID, r.FunctionID)
	return err
}
func (s *FunctionStore) UpdateLogs(id, logs string, truncated bool) error {
	_, err := s.db.Exec(`UPDATE function_runs SET data=json_set(data,'$.logs',?,'$.logs_truncated',json(?)) WHERE id=? AND status='running'`, logs, encodeJSON(truncated, "false"), id)
	return err
}
func (s *FunctionStore) Runs(id string) ([]models.FunctionRun, error) {
	return s.runs(`SELECT data FROM function_runs WHERE function_id=? ORDER BY rowid DESC LIMIT 100`, id)
}
func (s *FunctionStore) Pending() ([]models.FunctionRun, error) {
	return s.runs(`SELECT data FROM function_runs WHERE status='running' OR needs_cleanup=1`)
}
func (s *FunctionStore) runs(q string, args ...interface{}) ([]models.FunctionRun, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.FunctionRun{}
	for rows.Next() {
		var raw string
		var r models.FunctionRun
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *FunctionStore) Run(function, id string) (models.FunctionRun, error) {
	rs, err := s.runs(`SELECT data FROM function_runs WHERE function_id=? AND id=?`, function, id)
	if err != nil {
		return models.FunctionRun{}, err
	}
	if len(rs) == 0 {
		return models.FunctionRun{}, ErrFunctionNotFound
	}
	return rs[0], nil
}
func (s *FunctionStore) Activate(f models.Function, v models.FunctionVersion) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO function_versions(id,function_id,data) VALUES(?,?,?)`, v.ID, f.ID, encodeJSON(v, "{}")); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE functions SET data=? WHERE id=?`, encodeJSON(f, "{}"), f.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *FunctionStore) Versions(id string) ([]models.FunctionVersion, error) {
	rows, err := s.db.Query(`SELECT data FROM function_versions WHERE function_id=? ORDER BY rowid DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.FunctionVersion{}
	for rows.Next() {
		var raw string
		var v models.FunctionVersion
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *FunctionStore) Delete(server, id string) error {
	r, err := s.db.Exec(`DELETE FROM functions WHERE id=? AND server_id=? AND NOT EXISTS(SELECT 1 FROM function_runs WHERE function_id=? AND (status='running' OR needs_cleanup=1))`, id, server, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrFunctionBusy
	}
	return nil
}
func (s *FunctionStore) Prune(now time.Time) error {
	// Preserve active and uncertain remote resources regardless of age/count.
	_, err := s.db.Exec(`DELETE FROM function_runs WHERE status!='running' AND needs_cleanup=0 AND (json_extract(data,'$.started_at')<? OR id NOT IN (SELECT r.id FROM function_runs r WHERE r.function_id=function_runs.function_id ORDER BY r.rowid DESC LIMIT 100))`, now.Add(-30*24*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM function_versions WHERE id NOT IN (SELECT json_extract(data,'$.deployed.id') FROM functions WHERE json_extract(data,'$.deployed.id') IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM function_runs r WHERE json_extract(r.data,'$.version_id')=function_versions.id) AND id NOT IN (SELECT v.id FROM function_versions v WHERE v.function_id=function_versions.function_id ORDER BY v.rowid DESC LIMIT 20)`)
	return err
}
func (s *ServerStore) CheckFunctionDeletion(id string) error {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM functions WHERE server_id=?`, id).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("remove serverless functions before removing this server")
	}
	return nil
}

func (s *FunctionStore) SetTokenHash(server, id, hash string) error {
	_, e := s.db.Exec(`UPDATE functions SET token_hash=? WHERE id=? AND server_id=?`, hash, id, server)
	return e
}
func (s *FunctionStore) TokenHash(server, id string) (string, error) {
	var hash string
	e := s.db.QueryRow(`SELECT token_hash FROM functions WHERE id=? AND server_id=?`, id, server).Scan(&hash)
	return hash, e
}
func (s *FunctionStore) RecordSkipped(r models.FunctionRun) error {
	_, e := s.db.Exec(`INSERT INTO function_runs(id,function_id,server_id,kind,status,needs_cleanup,data) VALUES(?,?,?,?,?,0,?)`, r.ID, r.FunctionID, r.ServerID, r.Kind, r.Status, encodeJSON(r, "{}"))
	return e
}
