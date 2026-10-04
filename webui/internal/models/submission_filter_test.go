package models

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Filter contracts for the "Monitoring Perangkat" table
// (skipped when TEST_DATABASE_URL is unset — same infra as submission_test.go
// via setupAuthTestDB).
//
// The status filter is the page's core triage control (Sedang Mengerjakan /
// Terkumpul / Belum Mulai). Two rules make it trustworthy:
//
//  1. It must classify each DEVICE by its latest attempt, never by an older
//     attempt. Filtering before the per-device dedup resurrects a device that
//     already submitted: its stale open placeholder still matches
//     "in_progress", so the supervisor sees a finished exam as still running.
//  2. It must agree with the stat cards, which are computed with the dedup
//     applied FIRST (GetSubmissionStats). A filter that disagrees with the
//     number printed directly above it is worse than no filter at all.
//
// Search must likewise run after the dedup, so a search never surfaces a
// superseded attempt (stale name, stale score) as if it were the current state.
// ---------------------------------------------------------------------------

// mkFilterExam inserts an exam owned by a fresh guru for filter tests.
func mkFilterExam(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	ctx := context.Background()
	owner, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-filter-guru", Name: "Guru Filter",
		PasswordHash: "pass", Status: UserStatusActive,
		Role: SerializeRoles([]string{RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("F%07d", time.Now().UnixNano()%10000000)
	var id int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ('Ujian Filter', 'f.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
		RETURNING id`, token, owner.ID).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return id
}

type filterRow struct {
	mac       string
	name      string
	startTime interface{}
}

func mkRow(t *testing.T, pool *pgxpool.Pool, examID int, mac, name string, answers *string, startTime *string, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class,
		                         answers_json, score, start_time, created_at, submitted_at)
		VALUES ($1, $2, $3, '01', 'XII A', $4::text,
		        CASE WHEN $4::text IS NULL THEN NULL ELSE 80 END,
		        $5::text, $6::timestamptz,
		        CASE WHEN $4::text IS NULL THEN NULL ELSE $6::timestamptz END)`,
		examID, mac, name, answers, startTime, createdAt); err != nil {
		t.Fatalf("insert submission %s: %v", name, err)
	}
}

// TestStatusFilterClassifiesLatestAttemptOnly pins rule 1: a device that
// submitted must never reappear under "Sedang Mengerjakan" because of its own
// superseded placeholder.
func TestStatusFilterClassifiesLatestAttemptOnly(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()
	examID := mkFilterExam(t, pool)

	started := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	t0 := time.Now().Add(-90 * time.Minute)
	t1 := time.Now().Add(-30 * time.Minute)

	// Device 1: placeholder first, then submitted. Latest state = SUBMITTED.
	mkRow(t, pool, examID, "AA:00:00:00:00:01", "Sudah Kumpul", nil, &started, t0)
	mkRow(t, pool, examID, "AA:00:00:00:00:01", "Sudah Kumpul", strptr(`{"1":"A"}`), &started, t1)

	// Device 2: genuinely in progress (open, started, no answers).
	mkRow(t, pool, examID, "AA:00:00:00:00:02", "Masih Mengerjakan", nil, &started, t1)

	// Device 3: approved but never started.
	mkRow(t, pool, examID, "AA:00:00:00:00:03", "Belum Mulai", nil, nil, t1)

	res, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Status: "in_progress",
	})
	if err != nil {
		t.Fatalf("list in_progress: %v", err)
	}
	for _, s := range res.Submissions {
		if s.MACAddress == "AA:00:00:00:00:01" {
			t.Errorf("device 1 already submitted but appeared under " +
				"status=in_progress — the status filter is applied BEFORE the " +
				"per-device latest-attempt dedup, so its stale placeholder matches")
		}
	}
	if res.Total != 1 {
		t.Errorf("status=in_progress total = %d, want 1 (only device 2)", res.Total)
	}
	if got := res.Submissions[0].MACAddress; got != "AA:00:00:00:00:02" {
		t.Errorf("status=in_progress returned %q, want device 2", got)
	}

	// Rule 2: the filter must agree with the stat card rendered above it.
	if res.Stats.InProgress != res.Total {
		t.Errorf("filter disagrees with the stat card: status=in_progress returned "+
			"%d rows but the 'Sedang Mengerjakan' card says %d — the supervisor sees "+
			"two different numbers for the same thing",
			res.Total, res.Stats.InProgress)
	}

	// The other two buckets must be self-consistent too.
	sub, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Status: "submitted",
	})
	if err != nil {
		t.Fatalf("list submitted: %v", err)
	}
	if sub.Total != 1 || sub.Submissions[0].MACAddress != "AA:00:00:00:00:01" {
		t.Errorf("status=submitted = %d rows (first %q), want 1 (device 1)",
			sub.Total, firstMAC(sub.Submissions))
	}
	ns, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Status: "not_started",
	})
	if err != nil {
		t.Fatalf("list not_started: %v", err)
	}
	if ns.Total != 1 || ns.Submissions[0].MACAddress != "AA:00:00:00:00:03" {
		t.Errorf("status=not_started = %d rows (first %q), want 1 (device 3)",
			ns.Total, firstMAC(ns.Submissions))
	}

	// Unfiltered: all three devices, exactly once each.
	all, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25,
	})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if all.Total != 3 {
		t.Errorf("unfiltered total = %d, want 3 (one row per device)", all.Total)
	}
	seen := map[string]int{}
	for _, s := range all.Submissions {
		seen[s.MACAddress]++
	}
	for mac, n := range seen {
		if n != 1 {
			t.Errorf("device %s appeared %d times in the unfiltered list", mac, n)
		}
	}
}

func firstMAC(subs []Submission) string {
	if len(subs) == 0 {
		return ""
	}
	return subs[0].MACAddress
}

// TestSearchDoesNotMatchSupersededAttempt pins that search runs after the
// dedup: a device that changed its name must not be findable (and displayed)
// under the name of its superseded attempt.
func TestSearchDoesNotMatchSupersededAttempt(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()
	examID := mkFilterExam(t, pool)

	started := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	t0 := time.Now().Add(-90 * time.Minute)
	t1 := time.Now().Add(-30 * time.Minute)

	// First attempt under the old name, second attempt under the new name.
	mkRow(t, pool, examID, "BB:00:00:00:00:01", "Nama Lama", nil, &started, t0)
	mkRow(t, pool, examID, "BB:00:00:00:00:01", "Nama Baru", strptr(`{"1":"A"}`), &started, t1)

	old, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Search: "Nama Lama",
	})
	if err != nil {
		t.Fatalf("search old name: %v", err)
	}
	if old.Total != 0 {
		t.Errorf("searching the SUPERSEDED name %q returned %d rows — the search "+
			"filter runs before the dedup, so an old attempt is offered as the "+
			"device's current state (with its stale score)",
			"Nama Lama", old.Total)
	}

	fresh, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Search: "Nama Baru",
	})
	if err != nil {
		t.Fatalf("search new name: %v", err)
	}
	if fresh.Total != 1 {
		t.Errorf("searching the current name returned %d rows, want 1", fresh.Total)
	}
}

// TestSearchTreatsLikeMetacharactersLiterally pins that a search string is
// matched literally. Unescaped, the two LIKE metacharacters let ordinary
// queries match the wrong students: "A_B" would also match "AXB", and "50%"
// would also match "5000".
//
// The fixtures contain BOTH the literal-punctuation name and the name an
// unescaped pattern would wrongly pull in, so the test fails loudly if
// escaping is dropped.
func TestSearchTreatsLikeMetacharactersLiterally(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()
	examID := mkFilterExam(t, pool)

	started := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	now := time.Now()

	// (name, mac-suffix)
	rows := []struct{ name, mac string }{
		{"Siswa A_B", "01"},  // literal underscore
		{"Siswa AXB", "02"},  // what an unescaped "_" would wrongly match
		{"Siswa 50%", "03"},  // literal percent
		{"Siswa 5000", "04"}, // what an unescaped "%" would wrongly match
	}
	for _, r := range rows {
		mkRow(t, pool, examID, "CC:00:00:00:00:"+r.mac, r.name, nil, &started, now)
	}

	cases := []struct {
		query string
		want  string
	}{
		{"A_B", "Siswa A_B"},
		{"AXB", "Siswa AXB"},
		{"50%", "Siswa 50%"},
		{"5000", "Siswa 5000"},
	}
	for _, tc := range cases {
		res, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
			ExamID: examID, Page: 1, PerPage: 25, Search: tc.query,
		})
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		var got []string
		for _, s := range res.Submissions {
			got = append(got, s.StudentName)
		}
		if len(got) != 1 || !strings.EqualFold(got[0], tc.want) {
			t.Errorf("search %q matched %v, want exactly [%s] — LIKE "+
				"metacharacters must be escaped so a plain query cannot pull in "+
				"unrelated students", tc.query, got, tc.want)
		}
	}

	// A bare metacharacter must find only the name that literally contains it.
	underscore, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Search: "_",
	})
	if err != nil {
		t.Fatalf("search underscore: %v", err)
	}
	if underscore.Total != 1 || !strings.EqualFold(underscore.Submissions[0].StudentName, "Siswa A_B") {
		t.Errorf(`searching "_" matched %d rows, want 1 ("Siswa A_B") — the `+
			"underscore acted as a single-character wildcard", underscore.Total)
	}

	pct, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Search: "%",
	})
	if err != nil {
		t.Fatalf("search percent: %v", err)
	}
	if pct.Total != 1 || !strings.EqualFold(pct.Submissions[0].StudentName, "Siswa 50%") {
		t.Errorf(`searching "%%" matched %d rows, want 1 ("Siswa 50%%") — the `+
			"percent sign acted as a wildcard", pct.Total)
	}

	// A literal backslash in the query must not break the pattern either.
	bs, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 1, PerPage: 25, Search: `\`,
	})
	if err != nil {
		t.Fatalf("search backslash: %v", err)
	}
	if bs.Total != 0 {
		t.Errorf(`searching a literal backslash returned %d rows, want 0`, bs.Total)
	}
}

// TestFilterPaginationClampsWithinFilteredSet pins that paging stays inside
// the filtered result set: after a filter narrows the list, an out-of-range
// page is clamped to the last page of THAT set (not left empty), and the
// reported page matches what was served.
func TestFilterPaginationClampsWithinFilteredSet(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()
	examID := mkFilterExam(t, pool)

	started := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	now := time.Now()
	for i := 0; i < 7; i++ {
		mkRow(t, pool, examID,
			fmt.Sprintf("DD:00:00:00:00:%02d", i),
			fmt.Sprintf("Siswa %02d", i), strptr(`{"1":"A"}`), &started, now)
	}
	// One in-progress device, so status=in_progress yields a single page.
	mkRow(t, pool, examID, "DD:00:00:00:00:99", "Sisua Dikerjakan", nil, &started, now)

	res, err := ListSubmissionsByExam(ctx, pool, ListSubmissionsByExamOpts{
		ExamID: examID, Page: 5, PerPage: 5, Status: "in_progress",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Page != 1 {
		t.Errorf("out-of-range page under a filter served page %d, want 1 — the "+
			"clamp must land inside the FILTERED set", res.Page)
	}
	if res.Total != 1 || len(res.Submissions) != 1 {
		t.Errorf("filtered page = %d rows (total %d), want 1", len(res.Submissions), res.Total)
	}
}

func strptr(s string) *string { return &s }
