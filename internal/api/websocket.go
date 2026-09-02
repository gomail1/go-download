package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go-download-server/internal/event"
)

// WebSocket clients management
type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// WebSocketHub manages WebSocket clients and messages
type WebSocketHub struct {
	clients    map[*wsClient]bool
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
	mu         sync.Mutex
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*wsClient]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
	}
}

// Run starts the WebSocket hub
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// WebSocketHandler handles WebSocket connections
func (s *Server) WebSocketHandler(c *gin.Context) {
	// Upgrade HTTP connection to WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Register client
	GlobalWebSocketHub.register <- client

	// Start client read/write goroutines
	go client.readPump()
	go client.writePump()
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *wsClient) readPump() {
	defer func() {
		GlobalWebSocketHub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}
		// Ignore incoming messages for now
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *wsClient) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Printf("WebSocket write error: %v", err)
			break
		}
	}
}

// GlobalWebSocketHub is the global WebSocket hub instance
var GlobalWebSocketHub *WebSocketHub

// InitWebSocketHub initializes the global WebSocket hub
func InitWebSocketHub() {
	GlobalWebSocketHub = NewWebSocketHub()
	go GlobalWebSocketHub.Run()

	// Subscribe to task events
	event.Subscribe(event.EventTaskCreated, func(e event.Event) {
		broadcastEvent("task.created", e.Data)
	})
	event.Subscribe(event.EventTaskStarted, func(e event.Event) {
		broadcastEvent("task.started", e.Data)
	})
	event.Subscribe(event.EventTaskProgress, func(e event.Event) {
		broadcastEvent("task.progress", e.Data)
	})
	event.Subscribe(event.EventTaskPaused, func(e event.Event) {
		broadcastEvent("task.paused", e.Data)
	})
	event.Subscribe(event.EventTaskCompleted, func(e event.Event) {
		broadcastEvent("task.completed", e.Data)
	})
	event.Subscribe(event.EventTaskFailed, func(e event.Event) {
		broadcastEvent("task.failed", e.Data)
	})
}

// broadcastEvent broadcasts an event to all WebSocket clients
func broadcastEvent(eventType string, data interface{}) {
	// Create event message
	msg := map[string]interface{}{
		"type": eventType,
		"data": data,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	// Broadcast to all clients
	if GlobalWebSocketHub != nil {
		GlobalWebSocketHub.broadcast <- jsonData
	}
}
