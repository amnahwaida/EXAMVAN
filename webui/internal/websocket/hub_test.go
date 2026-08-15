package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	redis "github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Direct-client helpers (no real socket): register a Client straight into the
// hub's room map and drive handleClientMessage like readPump would.
// ---------------------------------------------------------------------------

func newDirectClient(h *Hub, room string, privileged bool) *Client {
	c := &Client{
		send:       make(chan []byte, 256),
		room:       room,
		hub:        h,
		privileged: privileged,
	}
	h.register <- c
	// The register send is received by Run before addClient commits the client
	// to the room map; wait for actual membership so later broadcasts are
	// deterministic.
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.RLock()
		_, present := h.rooms[room][c]
		h.mu.RUnlock()
		if present || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return c
}

// readMsg waits up to 2s for a broadcast on the client's send channel.
func readMsg(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case msg := <-c.send:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for room broadcast")
		return nil
	}
}

// assertNoMsg fails if a broadcast arrives within 200ms.
func assertNoMsg(t *testing.T, c *Client) {
	t.Helper()
	select {
	case msg := <-c.send:
		t.Fatalf("unexpected broadcast received: %s", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

// decodeMsg parses a marshaled SocketIO message back into event + payload.
func decodeMsg(t *testing.T, raw []byte) (string, map[string]interface{}) {
	t.Helper()
	msg, ok := ParseSocketIO(raw)
	if !ok {
		t.Fatalf("cannot parse broadcast %q", raw)
	}
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("payload not a map: %#v", msg.Payload)
	}
	return msg.Event, payload
}

// ---------------------------------------------------------------------------
// Room isolation + broadcast
// ---------------------------------------------------------------------------

// TestHubRoomIsolation locks the tenant boundary: a broadcast to one exam room
// must never reach a client in another room (one tenant must not receive
// another tenant's live student broadcasts).
func TestHubRoomIsolation(t *testing.T) {
	h := NewHub(nil)
	go h.Run()

	roomA := newDirectClient(h, "1", true)
	roomB := newDirectClient(h, "2", true)

	if h.GetRoomSize("1") != 1 || h.GetRoomSize("2") != 1 {
		t.Fatalf("room sizes = %d/%d, want 1/1", h.GetRoomSize("1"), h.GetRoomSize("2"))
	}

	h.BroadcastToRoom("1", "student_update", map[string]interface{}{"student_name": "Siswa A"})

	event, payload := decodeMsg(t, readMsg(t, roomA))
	if event != "student_update" || payload["student_name"] != "Siswa A" {
		t.Errorf("room A broadcast = %s %#v, want student_update Siswa A", event, payload)
	}
	assertNoMsg(t, roomB)
}

// ---------------------------------------------------------------------------
// Privileged gate (F1a): token-authed (non-privileged) clients are
// receive-only — heartbeat / exam_completed are ignored.
// ---------------------------------------------------------------------------

// TestPrivilegedGateHeartbeat locks the gate: a non-privileged (token-holder)
// client's heartbeat is dropped — no Redis presence write, no broadcast to the
// room. A privileged client's heartbeat lands in Redis and is broadcast.
func TestPrivilegedGateHeartbeat(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	h := NewHub(rdb)
	go h.Run()

	tokenClient := newDirectClient(h, "10", false) // any token holder (shared in static mode)
	privClient := newDirectClient(h, "10", true)   // session-authed pengawas/operator

	// (a) Non-privileged heartbeat → fully ignored.
	tokenClient.hub.handleClientMessage(tokenClient, SocketIOMessage{
		Event: "heartbeat",
		Payload: map[string]interface{}{
			"mac_address":  "AA:BB",
			"student_name": "Penyusup",
		},
	})
	if mr.Exists("heartbeat:10:AA:BB") {
		t.Error("non-privileged heartbeat must NOT write Redis presence")
	}
	assertNoMsg(t, privClient)
	assertNoMsg(t, tokenClient)

	// (b) Privileged heartbeat → Redis presence + broadcast to the room.
	privClient.hub.handleClientMessage(privClient, SocketIOMessage{
		Event: "heartbeat",
		Payload: map[string]interface{}{
			"mac_address":  "AA:BB",
			"student_name": "Siswa Asli",
			"exam_number":  "5",
		},
	})
	if !mr.Exists("heartbeat:10:AA:BB") {
		t.Error("privileged heartbeat must write Redis presence")
	}
	event, payload := decodeMsg(t, readMsg(t, tokenClient))
	if event != "student_update" || payload["event"] != "heartbeat" {
		t.Errorf("privileged broadcast = %s %#v, want student_update heartbeat", event, payload)
	}
	if payload["student_name"] != "Siswa Asli" {
		t.Errorf("broadcast student_name = %#v, want Siswa Asli", payload["student_name"])
	}
}

// TestPrivilegedGateExamCompleted locks the gate on the destructive event: a
// non-privileged client must not be able to delete a classmate's heartbeat
// (making them look offline) nor broadcast a fake completion.
func TestPrivilegedGateExamCompleted(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	h := NewHub(rdb)
	go h.Run()

	tokenClient := newDirectClient(h, "10", false)
	privClient := newDirectClient(h, "10", true)

	// Seed a live presence for the victim device.
	rdb.Set(context.Background(), "heartbeat:10:AA:BB", `{"student_name":"Korban"}`, 5*time.Minute)

	// (a) Non-privileged exam_completed → ignored: presence survives, no broadcast.
	tokenClient.hub.handleClientMessage(tokenClient, SocketIOMessage{
		Event:   "exam_completed",
		Payload: map[string]interface{}{"mac_address": "AA:BB"},
	})
	if !mr.Exists("heartbeat:10:AA:BB") {
		t.Error("non-privileged exam_completed must NOT delete another device's presence")
	}
	assertNoMsg(t, privClient)

	// (b) Privileged exam_completed → presence deleted + broadcast.
	privClient.hub.handleClientMessage(privClient, SocketIOMessage{
		Event:   "exam_completed",
		Payload: map[string]interface{}{"mac_address": "AA:BB"},
	})
	if mr.Exists("heartbeat:10:AA:BB") {
		t.Error("privileged exam_completed must delete the device's presence")
	}
	event, payload := decodeMsg(t, readMsg(t, tokenClient))
	if event != "student_update" || payload["event"] != "exam_completed" {
		t.Errorf("privileged completion broadcast = %s %#v", event, payload)
	}
}

// ---------------------------------------------------------------------------
// Payload sanitization (F1b)
// ---------------------------------------------------------------------------

// TestHeartbeatSanitization locks the sanitization boundary: client-supplied
// fields are scrubbed of markup before persisting to Redis and before
// broadcast, and device_info (diagnostic-only, never needed by dashboards) is
// NOT broadcast — it would leak to every token-holder in the room.
func TestHeartbeatSanitization(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	h := NewHub(rdb)
	go h.Run()

	receiver := newDirectClient(h, "10", false)
	sender := newDirectClient(h, "10", true)

	sender.hub.handleClientMessage(sender, SocketIOMessage{
		Event: "heartbeat",
		Payload: map[string]interface{}{
			"mac_address":  "AA:BB; DROP TABLE siswa",
			"student_name": "<script>alert(1)</script>Budi",
			"exam_number":  "5\" onmouseover=\"x",
			"student_class": "9A`+`",
			"device_info":  "<img src=x onerror=alert(2)>Pixel 9",
		},
	})

	// Redis presence must be the sanitized snapshot, no markup.
	stored, err := mr.Get("heartbeat:10:AA:BBDROPTABLEsiswa")
	if err != nil {
		t.Fatalf("redis presence missing: %v", err)
	}
	var storedMap map[string]interface{}
	if err := json.Unmarshal([]byte(stored), &storedMap); err != nil {
		t.Fatalf("redis presence not JSON: %v", err)
	}
	for k, v := range storedMap {
		if s, ok := v.(string); ok && strings.ContainsAny(s, "<>\"'`=;&") {
			t.Errorf("redis %s carries markup: %q", k, s)
		}
	}
	name, _ := storedMap["student_name"].(string)
	if !strings.Contains(name, "Budi") {
		t.Errorf("redis student_name = %#v, want a value containing Budi with markup stripped", storedMap["student_name"])
	}

	// Broadcast must carry the sanitized snapshot, never raw payload fields.
	_, payload := decodeMsg(t, readMsg(t, receiver))
	for k, v := range payload {
		if s, ok := v.(string); ok && strings.ContainsAny(s, "<>\"'`=;&") {
			t.Errorf("broadcast %s carries markup: %q", k, s)
		}
	}
	if _, leaked := payload["device_info"]; leaked {
		t.Error("device_info must NOT be broadcast (leaks to token holders)")
	}
	bcastName, _ := payload["student_name"].(string)
	if !strings.Contains(bcastName, "Budi") {
		t.Errorf("broadcast student_name = %#v, want a value containing Budi", payload["student_name"])
	}
	if payload["mac_address"] != "AA:BBDROPTABLEsiswa" {
		t.Errorf("broadcast mac_address = %#v, want sanitized AA:BBDROPTABLEsiswa", payload["mac_address"])
	}
}

// ---------------------------------------------------------------------------
// Ping / pong keep-alive
// ---------------------------------------------------------------------------

func TestPingPong(t *testing.T) {
	h := NewHub(nil)
	go h.Run()
	c := newDirectClient(h, "1", true)

	c.hub.handleClientMessage(c, SocketIOMessage{Event: "ping", Payload: nil})

	msg, ok := ParseSocketIO(readMsg(t, c))
	if !ok {
		t.Fatal("ping reply not parseable")
	}
	if msg.Event != "pong" {
		t.Errorf("ping reply = %s, want pong", msg.Event)
	}
}

// ---------------------------------------------------------------------------
// JoinRoom plumbing: the privileged flag set by the route must reach the
// Client. End-to-end over a real upgraded socket + miniredis.
// ---------------------------------------------------------------------------

func TestJoinRoomPrivilegedPlumbing(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	h := NewHub(rdb)
	go h.Run()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		priv := r.URL.Query().Get("priv") == "1"
		if err := h.JoinRoom(w, r, "7", priv); err != nil {
			t.Errorf("join room: %v", err)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Session-authed connection (privileged).
	privConn, _, err := websocket.DefaultDialer.Dial(wsURL+"?priv=1", nil)
	if err != nil {
		t.Fatalf("dial privileged: %v", err)
	}
	defer privConn.Close()
	// Token-authed connection (receive-only).
	tokenConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial token: %v", err)
	}
	defer tokenConn.Close()

	// Both dials return as soon as the 101 upgrade lands, which can race the
	// hub's register processing — poll until both clients are in the room.
	deadline := time.Now().Add(2 * time.Second)
	for h.GetRoomSize("7") != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("room 7 size = %d, want 2", h.GetRoomSize("7"))
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Let the hub process registration before the heartbeat lands.
	time.Sleep(50 * time.Millisecond)

	// Non-privileged heartbeat → must NOT reach Redis.
	if err := tokenConn.WriteMessage(websocket.TextMessage,
		[]byte(`["heartbeat",{"mac_address":"AA:BB","student_name":"Penyusup"}]`)); err != nil {
		t.Fatalf("token write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if mr.Exists("heartbeat:7:AA:BB") {
		t.Error("token-authed socket heartbeat reached Redis — privileged flag not wired")
	}

	// Privileged heartbeat → must reach Redis.
	if err := privConn.WriteMessage(websocket.TextMessage,
		[]byte(`["heartbeat",{"mac_address":"AA:BB","student_name":"Siswa Asli"}]`)); err != nil {
		t.Fatalf("privileged write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if !mr.Exists("heartbeat:7:AA:BB") {
		t.Error("session-authed socket heartbeat did not reach Redis — privileged flag not wired")
	}
}

