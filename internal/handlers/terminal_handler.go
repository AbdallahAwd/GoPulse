package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// TerminalHandler handles the interactive SSH terminal via WebSocket
type TerminalHandler struct {
	manager *services.ServiceManager
}

// NewTerminalHandler creates a new TerminalHandler
func NewTerminalHandler(manager *services.ServiceManager) *TerminalHandler {
	return &TerminalHandler{manager: manager}
}

// terminalMessage represents a message from the browser terminal
type terminalMessage struct {
	Type string `json:"type"` // "input", "resize"
	Data string `json:"data"` // keystrokes for "input"
	Cols int    `json:"cols"` // for "resize"
	Rows int    `json:"rows"` // for "resize"
}

// HandleTerminal upgrades to WebSocket and bridges to an SSH PTY session
func (h *TerminalHandler) HandleTerminal(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get initial terminal size from query params
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Get SSH client
	sshClient, err := h.manager.GetSSHClient(serverID)
	if err != nil {
		log.Printf("Failed to get SSH client for terminal: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: Failed to connect to server: "+err.Error()+"\033[0m\r\n"))
		return
	}

	// Connect SSH if needed
	if err := sshClient.Connect(); err != nil {
		log.Printf("Failed to connect SSH for terminal: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: SSH connection failed: "+err.Error()+"\033[0m\r\n"))
		return
	}

	// Get the underlying SSH client to create a session
	rawClient := sshClient.GetClient()
	if rawClient == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: SSH client is nil\033[0m\r\n"))
		return
	}

	// Create a new SSH session
	session, err := rawClient.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session for terminal: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: Failed to create session: "+err.Error()+"\033[0m\r\n"))
		return
	}
	defer session.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		log.Printf("Failed to request PTY: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: PTY request failed: "+err.Error()+"\033[0m\r\n"))
		return
	}

	// Get stdin pipe
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		log.Printf("Failed to get stdin pipe: %v", err)
		return
	}

	// Get stdout pipe
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout pipe: %v", err)
		return
	}

	// Get stderr pipe
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		log.Printf("Failed to get stderr pipe: %v", err)
		return
	}

	// Start shell
	if err := session.Shell(); err != nil {
		log.Printf("Failed to start shell: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\033[31mError: Shell failed: "+err.Error()+"\033[0m\r\n"))
		return
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Goroutine: SSH stdout → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, err := stdoutPipe.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("SSH stdout read error: %v", err)
				}
				close(done)
				return
			}
			if n > 0 {
				if err := conn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					log.Printf("WebSocket write error: %v", err)
					return
				}
			}
		}
	}()

	// Goroutine: SSH stderr → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 8192)
		for {
			n, err := stderrPipe.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("SSH stderr read error: %v", err)
				}
				return
			}
			if n > 0 {
				conn.WriteMessage(websocket.TextMessage, buf[:n])
			}
		}
	}()

	// Main loop: WebSocket → SSH stdin
	go func() {
		defer stdinPipe.Close()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("Terminal WebSocket read error: %v", err)
				}
				return
			}

			// Try to parse as JSON message
			var msg terminalMessage
			if err := json.Unmarshal(message, &msg); err == nil {
				switch msg.Type {
				case "input":
					if _, err := stdinPipe.Write([]byte(msg.Data)); err != nil {
						log.Printf("SSH stdin write error: %v", err)
						return
					}
				case "resize":
					if msg.Cols > 0 && msg.Rows > 0 {
						session.WindowChange(msg.Rows, msg.Cols)
					}
				}
			} else {
				// Raw text input (fallback)
				if _, err := stdinPipe.Write(message); err != nil {
					log.Printf("SSH stdin write error: %v", err)
					return
				}
			}
		}
	}()

	// Wait for SSH session to end
	session.Wait()
	<-done
	wg.Wait()
}
