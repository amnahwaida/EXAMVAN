package api

// ---------------------------------------------------------------------------
// Review alur ujian 14 Nov 2026 — P1 remote kill:
// "Hentikan Pengawasan" commit status inactive LALU broadcast exam_terminated;
// klien (Android ExamModePolicy + desktop exam_viewer) dirancang auto-submit
// saat menerima event itu. Tanpa toleransi di gerbang submit, seluruh
// auto-submit pasca-stop gagal 404 dan jawaban terstranded di perangkat.
//
// Entitlement = approval row 'approved' (sama dengan toleransi jadwal
// berakhir yang sudah ada): pengawas bisa menolak device dari monitoring
// untuk memblokir resubmit-nya; device tanpa approval tetap ditolak.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

func newStopPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "api")
}

// setupStopToleranceFixture membuat ujian + (opsional) approval row, lalu
// menyetel state ujian sesuai mode: "running" (aktif+started), "stopped"
// (inactive + started_at NULL — hasil models.StopExam), "ended" (aktif +
// end_time di masa lalu).
func setupStopToleranceFixture(t *testing.T, pool *pgxpool.Pool, mode, approvalStatus string) (int, string) {
	t.Helper()
	ctx := context.Background()

	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: fmt.Sprintf("stop-guru-%s-%s", mode, approvalStatus),
		Name:     "Guru Stop", PasswordHash: "x", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	token := fmt.Sprintf("ST%05d", len(mode)*1000+len(approvalStatus))
	questionsJSON := `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta","options":["Jakarta","Bogor"]}]`

	startedClause := "CURRENT_TIMESTAMP"
	status := "active"
	endTimeClause := "NULL::timestamptz"
	if mode == "stopped" {
		startedClause = "NULL"
		status = "inactive"
	}
	if mode == "ended" {
		endTimeClause = "NOW() - INTERVAL '2 hours'"
	}

	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, exam_started_at, end_time)
		VALUES ('Ujian Stop', 'stop.pdf', 1024, $1, $1, $2, 'medium', $3, $4, `+startedClause+`, `+endTimeClause+`)
		RETURNING id`, token, status, questionsJSON, owner).Scan(&examID); err != nil {
		t.Fatalf("insert exam (%s): %v", mode, err)
	}

	if approvalStatus != "" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, status)
			VALUES ($1, 'DEVICE:stop-tol', 'Siswa Stop', 'ST1', $2)`, examID, approvalStatus); err != nil {
			t.Fatalf("insert approval: %v", err)
		}
	}
	return examID, token
}

func postStopSubmit(t *testing.T, pool *pgxpool.Pool, examID int, token string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(newExamFixesRouter(pool, nil))
	defer srv.Close()

	payload := map[string]interface{}{
		"student_name":  "Siswa Stop",
		"exam_number":   "ST1",
		"student_class": "XII-A",
		"answers":       map[string]interface{}{"1": "jakarta"},
		"mac_address":   "DEVICE:stop-tol",
	}
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/exams/%d/submit", srv.URL, examID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Exam-Token", token)
	req.Header.Set("X-App-Version", "2.5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST submit: %v", err)
	}
	defer resp.Body.Close()
	return resp
}

func TestSubmitExamAfterStopToleratesApprovedDevice(t *testing.T) {
	pool := newStopPool(t)
	examID, token := setupStopToleranceFixture(t, pool, "stopped", "approved")

	resp := postStopSubmit(t, pool, examID, token)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 200/202 — auto-submit pasca pengawas stop wajib berhasil",
			resp.StatusCode)
	}

	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:stop-tol'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if total != 1 {
		t.Errorf("submission rows = %d, want 1 (jawaban harus durable)", total)
	}
}

func TestSubmitExamAfterStopStillBlocksUnapprovedDevice(t *testing.T) {
	pool := newStopPool(t)
	examID, token := setupStopToleranceFixture(t, pool, "stopped", "")

	resp := postStopSubmit(t, pool, examID, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (device tanpa approval tetap ditolak)", resp.StatusCode)
	}
}

func TestSubmitExamAfterStopStillBlocksRejectedDevice(t *testing.T) {
	pool := newStopPool(t)
	examID, token := setupStopToleranceFixture(t, pool, "stopped", "rejected")

	resp := postStopSubmit(t, pool, examID, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (device ditolak pengawas tetap ditolak)", resp.StatusCode)
	}
}

// TestSubmitExamPastEndTimeToleratesApprovedDevice mengunci toleransi
// jadwal berakhir yang sudah ada (recovery "Kirim Lagi") — perubahan
// gerbang tidak boleh mematahkan jalur ini.
func TestSubmitExamPastEndTimeToleratesApprovedDevice(t *testing.T) {
	pool := newStopPool(t)
	examID, token := setupStopToleranceFixture(t, pool, "ended", "approved")

	resp := postStopSubmit(t, pool, examID, token)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 200/202 (toleransi jadwal berakhir harus tetap berjalan)",
			resp.StatusCode)
	}
}
