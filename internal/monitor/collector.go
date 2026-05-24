package monitor

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
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
	ServerID string
	Stats    *models.SystemStats
	Error    string
}

// Collector is responsible for collecting system statistics periodically from all servers
type Collector struct {
	serverStore     *store.ServerStore
	providerFactory ProviderFactory
	providers       map[string]services.StatsProvider
	providersMu     sync.RWMutex
	cache           *StatsCache
	interval        time.Duration
	clients         map[*websocket.Conn]bool
	clientsMu       sync.RWMutex
	broadcast       chan ServerStatsUpdate
}

// NewCollector creates a new stats collector
func NewCollector(serverStore *store.ServerStore, factory ProviderFactory, interval time.Duration) *Collector {
	return &Collector{
		serverStore:     serverStore,
		providerFactory: factory,
		cache: &StatsCache{
			stats:      make(map[string]*models.SystemStats),
			lastUpdate: make(map[string]time.Time),
		},
		interval:  interval,
		clients:   make(map[*websocket.Conn]bool),
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
	go c.broadcaster()

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
	c.providersMu.RLock()
	provider, exists := c.providers[server.ID]
	c.providersMu.RUnlock()

	if !exists {
		var err error
		provider, err = c.providerFactory(server)
		if err != nil {
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
		log.Printf("Error collecting stats for server %s: %v", server.Name, err)
		c.broadcastUpdate(ServerStatsUpdate{
			ServerID: server.ID,
			Error:    err.Error(),
		})
		return
	}

	// Update cache
	c.cache.mu.Lock()
	c.cache.stats[server.ID] = stats
	c.cache.lastUpdate[server.ID] = time.Now()
	c.cache.mu.Unlock()

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
	return stats, c.cache.lastUpdate[serverID]
}

// broadcaster sends stats to all connected WebSocket clients
func (c *Collector) broadcaster() {
	for update := range c.broadcast {
		c.clientsMu.RLock()
		clients := make([]*websocket.Conn, 0, len(c.clients))
		for client := range c.clients {
			clients = append(clients, client)
		}
		c.clientsMu.RUnlock()

		message, _ := json.Marshal(map[string]interface{}{
			"type": "stats_update",
			"data": update,
		})

		for _, client := range clients {
			err := client.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("Error sending stats to WebSocket client: %v", err)
				c.RemoveClient(client)
			}
		}
	}
}

// AddClient registers a new WebSocket client
func (c *Collector) AddClient(conn *websocket.Conn) {
	c.clientsMu.Lock()
	c.clients[conn] = true
	c.clientsMu.Unlock()

	// Send current cached stats for all servers immediately
	c.cache.mu.RLock()
	for serverID, stats := range c.cache.stats {
		conn.WriteJSON(map[string]interface{}{
			"type": "stats_update",
			"data": ServerStatsUpdate{
				ServerID: serverID,
				Stats:    stats,
			},
		})
	}
	c.cache.mu.RUnlock()

	log.Printf("WebSocket client connected. Total clients: %d", len(c.clients))
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
