package handlers

import (
	"log"
	"net/http"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/monitor"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
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
	// If routed as /api/stats, it might return all?
	// Or if routed as /api/servers/{serverID}/stats, we get serverID.

	// For now, let's support optional query param ?server_id=... or just dump all if none?
	// But GetCachedStats(serverID) requires an ID.
	// If no ID, maybe return map of all?

	// Let's assume we want specific server stats here as per main.go routing plan (though I didn't add /stats to {serverID} route group yet in main.go, I added global /api/stats).
	// Let's make /api/stats return all servers stats map.

	// Wait, Collector doesn't have GetAllCachedStats exposed public yet.
	// It has GetCachedStats(serverID).

	// Let's update main.go to use /api/servers/{serverID}/stats for specific stats.
	// And /api/stats can remain for backward compatibility or global view (but needs to be implemented).

	// For now, let's just make it work with serverID query param or if inside /servers/{serverID} path.

	serverID := r.URL.Query().Get("server_id")
	if serverID == "" {
		serverID = chi.URLParam(r, "serverID")
	}

	if serverID != "" {
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

	// If no ID, return summary of all connected clients?
	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"message": "Specify server_id to get stats",
			"clients": h.collector.GetClientCount(),
		},
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
	h.collector.AddClient(conn)

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
			if err := conn.WriteMessage(websocket.PongMessage, message); err != nil {
				log.Printf("WebSocket pong error: %v", err)
				break
			}
		}
	}
}
