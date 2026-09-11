package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// M5 — heartbeat flusher
//
// Two defects (review_web_flow_dan_dead_code.md, Bagian 4.1 M5 / 6.2 #8):
//
//   1. Two webui instances behind the same Redis flush the same
//      heartbeat list concurrently. drainHeartbeatsQueue /
//      flushHeartbeatBatch had NO instance-level lock, so both instances
//      RPop the same batch... actually both pop DIFFERENT items but the
//      DB write path is a check-then-insert whose races are only guarded
//      per (exam, mac) — with the instance lock missing the two flushers
//      can interleave a check-then-insert for the SAME student (instance
//      A checks "no open row", instance B checks "no open row", both
//      INSERT → double placeholder row in Monitoring Perangkat).
//
//   2. flushHeartbeatBatch resurrects a SUBMITTED student: when a
//      student's latest row already carries answers (they submitted),
//      the lookup finds a row with non-empty answers_json, takes the
//      "insert a new empty row" branch and plants a ghost placeholder —
//      the student shows as "in-progress" after finishing. The other
//      three submission write paths never do this: they serialise on the
//      "approval:<exam_id>:<mac>" advisory lock and target the latest
//      row regardless of answers.
//
// The intended contract:
//
//   - flushHeartbeatBatch runs under the instance lock
//     "heartbeat-flusher" (pg TryAdvisoryLock): a second caller while
//     the lock is held flushes NOTHING (0 popped) and leaves the queue
//     intact for the holder / next tick.
//   - a heartbeat for a student whose latest row is already submitted
//     (answers_json non-empty) must NOT create a new row; the audit
//     (student_access_logs) row is still written.
// ---------------------------------------------------------------------------

// pushHeartbeat marshals and LPUSHes one heartbeat payload.
func pushHeartbeat(t *testing.T, rdb *goredis.Client, examID int, mac, event string) {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"exam_id":       examID,
		"mac_address":   mac,
		"student_name":  "Siswa M5",
		"exam_number":   "05",
		"student_class": "XII A",
		"device_info":   "Test",
		"ip_address":    "192.0.2.9",
		"event":         event,
		"last_seen":     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if err := rdb.LPush(context.Background(), heartbeatFlushQueueKey, payload).Err(); err != nil {
		t.Fatalf("push heartbeat: %v", err)
	}
}

// setupM5FlusherDB returns the package test pool (skips without
// TEST_DATABASE_URL) plus a seeded exam owned by a fresh guru.
func setupM5FlusherDB(t *testing.T) (*pgxpool.Pool, int) {
	t.Helper()
	pool := database.NewPackageTestPool(t, "server")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_users (username, name, password_hash, status, role)
		VALUES ('m5-flusher-guru', 'm5-flusher-guru', 'x', 'active', '["guru"]')
		ON CONFLICT (username) DO NOTHING`); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var ownerID int
	if err := pool.QueryRow(ctx, `SELECT id FROM admin_users WHERE username='m5-flusher-guru'`).Scan(&ownerID); err != nil {
		t.Fatalf("get owner: %v", err)
	}
	token := fmt.Sprintf("M5%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian M5', 'm5.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, ownerID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return pool, examID
}

// submittedSubmission inserts an already-SUBMITTED row (answers present) for
// (exam, mac).
func submittedSubmission(t *testing.T, pool *pgxpool.Pool, examID int, mac string) {
	t.Helper()
	now := time.Now().Format(time.RFC3339)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class, mac_address, answers_json, score, start_time, created_at, identity_data)
		VALUES ($1, 'Siswa M5', '05', 'XII A', $2, '{"1":"A"}', 90, $3, $4, '{}')`,
		examID, mac, now, time.Now()); err != nil {
		t.Fatalf("insert submitted submission: %v", err)
	}
}

// TestFlushHeartbeatBatchInstanceTryLock pins the instance-level lock: while
// the "heartbeat-flusher" advisory lock is held by another instance, a
// flushHeartbeatBatch call must flush NOTHING and leave the queue intact —
// never pop payloads it cannot durably process.
func TestFlushHeartbeatBatchInstanceTryLock(t *testing.T) {
	pool, examID := setupM5FlusherDB(t)
	ctx := context.Background()

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	pushHeartbeat(t, rdb, examID, "AA:BB:CC:DD:EE:01", "heartbeat")

	// Hold the lock "as another webui instance" for the whole call.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('heartbeat-flusher')::bigint)`).Scan(&got); err != nil || !got {
		t.Fatalf("acquire instance lock: got=%v err=%v", got, err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('heartbeat-flusher')::bigint)`)
	}()

	if n := flushHeartbeatBatch(ctx, rdb, pool); n != 0 {
		t.Fatalf("flushed %d payloads while another instance holds the lock, want 0", n)
	}
	if n, _ := rdb.LLen(ctx, heartbeatFlushQueueKey).Result(); n != 1 {
		t.Fatalf("queue len after locked flush = %d, want 1 (payloads must stay queued)", n)
	}
}

// TestFlushHeartbeatBatchNoGhostRowAfterSubmit pins the same-student
// resurrection bug: a heartbeat arriving AFTER the student's submission must
// not plant a fresh empty row — the student stays "submitted" in Monitoring
// Perangkat. The audit log row is still recorded.
func TestFlushHeartbeatBatchNoGhostRowAfterSubmit(t *testing.T) {
	pool, examID := setupM5FlusherDB(t)
	ctx := context.Background()

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	submittedSubmission(t, pool, examID, "AA:BB:CC:DD:EE:02")
	pushHeartbeat(t, rdb, examID, "AA:BB:CC:DD:EE:02", "heartbeat")

	if n := flushHeartbeatBatch(ctx, rdb, pool); n != 1 {
		t.Fatalf("flushed %d payloads, want 1", n)
	}

	var total, open int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN answers_json IS NULL OR answers_json = '' THEN 1 ELSE 0 END), 0)
		FROM submissions WHERE exam_id=$1`, examID).Scan(&total, &open); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if total != 1 || open != 0 {
		t.Fatalf("submissions after post-submit heartbeat: total=%d open=%d, want total=1 open=0 (M5: no ghost in-progress row after submit)", total, open)
	}

	// The audit insert still happened for the late heartbeat.
	var auditRows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM student_access_logs WHERE exam_id=$1 AND student_identifier=$2`,
		examID, "AA:BB:CC:DD:EE:02").Scan(&auditRows); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows = %d, want 1 (late heartbeats are still audited)", auditRows)
	}
}

// TestDrainHeartbeatsQueueStillDelivers guards the happy path against the new
// locking: with NO competing lock, a queued heartbeat must land in
// student_access_logs AND create the placeholder row for an unknown student.
func TestDrainHeartbeatsQueueStillDelivers(t *testing.T) {
	pool, examID := setupM5FlusherDB(t)
	ctx := context.Background()

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	pushHeartbeat(t, rdb, examID, "AA:BB:CC:DD:EE:03", "heartbeat")

	drainHeartbeatsQueue(ctx, rdb, pool)

	var auditRows, subRows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM student_access_logs WHERE exam_id=$1 AND student_identifier=$2`,
		examID, "AA:BB:CC:DD:EE:03").Scan(&auditRows); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows = %d, want 1", auditRows)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address=$2`,
		examID, "AA:BB:CC:DD:EE:03").Scan(&subRows); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if subRows != 1 {
		t.Fatalf("placeholder rows = %d, want 1 (heartbeat still creates the monitoring row)", subRows)
	}
}

// Compile-time guard: the flusher must remain wired to the models-independent
// DB path (no accidental import cycles).
var _ = models.SetSaasSetting
