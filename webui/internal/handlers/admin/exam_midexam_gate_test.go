package admin

// ---------------------------------------------------------------------------
// Review alur ujian 14 Nov 2026 — P2 mutasi mid-exam:
// DeleteExam menghapus submissions siswa secara live, SaveQuestions mengganti
// kunci jawaban mid-exam (penilaian inkonsisten antar siswa), dan ganti PDF
// saat siswa masih melihat PDF lama. Ujian yang SEDANG BERJALAN (aktif +
// sudah dimulai pengawas) dilindungi gerbang 409; ujian yang sudah di-stop
// atau di-tombstone tetap bisa dihapus (kebijakan expiry policy B).
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/websocket"

	r2client "github.com/examvan/webui/internal/handlers/r2"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

func setupMidExamGatePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "admin")
}

func setupMidExamGateFixture(t *testing.T, pool *pgxpool.Pool, username string) (superID, runningID, stoppedID int) {
	t.Helper()
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: username, Name: "Mid Gate", PasswordHash: "x",
		Status: models.UserStatusActive, Role: models.SerializeRoles([]string{models.RoleSuperAdmin}),
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	questionsJSON := `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta","options":["Jakarta","Bogor"]}]`
	var runningIDLocal, stoppedIDLocal int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, exam_started_at)
		VALUES ('Ujian Berjalan', 'run.pdf', 1024, $1, $1, 'active', 'medium', $2, $3, CURRENT_TIMESTAMP)
		RETURNING id`, fmt.Sprintf("MIDRUN%05d", len(username)), questionsJSON, su.ID).Scan(&runningIDLocal); err != nil {
		t.Fatalf("insert running exam: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, exam_started_at)
		VALUES ('Ujian Stop', 'stop.pdf', 1024, $1, $1, 'inactive', 'medium', $2, $3, NULL)
		RETURNING id`, fmt.Sprintf("MIDSTP%05d", len(username)), questionsJSON, su.ID).Scan(&stoppedIDLocal); err != nil {
		t.Fatalf("insert stopped exam: %v", err)
	}
	return su.ID, runningIDLocal, stoppedIDLocal
}

// newMidExamGateRouter mirrors the production wiring for the four endpoints
// under test, plus the /test/login/:id session seam and an R2 stub.
func newMidExamGateRouter(pool *pgxpool.Pool, storageDir string, hub *websocket.Hub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: storageDir})
		if hub != nil {
			c.Set("ws_hub", hub)
		}
		c.Set("r2", newStubR2())
	})

	r.POST("/test/login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		u, err := models.GetUserByID(c.Request.Context(), pool, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false})
			return
		}
		s := sessions.Default(c)
		s.Set(middleware.SessionKeyAdminID, u.ID)
		s.Set(middleware.SessionKeyUsername, u.Username)
		s.Set(middleware.SessionKeyRole, u.Role)
		s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	api := r.Group("/admin/api", middleware.AuthRequired())
	api.POST("/exams/:exam_id/delete", DeleteExam())
	api.POST("/exams/bulk-delete", BulkDelete())
	api.POST("/exams/:exam_id/questions", SaveQuestions())
	api.POST("/exams/:exam_id/edit", EditExam())
	api.POST("/submissions/:id/delete", DeleteSubmission())
	return r
}

func loginMidGate(t *testing.T, srv *httptest.Server, superID int) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+fmt.Sprint(superID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}
	return client
}

func midGatePost(client *http.Client, srv *httptest.Server, path string, body []byte, contentType string) *http.Response {
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Sprintf("POST %s: %v", path, err))
	}
	return resp
}

func examExistsMid(t *testing.T, pool *pgxpool.Pool, examID int) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exams WHERE id=$1`, examID).Scan(&n); err != nil {
		t.Fatalf("exam exists: %v", err)
	}
	return n > 0
}

var _ = r2client.ErrCodeNotConfigured // keep import if stub covers it

func TestDeleteExamRefusedWhileRunning(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-del-run")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/delete", runningID), nil, "")
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 409 (body: %s)", resp.StatusCode, body)
	}
	if !examExistsMid(t, pool, runningID) {
		t.Error("ujian berjalan tidak boleh terhapus oleh gerbang mid-exam")
	}
}

func TestDeleteExamAllowedAfterStop(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, _, stoppedID := setupMidExamGateFixture(t, pool, "midgate-del-stop")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/delete", stoppedID), nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ujian yang sudah di-stop boleh dihapus)", resp.StatusCode)
	}
	if examExistsMid(t, pool, stoppedID) {
		t.Error("ujian stopped harus terhapus")
	}
}

func TestSaveQuestionsWhileRunningFreezesQuestionsButAppliesSchedule(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-sq-run")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	// Kunci lama sebelum save.
	var oldQuestions string
	if err := pool.QueryRow(context.Background(),
		`SELECT questions_json FROM exams WHERE id=$1`, runningID).Scan(&oldQuestions); err != nil {
		t.Fatalf("read questions: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		// Kunci BERBEDA dikirim UI — saat ujian berjalan harus DIABAIKAN.
		// Soal tetap harus valid (kunci ada di pilihan) agar lolos gerbang
		// validasi dan benar-benar menyentuh gate freeze.
		"questions": []map[string]interface{}{{
			"number":  1,
			"type":    "single_choice",
			"key":     "MANIPULATED",
			"options": []interface{}{"MANIPULATED", "A", "B"},
		}},
		"security_level": "medium",
		"start_time":     "",
		"end_time":       "2030-01-02 15:04",
	})
	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/questions", runningID), body, "application/json")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 — jadwal/pengaturan harus tetap tersimpan (body: %s)", resp.StatusCode, b)
	}

	// Respons HARUS membedakan kunci dibekukan dari sukses polos — tanpa
	// sinyal ini guru melihat toast hijau "berhasil disimpan" padahal kunci
	// yang dikirimnya dibuang gate (jawaban tidak benar-benar tersimpan).
	var saveRes struct {
		Success         bool   `json:"success"`
		QuestionsFrozen bool   `json:"questions_frozen"`
		Message         string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&saveRes); err != nil {
		t.Fatalf("decode respons: %v", err)
	}
	if !saveRes.Success || !saveRes.QuestionsFrozen {
		t.Errorf("questions_frozen = %v, want true (success=%v)", saveRes.QuestionsFrozen, saveRes.Success)
	}
	if !strings.Contains(saveRes.Message, "TIDAK diubah") {
		t.Errorf("pesan harus menjelaskan kunci tidak diubah: %q", saveRes.Message)
	}

	var questionsAfter string
	var endAfter *string
	if err := pool.QueryRow(context.Background(),
		`SELECT questions_json, end_time::text FROM exams WHERE id=$1`, runningID).Scan(&questionsAfter, &endAfter); err != nil {
		t.Fatalf("read exam after save: %v", err)
	}
	if questionsAfter != oldQuestions {
		t.Errorf("questions_json berubah saat ujian berjalan — bundle penilaian wajib beku")
	}
	if endAfter == nil || !strings.Contains(*endAfter, "2030-01-02") {
		t.Errorf("end_time baru tidak tersimpan: %v", endAfter)
	}
}

func TestSaveQuestionsScheduleChangeBroadcastsWhileRunning(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-bcast")
	hub := websocket.NewHub(nil)
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), hub))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	captured := [][]interface{}{}
	orig := broadcastScheduleChanged
	broadcastScheduleChanged = func(h *websocket.Hub, examID int, endUTC string) {
		captured = append(captured, []interface{}{examID, endUTC})
	}
	t.Cleanup(func() { broadcastScheduleChanged = orig })

	newEnd := "2030-01-02 15:04"
	body, _ := json.Marshal(map[string]interface{}{
		"questions":      nil,
		"security_level": "medium",
		"start_time":     "",
		"end_time":       newEnd,
	})
	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/questions", runningID), body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(captured) != 1 || captured[0][0] != runningID {
		t.Fatalf("schedule_changed broadcast tidak terkirim: %v", captured)
	}
	endUTC := captured[0][1].(string)
	if !strings.Contains(endUTC, "2030-01-02") {
		t.Errorf("end_time broadcast salah: %q", endUTC)
	}

	// end_time benar-benar tersimpan UTC.
	var endAfter *string
	if err := pool.QueryRow(context.Background(),
		`SELECT end_time::text FROM exams WHERE id=$1`, runningID).Scan(&endAfter); err != nil {
		t.Fatalf("read end_time: %v", err)
	}
	if endAfter == nil || !strings.Contains(*endAfter, "2030-01-02") {
		t.Errorf("end_time tidak tersimpan: %v", endAfter)
	}
}

func TestEditExamRefusesPDFReplacementWhileRunning(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-edit-pdf")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "Ujian Berjalan (ganti PDF)")
	fw, _ := mw.CreateFormFile("pdf_file", "baru.pdf")
	_, _ = fw.Write([]byte("%PDF-1.4\n1 0 obj\n%%EOF\n"))
	mw.Close()

	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/edit", runningID), buf.Bytes(), mw.FormDataContentType())
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 409 (ganti PDF saat ujian berjalan harus ditolak; body: %s)", resp.StatusCode, body)
	}
}

func TestEditExamAllowsNameOnlyWhileRunning(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-edit-name")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "Ujian Berjalan (ubah nama saja)")
	mw.Close()

	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/edit", runningID), buf.Bytes(), mw.FormDataContentType())
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (edit nama saja boleh saat berjalan; body: %s)", resp.StatusCode, body)
	}
}

func TestBulkDeleteSkipsRunningExam(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, stoppedID := setupMidExamGateFixture(t, pool, "midgate-bulk")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	body, _ := json.Marshal(map[string]interface{}{"ids": []int{runningID, stoppedID}})
	resp := midGatePost(client, srv, "/admin/api/exams/bulk-delete", body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !examExistsMid(t, pool, runningID) {
		t.Error("ujian berjalan tidak boleh terhapus oleh bulk delete")
	}
	if examExistsMid(t, pool, stoppedID) {
		t.Error("ujian stopped harus terhapus oleh bulk delete")
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !strings.Contains(out.Message, "sedang berjalan") {
		t.Errorf("pesan harus menyebut ujian yang dilewati: %q", out.Message)
	}
}

// ---------------------------------------------------------------------------
// Recalculasi skor mid-exam (review alur ujian 14 Nov 2026): SaveQuestions
// schedule-only pada ujian BERJALAN tidak boleh memicu recalculasi —
// placeholder siswa yang masih mengerjakan akan tertulis Nilai 0 di tengah
// ujian. Seam goRecalculateScores dipakai untuk merekam pemanggilan.
// ---------------------------------------------------------------------------

func TestSaveQuestionsScheduleOnlyWhileRunningDoesNotRecalculate(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-recalc-run")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()
	client := loginMidGate(t, srv, superID)

	// Placeholder siswa yang masih mengerjakan (tanpa jawaban, tanpa nilai).
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class, mac_address)
		VALUES ($1, 'Siswa Jalan', 'RJ1', 'XII-A', 'DEVICE:mid-run')`, runningID); err != nil {
		t.Fatalf("insert placeholder: %v", err)
	}

	calls := 0
	origGo := goRecalculateScores
	goRecalculateScores = func(ctx context.Context, p *pgxpool.Pool, examID int) {
		calls++
	}
	t.Cleanup(func() { goRecalculateScores = origGo })

	body, _ := json.Marshal(map[string]interface{}{
		"questions":  nil,
		"start_time": "",
		"end_time":   "2030-06-01 10:00",
	})
	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/exams/%d/questions", runningID), body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if calls != 0 {
		t.Errorf("recalculasi dipanggil %d kali saat ujian berjalan — wajib 0", calls)
	}
}

// ---------------------------------------------------------------------------
// Audit trail penghapusan hasil siswa (review pasca-ujian 14 Nov 2026):
// DeleteSubmission menghapus jawaban siswa tanpa jejak audit — berbeda dengan
// DeleteExam yang mencatat. Kontrak: satu baris audit action
// submission_deleted dengan detail menyebut siswa.
// ---------------------------------------------------------------------------

func TestDeleteSubmissionWritesAuditTrail(t *testing.T) {
	pool := setupMidExamGatePool(t)
	superID, runningID, _ := setupMidExamGateFixture(t, pool, "midgate-audit-del")
	srv := httptest.NewServer(newMidExamGateRouter(pool, t.TempDir(), nil))
	defer srv.Close()

	// Submission siswa pada ujian tersebut.
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, mac_address)
		VALUES ($1, 'Siswa Audit', 'AD1', 'XII-A', '{"1":"jakarta"}', 80, 'DEVICE:audit-del')`,
		runningID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
	var subID int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:audit-del'`,
		runningID).Scan(&subID); err != nil {
		t.Fatalf("read submission id: %v", err)
	}

	client := loginMidGate(t, srv, superID)

	resp := midGatePost(client, srv, fmt.Sprintf("/admin/api/submissions/%d/delete", subID), nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var action, detail string
	var examIDNull int
	if err := pool.QueryRow(context.Background(), `
		SELECT action, detail, COALESCE(exam_id, 0) FROM admin_audit_logs
		WHERE action = 'submission_deleted' ORDER BY id DESC LIMIT 1`).
		Scan(&action, &detail, &examIDNull); err != nil {
		t.Fatalf("audit row submission_deleted tidak ditemukan: %v", err)
	}
	if !strings.Contains(detail, "Siswa Audit") {
		t.Errorf("detail audit harus menyebut siswa: %q", detail)
	}
	if examIDNull != runningID {
		t.Errorf("audit exam_id = %d, want %d", examIDNull, runningID)
	}
}
