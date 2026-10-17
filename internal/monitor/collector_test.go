package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

type testStatsProvider struct{ fail bool }

func (p *testStatsProvider) GetSystemStats(context.Context) (*models.SystemStats, error) {
	if p.fail {
		return nil, errors.New("unreachable")
	}
	return &models.SystemStats{CPU: models.CPUStats{Percent: 60}, Disk: []models.DiskStats{{UsedPercent: 80}}}, nil
}
func (p *testStatsProvider) GetHostInfo(context.Context) (map[string]interface{}, error) {
	return nil, nil
}
func TestCollectorPersistsSuccessFailureAndInvalidatesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	servers, err := store.NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	server := models.Server{ID: "a"}
	if err = servers.AddServer(server); err != nil {
		t.Fatal(err)
	}
	db, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := services.NewMonitoringService(db, servers, time.Second)
	provider := &testStatsProvider{}
	c := NewCollector(servers, func(models.Server) (services.StatsProvider, error) { return provider, nil }, time.Second, NewPersistentStatsHistory(db))
	c.SetMonitoring(service)
	c.collectServerStats(context.Background(), server)
	stats, _ := c.GetCachedStats("a")
	if stats == nil {
		t.Fatal("no cached stats")
	}
	points, err := c.QueryHistory("a", time.Hour)
	if err != nil || len(points) != 1 || points[0].CPUPercent != 60 {
		t.Fatal(points, err)
	}
	provider.fail = true
	c.collectServerStats(context.Background(), server)
	stats, _ = c.GetCachedStats("a")
	if stats != nil {
		t.Fatal("failed collector exposed stale readings")
	}
	raw, err := json.Marshal(ServerStatsUpdate{ServerID: "a", Stats: &models.SystemStats{}})
	if err != nil || !strings.Contains(string(raw), `"server_id":"a"`) || !strings.Contains(string(raw), `"stats":`) {
		t.Fatal("websocket shape incompatible with frontend", string(raw), err)
	}
}
func TestProviderFactoryFailureRecordsUnavailableMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	servers, err := store.NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	server := models.Server{ID: "a"}
	if err = servers.AddServer(server); err != nil {
		t.Fatal(err)
	}
	db, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCollector(servers, func(models.Server) (services.StatsProvider, error) { return nil, errors.New("connection failed") }, time.Second, NewPersistentStatsHistory(db))
	c.SetMonitoring(services.NewMonitoringService(db, servers, time.Second))
	c.collectServerStats(context.Background(), server)
	points, err := c.QueryHistory("a", time.Hour)
	if err != nil || len(points) != 1 || !points[0].Unavailable {
		t.Fatal(points, err)
	}
}
