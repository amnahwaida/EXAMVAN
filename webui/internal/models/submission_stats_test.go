package models

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Stats-stability contract for ListSubmissionsByExam (skipped when
// TEST_DATABASE_URL is unset — same infra as submission_test.go via
// setupAuthTestDB).
//
// The Stats cards on the monitoring page are an exam-level header: they stay
// stable while the operator searches/filters/pages the device list (the same
// contract the public HasilAPI documents for its stats). This test pins that
// the filter independence is deliberate — a future "fix" that scopes Stats to
// the active filter would make the header cards jump around and must be a
// conscious product decision, not an accident.
// ---------------------------------------------------------------------------

// TestListSubmissionsStatsAreFilterIndependent seeds one submitted and one
// in-progress device, then asserts the list honours Status/Search while
// Stats still reflects the full exam set.
func TestListSubmissionsStatsAreFilterIndependent(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-stats-guru", Name: "Guru Stats",
		PasswordHash: "pass", Status: UserStatusActive,
		Role: SerializeRoles([]string{RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	examID := setupSubmissionTestExam(t, pool, owner.ID)

	startedAt := time.Now().Format(time.RFC3339)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
		VALUES ($1, 'AA:AA:AA:AA:AA:01', 'Siswa Sudah Kumpul', '01', 'XII A', '{"1":"A"}', 80, $2, $3)`,
		examID, startedAt, time.Now()); err != nil {
		t.Fatalf("seed submitted row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, start_time, created_at)
		VALUES ($1, 'AA:AA:AA:AA:AA:02', 'Siswa Masih Mengerjakan', '02', 'XII A', $2, $3)`,
		examID, startedAt, time.Now()); err != nil {
		t.Fatalf("seed in-progress row: %v", err)
	}

	// Unfiltered: list and stats agree.
	full, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{ExamID: examID, Page: 1, PerPage: 25})
	if err != nil {
		t.Fatalf("list unfiltered: %v", err)
	}
	if full.Total != 2 {
		t.Errorf("unfiltered total = %d, want 2", full.Total)
	}

	// Status filter narrows the LIST only.
	submitted, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{ExamID: examID, Page: 1, PerPage: 25, Status: "submitted"})
	if err != nil {
		t.Fatalf("list submitted: %v", err)
	}
	if submitted.Total != 1 {
		t.Errorf("status=submitted total = %d, want 1", submitted.Total)
	}
	if submitted.Stats.Total != 2 || submitted.Stats.Submitted != 1 || submitted.Stats.InProgress != 1 {
		t.Errorf("stats under status filter = %+v, want stable {Total:2 Submitted:1 InProgress:1}", submitted.Stats)
	}

	// A search matching nothing empties the list but not the header.
	empty, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{ExamID: examID, Page: 1, PerPage: 25, Search: "tidak-ada-nama-ini"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if empty.Total != 0 {
		t.Errorf("empty-search total = %d, want 0", empty.Total)
	}
	if empty.Stats.Total != 2 {
		t.Errorf("stats under empty search total = %d, want stable 2", empty.Stats.Total)
	}
}
