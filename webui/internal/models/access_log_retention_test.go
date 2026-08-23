package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// Retensi student_access_logs (Lapis 1 — kapasitas)
// ---------------------------------------------------------------------------
//
// Kontrak yang dikunci test ini:
//
//  1. Purge menghapus HANYA baris lebih tua dari jendela retensi — heartbeat
//     ujian yang masih baru tidak boleh ikut terhapus.
//  2. days <= 0 adalah saklar MATI (hapus nol baris), BUKAN "hapus semua" —
//     salah konfigurasi tidak boleh berarti wipe seluruh audit trail.

// insertRetentionAccessLog inserts one access-log row with an explicit
// created_at so retention windows are tested deterministically.
func insertRetentionAccessLog(t *testing.T, pool *pgxpool.Pool, examID int, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO student_access_logs (exam_id, event, created_at)
		VALUES ($1, 'heartbeat', $2)`,
		examID, time.Now().UTC().Add(-age)); err != nil {
		t.Fatalf("insert access log (age %s): %v", age, err)
	}
}

func countRetentionLogs(t *testing.T, pool *pgxpool.Pool, examID int) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM student_access_logs WHERE exam_id = $1`, examID).Scan(&n); err != nil {
		t.Fatalf("count access logs: %v", err)
	}
	return n
}

func TestPurgeOldStudentAccessLogsRemovesOnlyExpired(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: fmt.Sprintf("retention_owner_%d", time.Now().UnixNano()), Name: "Retention Owner", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("RET%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Retention Fixture', 'ret.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, creator.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	insertRetentionAccessLog(t, pool, examID, 200*24*time.Hour) // jauh kedaluwarsa
	insertRetentionAccessLog(t, pool, examID, 95*24*time.Hour)  // melewati jendela 90 hari
	insertRetentionAccessLog(t, pool, examID, 10*24*time.Hour)  // masih di dalam jendela
	if got := countRetentionLogs(t, pool, examID); got != 3 {
		t.Fatalf("fixture logs = %d, want 3", got)
	}

	purged, err := PurgeOldStudentAccessLogs(ctx, pool, 90)
	if err != nil {
		t.Fatalf("PurgeOldStudentAccessLogs: %v", err)
	}
	if purged != 2 {
		t.Fatalf("purged = %d, want 2", purged)
	}
	if got := countRetentionLogs(t, pool, examID); got != 1 {
		t.Fatalf("remaining logs = %d, want 1 (recent heartbeat must survive)", got)
	}
}

func TestPurgeOldStudentAccessLogsZeroDaysIsDisabled(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	creator, err := CreateUser(ctx, pool, &AdminUser{
		Username: fmt.Sprintf("retention_off_%d", time.Now().UnixNano()), Name: "Retention Off", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("ROF%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Retention Disabled', 'rof.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, creator.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	insertRetentionAccessLog(t, pool, examID, 365*24*time.Hour)

	for _, days := range []int{0, -1} {
		purged, err := PurgeOldStudentAccessLogs(ctx, pool, days)
		if err != nil {
			t.Fatalf("PurgeOldStudentAccessLogs(%d): %v", days, err)
		}
		if purged != 0 {
			t.Fatalf("PurgeOldStudentAccessLogs(%d) = %d rows, want 0 (disabled, never wipe-all)", days, purged)
		}
	}
	if got := countRetentionLogs(t, pool, examID); got != 1 {
		t.Fatalf("logs after disabled purge = %d, want 1", got)
	}
}
