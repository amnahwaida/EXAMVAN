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
	"sync"
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
	r.GET("/api/exams/token/:token", ExamByToken())
	r.GET("/api/exams/:exam_id/pdf", ExamPDF())
	r.POST("/api/exams/:exam_id/submit", SubmitExam())
	r.GET("/api/exams/:exam_id/result", ExamResult())
	r.POST("/api/exams/:exam_id/access-log", AccessLog())
	r.POST("/api/exams/:exam_id/complete", CompleteExam())
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
// Fix 6: rate-limit presence (access-log & complete) per exam+MAC
// ---------------------------------------------------------------------------

// TestAccessLogRateLimitKeyedByExamAndMac locks in that the presence endpoint
// is throttled per exam+MAC, not per-IP alone: a classroom behind one shared
// NAT (many devices × login + ~1 heartbeat/min + logout) must not 429 each
// other, while a single device cannot spam beyond its own budget. Mirrors the
// SubmitExam per-exam+MAC pattern — before the fix the route was limited only
// by RateLimitIP(30/min), which a 30+ device room drained in the first minute.
func TestAccessLogRateLimitKeyedByExamAndMac(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}
	payload := map[string]interface{}{
		"event":        "heartbeat", // heartbeat = Redis-only, tanpa insert DB
		"mac_address":  "AA:BB:CC:DD:EE:01",
		"student_name": "Siswa A",
	}

	// Device 1 (known MAC): drain its own bucket (10 allowed, 11th rejected).
	for i := 0; i < presenceRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodPost,
			fmt.Sprintf("/api/exams/%d/access-log", examID), "203.0.113.10:9999", headers, payload)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("device 1 request %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/access-log", examID), "203.0.113.10:9999", headers, payload)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("device 1 over-cap status = %d, want 429", rec.Code)
	}

	// Device 2 (same NAT IP, different MAC): must NOT be blocked — the bucket
	// is per exam+MAC, so classmates behind the same IP keep their own budget.
	payload2 := map[string]interface{}{
		"event":        "heartbeat",
		"mac_address":  "AA:BB:CC:DD:EE:02",
		"student_name": "Siswa B",
	}
	rec = doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/access-log", examID), "203.0.113.10:9999", headers, payload2)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("device 2 same-IP status = 429, want not rate-limited (bucket must be per exam+MAC)")
	}

	// Device 3 (no MAC → "unknown"): bucket falls back to per exam+IP, so a
	// different IP keeps a fresh budget while a rotating-IP attacker stays
	// bound by the per-IP middleware limit on the route.
	payload3 := map[string]interface{}{
		"event":        "heartbeat",
		"student_name": "Siswa Tanpa MAC",
	}
	rec = doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/access-log", examID), "198.51.100.7:9999", headers, payload3)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("unknown-MAC device on fresh IP status = 429, want not rate-limited")
	}
}

// TestCompleteExamRateLimitKeyedByExamAndMac locks in the same per-exam+MAC
// bucket for the deadline-burst endpoint: every device calls /complete at exam
// end from one shared NAT, so a per-IP-only cap would 429 the tail of a large
// room and strand their presence as online.
func TestCompleteExamRateLimitKeyedByExamAndMac(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}
	payload := map[string]interface{}{"mac_address": "AA:BB:CC:DD:EE:01"}

	for i := 0; i < presenceRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodPost,
			fmt.Sprintf("/api/exams/%d/complete", examID), "203.0.113.10:9999", headers, payload)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("device 1 complete %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/complete", examID), "203.0.113.10:9999", headers, payload)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("device 1 over-cap complete status = %d, want 429", rec.Code)
	}

	// Classmate behind the same NAT, different MAC: must stay unblocked.
	payload2 := map[string]interface{}{"mac_address": "AA:BB:CC:DD:EE:02"}
	rec = doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/complete", examID), "203.0.113.10:9999", headers, payload2)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("classmate complete status = 429, want not rate-limited (bucket must be per exam+MAC)")
	}
}

// ---------------------------------------------------------------------------
// Fix 7: rate-limit polling /result per exam+MAC + aggregate per exam
// ---------------------------------------------------------------------------

// TestExamResultRateLimitKeyedByExamAndMac locks in that the post-submit
// polling endpoint is throttled per exam+MAC (plus a per-exam aggregate cap),
// not per-IP alone. The app polls /result every 2.5s while a submit is queued
// (≈24/min per device), and a whole classroom polls simultaneously at the
// deadline from one shared NAT — a per-IP-only limit 429s the third device.
// Mirrors the presence/submit per-exam+MAC pattern.
func TestExamResultRateLimitKeyedByExamAndMac(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}
	path := fmt.Sprintf("/api/exams/%d/result?job_id=abc123&mac_address=AA:BB:CC:DD:EE:01", examID)

	// Device 1 (known MAC): drain its own bucket (60 allowed, 61st rejected).
	for i := 0; i < resultRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", headers, nil)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("device 1 poll %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", headers, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("device 1 over-cap status = %d, want 429", rec.Code)
	}

	// Classmate behind the same NAT IP, different MAC: must NOT be blocked.
	path2 := fmt.Sprintf("/api/exams/%d/result?job_id=def456&mac_address=AA:BB:CC:DD:EE:02", examID)
	rec = doJSONRequest(router, http.MethodGet, path2, "203.0.113.10:9999", headers, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("classmate same-IP status = 429, want not rate-limited (bucket must be per exam+MAC)")
	}

	// Unknown MAC on a fresh IP: bucket falls back to per exam+IP.
	path3 := fmt.Sprintf("/api/exams/%d/result?job_id=ghi789", examID)
	rec = doJSONRequest(router, http.MethodGet, path3, "198.51.100.7:9999", headers, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("unknown-MAC poll on fresh IP status = 429, want not rate-limited")
	}
}

// ---------------------------------------------------------------------------
// Fix 8: rate-limit polling request-approval per exam+MAC
// ---------------------------------------------------------------------------

// TestRequestApprovalRateLimitKeyedByExamAndMac locks in that the waiting-room
// polling endpoint (request-approval, polled every 5s ≈ 12/min per device) is
// throttled per exam+MAC so a classroom behind one shared NAT does not 429
// each other. Mirrors the per-exam+MAC pattern used across the student routes.
func TestRequestApprovalRateLimitKeyedByExamAndMac(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	headers := map[string]string{"X-App-Version": "2.5.0"}
	payload := map[string]interface{}{
		"exam_id":       examID,
		"mac_address":   "AA:BB:CC:DD:EE:01",
		"student_name":  "Siswa A",
		"exam_number":   "1",
		"student_class": "XII-A",
		"token":         token,
	}

	// Device 1 (known MAC): drain its own bucket (30 allowed, 31st rejected).
	for i := 0; i < reqAppRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodPost,
			"/api/exams/request-approval", "203.0.113.10:9999", headers, payload)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("device 1 request %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodPost,
		"/api/exams/request-approval", "203.0.113.10:9999", headers, payload)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("device 1 over-cap status = %d, want 429", rec.Code)
	}

	// Classmate behind the same NAT IP, different MAC: must NOT be blocked.
	payload2 := map[string]interface{}{
		"exam_id":       examID,
		"mac_address":   "AA:BB:CC:DD:EE:02",
		"student_name":  "Siswa B",
		"exam_number":   "2",
		"student_class": "XII-A",
		"token":         token,
	}
	rec = doJSONRequest(router, http.MethodPost,
		"/api/exams/request-approval", "203.0.113.10:9999", headers, payload2)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("classmate same-IP status = 429, want not rate-limited (bucket must be per exam+MAC)")
	}
}

// ---------------------------------------------------------------------------
// Fix 9: rate-limit join wave (token) & unduhan PDF untuk ruangan satu NAT
// ---------------------------------------------------------------------------

// TestExamByTokenRateLimitPerToken locks in that the join endpoint
// (GET /api/exams/token/:token) is throttled per token, not per-IP alone:
// the whole room types its exam token at the same time at exam start from one
// shared NAT. The endpoint carries no per-device identity, so the aggregate
// per-token bucket is the right granularity (plus the per-IP middleware).
func TestExamByTokenRateLimitPerToken(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	_, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	path := fmt.Sprintf("/api/exams/token/%s", token)
	for i := 0; i < joinRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", nil, nil)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("join %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", nil, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("join over-cap status = %d, want 429", rec.Code)
	}

	// A DIFFERENT token (different exam) must have a fresh bucket.
	otherToken := fmt.Sprintf("Q%07d", time.Now().UnixNano()%10000000)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by, exam_started_at, auto_approve)
		SELECT 'Ujian Kedua', 'ujian.pdf', 2048, $1, $1, 'active', 'medium', id, CURRENT_TIMESTAMP, true
		FROM admin_users WHERE username = 'reqapp-guru' LIMIT 1`, otherToken); err != nil {
		t.Fatalf("insert second exam: %v", err)
	}
	rec = doJSONRequest(router, http.MethodGet,
		fmt.Sprintf("/api/exams/token/%s", otherToken), "203.0.113.10:9999", nil, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("other-token join status = 429, want not rate-limited (bucket must be per token)")
	}
}

// TestExamPDFRateLimitKeyedByExamAndMac locks in that the PDF download
// endpoint is throttled per exam+MAC, not per-IP alone: the whole room
// downloads the PDF at exam start from one shared NAT. Mirrors the
// per-exam+MAC pattern used across the student routes.
func TestExamPDFRateLimitKeyedByExamAndMac(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newExamFixesRouter(pool, rdb)

	insertApprovalRow(t, pool, examID, "AA:BB:CC:DD:EE:01")
	insertApprovalRow(t, pool, examID, "AA:BB:CC:DD:EE:02")

	headers := map[string]string{"X-Exam-Token": token, "X-Device-Id": "AA:BB:CC:DD:EE:01"}
	path := fmt.Sprintf("/api/exams/%d/pdf", examID)

	// Device 1: drain its own bucket (10 allowed, 11th rejected).
	for i := 0; i < pdfRateLimitMax; i++ {
		rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", headers, nil)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("device 1 download %d unexpectedly rate-limited", i+1)
		}
	}
	rec := doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", headers, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("device 1 over-cap status = %d, want 429", rec.Code)
	}

	// Classmate behind the same NAT IP, different device id: must NOT be blocked.
	headers2 := map[string]string{"X-Exam-Token": token, "X-Device-Id": "AA:BB:CC:DD:EE:02"}
	rec = doJSONRequest(router, http.MethodGet, path, "203.0.113.10:9999", headers2, nil)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("classmate same-IP status = 429, want not rate-limited (bucket must be per exam+MAC)")
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

// TestSubmitSyncConcurrentSameDeviceNoDuplicates locks in the per-device
// advisory lock in models.CreateSubmission (the sync submit path): N
// concurrent submits from the SAME device (a double-tap / a retry racing the
// first attempt) must end with exactly ONE durable submission row. The
// select-latest-then-update/insert is a check-then-act — without the lock two
// requests that BOTH find no latest row both INSERT, and the student appears
// twice in the monitoring table / hasil page (the duplicate-submission bug
// already fixed for the async queue path, now closed on the sync path too).
func TestSubmitSyncConcurrentSameDeviceNoDuplicates(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	ownerID := insertResultFixOwner(t, pool)
	token := fmt.Sprintf("Z%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at, questions_json)
		VALUES ('Ujian Submit Sync', 'sync.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP, $3)
		RETURNING id`, token, ownerID, `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta"}]`).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	router := newExamFixesRouter(pool, nil) // no Redis → the sync submit path
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	const n = 12
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // barrier: all requests leave together so the first-attempt
			// SELECTs overlap instead of finishing before later requests start
			rec := doJSONRequest(router, http.MethodPost,
				fmt.Sprintf("/api/exams/%d/submit", examID), "", headers,
				map[string]interface{}{
					"student_name": "Siti", "exam_number": "E1", "student_class": "XII-A",
					"mac_address": "DEVICE:sync-dedup",
					"identity_data": map[string]interface{}{
						"student_name": "Siti", "exam_number": "E1", "student_class": "XII-A",
					},
					"answers":    map[string]interface{}{"1": "jakarta"},
					"start_time": "2026-08-15 08:00:00",
				})
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range codes {
		if codes[i] != http.StatusOK {
			t.Fatalf("submit %d: status=%d, want 200", i, codes[i])
		}
	}

	// Exactly one durable row for the device — duplicates would show the
	// student twice in the monitoring table / hasil page.
	var cnt int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND mac_address = $2`,
		examID, "DEVICE:sync-dedup").Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("submission rows after concurrent same-device submits: got %d err=%v, want 1", cnt, err)
	}
}
