package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

var ErrDeploymentAlreadyExists = errors.New("deployment delivery already exists")

// DeploymentStore persists webhook delivery deduplication and deployment history.
type DeploymentStore struct {
	db *sql.DB
	mu sync.RWMutex
}

func NewDeploymentStore(legacyPath string) (*DeploymentStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}
	return &DeploymentStore{db: db}, nil
}

func (s *DeploymentStore) CreateDeployment(deployment models.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO deployments (
			id, project_id, delivery_id, source, repository, ref, branch,
			commit_sha, commit_message, pusher, status, queued_at, started_at,
			finished_at, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		deployment.ID,
		deployment.ProjectID,
		deployment.DeliveryID,
		deployment.Source,
		deployment.Repository,
		deployment.Ref,
		deployment.Branch,
		deployment.CommitSHA,
		deployment.CommitMessage,
		deployment.Pusher,
		string(deployment.Status),
		timeToText(deployment.QueuedAt),
		timeToText(deployment.StartedAt),
		timeToText(deployment.FinishedAt),
		deployment.Error,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: deployments.delivery_id") {
		return ErrDeploymentAlreadyExists
	}
	return err
}

func scanDeployment(scanner interface {
	Scan(dest ...interface{}) error
}) (models.Deployment, error) {
	var deployment models.Deployment
	var status, queuedAt, startedAt, finishedAt string
	err := scanner.Scan(
		&deployment.ID,
		&deployment.ProjectID,
		&deployment.DeliveryID,
		&deployment.Source,
		&deployment.Repository,
		&deployment.Ref,
		&deployment.Branch,
		&deployment.CommitSHA,
		&deployment.CommitMessage,
		&deployment.Pusher,
		&status,
		&queuedAt,
		&startedAt,
		&finishedAt,
		&deployment.Error,
	)
	if err != nil {
		return models.Deployment{}, err
	}
	deployment.Status = models.DeploymentStatus(status)
	deployment.QueuedAt = timeFromText(queuedAt)
	deployment.StartedAt = timeFromText(startedAt)
	deployment.FinishedAt = timeFromText(finishedAt)
	return deployment, nil
}

const deploymentSelectColumns = `id, project_id, delivery_id, source, repository, ref, branch,
	commit_sha, commit_message, pusher, status, queued_at, started_at, finished_at, error`

func (s *DeploymentStore) GetDeployment(id string) (models.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployment, err := scanDeployment(s.db.QueryRow(
		`SELECT `+deploymentSelectColumns+` FROM deployments WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Deployment{}, fmt.Errorf("deployment not found")
	}
	return deployment, err
}

func (s *DeploymentStore) GetDeploymentByDeliveryID(deliveryID string) (models.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployment, err := scanDeployment(s.db.QueryRow(
		`SELECT `+deploymentSelectColumns+` FROM deployments WHERE delivery_id = ?`, deliveryID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Deployment{}, fmt.Errorf("deployment not found")
	}
	return deployment, err
}

func (s *DeploymentStore) UpdateDeployment(deployment models.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`UPDATE deployments SET status = ?, started_at = ?, finished_at = ?, error = ? WHERE id = ?`,
		string(deployment.Status),
		timeToText(deployment.StartedAt),
		timeToText(deployment.FinishedAt),
		deployment.Error,
		deployment.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("deployment not found")
	}
	return nil
}

func (s *DeploymentStore) ListProjectDeployments(projectID string, limit int) ([]models.Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT `+deploymentSelectColumns+` FROM deployments WHERE project_id = ? ORDER BY queued_at DESC LIMIT ?`,
		projectID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deployments := make([]models.Deployment, 0)
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

// FailInterrupted marks work that could not survive a GoPulse restart.
func (s *DeploymentStore) FailInterrupted(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := timeToText(time.Now())
	_, err := s.db.Exec(
		`UPDATE deployments SET status = ?, finished_at = ?, error = ? WHERE status IN (?, ?)`,
		string(models.DeploymentStatusFailed),
		now,
		reason,
		string(models.DeploymentStatusQueued),
		string(models.DeploymentStatusRunning),
	)
	return err
}

// RecoverInterrupted marks work that was actively running as failed and
// returns queued work so the deployment manager can resume it after restart.
// Queued records are durable intents and are safe to execute again; running
// records may have changed the remote working tree and therefore require
// explicit failure/rollback handling instead of blind replay.
func (s *DeploymentStore) RecoverInterrupted(reason string) ([]models.Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT `+deploymentSelectColumns+` FROM deployments WHERE status = ? ORDER BY queued_at ASC`,
		string(models.DeploymentStatusQueued),
	)
	if err != nil {
		return nil, err
	}
	queued := make([]models.Deployment, 0)
	for rows.Next() {
		deployment, scanErr := scanDeployment(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		queued = append(queued, deployment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	_, err = s.db.Exec(
		`UPDATE deployments SET status = ?, finished_at = ?, error = ? WHERE status = ?`,
		string(models.DeploymentStatusFailed),
		timeToText(time.Now().UTC()),
		reason,
		string(models.DeploymentStatusRunning),
	)
	if err != nil {
		return nil, err
	}
	return queued, nil
}
