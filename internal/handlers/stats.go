package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/monitor"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return middleware.SameOrigin(r)
	},
}

// StatsHandler handles stats-related endpoints
type StatsHandler struct {
	collector *monitor.Collector
}

// NewStatsHandler creates a new StatsHandler
func NewStatsHandler(collector *monitor.Collector) *StatsHandler {
	return &StatsHandler{collector: collector}
}

// GetStats handles GET /api/stats (or /api/servers/{serverID}/stats if routed)
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		serverID = r.URL.Query().Get("server_id")
	}

	if serverID != "" {
		if !middleware.CanAccessServer(r, serverID) {
			respondError(w, http.StatusForbidden, "Server access denied", nil)
			return
		}
		stats, lastUpdate := h.collector.GetCachedStats(serverID)
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: true,
			Data: map[string]interface{}{
				"server_id":   serverID,
				"stats":       stats,
				"last_update": lastUpdate,
			},
		})
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"message": "Specify server_id to get stats",
			"clients": h.collector.GetClientCount(),
		},
	})
}

// GetStatsHistory handles GET /api/servers/{serverID}/stats/history?range=1h|6h|24h
func (h *StatsHandler) GetStatsHistory(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		serverID = r.URL.Query().Get("server_id")
	}
	if serverID == "" {
		respondError(w, http.StatusBadRequest, "Server ID required", nil)
		return
	}
	if !middleware.CanAccessServer(r, serverID) {
		respondError(w, http.StatusForbidden, "Server access denied", nil)
		return
	}

	ranges := map[string]time.Duration{"": 24 * time.Hour, "1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	duration, ok := ranges[r.URL.Query().Get("range")]
	if !ok {
		respondError(w, 400, "Range must be 1h, 6h, 24h, 7d or 30d", nil)
		return
	}
	snapshots, err := h.collector.QueryHistory(serverID, duration)
	if err != nil {
		respondError(w, 500, "Metrics history is unavailable", nil)
		return
	}
	if snapshots == nil {
		snapshots = []models.StatsSnapshot{}
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    snapshots,
	})
}

// WebSocketHandler handles GET /api/stats/ws
func (h *StatsHandler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Register client
	authorize, ok := r.Context().Value(middleware.ServerAuthorizerContextKey).(func(string) bool)
	if !ok {
		conn.Close()
		return
	}
	conn.SetReadLimit(1024)
	h.collector.AddAuthorizedClient(conn, authorize)

	// Handle incoming messages (mainly for ping/pong)
	go h.handleWebSocket(conn)
}

func (h *StatsHandler) handleWebSocket(conn *websocket.Conn) {
	defer h.collector.RemoveClient(conn)

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		// Echo ping messages
		if messageType == websocket.PingMessage {
			if err := conn.WriteControl(websocket.PongMessage, message, time.Now().Add(5*time.Second)); err != nil {
				log.Printf("WebSocket pong error: %v", err)
				break
			}
		}
	}
}
