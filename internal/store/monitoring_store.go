package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

var ErrAlertNotFound = errors.New("alert rule not found")
var ErrAlertTestBusy = errors.New("wait 30 seconds before requesting another test notification")

var ErrAlertLimit = errors.New("maximum 100 alert rules per server")

type MonitoringStore struct{ db *sql.DB }

func NewMonitoringStore(path string) (*MonitoringStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &MonitoringStore{db: db}, nil
}

func alertRules(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, serverID string) ([]models.AlertRule, error) {
	rows, err := q.Query(`SELECT data FROM alert_rules WHERE server_id=? ORDER BY rowid`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.AlertRule{}
	for rows.Next() {
		var data string
		var rule models.AlertRule
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &rule); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}
func (s *MonitoringStore) Rules(serverID string) ([]models.AlertRule, error) {
	return alertRules(s.db, serverID)
}
func (s *MonitoringStore) Rule(serverID, id string) (models.AlertRule, error) {
	var raw string
	err := s.db.QueryRow(`SELECT data FROM alert_rules WHERE server_id=? AND id=?`, serverID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlertRule{}, ErrAlertNotFound
	}
	var rule models.AlertRule
	if err == nil {
		err = json.Unmarshal([]byte(raw), &rule)
	}
	return rule, err
}

// MutateRule serializes edits with observations and webhook queue updates.
func (s *MonitoringStore) MutateRule(serverID, id string, create bool, change func(*models.AlertRule) error) (models.AlertRule, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return models.AlertRule{}, err
	}
	defer tx.Rollback()
	rule := models.AlertRule{ID: id, ServerID: serverID, State: "unknown"}
	if create {
		var count int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM alert_rules WHERE server_id=?`, serverID).Scan(&count); err != nil {
			return rule, err
		}
		if count >= 100 {
			return rule, ErrAlertLimit
		}
	} else {
		var raw string
		err = tx.QueryRow(`SELECT data FROM alert_rules WHERE server_id=? AND id=?`, serverID, id).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return rule, ErrAlertNotFound
		}
		if err != nil {
			return rule, err
		}
		if err = json.Unmarshal([]byte(raw), &rule); err != nil {
			return rule, err
		}
	}
	if err = change(&rule); err != nil {
		return rule, err
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return rule, err
	}
	if create {
		_, err = tx.Exec(`INSERT INTO alert_rules(id,server_id,data) VALUES(?,?,?)`, id, serverID, string(raw))
	} else {
		_, err = tx.Exec(`UPDATE alert_rules SET data=? WHERE id=? AND server_id=?`, string(raw), id, serverID)
	}
	if err != nil {
		return rule, err
	}
	return rule, tx.Commit()
}
func (s *MonitoringStore) DeleteRule(serverID, id string) error {
	result, err := s.db.Exec(`DELETE FROM alert_rules WHERE server_id=? AND id=?`, serverID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrAlertNotFound
	}
	return nil
}

// Observe atomically commits metric aggregation, state transitions, incident
// history and the notification outbox. Network I/O never holds this transaction.
func (s *MonitoringStore) Observe(serverID string, sample models.StatsSnapshot, record bool, evaluate func(*models.AlertRule) *models.AlertEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if record {
		n, fail := 1, 0
		if sample.Unavailable {
			n, fail = 0, 1
		}
		_, err = tx.Exec(`INSERT INTO metric_minutes(server_id,minute,samples,failures,cpu_sum,mem_sum,disk_sum,net_rx,net_tx) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(server_id,minute) DO UPDATE SET samples=samples+excluded.samples, failures=failures+excluded.failures,cpu_sum=cpu_sum+excluded.cpu_sum,mem_sum=mem_sum+excluded.mem_sum,disk_sum=disk_sum+excluded.disk_sum,net_rx=CASE WHEN excluded.samples>0 THEN excluded.net_rx ELSE net_rx END,net_tx=CASE WHEN excluded.samples>0 THEN excluded.net_tx ELSE net_tx END`, serverID, sample.Timestamp.Unix()/60, n, fail, sample.CPUPercent, sample.MemPercent, sample.DiskPercent, sample.NetRxBytes, sample.NetTxBytes)
		if err != nil {
			return err
		}
	}
	rules, err := alertRules(tx, serverID)
	if err != nil {
		return err
	}
	for i := range rules {
		rule := &rules[i]
		event := evaluate(rule)
		if event != nil {
			if err = insertAlertEvent(tx, *rule, event); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(rule)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE alert_rules SET data=? WHERE id=?`, string(raw), rule.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func insertAlertEvent(tx *sql.Tx, rule models.AlertRule, event *models.AlertEvent) error {
	event.ID = uuid.NewString()
	event.RuleID = rule.ID
	event.ServerID = rule.ServerID
	event.RuleName = rule.Name
	event.Metric = rule.Metric
	event.Threshold = rule.Threshold
	if event.DeliveryStatus == "" {
		event.DeliveryStatus = "pending"
		if rule.WebhookURL == "" {
			event.DeliveryStatus = "not_configured"
		} else if rule.SilencedUntil.After(event.OccurredAt) {
			event.DeliveryStatus = "silenced"
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO alert_events(id,rule_id,server_id,occurred_at,status,next_attempt,payload,destination,secret) VALUES(?,?,?,?,?,?,?,?,?)`, event.ID, rule.ID, rule.ServerID, event.OccurredAt.Unix(), event.DeliveryStatus, event.OccurredAt.Unix(), string(raw), rule.WebhookURL, rule.WebhookSecret)
	return err
}
func (s *MonitoringStore) QueueTest(serverID, id string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRow(`SELECT data FROM alert_rules WHERE server_id=? AND id=?`, serverID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAlertNotFound
	}
	if err != nil {
		return err
	}
	var rule models.AlertRule
	if err = json.Unmarshal([]byte(raw), &rule); err != nil {
		return err
	}
	if rule.WebhookURL == "" {
		return errors.New("no webhook configured")
	}
	var recent int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM alert_events WHERE rule_id=? AND occurred_at>? AND json_extract(payload,'$.state')='test'`, id, now.Add(-30*time.Second).Unix()).Scan(&recent); err != nil {
		return err
	}
	if recent > 0 {
		return ErrAlertTestBusy
	}
	// A test is explicitly requested and bypasses maintenance silencing.
	event := &models.AlertEvent{State: "test", OccurredAt: now, DeliveryStatus: "pending"}
	if err = insertAlertEvent(tx, rule, event); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MonitoringStore) History(serverID string, now time.Time, duration time.Duration) ([]models.StatsSnapshot, error) {
	// At most ~1000 buckets per response; no application-memory full-history scan.
	step := int64(duration/time.Minute)/1000 + 1
	rows, err := s.db.Query(`WITH buckets AS (
 SELECT (minute/?)*? AS bucket,SUM(samples) AS n,SUM(cpu_sum) AS cpu,SUM(mem_sum) AS mem,SUM(disk_sum) AS disk,MAX(CASE WHEN samples>0 THEN minute END) AS latest
 FROM metric_minutes WHERE server_id=? AND minute>=? AND minute<=? GROUP BY minute/?
 ) SELECT b.bucket,b.n,b.cpu,b.mem,b.disk,COALESCE(m.net_rx,0),COALESCE(m.net_tx,0)
 FROM buckets b LEFT JOIN metric_minutes m ON m.server_id=? AND m.minute=b.latest ORDER BY b.bucket`, step, step, serverID, now.Add(-duration).Unix()/60, now.Unix()/60, step, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.StatsSnapshot{}
	for rows.Next() {
		var minute int64
		var n int
		var cpu, mem, disk float64
		var rx, tx uint64
		if err = rows.Scan(&minute, &n, &cpu, &mem, &disk, &rx, &tx); err != nil {
			return nil, err
		}
		point := models.StatsSnapshot{Timestamp: time.Unix(minute*60, 0).UTC(), Unavailable: n == 0, NetRxBytes: rx, NetTxBytes: tx}
		if n > 0 {
			point.CPUPercent = cpu / float64(n)
			point.MemPercent = mem / float64(n)
			point.DiskPercent = disk / float64(n)
		}
		result = append(result, point)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *MonitoringStore) Events(serverID string) ([]models.AlertEvent, error) {
	rows, err := s.db.Query(`SELECT payload,status,attempts,last_error FROM alert_events WHERE server_id=? ORDER BY occurred_at DESC,rowid DESC LIMIT 100`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.AlertEvent{}
	for rows.Next() {
		var raw, status, last string
		var attempts int
		var event models.AlertEvent
		if err = rows.Scan(&raw, &status, &attempts, &last); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.DeliveryStatus = status
		event.Attempts = attempts
		event.LastError = last
		result = append(result, event)
	}
	return result, rows.Err()
}

// ClaimDelivery leases a single event. An interrupted attempt becomes eligible
// after 30 seconds, so process crashes cannot strand notifications.
func (s *MonitoringStore) ClaimDelivery(now time.Time) (*models.WebhookDelivery, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var d models.WebhookDelivery
	var raw string
	var attempts int
	err = tx.QueryRow(`SELECT payload,destination,secret,attempts FROM alert_events WHERE status IN ('pending','retry','delivering') AND next_attempt<=? AND NOT EXISTS (SELECT 1 FROM alert_events older WHERE older.rule_id=alert_events.rule_id AND older.rowid<alert_events.rowid AND older.status IN ('pending','retry','delivering')) ORDER BY occurred_at,rowid LIMIT 1`, now.Unix()).Scan(&raw, &d.URL, &d.Secret, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &d.Event); err != nil {
		return nil, err
	}
	d.Payload = []byte(raw)
	d.Event.Attempts = attempts + 1
	if _, err = tx.Exec(`UPDATE alert_events SET status='delivering',attempts=?,next_attempt=? WHERE id=?`, d.Event.Attempts, now.Add(30*time.Second).Unix(), d.Event.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &d, nil
}
func (s *MonitoringStore) CompleteDelivery(id, status, message string, next time.Time) error {
	_, err := s.db.Exec(`UPDATE alert_events SET status=?,last_error=?,next_attempt=? WHERE id=? AND status='delivering'`, status, message, next.Unix(), id)
	return err
}
func (s *MonitoringStore) Prune(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM metric_minutes WHERE minute<?`, now.Add(-30*24*time.Hour).Unix()/60); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM alert_events WHERE occurred_at<?`, now.Add(-30*24*time.Hour).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
