package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// ProjectStore handles storage and retrieval of project configurations.
type ProjectStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewProjectStore creates a SQLite-backed ProjectStore and imports legacy JSON once.
func NewProjectStore(legacyPath string) (*ProjectStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	store := &ProjectStore{
		legacyPath: legacyPath,
		db:         db,
	}

	err = importJSONOnce[models.Project](db, "projects", legacyPath, `SELECT COUNT(*) FROM projects`, insertProjectTx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func insertProjectTx(tx *sql.Tx, project models.Project) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO projects (
			id, server_id, name, type, source_type, source_url, build_type, port,
			host_port, status, path, container_id, process_id, domain, created_at,
			start_command, build_command, last_build, webhook_secret, webhook_enabled,
			last_deploy, env_vars, restart_on_boot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		project.ID,
		project.ServerID,
		project.Name,
		project.Type,
		project.SourceType,
		project.SourceURL,
		project.BuildType,
		project.Port,
		project.HostPort,
		string(project.Status),
		project.Path,
		project.ContainerID,
		project.ProcessID,
		project.Domain,
		timeToText(project.CreatedAt),
		project.StartCommand,
		project.BuildCommand,
		timeToText(project.LastBuild),
		project.WebhookSecret,
		boolToInt(project.WebhookEnabled),
		timeToText(project.LastDeploy),
		encodeJSON(project.EnvVars, "{}"),
		boolToInt(project.RestartOnBoot),
	)
	return err
}

func scanProject(scanner interface {
	Scan(dest ...interface{}) error
}) (models.Project, error) {
	var project models.Project
	var status string
	var createdAtRaw string
	var lastBuildRaw string
	var lastDeployRaw string
	var envVarsRaw string
	var webhookEnabled int
	var restartOnBoot int

	err := scanner.Scan(
		&project.ID,
		&project.ServerID,
		&project.Name,
		&project.Type,
		&project.SourceType,
		&project.SourceURL,
		&project.BuildType,
		&project.Port,
		&project.HostPort,
		&status,
		&project.Path,
		&project.ContainerID,
		&project.ProcessID,
		&project.Domain,
		&createdAtRaw,
		&project.StartCommand,
		&project.BuildCommand,
		&lastBuildRaw,
		&project.WebhookSecret,
		&webhookEnabled,
		&lastDeployRaw,
		&envVarsRaw,
		&restartOnBoot,
	)
	if err != nil {
		return models.Project{}, err
	}

	project.Status = models.ProjectStatus(status)
	project.CreatedAt = timeFromText(createdAtRaw)
	project.LastBuild = timeFromText(lastBuildRaw)
	project.LastDeploy = timeFromText(lastDeployRaw)
	project.WebhookEnabled = intToBool(webhookEnabled)
	project.RestartOnBoot = intToBool(restartOnBoot)
	if err := decodeJSON(envVarsRaw, &project.EnvVars); err != nil {
		return models.Project{}, err
	}
	return project, nil
}

const projectSelectColumns = `id, server_id, name, type, source_type, source_url, build_type, port,
	host_port, status, path, container_id, process_id, domain, created_at,
	start_command, build_command, last_build, webhook_secret, webhook_enabled,
	last_deploy, env_vars, restart_on_boot`

// AddProject adds a new project.
func (s *ProjectStore) AddProject(project models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = ?`, project.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("project with ID %s already exists", project.ID)
	}

	if project.CreatedAt.IsZero() {
		project.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT INTO projects (
			id, server_id, name, type, source_type, source_url, build_type, port,
			host_port, status, path, container_id, process_id, domain, created_at,
			start_command, build_command, last_build, webhook_secret, webhook_enabled,
			last_deploy, env_vars, restart_on_boot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		project.ID,
		project.ServerID,
		project.Name,
		project.Type,
		project.SourceType,
		project.SourceURL,
		project.BuildType,
		project.Port,
		project.HostPort,
		string(project.Status),
		project.Path,
		project.ContainerID,
		project.ProcessID,
		project.Domain,
		timeToText(project.CreatedAt),
		project.StartCommand,
		project.BuildCommand,
		timeToText(project.LastBuild),
		project.WebhookSecret,
		boolToInt(project.WebhookEnabled),
		timeToText(project.LastDeploy),
		encodeJSON(project.EnvVars, "{}"),
		boolToInt(project.RestartOnBoot),
	)
	return err
}

// RemoveProject removes a project by ID.
func (s *ProjectStore) RemoveProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}

// GetProject returns a project by ID.
func (s *ProjectStore) GetProject(id string) (models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT `+projectSelectColumns+` FROM projects WHERE id = ?`, id)
	project, err := scanProject(row)
	if err == sql.ErrNoRows {
		return models.Project{}, fmt.Errorf("project not found")
	}
	return project, err
}

// ListProjects returns all projects.
func (s *ProjectStore) ListProjects() []models.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT ` + projectSelectColumns + ` FROM projects ORDER BY rowid`)
	if err != nil {
		return []models.Project{}
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err == nil {
			projects = append(projects, project)
		}
	}
	return projects
}

// UpdateProject updates an existing project.
func (s *ProjectStore) UpdateProject(project models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if project.CreatedAt.IsZero() {
		var existingCreatedAt string
		err := s.db.QueryRow(`SELECT created_at FROM projects WHERE id = ?`, project.ID).Scan(&existingCreatedAt)
		if err == sql.ErrNoRows {
			return fmt.Errorf("project not found")
		}
		if err != nil {
			return err
		}
		project.CreatedAt = timeFromText(existingCreatedAt)
	}

	result, err := s.db.Exec(
		`UPDATE projects SET
			server_id = ?, name = ?, type = ?, source_type = ?, source_url = ?,
			build_type = ?, port = ?, host_port = ?, status = ?, path = ?,
			container_id = ?, process_id = ?, domain = ?, created_at = ?,
			start_command = ?, build_command = ?, last_build = ?, webhook_secret = ?,
			webhook_enabled = ?, last_deploy = ?, env_vars = ?, restart_on_boot = ?
		WHERE id = ?`,
		project.ServerID,
		project.Name,
		project.Type,
		project.SourceType,
		project.SourceURL,
		project.BuildType,
		project.Port,
		project.HostPort,
		string(project.Status),
		project.Path,
		project.ContainerID,
		project.ProcessID,
		project.Domain,
		timeToText(project.CreatedAt),
		project.StartCommand,
		project.BuildCommand,
		timeToText(project.LastBuild),
		project.WebhookSecret,
		boolToInt(project.WebhookEnabled),
		timeToText(project.LastDeploy),
		encodeJSON(project.EnvVars, "{}"),
		boolToInt(project.RestartOnBoot),
		project.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}
