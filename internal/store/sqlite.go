package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var sqliteRegistry = struct {
	sync.Mutex
	dbs map[string]*sql.DB
}{
	dbs: make(map[string]*sql.DB),
}

func sqlitePathForLegacyJSON(legacyPath string) string {
	return filepath.Join(filepath.Dir(legacyPath), "gopulse.db")
}

func openSQLiteStore(legacyPath string) (*sql.DB, error) {
	dbPath := sqlitePathForLegacyJSON(legacyPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, err
	}

	sqliteRegistry.Lock()
	defer sqliteRegistry.Unlock()

	if db := sqliteRegistry.dbs[dbPath]; db != nil {
		return db, nil
	}

	file, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dbPath, 0600); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to apply %s: %w", pragma, err)
		}
	}

	if err := migrateSQLiteSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	sqliteRegistry.dbs[dbPath] = db
	return db, nil
}

func migrateSQLiteSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS functions (id TEXT PRIMARY KEY,server_id TEXT NOT NULL,token_hash TEXT NOT NULL DEFAULT '',data TEXT NOT NULL,FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE RESTRICT)`,
		`CREATE INDEX IF NOT EXISTS idx_functions_server ON functions(server_id)`,
		`CREATE TABLE IF NOT EXISTS function_versions (id TEXT PRIMARY KEY,function_id TEXT NOT NULL,data TEXT NOT NULL,FOREIGN KEY(function_id) REFERENCES functions(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_function_versions ON function_versions(function_id)`,
		`CREATE TABLE IF NOT EXISTS function_runs (id TEXT PRIMARY KEY,function_id TEXT NOT NULL,server_id TEXT NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL,needs_cleanup INTEGER NOT NULL,data TEXT NOT NULL,FOREIGN KEY(function_id) REFERENCES functions(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_function_runs ON function_runs(function_id,status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_function_deploy ON function_runs(function_id) WHERE status='running' AND kind='deploy'`,

		`CREATE TABLE IF NOT EXISTS remote_backup_plans (id TEXT PRIMARY KEY,server_id TEXT NOT NULL,database_id TEXT NOT NULL,destination_id TEXT NOT NULL,enabled INTEGER NOT NULL,next_run_at INTEGER NOT NULL,data TEXT NOT NULL,FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE RESTRICT,FOREIGN KEY(destination_id) REFERENCES servers(id) ON DELETE RESTRICT,FOREIGN KEY(database_id) REFERENCES databases(id) ON DELETE RESTRICT)`,
		`CREATE INDEX IF NOT EXISTS idx_remote_backup_plans_due ON remote_backup_plans(enabled,next_run_at)`,
		`CREATE INDEX IF NOT EXISTS idx_remote_backup_plans_server ON remote_backup_plans(server_id)`,
		`CREATE TABLE IF NOT EXISTS remote_backup_runs (id TEXT PRIMARY KEY,plan_id TEXT NOT NULL,status TEXT NOT NULL,needs_cleanup INTEGER NOT NULL,data TEXT NOT NULL,FOREIGN KEY(plan_id) REFERENCES remote_backup_plans(id) ON DELETE RESTRICT)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_remote_backup_runs_active ON remote_backup_runs(plan_id) WHERE status='running'`,
		`CREATE INDEX IF NOT EXISTS idx_remote_backup_runs_plan ON remote_backup_runs(plan_id)`,
		`CREATE TABLE IF NOT EXISTS remote_backup_artifacts (id TEXT PRIMARY KEY,plan_id TEXT NOT NULL,pruned INTEGER NOT NULL,created_at INTEGER NOT NULL,data TEXT NOT NULL,FOREIGN KEY(plan_id) REFERENCES remote_backup_plans(id) ON DELETE RESTRICT)`,
		`CREATE INDEX IF NOT EXISTS idx_remote_backup_artifacts_plan ON remote_backup_artifacts(plan_id,created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS agent_credentials (id TEXT PRIMARY KEY,owner TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,data TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_credentials_owner ON agent_credentials(owner)`,
		`CREATE TABLE IF NOT EXISTS cron_jobs (id TEXT PRIMARY KEY, server_id TEXT NOT NULL, enabled INTEGER NOT NULL, next_run_at INTEGER NOT NULL, data TEXT NOT NULL, FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_jobs_server ON cron_jobs(server_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_jobs_due ON cron_jobs(enabled,next_run_at)`,
		`CREATE TABLE IF NOT EXISTS cron_runs (id TEXT PRIMARY KEY, job_id TEXT NOT NULL, server_id TEXT NOT NULL, status TEXT NOT NULL, data TEXT NOT NULL, FOREIGN KEY(job_id) REFERENCES cron_jobs(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_runs_job ON cron_runs(job_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cron_runs_active ON cron_runs(job_id) WHERE status='running'`,
		`CREATE TABLE IF NOT EXISTS resource_settings (id TEXT PRIMARY KEY, data TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, owner TEXT NOT NULL, resource TEXT NOT NULL, status TEXT NOT NULL, data TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_operations_request_key ON operations(owner,json_extract(data,'$.request_key')) WHERE json_extract(data,'$.request_key') IS NOT NULL AND json_extract(data,'$.request_key')!=''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_operations_active_resource ON operations(resource) WHERE status IN ('running', 'awaiting_approval')`,
		`CREATE TABLE IF NOT EXISTS tool_audit (id INTEGER PRIMARY KEY AUTOINCREMENT, actor TEXT NOT NULL, tool TEXT NOT NULL, server_id TEXT NOT NULL, outcome TEXT NOT NULL, operation_id TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS json_imports (
			name TEXT PRIMARY KEY,
			source_path TEXT NOT NULL,
			imported_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			user TEXT NOT NULL,
			auth_type TEXT NOT NULL,
			key_path TEXT NOT NULL,
			password TEXT NOT NULL,
			tags TEXT NOT NULL,
			is_db_only INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			role TEXT NOT NULL,
			permissions TEXT NOT NULL,
			allowed_servers TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source_url TEXT NOT NULL,
			build_type TEXT NOT NULL,
			port INTEGER NOT NULL,
			host_port INTEGER NOT NULL,
			status TEXT NOT NULL,
			path TEXT NOT NULL,
			container_id TEXT NOT NULL,
			process_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			created_at TEXT NOT NULL,
			start_command TEXT NOT NULL,
			build_command TEXT NOT NULL,
			last_build TEXT NOT NULL,
			webhook_secret TEXT NOT NULL,
			webhook_enabled INTEGER NOT NULL,
			last_deploy TEXT NOT NULL,
			env_vars TEXT NOT NULL,
			restart_on_boot INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_server_id ON projects(server_id)`,
		`CREATE TABLE IF NOT EXISTS deployments (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			delivery_id TEXT NOT NULL UNIQUE,
			source TEXT NOT NULL,
			repository TEXT NOT NULL,
			ref TEXT NOT NULL,
			branch TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			commit_message TEXT NOT NULL,
			pusher TEXT NOT NULL,
			status TEXT NOT NULL,
			queued_at TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			error TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_deployments_project_queued ON deployments(project_id, queued_at DESC)`,
		`CREATE TABLE IF NOT EXISTS domains (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			domain TEXT NOT NULL,
			port INTEGER NOT NULL,
			ssl INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(server_id, domain)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_domains_server_id ON domains(server_id)`,
		`CREATE TABLE IF NOT EXISTS databases (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			database_name TEXT NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			host_port INTEGER NOT NULL,
			container_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			auto_backup INTEGER NOT NULL,
			retention_days INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_databases_server_id ON databases(server_id)`,
		`CREATE TABLE IF NOT EXISTS load_balancers (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			domain TEXT NOT NULL,
			port INTEGER NOT NULL,
			algorithm TEXT NOT NULL,
			ssl INTEGER NOT NULL,
			health_check_path TEXT NOT NULL,
			health_check_interval INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at TEXT NOT NULL,
			UNIQUE(server_id, domain)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_load_balancers_server_id ON load_balancers(server_id)`,
		`CREATE TABLE IF NOT EXISTS load_balancer_backends (
			id TEXT PRIMARY KEY,
			load_balancer_id TEXT NOT NULL,
			target_server_id TEXT,
			target_project_id TEXT,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			weight INTEGER NOT NULL,
			backup INTEGER NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(load_balancer_id) REFERENCES load_balancers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backends_lb_id ON load_balancer_backends(load_balancer_id)`,
		`CREATE TABLE IF NOT EXISTS firewall_rules (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			action TEXT NOT NULL,
			direction TEXT NOT NULL,
			protocol TEXT NOT NULL,
			port TEXT NOT NULL,
			source TEXT NOT NULL,
			interface TEXT NOT NULL,
			enabled INTEGER NOT NULL,
			comment TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_firewall_rules_server_id ON firewall_rules(server_id)`,
		`CREATE TABLE IF NOT EXISTS firewall_backups (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			rules_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_firewall_backups_server_id ON firewall_backups(server_id)`,
		`CREATE TABLE IF NOT EXISTS firewall_schedules (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			rule_data TEXT NOT NULL,
			action_type TEXT NOT NULL,
			scheduled_at TEXT NOT NULL,
			executed INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_firewall_schedules_server_id ON firewall_schedules(server_id)`,
		`CREATE TABLE IF NOT EXISTS firewall_activity_logs (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			action TEXT NOT NULL,
			details TEXT NOT NULL,
			timestamp TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_firewall_activity_logs_server_id ON firewall_activity_logs(server_id)`,
		`CREATE TABLE IF NOT EXISTS metric_minutes (server_id TEXT NOT NULL, minute INTEGER NOT NULL, samples INTEGER NOT NULL, failures INTEGER NOT NULL, cpu_sum REAL NOT NULL, mem_sum REAL NOT NULL, disk_sum REAL NOT NULL, net_rx INTEGER NOT NULL, net_tx INTEGER NOT NULL, PRIMARY KEY(server_id,minute), FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_metric_minutes_retention ON metric_minutes(minute)`,
		`CREATE TABLE IF NOT EXISTS alert_rules (id TEXT PRIMARY KEY, server_id TEXT NOT NULL, data TEXT NOT NULL, FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_rules_server ON alert_rules(server_id)`,
		`CREATE TABLE IF NOT EXISTS alert_events (id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, server_id TEXT NOT NULL, occurred_at INTEGER NOT NULL, status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL, last_error TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL, destination TEXT NOT NULL, secret TEXT NOT NULL, FOREIGN KEY(rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE, FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_due ON alert_events(status,next_attempt)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_server ON alert_events(server_id,occurred_at DESC)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate SQLite schema: %w", err)
		}
	}

	// Extend legacy audit rows without rebuilding or dropping their history.
	for _, column := range []string{"owner", "agent_id"} {
		rows, err := db.Query("SELECT " + column + " FROM tool_audit LIMIT 1")
		if err == nil {
			rows.Close()
		} else {
			if _, err := db.Exec("ALTER TABLE tool_audit ADD COLUMN " + column + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("audit migration: %w", err)
			}
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_tool_audit_owner ON tool_audit(owner,id)`); err != nil {
		return err
	}

	// Dynamic migration: Ensure the load_balancers table has the error_message column
	// (in case the table was created on a previous run before this column was added).
	rows, err := db.Query("SELECT error_message FROM load_balancers LIMIT 1")
	if err != nil {
		// Column is missing, add it
		_, alterErr := db.Exec("ALTER TABLE load_balancers ADD COLUMN error_message TEXT")
		if alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return fmt.Errorf("failed to add error_message column to load_balancers: %w", alterErr)
		}
	} else {
		rows.Close()
	}

	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func encodeJSON(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func decodeJSON(raw string, target interface{}) error {
	if strings.TrimSpace(raw) == "" {
		raw = "null"
	}
	return json.Unmarshal([]byte(raw), target)
}

func timeToText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timeFromText(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return t
	}
	return time.Time{}
}

func importJSONOnce[T any](db *sql.DB, name string, sourcePath string, countSQL string, insert func(*sql.Tx, T) error) error {
	data, err := os.ReadFile(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read legacy %s JSON: %w", name, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}

	var records []T
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("failed to parse legacy %s JSON: %w", name, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var alreadyImported int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM json_imports WHERE name = ?`, name).Scan(&alreadyImported); err != nil {
		return err
	}
	if alreadyImported > 0 {
		return tx.Commit()
	}

	var existingRows int
	if err := tx.QueryRow(countSQL).Scan(&existingRows); err != nil {
		return err
	}
	if existingRows == 0 {
		for _, record := range records {
			if err := insert(tx, record); err != nil {
				return fmt.Errorf("failed to import legacy %s JSON: %w", name, err)
			}
		}
	}

	_, err = tx.Exec(
		`INSERT OR REPLACE INTO json_imports (name, source_path, imported_at) VALUES (?, ?, ?)`,
		name,
		sourcePath,
		timeToText(time.Now()),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
