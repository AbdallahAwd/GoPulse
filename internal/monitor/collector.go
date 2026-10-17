package monitor

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/gorilla/websocket"
)

// ProviderFactory is a function that returns a StatsProvider for a given server
type ProviderFactory func(server models.Server) (services.StatsProvider, error)

type StatsCache struct {
	mu         sync.RWMutex
	stats      map[string]*models.SystemStats
	lastUpdate map[string]time.Time
}

type ServerStatsUpdate struct {
	ServerID string              `json:"server_id"`
	Stats    *models.SystemStats `json:"stats,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// Collector is responsible for collecting system statistics periodically from all servers
type Collector struct {
	serverStore     *store.ServerStore
	providerFactory ProviderFactory
	providers       map[string]services.StatsProvider
	providersMu     sync.RWMutex
	cache           *StatsCache
	monitoring      *services.MonitoringService
	history         *StatsHistory
	interval        time.Duration
	clients         map[*websocket.Conn]func(string) bool
	clientsMu       sync.RWMutex
	broadcast       chan ServerStatsUpdate
}

// NewCollector creates a new stats collector
func NewCollector(serverStore *store.ServerStore, factory ProviderFactory, interval time.Duration, history *StatsHistory) *Collector {
	return &Collector{
		serverStore:     serverStore,
		providerFactory: factory,
		cache: &StatsCache{
			stats:      make(map[string]*models.SystemStats),
			lastUpdate: make(map[string]time.Time),
		},
		history:   history,
		interval:  interval,
		clients:   make(map[*websocket.Conn]func(string) bool),
		broadcast: make(chan ServerStatsUpdate, 100),
		providers: make(map[string]services.StatsProvider),
	}
}

// Start begins the background collection routine
func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("Starting multi-server stats collector with interval: %s", c.interval)

	// Collect stats immediately
	c.collectAllStats(ctx)

	// Start broadcaster goroutine
	go c.broadcaster(ctx)

	for {
		select {
		case <-ticker.C:
			c.collectAllStats(ctx)
		case <-ctx.Done():
			log.Println("Stopping stats collector")
			return
		}
	}
}

// collectAllStats iterates through all servers and collects stats
func (c *Collector) collectAllStats(ctx context.Context) {
	servers := c.serverStore.ListServers()

	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Add(1)
		go func(srv models.Server) {
			defer wg.Done()
			c.collectServerStats(ctx, srv)
		}(server)
	}
	wg.Wait()
}

// collectServerStats gathers stats for a single server
func (c *Collector) collectServerStats(ctx context.Context, server models.Server) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.providersMu.RLock()
	provider, exists := c.providers[server.ID]
	c.providersMu.RUnlock()

	if !exists {
		var err error
		provider, err = c.providerFactory(server)
		if err != nil {
			c.observe(server.ID, nil)
			log.Printf("Failed to get provider for server %s: %v", server.Name, err)
			c.broadcastUpdate(ServerStatsUpdate{
				ServerID: server.ID,
				Error:    err.Error(),
			})
			return
		}

		c.providersMu.Lock()
		c.providers[server.ID] = provider
		c.providersMu.Unlock()
	}

	stats, err := provider.GetSystemStats(ctx)
	if err != nil {
		c.observe(server.ID, nil)
		log.Printf("Error collecting stats for server %s: %v", server.Name, err)
		c.broadcastUpdate(ServerStatsUpdate{
			ServerID: server.ID,
			Error:    err.Error(),
		})
		return
	}

	if services.Snapshot(stats, time.Now()).Unavailable {
		c.observe(server.ID, nil)
		c.broadcastUpdate(ServerStatsUpdate{ServerID: server.ID, Error: "Metrics unavailable"})
		return
	}
	c.observe(server.ID, stats)
	// Update cache
	now := time.Now()
	c.cache.mu.Lock()
	c.cache.stats[server.ID] = stats
	c.cache.lastUpdate[server.ID] = now
	c.cache.mu.Unlock()

	// Record history snapshot
	if c.history != nil {
		var diskPercent float64
		if len(stats.Disk) > 0 {
			diskPercent = stats.Disk[0].UsedPercent
		}
		c.history.Add(server.ID, models.StatsSnapshot{
			Timestamp:   now,
			CPUPercent:  stats.CPU.Percent,
			MemPercent:  stats.Memory.UsedPercent,
			DiskPercent: diskPercent,
			NetRxBytes:  stats.Network.BytesRecv,
			NetTxBytes:  stats.Network.BytesSent,
		})
	}

	// Broadcast
	c.broadcastUpdate(ServerStatsUpdate{
		ServerID: server.ID,
		Stats:    stats,
	})
}

func (c *Collector) broadcastUpdate(update ServerStatsUpdate) {
	select {
	case c.broadcast <- update:
	default:
		// Channel full
	}
}

// GetCachedStats returns the latest cached statistics for a server
func (c *Collector) GetCachedStats(serverID string) (*models.SystemStats, time.Time) {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	stats, ok := c.cache.stats[serverID]
	if !ok {
		return nil, time.Time{}
	}
	if c.monitoring != nil && time.Since(c.cache.lastUpdate[serverID]) > c.monitoring.MaxGap {
		return nil, c.cache.lastUpdate[serverID]
	}
	return stats, c.cache.lastUpdate[serverID]
}

// broadcaster sends stats to all connected WebSocket clients
func (c *Collector) broadcaster(ctx context.Context) {
	for {
		var update ServerStatsUpdate
		select {
		case <-ctx.Done():
			return
		case update = <-c.broadcast:
		}
		c.clientsMu.RLock()
		clients := make([]*websocket.Conn, 0, len(c.clients))
		for client, authorize := range c.clients {
			if authorize(update.ServerID) {
				clients = append(clients, client)
			}
		}
		c.clientsMu.RUnlock()

		message, _ := json.Marshal(map[string]interface{}{
			"type": "stats_update",
			"data": update,
		})

		for _, client := range clients {
			client.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := client.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("Error sending stats to WebSocket client: %v", err)
				c.RemoveClient(client)
			}
		}
	}
}

// AddClient registers a new WebSocket client
func (c *Collector) AddClient(conn *websocket.Conn) { c.AddScopedClient(conn, true, nil) }
func (c *Collector) AddScopedClient(conn *websocket.Conn, admin bool, allowed []string) {
	var scope map[string]bool
	if !admin {
		scope = make(map[string]bool)
		for _, id := range allowed {
			scope[id] = true
		}
	}
	c.AddAuthorizedClient(conn, func(id string) bool { return admin || scope[id] })
}
func (c *Collector) AddAuthorizedClient(conn *websocket.Conn, authorize func(string) bool) {
	// Register after the initial snapshot so only one writer uses the connection.
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()
	for id, stats := range c.cache.stats {
		if !authorize(id) {
			continue
		}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(map[string]interface{}{"type": "stats_update", "data": ServerStatsUpdate{ServerID: id, Stats: stats}}); err != nil {
			conn.Close()
			return
		}
	}
	c.clients[conn] = authorize
}

// RemoveClient unregisters a WebSocket client
func (c *Collector) RemoveClient(conn *websocket.Conn) {
	c.clientsMu.Lock()
	delete(c.clients, conn)
	clientCount := len(c.clients)
	c.clientsMu.Unlock()

	conn.Close()
	log.Printf("WebSocket client disconnected. Total clients: %d", clientCount)
}

// GetClientCount returns the number of connected clients
func (c *Collector) GetClientCount() int {
	c.clientsMu.RLock()
	defer c.clientsMu.RUnlock()
	return len(c.clients)
}

// GetHistory returns historical stats snapshots for a server
func (c *Collector) GetHistory(serverID string, duration time.Duration) []models.StatsSnapshot {
	if c.history == nil {
		return nil
	}
	return c.history.Get(serverID, duration)
}

func (c *Collector) SetMonitoring(service *services.MonitoringService) { c.monitoring = service }
func (c *Collector) observe(serverID string, stats *models.SystemStats) {
	if stats == nil {
		c.cache.mu.Lock()
		delete(c.cache.stats, serverID)
		delete(c.cache.lastUpdate, serverID)
		c.cache.mu.Unlock()
	}
	if c.monitoring != nil {
		if err := c.monitoring.Observe(serverID, stats, time.Now().UTC()); err != nil {
			log.Printf("Persisting metrics and alerts for %s failed: %v", serverID, err)
		}
	}
}
func (c *Collector) QueryHistory(serverID string, duration time.Duration) ([]models.StatsSnapshot, error) {
	if c.history == nil {
		return []models.StatsSnapshot{}, nil
	}
	return c.history.Query(serverID, duration)
}
