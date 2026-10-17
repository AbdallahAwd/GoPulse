package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func monitoringFixture(t *testing.T) (*MonitoringStore, *ServerStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitoring.json")
	servers, err := NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err = servers.AddServer(models.Server{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, servers, path
}
func TestMetricsAggregatePersistAndCascade(t *testing.T) {
	s, servers, path := monitoringFixture(t)
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	observe := func(id string, point models.StatsSnapshot) {
		t.Helper()
		if err := s.Observe(id, point, true, func(*models.AlertRule) *models.AlertEvent { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	observe("a", models.StatsSnapshot{Timestamp: now, CPUPercent: 10, MemPercent: 20, DiskPercent: 30, NetRxBytes: 100})
	observe("a", models.StatsSnapshot{Timestamp: now.Add(5 * time.Second), CPUPercent: 30, MemPercent: 40, DiskPercent: 50, NetRxBytes: 200})
	observe("a", models.StatsSnapshot{Timestamp: now.Add(10 * time.Second), Unavailable: true})
	observe("a", models.StatsSnapshot{Timestamp: now.Add(time.Minute), Unavailable: true})
	observe("b", models.StatsSnapshot{Timestamp: now, CPUPercent: 99})
	// Close the actual SQLite connection and reopen the same disk file.
	sqliteRegistry.Lock()
	delete(sqliteRegistry.dbs, sqlitePathForLegacyJSON(path))
	err := s.db.Close()
	sqliteRegistry.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s, err = NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	servers.db = s.db
	history, err := s.History("a", now.Add(2*time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].CPUPercent != 20 || history[0].MemPercent != 30 || history[0].DiskPercent != 40 || history[0].NetRxBytes != 200 || history[0].Unavailable || !history[1].Unavailable {
		t.Fatalf("unexpected persisted summary: %+v", history)
	}
	if err = servers.RemoveServer("a"); err != nil {
		t.Fatal(err)
	}
	history, err = s.History("a", now.Add(2*time.Minute), time.Hour)
	if err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
}
func TestHistoryBoundedAndRetention(t *testing.T) {
	s, _, _ := monitoringFixture(t)
	now := time.Now().UTC().Truncate(time.Minute)
	_, err := s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<44000) INSERT INTO metric_minutes(server_id,minute,samples,failures,cpu_sum,mem_sum,disk_sum,net_rx,net_tx) SELECT 'a',?-x,1,0,20,30,40,x,x FROM n`, now.Unix()/60)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Prune(now); err != nil {
		t.Fatal(err)
	}
	history, err := s.History("a", now, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) > 1000 || len(history) == 0 {
		t.Fatal("unbounded or empty history", len(history))
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM metric_minutes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 43201 {
		t.Fatal("retention failed", count)
	}
	for i := 1; i < len(history); i++ {
		if !history[i].Timestamp.After(history[i-1].Timestamp) {
			t.Fatal("unordered history")
		}
	}
}
func TestObservationAndOutboxRollbackTogether(t *testing.T) {
	s, _, _ := monitoringFixture(t)
	rule, err := s.MutateRule("a", "rule", true, func(r *models.AlertRule) error {
		r.Enabled = true
		r.WebhookURL = "https://hooks.example/test"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A DB failure after metrics are inserted must roll the entire observation back.
	_, err = s.db.Exec(`CREATE TRIGGER reject_alert BEFORE INSERT ON alert_events BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	err = s.Observe("a", models.StatsSnapshot{Timestamp: now}, true, func(r *models.AlertRule) *models.AlertEvent {
		r.State = "firing"
		return &models.AlertEvent{State: "firing", OccurredAt: now}
	})
	if err == nil {
		t.Fatal("expected transaction error")
	}
	rule, err = s.Rule("a", rule.ID)
	if err != nil || rule.State != "unknown" {
		t.Fatal(rule, err)
	}
	history, err := s.History("a", now, time.Hour)
	if err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
}
func TestDeliveryLeaseSurvivesInterruptedWorker(t *testing.T) {
	s, servers, _ := monitoringFixture(t)
	now := time.Now().UTC()
	_, err := s.MutateRule("a", "rule", true, func(r *models.AlertRule) error { r.WebhookURL = "https://hooks.example/test"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.QueueTest("a", "rule", now); err != nil {
		t.Fatal(err)
	}
	first, err := s.ClaimDelivery(now)
	if err != nil || first == nil {
		t.Fatal(first, err)
	}
	next, err := s.ClaimDelivery(now.Add(time.Second))
	if err != nil || next != nil {
		t.Fatal("leased delivery was double claimed", next, err)
	}
	next, err = s.ClaimDelivery(now.Add(31 * time.Second))
	if err != nil || next == nil || next.Event.ID != first.Event.ID || next.Event.Attempts != 2 {
		t.Fatal(next, err)
	}
	if err = servers.RemoveServer("a"); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events("a")
	if err != nil || len(events) != 0 {
		t.Fatal("event cascade failed", events, err)
	}
	rules, err := s.Rules("a")
	if err != nil || len(rules) != 0 {
		t.Fatal("rule cascade failed", rules, err)
	}
}
