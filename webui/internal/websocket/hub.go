// Package websocket implements a room-based WebSocket hub for EXAMVAN.
//
// Each exam maps to a room (keyed by exam ID). Clients join a room to
// receive real-time updates (new submissions, heartbeats, score changes).
//
// The hub uses gorilla/websocket for the transport layer and exposes a
// SocketIO-compatible message format for backward compatibility with the
// legacy Node frontend.
package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Upgrader
// ---------------------------------------------------------------------------

var upgrader = ws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Validate Origin header against Host to prevent CSWSH attacks.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// No Origin header (non-browser client) — allow.
			return true
		}
		host := r.Host
		// Allow requests whose Origin matches the Host (including scheme variants).
		return strings.Contains(origin, "://"+host) || strings.Contains(origin, "://localhost")
	},
}

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// SocketIOMessage mimics the Socket.IO wire format:
//
//	["event_name", payload]
//
// This is a 2-element JSON array so legacy clients can parse it directly.
type SocketIOMessage struct {
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

// MarshalSocketIO encodes a message in SocketIO-compatible wire format.
func MarshalSocketIO(msg SocketIOMessage) ([]byte, error) {
	// Encode as a 2-element array: ["event", payload]
	wrapper := []interface{}{msg.Event, msg.Payload}
	return json.Marshal(wrapper)
}

// ParseSocketIO attempts to parse a message in SocketIO wire format.
// Returns the parsed message and true if successful.
func ParseSocketIO(data []byte) (SocketIOMessage, bool) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) < 2 {
		return SocketIOMessage{}, false
	}
	var event string
	if err := json.Unmarshal(arr[0], &event); err != nil {
		return SocketIOMessage{}, false
	}
	var payload interface{}
	json.Unmarshal(arr[1], &payload) //nolint:errcheck
	return SocketIOMessage{Event: event, Payload: payload}, true
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client represents a single WebSocket connection.
type Client struct {
	conn   *ws.Conn
	send   chan []byte
	room   string // exam ID
	hub    *Hub
	mu     sync.Mutex
	closed bool
}

// readPump reads messages from the WebSocket connection and dispatches
// them to the hub. Runs until the connection is closed.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err, ws.CloseGoingAway, ws.CloseNormalClosure) {
				log.Printf("websocket: unexpected close: %v", err)
			}
			return
		}

		// Try parsing as SocketIO format.
		if msg, ok := ParseSocketIO(data); ok {
			c.hub.handleClientMessage(c, msg)
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(45 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(ws.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(ws.TextMessage)
			if err != nil {
				return
			}
			w.Write(message) //nolint:errcheck
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(ws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close marks the client as closed and closes its send channel.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.send)
	}
}

// ---------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------

// Hub maintains the set of active clients and broadcasts messages to them
// on a per-room (per-exam) basis.
type Hub struct {
	// rooms maps exam ID -> set of clients.
	rooms map[string]map[*Client]bool

	// register and unregister are channels for clients joining/leaving.
	register   chan *Client
	unregister chan *Client

	// broadcast routes a message to all clients in a specific room.
	broadcast chan *roomMessage

	mu sync.RWMutex
}

// roomMessage carries a message targeted at a specific room.
type roomMessage struct {
	room    string
	message []byte
}

// NewHub creates and returns a new Hub. Call Run() as a goroutine.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *roomMessage, 256),
	}
}

// Run starts the hub's event loop. This must be run as a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.addClient(client)

		case client := <-h.unregister:
			h.removeClient(client)

		case msg := <-h.broadcast:
			h.broadcastToRoom(msg.room, msg.message)
		}
	}
}

// addClient adds a client to its room.
func (h *Hub) addClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, exists := h.rooms[client.room]
	if !exists {
		room = make(map[*Client]bool)
		h.rooms[client.room] = room
	}
	room[client] = true

	log.Printf("websocket: client joined room %s (total: %d)", client.room, len(room))
}

// removeClient removes a client from its room and closes the send channel.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if room, exists := h.rooms[client.room]; exists {
		delete(room, client)
		client.Close()
		if len(room) == 0 {
			delete(h.rooms, client.room)
		}
		log.Printf("websocket: client left room %s (remaining: %d)", client.room, len(room))
	}
}

// broadcastToRoom sends a message to all clients in the specified room.
func (h *Hub) broadcastToRoom(roomID string, message []byte) {
	h.mu.RLock()
	clients, exists := h.rooms[roomID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	for client := range clients {
		select {
		case client.send <- message:
		default:
			// Client's send buffer is full; drop the client.
			go func(c *Client) {
				h.unregister <- c
			}(client)
		}
	}
}

// handleClientMessage processes incoming messages from clients.
// Currently handles "ping" → "pong" for connection keep-alive.
func (h *Hub) handleClientMessage(client *Client, msg SocketIOMessage) {
	switch msg.Event {
	case "ping":
		payload, _ := MarshalSocketIO(SocketIOMessage{Event: "pong", Payload: time.Now().UTC().Format(time.RFC3339)})
		select {
		case client.send <- payload:
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// JoinRoom registers a WebSocket connection into a room and returns a Client
// ready to receive messages. roomID is typically the exam ID (stringified).
func (h *Hub) JoinRoom(w http.ResponseWriter, r *http.Request, roomID string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
		room: roomID,
		hub:  h,
	}

	h.register <- client

	go client.writePump()
	go client.readPump()

	return nil
}

// LeaveRoom is optional — clients automatically leave when their connection
// closes. This method is exposed for explicit removal in tests.
func (h *Hub) LeaveRoom(client *Client) {
	h.unregister <- client
}

// BroadcastToRoom sends a SocketIO-compatible message to all clients in a room.
func (h *Hub) BroadcastToRoom(roomID string, event string, payload interface{}) {
	data, err := MarshalSocketIO(SocketIOMessage{Event: event, Payload: payload})
	if err != nil {
		log.Printf("websocket: marshal broadcast: %v", err)
		return
	}
	select {
	case h.broadcast <- &roomMessage{room: roomID, message: data}:
	default:
		log.Printf("websocket: broadcast buffer full for room %s, dropping message", roomID)
	}
}

// BroadcastToExam is a convenience alias for BroadcastToRoom that accepts an int exam ID.
func (h *Hub) BroadcastToExam(examID int, event string, payload interface{}) {
	h.BroadcastToRoom(itos(examID), event, payload)
}

// GetRoomSize returns the number of clients in a room.
func (h *Hub) GetRoomSize(roomID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if room, exists := h.rooms[roomID]; exists {
		return len(room)
	}
	return 0
}

// RoomCount returns the total number of active rooms.
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

// ClientCount returns the total number of connected clients across all rooms.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, room := range h.rooms {
		total += len(room)
	}
	return total
}

// itos converts an int to its string representation.
func itos(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	digits := make([]byte, 0, 20)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	if negative {
		digits = append(digits, '-')
	}
	// Reverse.
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
