package database

import (
	"context"
	"testing"
	"time"
)

// The heartbeat flusher used to insert a fresh open placeholder on every
// heartbeat while the latest row was still open. The one-time migration must
// remove those stale open rows (keeping the newest live attempt) without
// touching submitted attempts or a different student's group.
func TestSchemaDedupeOpenSubmissionPlaceholders(t *testing.T) {
	pool := NewPackageTestPool(t, "database")
	ctx := context.Background()

	var ownerID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO admin_users (username, name, password_hash, status, role)
		VALUES ('dedupe-owner', 'dedupe-owner', 'x', 'active', '["guru"]')
		RETURNING id`).Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Dedupe', 'd.pdf', 1, 'DEDUPE1', 'DEDUPE1', 'active', 'medium', $1)
		RETURNING id`, ownerID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	base := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	insert := func(mac, examNumber string, answers *string, at time.Time) int {
		t.Helper()
		var id int
		if err := pool.QueryRow(ctx, `
			INSERT INTO submissions (exam_id, mac_address, exam_number, student_name, answers_json, created_at)
			VALUES ($1, $2, $3, 'Siswa', $4, $5)
			RETURNING id`, examID, mac, examNumber, answers, at).Scan(&id); err != nil {
			t.Fatalf("insert submission: %v", err)
		}
		return id
	}

	// Group 1: two stale open rows + a newer submitted attempt → no open left.
	insert("AA:00:00:00:00:01", "01", nil, base)
	insert("AA:00:00:00:00:01", "01", nil, base.Add(time.Minute))
	submitted := `{"1":"A"}`
	insert("AA:00:00:00:00:01", "01", &submitted, base.Add(2*time.Minute))

	// Group 2: two open rows, no submit → keep only the newest open row.
	insert("AA:00:00:00:00:02", "01", nil, base)
	keepID := insert("AA:00:00:00:00:02", "01", nil, base.Add(time.Minute))

	// Group 3: another student on the same device → must be untouched.
	otherID := insert("AA:00:00:00:00:02", "02", nil, base.Add(3*time.Minute))

	if _, err := pool.Exec(ctx,
		`DELETE FROM schema_migrations WHERE name = 'dedupe_open_submission_placeholders'`); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := ApplySchema(ctx, pool); err != nil {
		t.Fatalf("re-apply schema: %v", err)
	}

	var openGroup1 int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM submissions
		WHERE exam_id=$1 AND mac_address='AA:00:00:00:00:01' AND exam_number='01'
		  AND (answers_json IS NULL OR answers_json='')`, examID).Scan(&openGroup1); err != nil {
		t.Fatalf("count group 1: %v", err)
	}
	if openGroup1 != 0 {
		t.Errorf("group 1 open rows = %d, want 0 (newer submitted attempt makes old opens stale)", openGroup1)
	}

	var openGroup2 int
	var kept int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MAX(id),0) FROM submissions
		WHERE exam_id=$1 AND mac_address='AA:00:00:00:00:02' AND exam_number='01'
		  AND (answers_json IS NULL OR answers_json='')`, examID).Scan(&openGroup2, &kept); err != nil {
		t.Fatalf("count group 2: %v", err)
	}
	if openGroup2 != 1 || kept != keepID {
		t.Errorf("group 2 open rows = %d (kept id %d), want exactly the newest open row %d", openGroup2, kept, keepID)
	}

	var otherCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE id=$1`, otherID).Scan(&otherCount); err != nil {
		t.Fatalf("count other student: %v", err)
	}
	if otherCount != 1 {
		t.Errorf("other student's open row was deleted — different exam_number must be its own group")
	}
}
