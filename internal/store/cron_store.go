package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

var ErrCronBusy = errors.New("job is already running")

type CronStore struct{ db *sql.DB }

func NewCronStore(path string) (*CronStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &CronStore{db}, nil
}

func (s *CronStore) Create(job models.CronJob) error {
	job.LastRun = nil
	_, err := s.db.Exec(`INSERT INTO cron_jobs(id,server_id,enabled,next_run_at,data) VALUES(?,?,?,?,?)`, job.ID, job.ServerID, boolToInt(job.Enabled), job.NextRunAt.Unix(), encodeJSON(job, "{}"))
	return err
}

func (s *CronStore) Update(job models.CronJob) error {
	job.LastRun = nil
	result, err := s.db.Exec(`UPDATE cron_jobs SET enabled=?,next_run_at=?,data=? WHERE id=? AND server_id=?`, boolToInt(job.Enabled), job.NextRunAt.Unix(), encodeJSON(job, "{}"), job.ID, job.ServerID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *CronStore) Get(serverID, id string) (models.CronJob, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM cron_jobs WHERE id=? AND server_id=?`, id, serverID).Scan(&raw)
	var job models.CronJob
	if err == nil {
		err = json.Unmarshal([]byte(raw), &job)
	}
	return job, err
}

// Due uses the scheduling index and omits run history from the dispatch loop.
func (s *CronStore) Due(now time.Time) ([]models.CronJob, error) {
	rows, err := s.db.Query(`SELECT data FROM cron_jobs WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at LIMIT 500`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []models.CronJob{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var job models.CronJob
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *CronStore) List(serverID string) ([]models.CronJob, error) {
	rows, err := s.db.Query(`SELECT j.data, COALESCE((SELECT json_remove(r.data,'$.output') FROM cron_runs r WHERE r.job_id=j.id ORDER BY (r.status='running') DESC,r.rowid DESC LIMIT 1),'') FROM cron_jobs j WHERE (?='' OR j.server_id=?) ORDER BY j.rowid DESC`, serverID, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []models.CronJob{}
	for rows.Next() {
		var raw, last string
		if err := rows.Scan(&raw, &last); err != nil {
			return nil, err
		}
		var job models.CronJob
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return nil, err
		}
		if last != "" {
			job.LastRun = &models.CronRun{}
			if err := json.Unmarshal([]byte(last), job.LastRun); err != nil {
				return nil, err
			}
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *CronStore) Delete(serverID, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cron_runs WHERE job_id=? AND status='running'`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrCronBusy
	}
	result, err := tx.Exec(`DELETE FROM cron_jobs WHERE id=? AND server_id=?`, id, serverID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// Claim atomically records an invocation and advances the schedule. The expected
// timestamp prevents two scheduler ticks/processes from claiming the same slot.
func (s *CronStore) Claim(job models.CronJob, run models.CronRun, scheduled bool, expected time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if scheduled {
		job.LastRun = nil
		result, err := tx.Exec(`UPDATE cron_jobs SET next_run_at=?,data=? WHERE id=? AND enabled=1 AND next_run_at=?`, job.NextRunAt.Unix(), encodeJSON(job, "{}"), job.ID, expected.Unix())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
	}
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cron_runs WHERE job_id=? AND status='running'`, job.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		if !scheduled {
			return ErrCronBusy
		}
		run.Status, run.Error = "skipped", "Previous run is still active; overlapping execution was skipped."
		run.FinishedAt = run.StartedAt
	}
	if _, err := tx.Exec(`INSERT INTO cron_runs(id,job_id,server_id,status,data) VALUES(?,?,?,?,?)`, run.ID, run.JobID, run.ServerID, run.Status, encodeJSON(run, "{}")); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *CronStore) Finish(run models.CronRun) error {
	_, err := s.db.Exec(`UPDATE cron_runs SET status=?,data=? WHERE id=? AND job_id=?`, run.Status, encodeJSON(run, "{}"), run.ID, run.JobID)
	if err != nil {
		return err
	}
	return s.Prune(run.JobID)
}

func (s *CronStore) Prune(jobID string) error {
	_, err := s.db.Exec(`DELETE FROM cron_runs WHERE job_id=? AND status!='running' AND id NOT IN (SELECT id FROM cron_runs WHERE job_id=? ORDER BY rowid DESC LIMIT 100)`, jobID, jobID)
	return err
}

func (s *CronStore) Runs(serverID, jobID string, limit, offset int) ([]models.CronRun, error) {
	rows, err := s.db.Query(`SELECT json_remove(data,'$.output') FROM cron_runs WHERE server_id=? AND job_id=? ORDER BY rowid DESC LIMIT ? OFFSET ?`, serverID, jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []models.CronRun{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var run models.CronRun
		if err := json.Unmarshal([]byte(raw), &run); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *CronStore) Run(serverID, jobID, runID string) (models.CronRun, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM cron_runs WHERE id=? AND job_id=? AND server_id=?`, runID, jobID, serverID).Scan(&raw)
	var run models.CronRun
	if err == nil {
		err = json.Unmarshal([]byte(raw), &run)
	}
	return run, err
}

// Recover does not retry commands whose remote outcome is unknown.
func (s *CronStore) Recover() error {
	rows, err := s.db.Query(`SELECT data FROM cron_runs WHERE status='running'`)
	if err != nil {
		return err
	}
	runs := []models.CronRun{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var run models.CronRun
		if err := json.Unmarshal([]byte(raw), &run); err != nil {
			rows.Close()
			return err
		}
		runs = append(runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, run := range runs {
		run.Status, run.Error = "interrupted", "GoPulse restarted. Verify the command's remote outcome before running again."
		run.FinishedAt = time.Now().UTC()
		run.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
		if err := s.Finish(run); err != nil {
			return err
		}
	}
	return nil
}
