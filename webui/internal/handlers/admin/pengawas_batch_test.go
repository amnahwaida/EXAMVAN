package admin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// Direct integration tests: fetchStudentAccessLogsBatch &
// fetchSubmissionHistoryBatch — the two batch fetchers behind the pengawas
// detail submissions list (they replaced the per-row N+1 queries). These
// drive the functions directly against a real database (no HTTP layer) so
// the invariants the N+1 fix relies on are pinned at the unit boundary:
// grouping by MAC, per-device chronological ordering (created_at ASC),
// field mapping, and per-exam scoping.
// ---------------------------------------------------------------------------

// insertBatchExam creates a second active exam (distinct token prefix) used to
// prove the batch fetchers never leak rows across exams.
func insertBatchExam(t *testing.T, pool *pgxpool.Pool, ownerID int) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian Batch Lain', 'batch2.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, fmt.Sprintf("BA%06d", time.Now().UnixNano()%1000000), ownerID).Scan(&id); err != nil {
		t.Fatalf("insert second exam: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// fetchStudentAccessLogsBatch
// ---------------------------------------------------------------------------

func TestFetchStudentAccessLogsBatchGroupsAndOrders(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	macA := "AA:BB:CC:DD:EE:A1"
	macB := "AA:BB:CC:DD:EE:A2"
	older := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 1, 9, 5, 0, 0, time.UTC)

	// Insert deliberately out of chronological order: each device's group must
	// come back ordered by created_at ASC.
	for _, row := range []struct {
		mac, event, ip, dev string
		at                  time.Time
	}{
		{macA, "heartbeat", "10.0.0.1", "Pixel 8", newer},
		{macB, "login", "10.0.0.2", "Galaxy A15", newer},
		{macA, "login", "10.0.0.1", "Pixel 8", older},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO student_access_logs (exam_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at)
			VALUES ($1, $2, 'Siswa A', '01', 'XII A', $3, $4, $5, $6)`,
			fx.ExamID, row.mac, row.event, row.ip, row.dev, row.at); err != nil {
			t.Fatalf("insert access log: %v", err)
		}
	}

	out := fetchStudentAccessLogsBatch(ctx, pool, fx.ExamID, []string{macA, macB})

	logsA := out[macA]
	if len(logsA) != 2 {
		t.Fatalf("logs for %s = %d, want 2", macA, len(logsA))
	}
	if logsA[0].Event != "login" || logsA[1].Event != "heartbeat" {
		t.Errorf("group order = [%s, %s], want [login, heartbeat] (created_at ASC)", logsA[0].Event, logsA[1].Event)
	}
	// created_at comes back as RFC3339; compare the instant, not the string,
	// so the assertion survives any DB session timezone.
	firstAt, err := time.Parse(time.RFC3339, logsA[0].CreatedAt)
	if err != nil {
		t.Fatalf("parse created_at %q: %v", logsA[0].CreatedAt, err)
	}
	if !firstAt.Equal(older) {
		t.Errorf("first log created_at = %v, want %v", firstAt, older)
	}
	first := logsA[0]
	if first.IPAddress != "10.0.0.1" || first.DeviceInfo != "Pixel 8" {
		t.Errorf("ip/device = %q/%q, want 10.0.0.1/Pixel 8", first.IPAddress, first.DeviceInfo)
	}
	if first.StudentName != "Siswa A" || first.ExamNumber != "01" || first.StudentClass != "XII A" {
		t.Errorf("student identity = %q/%q/%q, want Siswa A/01/XII A", first.StudentName, first.ExamNumber, first.StudentClass)
	}

	logsB := out[macB]
	if len(logsB) != 1 {
		t.Fatalf("logs for %s = %d, want 1", macB, len(logsB))
	}
	if logsB[0].Event != "login" {
		t.Errorf("event = %q, want login", logsB[0].Event)
	}
}

func TestFetchStudentAccessLogsBatchParsesIdentityData(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event, identity_data)
		VALUES ($1, $2, 'login', $3)`,
		fx.ExamID, "AA:BB:CC:DD:EE:A3", `{"student_name":"Siswa A","exam_number":"01","kelas":"XII A"}`); err != nil {
		t.Fatalf("insert access log with identity: %v", err)
	}
	// NULL identity_data must scan cleanly (IdentityData stays nil).
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event)
		VALUES ($1, $2, 'heartbeat')`, fx.ExamID, "AA:BB:CC:DD:EE:A4"); err != nil {
		t.Fatalf("insert access log without identity: %v", err)
	}

	out := fetchStudentAccessLogsBatch(ctx, pool, fx.ExamID, []string{"AA:BB:CC:DD:EE:A3", "AA:BB:CC:DD:EE:A4"})

	login := out["AA:BB:CC:DD:EE:A3"][0]
	if login.IdentityData == nil || login.IdentityData["student_name"] != "Siswa A" {
		t.Errorf("identity_data = %v, want parsed map with student_name=Siswa A", login.IdentityData)
	}
	nilLogs := out["AA:BB:CC:DD:EE:A4"]
	if len(nilLogs) != 1 || nilLogs[0].IdentityData != nil {
		t.Errorf("NULL identity_data log = %+v, want IdentityData nil", nilLogs)
	}
}

func TestFetchStudentAccessLogsBatchScopesByExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	otherExam := insertBatchExam(t, pool, fx.GuruID)

	mac := "AA:BB:CC:DD:EE:A5"
	for _, eid := range []int{fx.ExamID, otherExam} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO student_access_logs (exam_id, student_identifier, event)
			VALUES ($1, $2, 'login')`, eid, mac); err != nil {
			t.Fatalf("insert access log for exam %d: %v", eid, err)
		}
	}

	out := fetchStudentAccessLogsBatch(ctx, pool, fx.ExamID, []string{mac})
	if len(out[mac]) != 1 {
		t.Errorf("logs for %s = %d, want 1 — the other exam's log must not leak", mac, len(out[mac]))
	}
}

func TestFetchStudentAccessLogsBatchEmptyAndUnknown(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	// No MACs → no query runs and an empty map comes back.
	if out := fetchStudentAccessLogsBatch(ctx, pool, fx.ExamID, nil); len(out) != 0 {
		t.Errorf("empty macs returned %d groups, want 0", len(out))
	}

	mac := "AA:BB:CC:DD:EE:A6"
	if _, err := pool.Exec(ctx, `
		INSERT INTO student_access_logs (exam_id, student_identifier, event)
		VALUES ($1, $2, 'login')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert access log: %v", err)
	}

	out := fetchStudentAccessLogsBatch(ctx, pool, fx.ExamID, []string{mac, "AA:BB:CC:DD:EE:FF"})
	if len(out[mac]) != 1 {
		t.Errorf("logs for %s = %d, want 1", mac, len(out[mac]))
	}
	// An unknown MAC simply has no group (nil slice), never an error.
	if got := out["AA:BB:CC:DD:EE:FF"]; got != nil && len(got) != 0 {
		t.Errorf("unknown MAC got %d logs, want none", len(got))
	}
}

// ---------------------------------------------------------------------------
// fetchSubmissionHistoryBatch
// ---------------------------------------------------------------------------

func TestFetchSubmissionHistoryBatchGroupsAndOrders(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	macA := "AA:BB:CC:DD:EE:B1"
	macB := "AA:BB:CC:DD:EE:B2"
	older := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 2, 9, 10, 0, 0, time.UTC)

	score1, score2 := 70.0, 85.0
	answersB := `{"1":"b"}`
	for _, row := range []struct {
		mac, start string
		answers    *string
		score      *float64
		at         time.Time
	}{
		{macA, "2026-08-02 09:05:00", spT(`{"1":"a"}`), &score2, newer}, // second (completed) attempt
		{macB, "2026-08-02 09:01:00", &answersB, &score1, older},
		{macA, "2026-08-02 09:00:00", nil, nil, older}, // first attempt still open
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
			VALUES ($1, $2, 'Siswa B', '02', 'XI B', $3, $4, $5, $6)`,
			fx.ExamID, row.mac, row.answers, row.score, row.start, row.at); err != nil {
			t.Fatalf("insert submission: %v", err)
		}
	}

	hist := fetchSubmissionHistoryBatch(ctx, pool, fx.ExamID, []string{macA, macB})

	a := hist[macA]
	if len(a) != 2 {
		t.Fatalf("history for %s = %d, want 2 attempts", macA, len(a))
	}
	if !a[0].CreatedAt.Equal(older) || !a[1].CreatedAt.Equal(newer) {
		t.Errorf("group order = [%v, %v], want [older, newer] (created_at ASC)", a[0].CreatedAt, a[1].CreatedAt)
	}
	// First attempt is open: no answers/score yet.
	if a[0].AnswersJSON != nil || a[0].Score != nil {
		t.Errorf("open attempt answers/score = %v/%v, want nil", a[0].AnswersJSON, a[0].Score)
	}
	if a[1].AnswersJSON == nil || *a[1].AnswersJSON != `{"1":"a"}` || a[1].Score == nil || *a[1].Score != score2 {
		t.Errorf("completed attempt answers/score = %v/%v, want filled row", a[1].AnswersJSON, a[1].Score)
	}
	if a[1].StartTime == nil || *a[1].StartTime != "2026-08-02 09:05:00" {
		t.Errorf("start_time = %v, want 2026-08-02 09:05:00", a[1].StartTime)
	}

	b := hist[macB]
	if len(b) != 1 {
		t.Fatalf("history for %s = %d, want 1", macB, len(b))
	}
	if b[0].Score == nil || *b[0].Score != score1 {
		t.Errorf("score = %v, want %v", b[0].Score, score1)
	}
}

func TestFetchSubmissionHistoryBatchScopesByExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()
	otherExam := insertBatchExam(t, pool, fx.GuruID)

	mac := "AA:BB:CC:DD:EE:B3"
	for _, eid := range []int{fx.ExamID, otherExam} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, answers_json)
			VALUES ($1, $2, '{}')`, eid, mac); err != nil {
			t.Fatalf("insert submission for exam %d: %v", eid, err)
		}
	}

	hist := fetchSubmissionHistoryBatch(ctx, pool, fx.ExamID, []string{mac})
	if len(hist[mac]) != 1 {
		t.Errorf("history for %s = %d, want 1 — the other exam's submission must not leak", mac, len(hist[mac]))
	}
}

func TestFetchSubmissionHistoryBatchEmptyAndUnknown(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	if out := fetchSubmissionHistoryBatch(ctx, pool, fx.ExamID, nil); len(out) != 0 {
		t.Errorf("empty macs returned %d groups, want 0", len(out))
	}

	mac := "AA:BB:CC:DD:EE:B4"
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, answers_json)
		VALUES ($1, $2, '{}')`, fx.ExamID, mac); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	hist := fetchSubmissionHistoryBatch(ctx, pool, fx.ExamID, []string{mac, "AA:BB:CC:DD:EE:FF"})
	if len(hist[mac]) != 1 {
		t.Errorf("history for %s = %d, want 1", mac, len(hist[mac]))
	}
	if got := hist["AA:BB:CC:DD:EE:FF"]; got != nil && len(got) != 0 {
		t.Errorf("unknown MAC got %d rows, want none", len(got))
	}
}

// spT returns a pointer to s (test helper; no package-level collision).
func spT(s string) *string { return &s }
