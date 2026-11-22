package admin

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/websocket"
)

// examScheduleJobInterval is how often the automatic schedule pass runs.
// The tolerance is expressed in minutes, so 15s keeps the automatic start/stop
// close enough to the configured window without busy-looping.
const examScheduleJobInterval = 15 * time.Second

// examScheduleJobAdvisoryLockKey serializes the pass across replicas: only one
// instance auto-starts/stops at a time. The lock is session-level on a
// dedicated pooled connection and released when the pass ends.
const examScheduleJobAdvisoryLockKey int64 = 0x45534348 // "ESCH"

// StartExamScheduleJob runs a background loop that:
//   - auto-starts exams when now >= start_time - tolerance (unless the schedule
//     was already consumed by a manual/automatic start), and
//   - auto-stops running exams when now >= end_time + tolerance, broadcasting
//     exam_terminated so clients auto-submit exactly like a manual stop.
//
// The pass runs immediately, then every examScheduleJobInterval. It stops when
// ctx is canceled during graceful shutdown. Safe to call with a nil pool.
func StartExamScheduleJob(ctx context.Context, pool *pgxpool.Pool, hub *websocket.Hub) {
	if pool == nil {
		return
	}

	go func() {
		runExamSchedulePass(ctx, pool, hub)
		ticker := time.NewTicker(examScheduleJobInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("exam-schedule job: stopped")
				return
			case <-ticker.C:
				runExamSchedulePass(ctx, pool, hub)
			}
		}
	}()
	log.Printf("exam-schedule job: started (every %s)", examScheduleJobInterval)
}

// runExamSchedulePass stops expired running exams first (freeing concurrent
// slots), then starts exams whose tolerance window is open.
func runExamSchedulePass(ctx context.Context, pool *pgxpool.Pool, hub *websocket.Hub) {
	if pool == nil {
		return
	}
	lockConn, ok := acquireExamScheduleLock(ctx, pool)
	if !ok {
		return
	}
	defer releaseExamScheduleLock(lockConn)

	now := time.Now().UTC()

	stops, err := models.ListExamsToAutoStop(ctx, pool, now)
	if err != nil {
		log.Printf("exam-schedule job: list auto-stop: %v", err)
	} else {
		for _, exam := range stops {
			tag, err := pool.Exec(ctx, `UPDATE exams
				SET status = 'inactive', exam_started_at = NULL
				WHERE id = $1 AND status = 'active' AND exam_started_at IS NOT NULL`, exam.ID)
			if err != nil {
				log.Printf("exam-schedule job: auto-stop exam %d: %v", exam.ID, err)
				continue
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			log.Printf("exam-schedule job: auto-stopped exam %d (end_time + tolerance reached)", exam.ID)
			if hub != nil {
				hub.BroadcastToRoom(strconv.Itoa(exam.ID), "exam_terminated", map[string]interface{}{
					"exam_id": exam.ID,
					"message": "Ujian dihentikan otomatis sesuai jadwal",
				})
			}
		}
	}

	starts, err := models.ListExamsToAutoStart(ctx, pool, now)
	if err != nil {
		log.Printf("exam-schedule job: list auto-start: %v", err)
		return
	}
	for i := range starts {
		exam := &starts[i]
		status, msg := startExamCore(ctx, pool, exam, false)
		if status != 200 {
			// Quota full or a concurrent manual action: retry next tick. The
			// exam remains eligible until its window closes.
			log.Printf("exam-schedule job: auto-start exam %d skipped: %s", exam.ID, msg)
			continue
		}
		log.Printf("exam-schedule job: auto-started exam %d (start_time - tolerance reached)", exam.ID)
	}
}

func acquireExamScheduleLock(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, bool) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		log.Printf("exam-schedule job: acquire lock connection: %v", err)
		return nil, false
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, examScheduleJobAdvisoryLockKey).Scan(&locked); err != nil {
		log.Printf("exam-schedule job: try advisory lock: %v", err)
		conn.Release()
		return nil, false
	}
	if !locked {
		conn.Release()
		return nil, false
	}
	return conn, true
}

func releaseExamScheduleLock(conn *pgxpool.Conn) {
	if conn == nil {
		return
	}
	_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, examScheduleJobAdvisoryLockKey)
	conn.Release()
}
