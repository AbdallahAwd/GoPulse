package monitor

import (
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

// StatsHistory stores historical stats snapshots per server in a ring buffer
type StatsHistory struct {
	persistent *store.MonitoringStore
	mu         sync.RWMutex
	data       map[string][]models.StatsSnapshot
	maxSize    int
}

// NewStatsHistory creates a new StatsHistory with the given max entries per server
func NewStatsHistory(maxSize int) *StatsHistory {
	return &StatsHistory{
		data:    make(map[string][]models.StatsSnapshot),
		maxSize: maxSize,
	}
}

// Add appends a snapshot for a server, evicting the oldest if at capacity
func (h *StatsHistory) Add(serverID string, snapshot models.StatsSnapshot) {
	if h.persistent != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	entries := h.data[serverID]
	entries = append(entries, snapshot)

	// Evict oldest if over capacity
	if len(entries) > h.maxSize {
		entries = entries[len(entries)-h.maxSize:]
	}

	h.data[serverID] = entries
}

// Get returns snapshots for a server within the given duration from now
func (h *StatsHistory) Get(serverID string, duration time.Duration) []models.StatsSnapshot {
	if h.persistent != nil {
		points, _ := h.persistent.History(serverID, time.Now(), duration)
		return points
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	entries, ok := h.data[serverID]
	if !ok {
		return nil
	}

	cutoff := time.Now().Add(-duration)
	result := []models.StatsSnapshot{}

	for _, e := range entries {
		if e.Timestamp.After(cutoff) {
			result = append(result, e)
		}
	}

	return result
}

// NewPersistentStatsHistory reads the durable minute summaries written by the
// monitoring service; the in-memory constructor remains available to embedders.
func NewPersistentStatsHistory(s *store.MonitoringStore) *StatsHistory {
	return &StatsHistory{persistent: s}
}
func (h *StatsHistory) Query(serverID string, duration time.Duration) ([]models.StatsSnapshot, error) {
	if h.persistent != nil {
		return h.persistent.History(serverID, time.Now(), duration)
	}
	return h.Get(serverID, duration), nil
}
