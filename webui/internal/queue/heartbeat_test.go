package queue

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// M8 — worker heartbeat freshness
//
// The heartbeat used to be written only inside flushBatch, so a healthy but
// IDLE worker (empty queue — the normal state outside school hours) let the
// key expire and GET /admin/api/queue/status reported WorkerActive=false: a
// false "worker dead" alarm that led admins to restart the service. The
// contract:
//
//   - a running worker keeps worker_heartbeat alive even with zero jobs —
//     the heartbeat ticker refreshes it every workerHeartbeatInterval;
//   - Stop removes the key immediately, so the status flips to inactive
//     right after a real shutdown instead of pinning a stale stamp until
//     the TTL lapses.
// ---------------------------------------------------------------------------

func startHeartbeatWorker(t *testing.T) (*miniredis.Miniredis, *Worker) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	prevInterval, prevTTL := workerHeartbeatInterval, workerHeartbeatTTL
	workerHeartbeatInterval = 20 * time.Millisecond
	workerHeartbeatTTL = 100 * time.Millisecond
	t.Cleanup(func() { workerHeartbeatInterval, workerHeartbeatTTL = prevInterval, prevTTL })

	return mr, StartWorker(rdb, nil)
}

func waitHeartbeat(t *testing.T, mr *miniredis.Miniredis) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mr.TTL(workerHeartbeatKey) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("worker_heartbeat key never appeared")
}

func TestWorkerHeartbeatStaysFreshWhileIdle(t *testing.T) {
	mr, w := startHeartbeatWorker(t)
	defer w.Stop()

	waitHeartbeat(t, mr)

	// With a compressed TTL of 100ms refreshed every 20ms, a write-once
	// heartbeat (the old flushBatch-only behavior) dies within one TTL,
	// while a healthy idle worker must keep it alive indefinitely.
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := mr.TTL(workerHeartbeatKey); got <= 0 {
			t.Fatalf("worker_heartbeat expired while worker idle — admin status flips to WorkerActive=false (false 'worker dead' alarm)")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWorkerStopClearsHeartbeatKey(t *testing.T) {
	mr, w := startHeartbeatWorker(t)
	waitHeartbeat(t, mr)

	w.Stop()

	if got := mr.TTL(workerHeartbeatKey); got > 0 {
		t.Fatalf("worker_heartbeat still alive %v after Stop — stale 'active' stamp pinned until TTL expiry", got)
	}
}
