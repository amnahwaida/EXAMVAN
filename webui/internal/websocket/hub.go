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
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
	redis "github.com/redis/go-redis/v9"
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
		
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		// Allow requests whose Origin host matches Host exactly, or is localhost/127.0.0.1
		return u.Host == r.Host || u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
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

// wsString extracts a string value from a decoded JSON payload map.
// Returns "" when the key is missing or not a string.
func wsString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// sanitizeWSField strips HTML/JS metacharacters and control characters from a
// client-supplied websocket payload field and caps its length. The sanitized
// value is persisted to Redis AND broadcast to the monitoring room, where a
// legacy dashboard may render it into HTML — so it must never carry markup
// (this is a security boundary, not a display preference).
func sanitizeWSField(raw string, maxLen int) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == '&' || r == '<' || r == '>' || r == '"' || r == '\'' || r == '`' || r == '=':
			// Strip HTML/JS metacharacters.
			continue
		case r < 0x20 || r == 0x7f:
			// Strip control characters.
			continue
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// sanitizeWSMac restricts a MAC / device identifier to a safe character set
// and caps its length, mirroring sanitizeMAC in the HTTP layer. Returns ""
// when nothing usable remains so the caller can drop the event.
func sanitizeWSMac(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == ':' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if len(s) > 100 {
		s = s[:100]
	}
	return s
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
	// privileged marks a session-authenticated client (admin / pengawas /
	// operator). Token-authenticated clients (anyone holding the exam token —
	// SHARED by the whole class in static mode) are receive-only: they may
	// ping and receive room broadcasts, but their heartbeat / exam_completed
	// events are IGNORED. Without this gate any student could inject fake
	// heartbeats (phantom students in the monitoring dashboard) and delete
	// any device's heartbeat (making classmates appear offline).
	privileged bool
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

// trySend performs a non-blocking send on the client's send channel, guarded by
// the client mutex so it can never send on a channel that Close() has closed.
// Direct sends from the readPump goroutine (e.g. ping replies) MUST use this,
// because Close() may run concurrently in the hub's Run goroutine. Returns
// false if the client is closed or its buffer is full.
func (c *Client) trySend(msg []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- msg:
		return true
	default:
		return false
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

	rdb *redis.Client
}

// roomMessage carries a message targeted at a specific room.
type roomMessage struct {
	room    string
	message []byte
}

// NewHub creates and returns a new Hub. Call Run() as a goroutine.
func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *roomMessage, 4096),
		rdb:        rdb,
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
		client.trySend(payload)
	case "heartbeat":
		if !client.privileged {
			// Token-authed clients may only ping + receive: without this gate
			// any token holder (the token is shared by the whole class in
			// static mode) could inject fake heartbeats and phantom students
			// into the monitoring room. Student presence is reported over HTTP
			// (AccessLog) anyway.
			log.Printf("websocket: ignoring heartbeat from non-privileged client in room %s", client.room)
			return
		}
		payloadMap, ok := msg.Payload.(map[string]interface{})
		if !ok || h.rdb == nil {
			return
		}

		// Enforce client.room as the only source of truth for exam ID to prevent cross-exam spoofing
		examIDStr := client.room
		examID, err := strconv.Atoi(examIDStr)
		if err != nil || examID == 0 {
			return
		}

		// Sanitize every client-supplied field before persisting / broadcasting:
		// the payload is attacker-controlled and is consumed by the monitoring
		// dashboards.
		macAddress := sanitizeWSMac(wsString(payloadMap, "mac_address"))
		if macAddress == "" {
			return
		}
		studentName := sanitizeWSField(wsString(payloadMap, "student_name"), 200)
		examNumber := sanitizeWSField(wsString(payloadMap, "exam_number"), 100)
		studentClass := sanitizeWSField(wsString(payloadMap, "student_class"), 100)
		deviceInfo := sanitizeWSField(wsString(payloadMap, "device_info"), 200)

		heartbeatData := map[string]interface{}{
			"student_name":  studentName,
			"exam_number":   examNumber,
			"student_class": studentClass,
			"device_info":   deviceInfo,
			"event":         "heartbeat",
			"last_seen":     time.Now().UTC().Format(time.RFC3339),
		}

		key := fmt.Sprintf("heartbeat:%d:%s", examID, macAddress)
		payloadBytes, err := json.Marshal(heartbeatData)
		if err == nil {
			ctx := context.Background()
			_ = h.rdb.Set(ctx, key, payloadBytes, 5*time.Minute).Err()

			// Push to pending queue for database sync
			heartbeatData["exam_id"] = examID
			heartbeatData["mac_address"] = macAddress
			if queuePayload, err := json.Marshal(heartbeatData); err == nil {
				_ = h.rdb.LPush(ctx, "examvan:heartbeats:pending", queuePayload).Err()
			}
		}

		// Broadcast a SANITIZED snapshot only — never the raw client payload:
		// device_info is diagnostic and not needed by the monitoring
		// dashboards, so it is deliberately NOT broadcast (it would leak to
		// every token-holder in the room), and every field is trimmed/capped.
		h.BroadcastToRoom(client.room, "student_update", map[string]interface{}{
			"student_name":  studentName,
			"exam_number":   examNumber,
			"student_class": studentClass,
			"event":         "heartbeat",
			"last_seen":     time.Now().UTC().Format(time.RFC3339),
			"exam_id":       examIDStr,
			"mac_address":   macAddress,
		})

	case "exam_completed":
		if !client.privileged {
			// Same gate as heartbeat: a token-holder must not be able to delete
			// another device's heartbeat (making a classmate look offline to
			// the monitoring dashboard) or broadcast a fake completion.
			log.Printf("websocket: ignoring exam_completed from non-privileged client in room %s", client.room)
			return
		}
		payloadMap, ok := msg.Payload.(map[string]interface{})
		if !ok || h.rdb == nil {
			return
		}

		// Enforce client.room as the only source of truth for exam ID to prevent cross-exam spoofing
		examIDStr := client.room
		examID, err := strconv.Atoi(examIDStr)
		if err != nil || examID == 0 {
			return
		}

		macAddress := sanitizeWSMac(wsString(payloadMap, "mac_address"))
		if macAddress == "" {
			return
		}

		// Delete heartbeat from Redis so student shows offline immediately
		key := fmt.Sprintf("heartbeat:%d:%s", examID, macAddress)
		_ = h.rdb.Del(context.Background(), key).Err()

		// Broadcast a sanitized completion update (device_info and other
		// attacker-controlled fields are never echoed back to the room).
		h.BroadcastToRoom(client.room, "student_update", map[string]interface{}{
			"event":       "exam_completed",
			"exam_id":     examIDStr,
			"mac_address": macAddress,
		})
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// JoinRoom registers a WebSocket connection into a room and returns a Client
// ready to receive messages. roomID is typically the exam ID (stringified).
// JoinRoom registers a WebSocket connection into a room and returns a Client
// ready to receive messages. roomID is typically the exam ID (stringified).
// privileged marks a session-authenticated client (admin / pengawas / operator):
// only those may send mutating events (heartbeat / exam_completed).
// Token-authenticated clients hold a token SHARED by the whole class in static
// mode, so they are receive-only (ping + room broadcasts) to keep one student
// from injecting phantom heartbeats or deleting a classmate's presence.
func (h *Hub) JoinRoom(w http.ResponseWriter, r *http.Request, roomID string, privileged bool) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		conn:       conn,
		send:       make(chan []byte, 256),
		room:       roomID,
		hub:        h,
		privileged: privileged,
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
	h.broadcast <- &roomMessage{room: roomID, message: data}
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
