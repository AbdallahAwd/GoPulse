package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

var ErrRemoteBackupBusy = errors.New("a backup or restore test is already running for this plan")
var ErrRemoteBackupNotFound = errors.New("remote backup plan, run or artifact not found")

type RemoteBackupStore struct{ db *sql.DB }

func NewRemoteBackupStore(path string) (*RemoteBackupStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &RemoteBackupStore{db}, nil
}
func (s *RemoteBackupStore) Save(plan models.RemoteBackupPlan, create bool) error {
	plan.LastRun = nil
	if create {
		_, err := s.db.Exec(`INSERT INTO remote_backup_plans(id,server_id,database_id,destination_id,enabled,next_run_at,data) VALUES(?,?,?,?,?,?,?)`, plan.ID, plan.ServerID, plan.DatabaseID, plan.DestinationID, boolToInt(plan.Enabled), plan.NextRunAt.Unix(), encodeJSON(plan, "{}"))
		return err
	}
	result, err := s.db.Exec(`UPDATE remote_backup_plans SET enabled=?,next_run_at=?,data=? WHERE id=? AND server_id=? AND NOT EXISTS(SELECT 1 FROM remote_backup_runs WHERE plan_id=? AND status='running')`, boolToInt(plan.Enabled), plan.NextRunAt.Unix(), encodeJSON(plan, "{}"), plan.ID, plan.ServerID, plan.ID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrRemoteBackupBusy
	}
	return nil
}
func (s *RemoteBackupStore) Get(serverID, id string) (models.RemoteBackupPlan, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM remote_backup_plans WHERE id=? AND server_id=?`, id, serverID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRemoteBackupNotFound
	}
	var plan models.RemoteBackupPlan
	if err == nil {
		err = json.Unmarshal([]byte(raw), &plan)
	}
	return plan, err
}
func (s *RemoteBackupStore) Plans(serverID string) ([]models.RemoteBackupPlan, error) {
	rows, err := s.db.Query(`SELECT p.data,COALESCE((SELECT r.data FROM remote_backup_runs r WHERE r.plan_id=p.id ORDER BY (r.status='running') DESC,r.rowid DESC LIMIT 1),'') FROM remote_backup_plans p WHERE (?='' OR p.server_id=?) ORDER BY p.rowid DESC`, serverID, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.RemoteBackupPlan{}
	for rows.Next() {
		var raw, last string
		var plan models.RemoteBackupPlan
		if err = rows.Scan(&raw, &last); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &plan); err != nil {
			return nil, err
		}
		if last != "" {
			plan.LastRun = &models.RemoteBackupRun{}
			if err = json.Unmarshal([]byte(last), plan.LastRun); err != nil {
				return nil, err
			}
		}
		result = append(result, plan)
	}
	return result, rows.Err()
}

// Claim atomically advances a due schedule and records an invocation. Missed
// capacity/overlap slots are recorded and skipped rather than replayed later.
func (s *RemoteBackupStore) Claim(plan models.RemoteBackupPlan, run models.RemoteBackupRun, expected *time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if expected != nil {
		plan.LastRun = nil
		result, err := tx.Exec(`UPDATE remote_backup_plans SET next_run_at=?,data=? WHERE id=? AND enabled=1 AND next_run_at=?`, plan.NextRunAt.Unix(), encodeJSON(plan, "{}"), plan.ID, expected.Unix())
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return sql.ErrNoRows
		}
	}
	var active int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM remote_backup_runs WHERE plan_id=? AND status='running'`, plan.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		if expected == nil {
			return ErrRemoteBackupBusy
		}
		run.Status = "skipped"
		run.Phase = "skipped"
		run.NeedsCleanup = false
		run.Error = "Previous execution still active; schedule skipped."
		run.FinishedAt = run.StartedAt
	}
	if _, err = tx.Exec(`INSERT INTO remote_backup_runs(id,plan_id,status,needs_cleanup,data) VALUES(?,?,?,?,?)`, run.ID, run.PlanID, run.Status, boolToInt(run.NeedsCleanup), encodeJSON(run, "{}")); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *RemoteBackupStore) UpdateRun(run models.RemoteBackupRun) error {
	_, err := s.db.Exec(`UPDATE remote_backup_runs SET status=?,needs_cleanup=?,data=? WHERE id=? AND plan_id=?`, run.Status, boolToInt(run.NeedsCleanup), encodeJSON(run, "{}"), run.ID, run.PlanID)
	return err
}
func (s *RemoteBackupStore) Runs(planID string) ([]models.RemoteBackupRun, error) {
	return s.runs(`SELECT data FROM remote_backup_runs WHERE plan_id=? ORDER BY rowid DESC LIMIT 100`, planID)
}
func (s *RemoteBackupStore) CleanupPending() ([]models.RemoteBackupRun, error) {
	return s.runs(`SELECT data FROM remote_backup_runs WHERE needs_cleanup=1 AND status!='running' ORDER BY rowid LIMIT 100`)
}
func (s *RemoteBackupStore) ActiveRuns() ([]models.RemoteBackupRun, error) {
	return s.runs(`SELECT data FROM remote_backup_runs WHERE status='running'`)
}
func (s *RemoteBackupStore) Run(planID, id string) (models.RemoteBackupRun, error) {
	var raw string
	var run models.RemoteBackupRun
	err := s.db.QueryRow(`SELECT data FROM remote_backup_runs WHERE id=? AND plan_id=?`, id, planID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRemoteBackupNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &run)
	}
	return run, err
}
func (s *RemoteBackupStore) runs(query string, args ...interface{}) ([]models.RemoteBackupRun, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.RemoteBackupRun{}
	for rows.Next() {
		var raw string
		var run models.RemoteBackupRun
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &run); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}
func (s *RemoteBackupStore) SaveArtifact(a models.RemoteBackupArtifact, create bool) error {
	if create {
		_, err := s.db.Exec(`INSERT INTO remote_backup_artifacts(id,plan_id,pruned,created_at,data) VALUES(?,?,?,?,?)`, a.ID, a.PlanID, boolToInt(a.Pruned), a.CreatedAt.Unix(), encodeJSON(a, "{}"))
		return err
	}
	_, err := s.db.Exec(`UPDATE remote_backup_artifacts SET pruned=?,data=? WHERE id=? AND plan_id=?`, boolToInt(a.Pruned), encodeJSON(a, "{}"), a.ID, a.PlanID)
	return err
}
func (s *RemoteBackupStore) Artifact(planID, id string) (models.RemoteBackupArtifact, error) {
	var raw string
	var a models.RemoteBackupArtifact
	err := s.db.QueryRow(`SELECT data FROM remote_backup_artifacts WHERE id=? AND plan_id=?`, id, planID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRemoteBackupNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &a)
	}
	return a, err
}
func (s *RemoteBackupStore) Artifacts(planID string, all bool) ([]models.RemoteBackupArtifact, error) {
	rows, err := s.db.Query(`SELECT data FROM remote_backup_artifacts WHERE plan_id=? AND (? OR pruned=0) ORDER BY created_at DESC,rowid DESC LIMIT 100`, planID, all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.RemoteBackupArtifact{}
	for rows.Next() {
		var raw string
		var a models.RemoteBackupArtifact
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *RemoteBackupStore) ExpiredArtifacts(plan models.RemoteBackupPlan, now time.Time) ([]models.RemoteBackupArtifact, error) {
	rows, err := s.db.Query(`SELECT data FROM remote_backup_artifacts WHERE plan_id=? AND pruned=0 AND created_at<?
 AND id NOT IN (SELECT id FROM remote_backup_artifacts WHERE plan_id=? AND pruned=0 ORDER BY created_at DESC,rowid DESC LIMIT ?)
 AND id!=COALESCE((SELECT id FROM remote_backup_artifacts WHERE plan_id=? AND pruned=0 AND json_extract(data,'$.test_status')='passed' ORDER BY created_at DESC,rowid DESC LIMIT 1),'')
 ORDER BY created_at LIMIT 100`, plan.ID, now.Add(-time.Duration(plan.RetentionDays)*24*time.Hour).Unix(), plan.ID, plan.KeepLast, plan.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.RemoteBackupArtifact{}
	for rows.Next() {
		var raw string
		var a models.RemoteBackupArtifact
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// Database deletion and plan creation share this lock within the supported
// single control-plane process, including separate store instances.
var databaseLifecycleLocks sync.Map

func LockDatabaseLifecycle(id string) func() {
	value, _ := databaseLifecycleLocks.LoadOrStore(id, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

var ErrRemoteBackupDependency = errors.New("remote backup plans reference this resource; pause and remove those plans first, preserving archives and recovery keys")

func (s *DatabaseStore) CheckRemoteBackupDeletion(id string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM remote_backup_plans WHERE database_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrRemoteBackupDependency
	}
	return nil
}
func (s *ServerStore) CheckRemoteBackupDeletion(id string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM remote_backup_plans WHERE server_id=? OR destination_id=?`, id, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrRemoteBackupDependency
	}
	return nil
}
func (s *RemoteBackupStore) Delete(serverID, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled, active int
	if err = tx.QueryRow(`SELECT enabled FROM remote_backup_plans WHERE id=? AND server_id=?`, id, serverID).Scan(&enabled); errors.Is(err, sql.ErrNoRows) {
		return ErrRemoteBackupNotFound
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM remote_backup_runs WHERE plan_id=? AND (status='running' OR needs_cleanup=1)`, id).Scan(&active); err != nil {
		return err
	}
	if enabled != 0 || active != 0 {
		return ErrRemoteBackupBusy
	}
	for _, query := range []string{`DELETE FROM remote_backup_artifacts WHERE plan_id=?`, `DELETE FROM remote_backup_runs WHERE plan_id=?`, `DELETE FROM remote_backup_plans WHERE id=?`} {
		if _, err = tx.Exec(query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
