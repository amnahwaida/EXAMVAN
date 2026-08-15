package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/queue"
)

// ---------------------------------------------------------------------------
// Review-lanjutan fixes (14 Agustus 2026): regresi untuk dedup retry,
// life-cycle approval async, auth endpoint hasil, enforce persetujuan
// server-side, dan rate-limit per-IP untuk device tanpa MAC.
// ---------------------------------------------------------------------------

// newExamFixesRouter mirrors the production wiring for the student API surface
// under test (PDF / submit / result / request-approval), with an optional
// injected Redis client and an R2 stub. Identity validation requires
// student_name/exam_number/student_class, and the version gate requires
// X-App-Version, so every submit/result request in these tests sets both.
func newExamFixesRouter(pool *pgxpool.Pool, rdb *goredis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		if rdb != nil {
			c.Set("redis", rdb)
		}
		stub := &stubR2{enabled: true}
		c.Set("r2", stub)
	})
	r.POST("/api/exams/request-approval", RequestApproval())
	r.GET("/api/exams/:exam_id/pdf", ExamPDF())
	r.POST("/api/exams/:exam_id/submit", SubmitExam())
	r.GET("/api/exams/:exam_id/result", ExamResult())
	return r
}

// doJSONRequest performs an HTTP request against the fixes router with a
// RemoteAddr override and optional headers, returning the recorder.
func doJSONRequest(router *gin.Engine, method, path, remoteAddr string, headers map[string]string, body interface{}) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// insertApprovalRow directly marks a device approved (as pengawas would).
func insertApprovalRow(t *testing.T, pool *pgxpool.Pool, examID int, mac string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Siswa Fix', 'F1', 'XII-A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fix 1: retry submit tidak boleh membuat baris duplikat (sync path)
// ---------------------------------------------------------------------------

// TestSubmitExamRetryDoesNotDuplicateRow proves the full HTTP loop is
// idempotent: submitting the same answers twice from the same device + exam
// number (the second request simulating a retry after a lost response) must
// leave exactly ONE persisted submission row.
func TestSubmitExamRetryDoesNotDuplicateRow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	router := newExamFixesRouter(pool, nil)

	payload := map[string]interface{}{
		"student_name":  "Siswa Retry",
		"exam_number":   "R9",
		"student_class": "XII-A",
		"answers":       map[string]interface{}{"1": "jakarta"},
		"mac_address":   "DEVICE:http-retry",
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	for i := 1; i <= 2; i++ {
		rec := doJSONRequest(router, http.MethodPost,
			fmt.Sprintf("/api/exams/%d/submit", examID), "", headers, payload)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200 (%s)", i, rec.Code, rec.Body.String())
		}
	}

	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:http-retry'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != 1 {
		t.Errorf("rows after double submit = %d, want 1 (retry must not duplicate)", total)
	}
}

// ---------------------------------------------------------------------------
// Fix 2: approval dicabut hanya setelah persistensi durable (async path)
// ---------------------------------------------------------------------------

// TestSubmitExamAsyncKeepsApprovalUntilPersisted locks in the new lifecycle:
// enqueueing an async job must NOT revoke the device's approval — revocation
// happens later, in the queue worker, only after the batch commit. The old
// handler deleted the approval at enqueue time, so a job that failed
// permanently stranded the student: answers reported "success", approval gone,
// and no way back in.
func TestSubmitExamAsyncKeepsApprovalUntilPersisted(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	// Device goes through the join gate → auto-approved (approval row exists).
	insertApprovalRow(t, pool, examID, "DEVICE:async-keep")

	router := newExamFixesRouter(pool, rdb)
	payload := map[string]interface{}{
		"student_name":  "Siswa Async",
		"exam_number":   "A1",
		"student_class": "XII-A",
		"answers":       map[string]interface{}{"1": "jakarta"},
		"mac_address":   "DEVICE:async-keep",
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "", headers, payload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (async submission) (%s)", rec.Code, rec.Body.String())
	}

	// The job is queued but NOT yet durable → approval must still exist, so the
	// device can retry the submit if the worker never persists it.
	var approvals int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM exam_approvals WHERE exam_id=$1 AND mac_address='DEVICE:async-keep'`,
		examID).Scan(&approvals); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if approvals != 1 {
		t.Errorf("approvals right after enqueue = %d, want 1 (revoke only after durable commit)", approvals)
	}

	// Sanity: the job actually landed in the Redis queue.
	if n, err := rdb.LLen(ctxBF, queue.QueueKey).Result(); err != nil || n != 1 {
		t.Errorf("queue length = %d (err %v), want 1", n, err)
	}
}

var ctxBF = context.Background()

// ---------------------------------------------------------------------------
// Fix 3: endpoint hasil tidak boleh publik tanpa kredensial
// ---------------------------------------------------------------------------

// TestExamResultRequiresTokenOrApproval locks in result-endpoint auth: a poll
// WITHOUT a valid exam token (and without an approved-device row) must be
// rejected, even when the queried submission exists. Previously the endpoint
// was fully public — anyone who knew a device identifier + identity payload
// could read a student's score before the results were published.
func TestExamResultRequiresTokenOrApproval(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	ownerID := insertResultFixOwner(t, pool)
	token := fmt.Sprintf("Z%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at, questions_json)
		VALUES ('Ujian Hasil Fix', 'fix.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP, $3)
		RETURNING id`, token, ownerID, `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta"}]`).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	answers := `{"1":"jakarta"}`
	score := 80.0
	identity := `{"student_name":"Budi","exam_number":"H1","student_class":"XII-A"}`
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1,'Budi','H1','XII-A',$2,$3,'2026-08-09 08:00:00','DEVICE:result-auth',$4)`,
		examID, answers, score, identity); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	router := newExamFixesRouter(pool, nil)
	// The identity fallback additionally requires the per-submission job_id
	// secret (only the submitting device received it); the positive case
	// carries its own job_id.
	path := fmt.Sprintf("/api/exams/%d/result?job_id=jobsecret&mac_address=%s&identity_data=%s",
		examID, "DEVICE:result-auth", url.QueryEscape(identity))

	// No credential at all → rejected.
	rec := doJSONRequest(router, http.MethodGet, path, "", nil, nil)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusNotFound {
		t.Fatalf("unauthenticated status = %d, want 401/404 (%s)", rec.Code, rec.Body.String())
	}

	// Valid exam token + the device's own job_id → allowed, score readable.
	rec = doJSONRequest(router, http.MethodGet, path, "",
		map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success || body.Status != "done" {
		t.Fatalf("got success=%v status=%q, want true/done", body.Success, body.Status)
	}
}

// ---------------------------------------------------------------------------
// Fix 4: akses PDF wajib lewat persetujuan pengawas (server-side)
// ---------------------------------------------------------------------------

// TestExamPDFRequiresApprovedDevice locks in the server-side approval gate for
// the exam content: with manual approval mode (auto_approve off) a valid-token
// device that was never approved must be denied the PDF (403), and once the
// pengawas approves it the download succeeds (302 → signed URL). Previously
// the PDF was available to anyone holding the token, making the client-side
// waiting screen purely cosmetic in static-token mode.
func TestExamPDFRequiresApprovedDevice(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)
	router := newExamFixesRouter(pool, nil)

	// Valid token + a device id that was NEVER approved → 403.
	rec := doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/%d/pdf", examID), "",
		map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0",
			"X-Device-Id": "DEVICE:pdf-no"}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unapproved status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}

	// After pengawas approval of THAT device → content served (302 signed URL).
	insertApprovalRow(t, pool, examID, "DEVICE:pdf-ok")
	rec = doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/%d/pdf", examID), "",
		map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0",
			"X-Device-Id": "DEVICE:pdf-ok"}, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("approved status = %d, want 302 (%s)", rec.Code, rec.Body.String())
	}
}

// TestExamPDFRequiresApprovedDeviceEvenWithAutoApprove locks the gate for
// auto-approve exams too: the auto flag approves devices automatically at
// request-approval time, but a raw token holder that never requested approval
// must still be denied — otherwise the gate is meaningless for the bypass case
// the fix targets.
func TestExamPDFRequiresApprovedDeviceEvenWithAutoApprove(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	router := newExamFixesRouter(pool, nil)

	rec := doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/%d/pdf", examID), "",
		map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("token-only status = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Fix 5: rate-limit submit per-IP saat identitas device tidak tersedia
// ---------------------------------------------------------------------------

// TestSubmitExamRateLimitKeyedByIPWhenMacUnknown locks in that devices without
// a resolvable identifier (sanitizeMAC → "unknown", e.g. fresh installs or
// emulated devices) get per-IP rate-limit buckets instead of all sharing one
// global "unknown" bucket. Before the fix, 11 classroom devices with no MAC
// drained a single bucket and mutually blocked each other; now each IP has its
// own bucket.
func TestSubmitExamRateLimitKeyedByIPWhenMacUnknown(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	payload := map[string]interface{}{
		"student_name":  "Siswa Tanpa MAC",
		"exam_number":   "X1",
		"student_class": "XII-A",
		"answers":       map[string]interface{}{"1": "jakarta"},
		// mac_address deliberately omitted → sanitizeMAC yields "unknown".
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	// IP A: drain its own bucket (10 allowed, 11th rejected).
	for i := 0; i < submitRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodPost,
			fmt.Sprintf("/api/exams/%d/submit", examID), "203.0.113.10:9999", headers, payload)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("IP A request %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "203.0.113.10:9999", headers, payload)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A over-cap status = %d, want 429", rec.Code)
	}

	// IP B (same "unknown" MAC, no shared bucket) must NOT be blocked.
	rec = doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "198.51.100.7:9999", headers, payload)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("IP B status = 429, want not rate-limited (buckets must be per-IP for unknown MAC)")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// insertResultFixOwner creates the minimal owner account for the result tests
// (submissions/exams.created_by references admin_users).
func insertResultFixOwner(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO admin_users (username, name, password_hash, status, role)
		VALUES ('result-fix-guru', 'Guru Hasil Fix', 'pass', 'active', '["guru"]')
		RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	return id
}

// TestExamResultIdentityFallbackRequiresJobID locks in the job_id requirement
// for the DB identity fallback: in static-token mode the exam token is SHARED
// by the whole class, so a token alone must NOT unlock another device's score
// before the teacher publishes results. Only a poll that carries the
// per-submission job_id — the secret the submitting device alone received —
// may recover the persisted submission by mac + identity. Without the fix a
// classmate holding the shared token could read any device's score by
// guessing its mac + identity payload.
func TestExamResultIdentityFallbackRequiresJobID(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	ownerID := insertResultFixOwner(t, pool)
	token := fmt.Sprintf("Z%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at, questions_json)
		VALUES ('Ujian Hasil JobID', 'jobid.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP, $3)
		RETURNING id`, token, ownerID, `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta"}]`).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	identity := `{"student_name":"Siti","exam_number":"E1","student_class":"XII-A"}`
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1,'Siti','E1','XII-A','{"1":"jakarta"}',100,'2026-08-09 08:00:00','DEVICE:jobid',$2)`,
		examID, identity); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	router := newExamFixesRouter(pool, nil)
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	// Token + mac + identity but NO job_id → the fallback is not served; the
	// poll reports pending, never the score.
	rec := doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/%d/result?mac_address=%s&identity_data=%s",
			examID, "DEVICE:jobid", url.QueryEscape(identity)), "", headers, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-job_id status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool     `json:"success"`
		Status  string   `json:"status"`
		Score   *float64 `json:"score"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode no-job_id: %v", err)
	}
	if body.Status != "pending" || body.Score != nil {
		t.Fatalf("no-job_id poll: got status=%q score=%v, want pending/nil", body.Status, body.Score)
	}

	// Same poll WITH the device's own job_id → the durable fallback serves the
	// score (Redis result expired / absent).
	rec = doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/%d/result?job_id=jobsecret123&mac_address=%s&identity_data=%s",
			examID, "DEVICE:jobid", url.QueryEscape(identity)), "", headers, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("with-job_id status = %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode with-job_id: %v", err)
	}
	if !body.Success || body.Status != "done" || body.Score == nil || *body.Score != 100 {
		t.Fatalf("with-job_id poll: got success=%v status=%q score=%v, want true/done/100", body.Success, body.Status, body.Score)
	}
}