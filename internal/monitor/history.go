package monitor

import (
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// StatsHistory stores historical stats snapshots per server in a ring buffer
type StatsHistory struct {
	mu      sync.RWMutex
	data    map[string][]models.StatsSnapshot
	maxSize int
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
