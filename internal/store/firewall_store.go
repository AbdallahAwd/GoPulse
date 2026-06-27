package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// FirewallStore handles SQL persistence for the firewall module tables
type FirewallStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewFirewallStore creates a SQLite-backed FirewallStore
func NewFirewallStore(legacyPath string) (*FirewallStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	return &FirewallStore{
		legacyPath: legacyPath,
		db:         db,
	}, nil
}

// -----------------------------------------------------------------------------
// FIREWALL RULES CRUDS
// -----------------------------------------------------------------------------

const ruleSelectColumns = `id, server_id, action, direction, protocol, port, source, interface, enabled, comment, created_at`

func scanRule(scanner interface {
	Scan(dest ...interface{}) error
}) (models.FirewallRule, error) {
	var rule models.FirewallRule
	var enabledInt int
	var createdAtRaw string

	err := scanner.Scan(
		&rule.ID,
		&rule.ServerID,
		&rule.Action,
		&rule.Direction,
		&rule.Protocol,
		&rule.Port,
		&rule.Source,
		&rule.Interface,
		&enabledInt,
		&rule.Comment,
		&createdAtRaw,
	)
	if err != nil {
		return models.FirewallRule{}, err
	}

	rule.Enabled = intToBool(enabledInt)
	rule.CreatedAt = timeFromText(createdAtRaw)
	return rule, nil
}

// AddRule inserts a new firewall rule
func (s *FirewallStore) AddRule(rule models.FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM firewall_rules WHERE id = ?`, rule.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("rule with ID %s already exists", rule.ID)
	}

	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO firewall_rules (
			id, server_id, action, direction, protocol, port, source, interface, enabled, comment, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID,
		rule.ServerID,
		rule.Action,
		rule.Direction,
		rule.Protocol,
		rule.Port,
		rule.Source,
		rule.Interface,
		boolToInt(rule.Enabled),
		rule.Comment,
		timeToText(rule.CreatedAt),
	)
	return err
}

// GetRule fetches a specific rule by ID
func (s *FirewallStore) GetRule(id string) (models.FirewallRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT `+ruleSelectColumns+` FROM firewall_rules WHERE id = ?`, id)
	rule, err := scanRule(row)
	if err == sql.ErrNoRows {
		return models.FirewallRule{}, fmt.Errorf("rule not found")
	}
	return rule, err
}

// UpdateRule updates a firewall rule's configuration
func (s *FirewallStore) UpdateRule(rule models.FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rule.CreatedAt.IsZero() {
		var existingCreatedAt string
		err := s.db.QueryRow(`SELECT created_at FROM firewall_rules WHERE id = ?`, rule.ID).Scan(&existingCreatedAt)
		if err == sql.ErrNoRows {
			return fmt.Errorf("rule not found")
		}
		if err != nil {
			return err
		}
		rule.CreatedAt = timeFromText(existingCreatedAt)
	}

	result, err := s.db.Exec(
		`UPDATE firewall_rules SET
			server_id = ?, action = ?, direction = ?, protocol = ?, port = ?, source = ?, interface = ?, enabled = ?, comment = ?, created_at = ?
		WHERE id = ?`,
		rule.ServerID,
		rule.Action,
		rule.Direction,
		rule.Protocol,
		rule.Port,
		rule.Source,
		rule.Interface,
		boolToInt(rule.Enabled),
		rule.Comment,
		timeToText(rule.CreatedAt),
		rule.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// DeleteRule removes a rule from the database
func (s *FirewallStore) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM firewall_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// ListRules lists all rules for a given server ID
func (s *FirewallStore) ListRules(serverID string) ([]models.FirewallRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT `+ruleSelectColumns+` FROM firewall_rules WHERE server_id = ? ORDER BY rowid`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.FirewallRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err == nil {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// DeleteAllRules removes all rules for a server ID (used on Reset)
func (s *FirewallStore) DeleteAllRules(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM firewall_rules WHERE server_id = ?`, serverID)
	return err
}

// -----------------------------------------------------------------------------
// FIREWALL BACKUPS CRUDS
// -----------------------------------------------------------------------------

const backupSelectColumns = `id, server_id, name, rules_json, created_at`

func scanBackup(scanner interface {
	Scan(dest ...interface{}) error
}) (models.FirewallBackup, error) {
	var backup models.FirewallBackup
	var createdAtRaw string

	err := scanner.Scan(
		&backup.ID,
		&backup.ServerID,
		&backup.Name,
		&backup.RulesJSON,
		&createdAtRaw,
	)
	if err != nil {
		return models.FirewallBackup{}, err
	}

	backup.CreatedAt = timeFromText(createdAtRaw)
	return backup, nil
}

// AddBackup stores a rules backup
func (s *FirewallStore) AddBackup(backup models.FirewallBackup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if backup.CreatedAt.IsZero() {
		backup.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO firewall_backups (id, server_id, name, rules_json, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		backup.ID,
		backup.ServerID,
		backup.Name,
		backup.RulesJSON,
		timeToText(backup.CreatedAt),
	)
	return err
}

// GetBackup retrieves a backup by ID
func (s *FirewallStore) GetBackup(id string) (models.FirewallBackup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT `+backupSelectColumns+` FROM firewall_backups WHERE id = ?`, id)
	backup, err := scanBackup(row)
	if err == sql.ErrNoRows {
		return models.FirewallBackup{}, fmt.Errorf("backup not found")
	}
	return backup, err
}

// DeleteBackup deletes a backup by ID
func (s *FirewallStore) DeleteBackup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM firewall_backups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("backup not found")
	}
	return nil
}

// ListBackups list all backups for a server ID
func (s *FirewallStore) ListBackups(serverID string) ([]models.FirewallBackup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT `+backupSelectColumns+` FROM firewall_backups WHERE server_id = ? ORDER BY created_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []models.FirewallBackup
	for rows.Next() {
		backup, err := scanBackup(rows)
		if err == nil {
			backups = append(backups, backup)
		}
	}
	return backups, nil
}

// -----------------------------------------------------------------------------
// FIREWALL SCHEDULES CRUDS
// -----------------------------------------------------------------------------

const scheduleSelectColumns = `id, server_id, rule_data, action_type, scheduled_at, executed, status, error_message, created_at`

func scanSchedule(scanner interface {
	Scan(dest ...interface{}) error
}) (models.FirewallSchedule, error) {
	var sched models.FirewallSchedule
	var executedInt int
	var scheduledAtRaw, createdAtRaw string

	err := scanner.Scan(
		&sched.ID,
		&sched.ServerID,
		&sched.RuleData,
		&sched.ActionType,
		&scheduledAtRaw,
		&executedInt,
		&sched.Status,
		&sched.ErrorMessage,
		&createdAtRaw,
	)
	if err != nil {
		return models.FirewallSchedule{}, err
	}

	sched.Executed = intToBool(executedInt)
	sched.ScheduledAt = timeFromText(scheduledAtRaw)
	sched.CreatedAt = timeFromText(createdAtRaw)
	return sched, nil
}

// AddSchedule adds a scheduled job
func (s *FirewallStore) AddSchedule(sched models.FirewallSchedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sched.CreatedAt.IsZero() {
		sched.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO firewall_schedules (id, server_id, rule_data, action_type, scheduled_at, executed, status, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sched.ID,
		sched.ServerID,
		sched.RuleData,
		sched.ActionType,
		timeToText(sched.ScheduledAt),
		boolToInt(sched.Executed),
		sched.Status,
		sched.ErrorMessage,
		timeToText(sched.CreatedAt),
	)
	return err
}

// GetSchedule gets a schedule by ID
func (s *FirewallStore) GetSchedule(id string) (models.FirewallSchedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT `+scheduleSelectColumns+` FROM firewall_schedules WHERE id = ?`, id)
	sched, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return models.FirewallSchedule{}, fmt.Errorf("schedule not found")
	}
	return sched, err
}

// UpdateSchedule updates an existing schedule
func (s *FirewallStore) UpdateSchedule(sched models.FirewallSchedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`UPDATE firewall_schedules SET
			server_id = ?, rule_data = ?, action_type = ?, scheduled_at = ?, executed = ?, status = ?, error_message = ?, created_at = ?
		WHERE id = ?`,
		sched.ServerID,
		sched.RuleData,
		sched.ActionType,
		timeToText(sched.ScheduledAt),
		boolToInt(sched.Executed),
		sched.Status,
		sched.ErrorMessage,
		timeToText(sched.CreatedAt),
		sched.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("schedule not found")
	}
	return nil
}

// DeleteSchedule deletes a schedule
func (s *FirewallStore) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM firewall_schedules WHERE id = ?`, id)
	return err
}

// ListSchedules list all schedules for a server
func (s *FirewallStore) ListSchedules(serverID string) ([]models.FirewallSchedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT `+scheduleSelectColumns+` FROM firewall_schedules WHERE server_id = ? ORDER BY scheduled_at ASC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []models.FirewallSchedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err == nil {
			schedules = append(schedules, sched)
		}
	}
	return schedules, nil
}

// ListPendingSchedules returns all unexecuted schedules whose schedule time has passed
func (s *FirewallStore) ListPendingSchedules() ([]models.FirewallSchedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	rows, err := s.db.Query(`SELECT `+scheduleSelectColumns+` FROM firewall_schedules WHERE executed = 0 AND status = 'pending' ORDER BY scheduled_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []models.FirewallSchedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err == nil {
			// Compare scheduled time
			if sched.ScheduledAt.Before(now) || sched.ScheduledAt.Equal(now) {
				schedules = append(schedules, sched)
			}
		}
	}
	return schedules, nil
}

// -----------------------------------------------------------------------------
// FIREWALL ACTIVITY LOGS CRUDS
// -----------------------------------------------------------------------------

const logSelectColumns = `id, server_id, action, details, timestamp`

func scanLog(scanner interface {
	Scan(dest ...interface{}) error
}) (models.FirewallActivityLog, error) {
	var act models.FirewallActivityLog
	var timestampRaw string

	err := scanner.Scan(
		&act.ID,
		&act.ServerID,
		&act.Action,
		&act.Details,
		&timestampRaw,
	)
	if err != nil {
		return models.FirewallActivityLog{}, err
	}

	act.Timestamp = timeFromText(timestampRaw)
	return act, nil
}

// AddActivityLog appends a new action log entry
func (s *FirewallStore) AddActivityLog(act models.FirewallActivityLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if act.Timestamp.IsZero() {
		act.Timestamp = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO firewall_activity_logs (id, server_id, action, details, timestamp)
		VALUES (?, ?, ?, ?, ?)`,
		act.ID,
		act.ServerID,
		act.Action,
		act.Details,
		timeToText(act.Timestamp),
	)
	return err
}

// ListActivityLogs list logs for a server
func (s *FirewallStore) ListActivityLogs(serverID string) ([]models.FirewallActivityLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT `+logSelectColumns+` FROM firewall_activity_logs WHERE server_id = ? ORDER BY timestamp DESC LIMIT 100`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.FirewallActivityLog
	for rows.Next() {
		act, err := scanLog(rows)
		if err == nil {
			logs = append(logs, act)
		}
	}
	return logs, nil
}
