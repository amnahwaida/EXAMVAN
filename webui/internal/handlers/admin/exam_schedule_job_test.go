package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

func uploadScheduleExam(t *testing.T, pool *pgxpool.Pool, name string) (int, *quotaTestClient) {
	t.Helper()
	userID := createSchoolQuotaUser(t, pool, "job-"+name, "SMK Job "+name,
		[]string{models.RoleGuru}, 10)
	tc, _ := newQuotaTestClient(t, pool)
	tc.login(t, userID)
	if status, body := tc.upload(t, name, schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("upload %s: status=%d body=%s", name, status, body)
	}
	var examID int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM exams WHERE name = $1`, name).Scan(&examID); err != nil {
		t.Fatalf("find exam %s: %v", name, err)
	}
	return examID, tc
}

func examScheduleState(t *testing.T, pool *pgxpool.Pool, examID int) (status string, started, scheduleStarted bool, tolerance int) {
	t.Helper()
	var startedAt, scheduleStartedAt *string
	if err := pool.QueryRow(context.Background(), `SELECT status,
			to_char(exam_started_at, 'YYYY-MM-DD HH24:MI:SS'),
			to_char(schedule_started_at, 'YYYY-MM-DD HH24:MI:SS'),
			schedule_tolerance_minutes
		FROM exams WHERE id = $1`, examID).Scan(&status, &startedAt, &scheduleStartedAt, &tolerance); err != nil {
		t.Fatalf("read exam schedule state: %v", err)
	}
	return status, startedAt != nil, scheduleStartedAt != nil, tolerance
}

func TestExamScheduleToleranceDefaultsToFiveMinutes(t *testing.T) {
	pool := setupVoucherITDB(t)
	examID, _ := uploadScheduleExam(t, pool, "job-default-tolerance")

	if _, _, _, tolerance := examScheduleState(t, pool, examID); tolerance != models.DefaultScheduleToleranceMinutes {
		t.Fatalf("default tolerance = %d, want %d", tolerance, models.DefaultScheduleToleranceMinutes)
	}
}

func TestUpdateExamQuestionsResetsScheduleMarkerOnlyWhenNotRunning(t *testing.T) {
	pool := setupVoucherITDB(t)
	examID, _ := uploadScheduleExam(t, pool, "job-marker-reset")
	ctx := context.Background()

	questions, identity, security, panel := "[]", "[]", "medium", "#ffffff"
	firstStart := "2030-01-01T09:00:00Z"
	firstEnd := "2030-01-01T10:00:00Z"
	secondStart := "2030-01-02T09:00:00Z"
	secondEnd := "2030-01-02T10:00:00Z"

	if _, err := pool.Exec(ctx, `UPDATE exams
		SET schedule_started_at = now(), exam_started_at = NULL, status = 'inactive'
		WHERE id = $1`, examID); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := models.UpdateExamQuestions(ctx, pool, examID,
		&questions, &security, &identity, &panel, &firstStart, &firstEnd, nil, 0, 5); err != nil {
		t.Fatalf("update not-running exam: %v", err)
	}
	if _, _, marker, _ := examScheduleState(t, pool, examID); marker {
		t.Fatal("changing start_time while not running must re-arm the schedule")
	}

	if _, err := pool.Exec(ctx, `UPDATE exams
		SET schedule_started_at = now(), exam_started_at = now(), status = 'active'
		WHERE id = $1`, examID); err != nil {
		t.Fatalf("seed running marker: %v", err)
	}
	if err := models.UpdateExamQuestions(ctx, pool, examID,
		&questions, &security, &identity, &panel, &secondStart, &secondEnd, nil, 0, 5); err != nil {
		t.Fatalf("update running exam: %v", err)
	}
	if _, _, marker, _ := examScheduleState(t, pool, examID); !marker {
		t.Fatal("changing start_time while running must keep the consumed marker")
	}
}

func TestExamScheduleJobAutoStartsWithinTolerance(t *testing.T) {
	pool := setupVoucherITDB(t)
	examID, _ := uploadScheduleExam(t, pool, "job-auto-start")
	ctx := context.Background()

	// start_time is 4 minutes ahead, tolerance is 5: the lead window is open.
	if _, err := pool.Exec(ctx, `UPDATE exams
		SET start_time = now() + interval '4 minutes',
		    end_time = now() + interval '1 hour',
		    schedule_tolerance_minutes = 5
		WHERE id = $1`, examID); err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	runExamSchedulePass(ctx, pool, nil)

	status, started, scheduleStarted, _ := examScheduleState(t, pool, examID)
	if status != "active" || !started || !scheduleStarted {
		t.Fatalf("after auto-start: status=%s started=%v scheduleStarted=%v, want active/true/true",
			status, started, scheduleStarted)
	}

	// A manual stop must not be undone: schedule_started_at keeps the exam out
	// of the auto-start window.
	if err := models.StopExam(ctx, pool, examID); err != nil {
		t.Fatalf("stop exam: %v", err)
	}
	runExamSchedulePass(ctx, pool, nil)

	status, started, scheduleStarted, _ = examScheduleState(t, pool, examID)
	if status != "inactive" || started {
		t.Fatalf("manual stop was undone: status=%s started=%v", status, started)
	}
	if !scheduleStarted {
		t.Fatal("schedule_started_at must be retained after stop")
	}
}

func TestExamScheduleJobDoesNotStartBeforeTolerance(t *testing.T) {
	pool := setupVoucherITDB(t)
	examID, _ := uploadScheduleExam(t, pool, "job-no-early-start")
	ctx := context.Background()

	// start_time is 6 minutes ahead with a 5 minute tolerance: not yet.
	if _, err := pool.Exec(ctx, `UPDATE exams
		SET start_time = now() + interval '6 minutes',
		    end_time = now() + interval '1 hour',
		    schedule_tolerance_minutes = 5
		WHERE id = $1`, examID); err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	runExamSchedulePass(ctx, pool, nil)

	status, started, scheduleStarted, _ := examScheduleState(t, pool, examID)
	if status != "inactive" || started || scheduleStarted {
		t.Fatalf("started before tolerance: status=%s started=%v scheduleStarted=%v",
			status, started, scheduleStarted)
	}
}

func TestExamScheduleJobAutoStopsAfterTolerance(t *testing.T) {
	pool := setupVoucherITDB(t)
	examID, _ := uploadScheduleExam(t, pool, "job-auto-stop")
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE exams
		SET status = 'active',
		    start_time = now() - interval '2 hours',
		    end_time = now() - interval '2 minutes',
		    exam_started_at = now() - interval '2 hours',
		    schedule_started_at = now() - interval '2 hours',
		    schedule_tolerance_minutes = 5
		WHERE id = $1`, examID); err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	// end_time + 5 minutes has not passed yet.
	runExamSchedulePass(ctx, pool, nil)
	status, started, scheduleStarted, _ := examScheduleState(t, pool, examID)
	if status != "active" || !started {
		t.Fatalf("stopped before tolerance: status=%s started=%v", status, started)
	}

	// Move end_time past the tolerance: the next pass must stop it.
	if _, err := pool.Exec(ctx, `UPDATE exams
		SET end_time = now() - interval '6 minutes' WHERE id = $1`, examID); err != nil {
		t.Fatalf("move end_time: %v", err)
	}
	runExamSchedulePass(ctx, pool, nil)

	status, started, scheduleStarted, _ = examScheduleState(t, pool, examID)
	if status != "inactive" || started {
		t.Fatalf("after auto-stop: status=%s started=%v, want inactive/false", status, started)
	}
	if !scheduleStarted {
		t.Fatal("schedule_started_at must be retained after auto-stop")
	}
}
