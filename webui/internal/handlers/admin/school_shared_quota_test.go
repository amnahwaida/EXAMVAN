package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Shared school-pool quota: in a school instansi that runs a package, EVERY
// account (the operator included) draws its exam/storage/PDF/concurrent quota
// from a single pool sourced from the operator's active redemption. Five
// sub-accounts can therefore no longer each spend a full package
// (5 × 3 exams ≫ a 3-exam school package). These tests drive the real
// handlers over the DB-backed integration infra (setupVoucherITDB, skipped
// when TEST_DATABASE_URL is unset).
// ---------------------------------------------------------------------------

// newSchoolQuotaTestRouter mirrors production wiring for the endpoints under
// test: sessions → AuthRequired, the /test/login/:id session seam, and the R2
// stub (UploadExam requires R2 — the else branch rejects without it).
func newSchoolQuotaTestRouter(pool *pgxpool.Pool) (*gin.Engine, *stubR2) {
	stub := newStubR2()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{StoragePath: ""})
		c.Set("r2", stub)
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
		s.Set(middleware.SessionKeyName, u.Name)
		s.Set(middleware.SessionKeyRole, u.Role)
		s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	api := r.Group("/admin/api", middleware.AuthRequired())
	api.POST("/upload", UploadExam())
	api.POST("/exams/:exam_id/start", StartExam())
	api.POST("/exams/:exam_id/toggle", ToggleExam())
	api.POST("/exams/:exam_id/edit", EditExam())
	api.POST("/exams/bulk-toggle", BulkToggle())
	api.POST("/exams/:exam_id/questions", SaveQuestions())
	return r, stub
}

// quotaTestClient is an HTTP client bound to the school-quota router, carrying
// a session cookie jar, with small wrappers for the endpoints under test.
type quotaTestClient struct {
	srv    *httptest.Server
	client *http.Client
}

func newQuotaTestClient(t *testing.T, pool *pgxpool.Pool) (*quotaTestClient, *stubR2) {
	t.Helper()
	r, stub := newSchoolQuotaTestRouter(pool)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &quotaTestClient{srv: srv, client: &http.Client{Jar: jar}}, stub
}

func (tc *quotaTestClient) login(t *testing.T, userID int) {
	t.Helper()
	status, resp := postForm(t, tc.client, tc.srv, "/test/login/"+strconv.Itoa(userID), nil)
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("test login as user %d: status=%d resp=%+v", userID, status, resp)
	}
}

// upload posts a PDF to /admin/api/upload as the currently-logged-in user and
// returns the HTTP status and raw body.
func (tc *quotaTestClient) upload(t *testing.T, name string, pdf []byte) (int, string) {
	t.Helper()
	body, contentType := editExamMultipart(t, name, pdf, name+".pdf")
	req, err := http.NewRequest(http.MethodPost, tc.srv.URL+"/admin/api/upload", body)
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readAllString(t, resp)
}

// start posts to /admin/api/exams/:id/start as the currently-logged-in user.
func (tc *quotaTestClient) start(t *testing.T, examID int) (int, apiResp) {
	t.Helper()
	return postForm(t, tc.client, tc.srv, "/admin/api/exams/"+strconv.Itoa(examID)+"/start", nil)
}

// toggle posts to /admin/api/exams/:id/toggle as the currently-logged-in user.
func (tc *quotaTestClient) toggle(t *testing.T, examID int) (int, apiResp) {
	t.Helper()
	return postForm(t, tc.client, tc.srv, "/admin/api/exams/"+strconv.Itoa(examID)+"/toggle", nil)
}

// edit posts an EditExam request (name + replacement PDF) as the
// currently-logged-in user and returns the HTTP status and raw body.
func (tc *quotaTestClient) edit(t *testing.T, examID int, newName string, pdf []byte) (int, string) {
	t.Helper()
	body, contentType := editExamMultipart(t, newName, pdf, newName+".pdf")
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/admin/api/exams/%d/edit", tc.srv.URL, examID), body)
	if err != nil {
		t.Fatalf("new edit request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := tc.client.Do(req)
	if err != nil {
		t.Fatalf("edit request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readAllString(t, resp)
}

// bulkToggle posts a bulk-toggle request as the currently-logged-in user.
func (tc *quotaTestClient) bulkToggle(t *testing.T, ids []int, status string) (int, apiResp) {
	t.Helper()
	return postJSON(t, tc.client, tc.srv, "/admin/api/exams/bulk-toggle",
		map[string]interface{}{"ids": ids, "status": status})
}

func readAllString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(b)
}

// schoolTestPDF builds a payload validatePDF accepts (%%PDF prefix, %%EOF in
// the tail, no dangerous signatures) of exactly n bytes.
func schoolTestPDF(n int) []byte {
	filler := n - len("%PDF-1.4\n") - len("\n%%EOF\n")
	if filler < 0 {
		filler = 0
	}
	return []byte("%PDF-1.4\n" + strings.Repeat("a", filler) + "\n%%EOF\n")
}

// createSchoolQuotaUser creates an account in the given instansi with the
// given roles and per-account limits.
func createSchoolQuotaUser(t *testing.T, pool *pgxpool.Pool, username, instansi string, roles []string, maxExams int) int {
	t.Helper()
	u, err := models.CreateUser(context.Background(), pool, &models.AdminUser{
		Username: username, Name: username, PasswordHash: "x",
		Status: models.UserStatusActive, Instansi: instansi,
		Role:              models.SerializeRoles(roles),
		MaxExams:          maxExams,
		MaxPDFSize:        1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:    50 * 1024 * 1024,
		Package:           "free",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return u.ID
}

// plantSchoolRedemption plants an ACTIVE school-package redemption for the
// operator — the snapshot the shared school pool is sourced from. Limits are
// caller-chosen so each test drives one dimension of the pool independently.
func plantSchoolRedemption(t *testing.T, pool *pgxpool.Pool, userID int, maxExams, maxPDF, maxConcurrent, maxStorage int64) {
	t.Helper()
	createSchoolVoucher(t, pool)
	ctx := context.Background()
	var vid int
	if err := pool.QueryRow(ctx, `SELECT id FROM vouchers WHERE code = 'IT-SEKOLAH'`).Scan(&vid); err != nil {
		t.Fatalf("plantSchoolRedemption: find IT-SEKOLAH voucher: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO voucher_redemptions
			(voucher_id, user_id, remaining_seconds, activated_at, is_active, package,
			 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
		VALUES ($1, $2, $3, now(), true, 'sekolah-test', $4, $5, $6, $7, 0, $8)`,
		vid, userID, 30*86400, maxExams, maxPDF, maxConcurrent, maxStorage,
		models.SerializeRoles([]string{models.RoleOperator})); err != nil {
		t.Fatalf("plantSchoolRedemption: insert redemption for user %d: %v", userID, err)
	}
}

// assertOrphanCleaned asserts the just-rejected upload (which DID reach R2
// before the quota gate) had its orphan object deleted.
func assertOrphanCleaned(t *testing.T, stub *stubR2, uploadsBefore int) {
	t.Helper()
	if len(stub.uploads) != uploadsBefore+1 {
		t.Fatalf("expected one R2 upload, got %d (uploads=%v)", len(stub.uploads), stub.uploads)
	}
	last := stub.uploads[len(stub.uploads)-1]
	if len(stub.deletes) == 0 || stub.deletes[len(stub.deletes)-1] != last {
		t.Fatalf("rejected upload's R2 orphan not cleaned: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}
}

// ---------------------------------------------------------------------------
// Total-exam pool
// ---------------------------------------------------------------------------

func TestSchoolPoolCapsTotalExamsAcrossAccounts(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-exams", "SMK Pool Exam", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub1 := createSchoolQuotaUser(t, pool, "sub-pool-exams-1", "SMK Pool Exam", []string{models.RoleGuru}, 3)
	sub2 := createSchoolQuotaUser(t, pool, "sub-pool-exams-2", "SMK Pool Exam", []string{models.RoleGuru}, 3)
	// School package: 3 exams for the WHOLE school (the old behaviour gave
	// each of the 3 accounts its own 3 → up to 9).
	plantSchoolRedemption(t, pool, op, 3, 50*1024*1024, 3, 500*1024*1024)

	tc, stub := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	// Operator (1 upload) + two sub-accounts (1 each) fill the 3-exam pool.
	tc.login(t, op)
	if status, _ := tc.upload(t, "op-exam", pdf); status != http.StatusOK {
		t.Fatalf("operator upload: status=%d", status)
	}
	tc.login(t, sub1)
	if status, _ := tc.upload(t, "sub1-exam", pdf); status != http.StatusOK {
		t.Fatalf("sub1 upload: status=%d", status)
	}
	tc.login(t, sub2)
	if status, _ := tc.upload(t, "sub2-exam", pdf); status != http.StatusOK {
		t.Fatalf("sub2 upload: status=%d", status)
	}

	if n, err := models.CountExamsByInstansi(ctx, pool, "SMK Pool Exam"); err != nil || n != 3 {
		t.Fatalf("school pool usage: got %d err=%v, want 3", n, err)
	}

	// 4th upload by a sub-account → 403 school-pool limit + orphan cleanup.
	uploadsBefore := len(stub.uploads)
	tc.login(t, sub1)
	status, body := tc.upload(t, "sub1-exam-4", pdf)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas pembuatan ujian sekolah") {
		t.Fatalf("sub1 4th upload: status=%d body=%s", status, body)
	}
	assertOrphanCleaned(t, stub, uploadsBefore)

	// The operator's OWN upload is capped by the pool too: the operator's
	// usage spends the school package, so no bypass once the school is full.
	uploadsBefore = len(stub.uploads)
	tc.login(t, op)
	status, body = tc.upload(t, "op-exam-4", pdf)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas pembuatan ujian sekolah") {
		t.Fatalf("operator 4th upload: status=%d body=%s", status, body)
	}
	assertOrphanCleaned(t, stub, uploadsBefore)

	if n, err := models.CountExamsByInstansi(ctx, pool, "SMK Pool Exam"); err != nil || n != 3 {
		t.Fatalf("school pool usage after rejections: got %d err=%v, want 3", n, err)
	}
}

// ---------------------------------------------------------------------------
// Storage pool
// ---------------------------------------------------------------------------

func TestSchoolPoolStorageCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-storage", "SMK Pool Storage", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub := createSchoolQuotaUser(t, pool, "sub-pool-storage", "SMK Pool Storage", []string{models.RoleGuru}, 10)
	// 300-byte school storage pool (tiny on purpose).
	plantSchoolRedemption(t, pool, op, 10, 50*1024*1024, 10, 300)

	tc, stub := newQuotaTestClient(t, pool)
	big := schoolTestPDF(200)

	tc.login(t, op)
	if status, _ := tc.upload(t, "op-storage-1", big); status != http.StatusOK {
		t.Fatalf("operator storage upload: status=%d", status)
	}

	// Sub-account: 200 + 200 = 400 > 300 → 403 school storage. The storage
	// pre-check rejects BEFORE the PDF ever reaches R2 (friendly early
	// rejection; the in-transaction gate below is the race backstop).
	uploadsBefore := len(stub.uploads)
	tc.login(t, sub)
	status, body := tc.upload(t, "sub-storage-1", big)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas kapasitas storage sekolah") {
		t.Fatalf("sub storage overflow: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore || len(stub.deletes) != 0 {
		t.Fatalf("storage rejection must not reach R2: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}

	// The operator is capped by the school storage pool too.
	uploadsBefore = len(stub.uploads)
	tc.login(t, op)
	status, body = tc.upload(t, "op-storage-2", big)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas kapasitas storage sekolah") {
		t.Fatalf("operator storage overflow: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore || len(stub.deletes) != 0 {
		t.Fatalf("storage rejection must not reach R2: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}

	// Small upload still fits: 200 + 50 = 250 ≤ 300.
	tc.login(t, sub)
	if status, _ := tc.upload(t, "sub-storage-2", schoolTestPDF(50)); status != http.StatusOK {
		t.Fatalf("sub small storage upload: status=%d", status)
	}
}

// ---------------------------------------------------------------------------
// PDF-size pool
// ---------------------------------------------------------------------------

func TestSchoolPoolPDFSizeCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-pdf", "SMK Pool PDF", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub := createSchoolQuotaUser(t, pool, "sub-pool-pdf", "SMK Pool PDF", []string{models.RoleGuru}, 10)
	// 100-byte school PDF limit (tiny on purpose).
	plantSchoolRedemption(t, pool, op, 10, 100, 10, 500*1024*1024)

	tc, stub := newQuotaTestClient(t, pool)

	// Oversized PDF is rejected BEFORE it ever reaches R2 — and the operator
	// is subject to the pool PDF limit just like a sub-account.
	uploadsBefore := len(stub.uploads)
	tc.login(t, op)
	status, body := tc.upload(t, "op-pdf-oversize", schoolTestPDF(200))
	if status != http.StatusForbidden || !strings.Contains(body, "Ukuran file melebihi batas paket sekolah") {
		t.Fatalf("operator oversize PDF: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore {
		t.Fatalf("PDF-size rejection must not upload to R2: uploads=%v", stub.uploads)
	}

	// An in-limit PDF uploads fine (sub-account).
	tc.login(t, sub)
	if status, _ := tc.upload(t, "sub-pdf-ok", schoolTestPDF(50)); status != http.StatusOK {
		t.Fatalf("sub in-limit PDF: status=%d", status)
	}
}

// ---------------------------------------------------------------------------
// Concurrent pool (start + toggle)
// ---------------------------------------------------------------------------

func TestSchoolPoolConcurrentCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-conc", "SMK Pool Conc", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub1 := createSchoolQuotaUser(t, pool, "sub-pool-conc-1", "SMK Pool Conc", []string{models.RoleGuru}, 10)
	sub2 := createSchoolQuotaUser(t, pool, "sub-pool-conc-2", "SMK Pool Conc", []string{models.RoleGuru}, 10)
	// School package: only 1 exam may run at a time across the WHOLE school.
	plantSchoolRedemption(t, pool, op, 10, 50*1024*1024, 1, 500*1024*1024)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	upload := func(userID int, name string) int {
		tc.login(t, userID)
		status, body := tc.upload(t, name, pdf)
		if status != http.StatusOK {
			t.Fatalf("upload %s: status=%d body=%s", name, status, body)
		}
		var examID int
		if err := pool.QueryRow(context.Background(),
			`SELECT id FROM exams WHERE name = $1 ORDER BY id DESC LIMIT 1`, name).Scan(&examID); err != nil {
			t.Fatalf("find exam %s: %v", name, err)
		}
		return examID
	}

	// Operator starts exam A → the school's single concurrent slot is taken.
	examA := upload(op, "op-conc-a")
	tc.login(t, op)
	if status, resp := tc.start(t, examA); status != http.StatusOK {
		t.Fatalf("operator start A: status=%d resp=%+v", status, resp)
	}

	// Sub-account start → 403 school-pool limit.
	examB := upload(sub1, "sub-conc-b")
	tc.login(t, sub1)
	status, resp := tc.start(t, examB)
	if status != http.StatusForbidden || !strings.Contains(resp.Body, "Batas ujian serentak sekolah") {
		t.Fatalf("sub1 start B: status=%d resp=%+v", status, resp)
	}

	// The operator's OWN start is capped by the pool too.
	examC := upload(op, "op-conc-c")
	tc.login(t, op)
	status, resp = tc.start(t, examC)
	if status != http.StatusForbidden || !strings.Contains(resp.Body, "Batas ujian serentak sekolah") {
		t.Fatalf("operator start C: status=%d resp=%+v", status, resp)
	}

	// Stopping A (toggle → inactive) frees the slot; the sub may now start B.
	tc.login(t, op)
	if status, resp := tc.toggle(t, examA); status != http.StatusOK {
		t.Fatalf("toggle A: status=%d resp=%+v", status, resp)
	}
	tc.login(t, sub1)
	if status, resp := tc.start(t, examB); status != http.StatusOK {
		t.Fatalf("sub1 start B after stop: status=%d resp=%+v", status, resp)
	}

	// Toggle path: re-activating an already-started exam counts against the
	// pool too. B is stopped, D is started → 1 running; re-activating A would
	// make 2 → 403.
	tc.login(t, sub1)
	if status, resp := tc.toggle(t, examB); status != http.StatusOK {
		t.Fatalf("toggle B: status=%d resp=%+v", status, resp)
	}
	examD := upload(sub2, "sub-conc-d")
	tc.login(t, sub2)
	if status, resp := tc.start(t, examD); status != http.StatusOK {
		t.Fatalf("sub2 start D: status=%d resp=%+v", status, resp)
	}
	tc.login(t, op)
	status, resp = tc.toggle(t, examA)
	if status != http.StatusForbidden || !strings.Contains(resp.Body, "Batas ujian serentak sekolah") {
		t.Fatalf("toggle A while pool full: status=%d resp=%+v", status, resp)
	}
}

// ---------------------------------------------------------------------------
// Per-account limits still apply inside a school pool
// ---------------------------------------------------------------------------

func TestSchoolPoolSubAccountOwnLimitStillApplies(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-own", "SMK Pool Own", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub := createSchoolQuotaUser(t, pool, "sub-pool-own", "SMK Pool Own", []string{models.RoleGuru}, 1)
	// Generous school pool (5 exams) — the sub's own 1-exam limit must bind.
	plantSchoolRedemption(t, pool, op, 5, 50*1024*1024, 5, 500*1024*1024)

	tc, stub := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	tc.login(t, sub)
	if status, _ := tc.upload(t, "sub-own-1", pdf); status != http.StatusOK {
		t.Fatalf("sub first upload: status=%d", status)
	}
	uploadsBefore := len(stub.uploads)
	status, body := tc.upload(t, "sub-own-2", pdf)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas akun Anda adalah 1 ujian") {
		t.Fatalf("sub second upload: status=%d body=%s", status, body)
	}
	assertOrphanCleaned(t, stub, uploadsBefore)

	// The operator still has pool room (1/5) and is not bound by the sub's
	// per-account limit.
	tc.login(t, op)
	if status, _ := tc.upload(t, "op-own-1", pdf); status != http.StatusOK {
		t.Fatalf("operator upload: status=%d", status)
	}
}

// ---------------------------------------------------------------------------
// No pool outside a real school instansi
// ---------------------------------------------------------------------------

func TestSchoolPoolNotAppliedInPersonalBucket(t *testing.T) {
	pool := setupVoucherITDB(t)

	// The shared "personal" bucket is not a school: an operator holding a
	// school redemption there must keep the historical per-account behaviour
	// (operator bypass), never a cross-tenant school pool.
	op := createSchoolQuotaUser(t, pool, "op-personal-pool", "personal", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub := createSchoolQuotaUser(t, pool, "sub-personal-pool", "personal", []string{models.RoleGuru}, 3)
	plantSchoolRedemption(t, pool, op, 3, 50*1024*1024, 3, 500*1024*1024)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	// Operator bypass preserved: 6 uploads despite the 3-exam redemption.
	tc.login(t, op)
	for i := 0; i < 6; i++ {
		status, body := tc.upload(t, "op-personal-exam", pdf)
		if status != http.StatusOK {
			t.Fatalf("operator personal upload %d: status=%d body=%s", i, status, body)
		}
	}

	// Sub-accounts keep their own per-account limits (no school pool).
	tc.login(t, sub)
	for i := 0; i < 3; i++ {
		if status, body := tc.upload(t, "sub-personal-exam", pdf); status != http.StatusOK {
			t.Fatalf("sub personal upload %d: status=%d body=%s", i, status, body)
		}
	}
	status, body := tc.upload(t, "sub-personal-exam-4", pdf)
	if status != http.StatusForbidden || !strings.Contains(body, "Batas akun Anda adalah 3 ujian") {
		t.Fatalf("sub personal 4th upload: status=%d body=%s", status, body)
	}
}

// ---------------------------------------------------------------------------
// Bulk activation pool
// ---------------------------------------------------------------------------

func TestSchoolPoolConcurrentBulkToggleCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-bulk", "SMK Pool Bulk", []string{models.RoleGuru, models.RoleOperator}, 10)
	sub1 := createSchoolQuotaUser(t, pool, "sub-pool-bulk-1", "SMK Pool Bulk", []string{models.RoleGuru}, 10)
	sub2 := createSchoolQuotaUser(t, pool, "sub-pool-bulk-2", "SMK Pool Bulk", []string{models.RoleGuru}, 10)
	// School package: only 1 exam may run at a time across the WHOLE school.
	plantSchoolRedemption(t, pool, op, 10, 50*1024*1024, 1, 500*1024*1024)

	tc, _ := newQuotaTestClient(t, pool)
	pdf := schoolTestPDF(64)

	uploadAndGet := func(userID int, name string) int {
		tc.login(t, userID)
		status, body := tc.upload(t, name, pdf)
		if status != http.StatusOK {
			t.Fatalf("upload %s: status=%d body=%s", name, status, body)
		}
		var examID int
		if err := pool.QueryRow(context.Background(),
			`SELECT id FROM exams WHERE name = $1 ORDER BY id DESC LIMIT 1`, name).Scan(&examID); err != nil {
			t.Fatalf("find exam %s: %v", name, err)
		}
		return examID
	}
	startOrFail := func(userID int, examID int, wantOK bool) {
		tc.login(t, userID)
		status, resp := tc.start(t, examID)
		if wantOK && status != http.StatusOK {
			t.Fatalf("start exam %d: status=%d resp=%+v", examID, status, resp)
		}
		if !wantOK && (status != http.StatusForbidden || !strings.Contains(resp.Body, "Batas ujian serentak sekolah")) {
			t.Fatalf("start exam %d: status=%d resp=%+v", examID, status, resp)
		}
	}

	// A running (1/1 pool), so B cannot start — B stays never-started.
	examA := uploadAndGet(op, "bulk-a")
	startOrFail(op, examA, true)
	examB := uploadAndGet(sub1, "bulk-b")
	startOrFail(sub1, examB, false)

	// A stopped → C starts (1/1 pool again).
	tc.login(t, op)
	if status, resp := tc.toggle(t, examA); status != http.StatusOK {
		t.Fatalf("toggle A: status=%d resp=%+v", status, resp)
	}
	examC := uploadAndGet(sub2, "bulk-c")
	startOrFail(sub2, examC, true)

	// Bulk-activating A (started-but-inactive) on top of running C would give
	// the school 2 running exams > 1 → 403. B was never started, so it does
	// not contribute.
	tc.login(t, op)
	status, resp := tc.bulkToggle(t, []int{examA, examB}, "active")
	if status != http.StatusForbidden || !strings.Contains(resp.Body, "Batas ujian serentak sekolah") {
		t.Fatalf("bulk activate A+B: status=%d resp=%+v", status, resp)
	}

	// Stopping C frees the slot: bulk-activating A alone is now allowed
	// (reaching the limit is allowed, only exceeding it is rejected).
	tc.login(t, sub2)
	if status, resp := tc.toggle(t, examC); status != http.StatusOK {
		t.Fatalf("toggle C: status=%d resp=%+v", status, resp)
	}
	tc.login(t, op)
	status, resp = tc.bulkToggle(t, []int{examA}, "active")
	if status != http.StatusOK {
		t.Fatalf("bulk activate A after stop: status=%d resp=%+v", status, resp)
	}
}

// ---------------------------------------------------------------------------
// EditExam (PDF replacement) pool
// ---------------------------------------------------------------------------

func TestSchoolPoolEditReplacementCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-edit", "SMK Pool Edit", []string{models.RoleGuru, models.RoleOperator}, 10)
	// 100-byte school PDF limit; applies to the operator too.
	plantSchoolRedemption(t, pool, op, 10, 100, 10, 500*1024*1024)

	tc, stub := newQuotaTestClient(t, pool)

	tc.login(t, op)
	if status, _ := tc.upload(t, "edit-exam", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("initial upload: status=%d", status)
	}
	var examID int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM exams WHERE name = 'edit-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	// Replacement PDF above the school PDF limit → 403 BEFORE any R2 object
	// is uploaded (the old PDF must stay untouched).
	uploadsBefore := len(stub.uploads)
	status, body := tc.edit(t, examID, "edit-exam", schoolTestPDF(200))
	if status != http.StatusForbidden || !strings.Contains(body, "Ukuran file melebihi batas paket sekolah") {
		t.Fatalf("edit oversize PDF: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore || len(stub.deletes) != 0 {
		t.Fatalf("rejected edit must not touch R2: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}

	// In-limit replacement works and swaps the R2 object (old deleted after
	// the new one is committed).
	if status, body := tc.edit(t, examID, "edit-exam", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("edit in-limit PDF: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore+1 || len(stub.deletes) != 1 {
		t.Fatalf("in-limit edit must upload the new object and delete the old: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}
}

// Storage is a DELTA check on replacement: only the growth over the old PDF's
// bytes counts against the school storage pool.
func TestSchoolPoolEditStorageDeltaCapped(t *testing.T) {
	pool := setupVoucherITDB(t)

	op := createSchoolQuotaUser(t, pool, "op-pool-edit-storage", "SMK Pool EditSto", []string{models.RoleGuru, models.RoleOperator}, 10)
	// 300-byte school storage pool (tiny on purpose); PDF limit generous so
	// only the storage dimension binds.
	plantSchoolRedemption(t, pool, op, 10, 1024*1024, 10, 300)

	tc, stub := newQuotaTestClient(t, pool)

	tc.login(t, op)
	if status, _ := tc.upload(t, "edit-sto-exam", schoolTestPDF(200)); status != http.StatusOK {
		t.Fatalf("initial upload: status=%d", status)
	}
	var examID int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM exams WHERE name = 'edit-sto-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	// Shrinking is always fine (200 → 150 frees 50 bytes).
	if status, body := tc.edit(t, examID, "edit-sto-exam", schoolTestPDF(150)); status != http.StatusOK {
		t.Fatalf("edit smaller: status=%d body=%s", status, body)
	}
	// Growing up to the cap exactly is allowed (150 → 300 fills the pool).
	if status, body := tc.edit(t, examID, "edit-sto-exam", schoolTestPDF(300)); status != http.StatusOK {
		t.Fatalf("edit to pool cap: status=%d body=%s", status, body)
	}
	// Growing past it → 403 school storage, R2 objects untouched.
	uploadsBefore, deletesBefore := len(stub.uploads), len(stub.deletes)
	status, body := tc.edit(t, examID, "edit-sto-exam", schoolTestPDF(301))
	if status != http.StatusForbidden || !strings.Contains(body, "Batas kapasitas storage sekolah") {
		t.Fatalf("edit storage overflow: status=%d body=%s", status, body)
	}
	if len(stub.uploads) != uploadsBefore || len(stub.deletes) != deletesBefore {
		t.Fatalf("storage rejection must not touch R2: uploads=%v deletes=%v", stub.uploads, stub.deletes)
	}
}

// The account's own limits still bind on replacement when no school pool
// constrains that dimension harder.
func TestSchoolPoolEditPerAccountPDFLimitStillApplies(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-edit2", "SMK Pool Edit2", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 10, 1024*1024, 10, 50*1024*1024)

	// Sub-account with a tiny own PDF limit (100 bytes).
	sub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "sub-pool-edit2", Name: "sub-pool-edit2", PasswordHash: "x",
		Status: models.UserStatusActive, Instansi: "SMK Pool Edit2",
		Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 10, MaxPDFSize: 100, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create sub: %v", err)
	}

	tc, _ := newQuotaTestClient(t, pool)

	tc.login(t, sub.ID)
	if status, _ := tc.upload(t, "edit2-exam", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("initial upload: status=%d", status)
	}
	var examID int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'edit2-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	// The school pool allows the size, the account's own PDF limit rejects it.
	status, body := tc.edit(t, examID, "edit2-exam", schoolTestPDF(150))
	if status != http.StatusForbidden || !strings.Contains(body, "Ukuran file melebihi batas akun Anda") {
		t.Fatalf("edit above own PDF limit: status=%d body=%s", status, body)
	}
}

// The account's own storage limit still binds on replacement (delta check).
func TestSchoolPoolEditPerAccountStorageLimitStillApplies(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-edit3", "SMK Pool Edit3", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 10, 1024*1024, 10, 50*1024*1024)

	// Sub-account with a tiny own storage limit (300 bytes).
	sub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "sub-pool-edit3", Name: "sub-pool-edit3", PasswordHash: "x",
		Status: models.UserStatusActive, Instansi: "SMK Pool Edit3",
		Role: models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 10, MaxPDFSize: 1024 * 1024, MaxConcurrentExams: 2,
		MaxStorageSize: 300, Package: "free",
	})
	if err != nil {
		t.Fatalf("create sub: %v", err)
	}

	tc, _ := newQuotaTestClient(t, pool)

	tc.login(t, sub.ID)
	if status, _ := tc.upload(t, "edit3-exam", schoolTestPDF(200)); status != http.StatusOK {
		t.Fatalf("initial upload: status=%d", status)
	}
	var examID int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'edit3-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	// 200 → 301 grows past the own 300-byte cap → 403 account storage.
	status, body := tc.edit(t, examID, "edit3-exam", schoolTestPDF(301))
	if status != http.StatusForbidden || !strings.Contains(body, "Batas kapasitas storage tercapai. Batas akun Anda adalah 0.0 MB") {
		t.Fatalf("edit above own storage limit: status=%d body=%s", status, body)
	}
}

// ---------------------------------------------------------------------------
// Atomic concurrent enforcement
// ---------------------------------------------------------------------------

// TestSchoolPoolConcurrentStartRace fires 4 simultaneous starts at a
// 1-concurrent-slot school pool. The FOR UPDATE pool lock serializes them, so
// EXACTLY one start may win; without the atomic path two concurrent starts
// could both count the same free slot and both succeed.
func TestSchoolPoolConcurrentStartRace(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-race", "SMK Pool Race", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 10, 50*1024*1024, 1, 500*1024*1024)

	subIDs := make([]int, 4)
	for i := range subIDs {
		subIDs[i] = createSchoolQuotaUser(t, pool, fmt.Sprintf("sub-pool-race-%d", i), "SMK Pool Race", []string{models.RoleGuru}, 10)
	}

	pdf := schoolTestPDF(64)
	type runner struct {
		tc     *quotaTestClient
		examID int
		req    *http.Request
	}
	runners := make([]runner, 4)
	for i := 0; i < 4; i++ {
		tc, _ := newQuotaTestClient(t, pool)
		tc.login(t, subIDs[i])
		status, body := tc.upload(t, fmt.Sprintf("race-%d", i), pdf)
		if status != http.StatusOK {
			t.Fatalf("race upload %d: status=%d body=%s", i, status, body)
		}
		var examID int
		if err := pool.QueryRow(ctx,
			`SELECT id FROM exams WHERE name = $1`, fmt.Sprintf("race-%d", i)).Scan(&examID); err != nil {
			t.Fatalf("find race exam %d: %v", i, err)
		}
		req, err := http.NewRequest(http.MethodPost,
			tc.srv.URL+"/admin/api/exams/"+strconv.Itoa(examID)+"/start", nil)
		if err != nil {
			t.Fatalf("new start request %d: %v", i, err)
		}
		runners[i] = runner{tc: tc, examID: examID, req: req}
	}

	start := make(chan struct{})
	results := make(chan int, len(runners))
	for i := range runners {
		go func(r *runner) {
			<-start
			resp, err := r.tc.client.Do(r.req)
			if err != nil {
				results <- -1
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}(&runners[i])
	}
	close(start)

	ok, rejected := 0, 0
	for range runners {
		switch code := <-results; code {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			rejected++
		default:
			t.Fatalf("unexpected start status %d", code)
		}
	}
	if ok != 1 || rejected != 3 {
		t.Fatalf("concurrent starts: want exactly 1 success + 3 rejects, got %d + %d", ok, rejected)
	}

	// The DB agrees: exactly one exam in the school is running.
	if n, err := models.CountRunningExamsByInstansi(ctx, pool, "SMK Pool Race", 0); err != nil || n != 1 {
		t.Fatalf("running exams after race: got %d err=%v, want 1", n, err)
	}
}

// ---------------------------------------------------------------------------
// Operator without a school pool (personal bucket) — per-account quota binds
// ---------------------------------------------------------------------------

// TestOperatorPersonalBucketQuotaBinds locks in the fail-open fix: an operator
// who redeemed a school package but has NOT yet claimed a school name sits in
// the shared "personal" bucket, where schoolPoolQuota returns ok=false (no
// school pool). Before the fix UploadExam/StartExam/ToggleExam bypassed the
// operator's per-account columns UNCONDITIONALLY on the assumption the school
// pool would be the gate — so this transient state had NO quota at all:
// unbounded exam/PDF/storage creation and unbounded concurrent exams. The
// operator's own columns carry the redeemed package snapshot (or the free
// defaults), so when no pool applies they must bind exactly like a legacy
// no-package school.
func TestOperatorPersonalBucketQuotaBinds(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "op-personal-quota", Name: "op-personal-quota", PasswordHash: "x",
		Status: models.UserStatusActive, Instansi: "personal",
		Role: models.SerializeRoles([]string{models.RoleGuru, models.RoleOperator}),
		MaxExams: 2, MaxPDFSize: 1024 * 1024, MaxConcurrentExams: 1,
		MaxStorageSize: 300, Package: "sekolah-test",
	})
	if err != nil {
		t.Fatalf("create personal-bucket operator: %v", err)
	}

	tc, _ := newQuotaTestClient(t, pool)
	tc.login(t, op.ID)

	// Two uploads fit the per-account 2-exam cap (storage: 200 + 50 ≤ 300).
	if status, body := tc.upload(t, "personal-1", schoolTestPDF(200)); status != http.StatusOK {
		t.Fatalf("personal upload 1: status=%d body=%s", status, body)
	}
	if status, body := tc.upload(t, "personal-2", schoolTestPDF(50)); status != http.StatusOK {
		t.Fatalf("personal upload 2: status=%d body=%s", status, body)
	}

	// Third upload → the account exam cap binds (was unlimited pre-fix).
	status, body := tc.upload(t, "personal-3", schoolTestPDF(50))
	if status != http.StatusForbidden || !strings.Contains(body, "Batas pembuatan ujian tercapai. Batas akun Anda adalah 2 ujian.") {
		t.Fatalf("personal 3rd upload: status=%d body=%s, want the per-account exam cap", status, body)
	}

	// Concurrent cap binds too: starting a second exam while one runs → 403.
	var id1, id2 int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'personal-1'`).Scan(&id1); err != nil {
		t.Fatalf("find personal-1: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'personal-2'`).Scan(&id2); err != nil {
		t.Fatalf("find personal-2: %v", err)
	}
	if s, r := tc.start(t, id1); s != http.StatusOK {
		t.Fatalf("start personal-1: status=%d resp=%+v", s, r)
	}
	s, r := tc.start(t, id2)
	if s != http.StatusForbidden || !strings.Contains(r.Message, "Batas ujian serentak tercapai. Maksimal 1 ujian dapat berjalan bersamaan.") {
		t.Fatalf("start personal-2 while one runs: status=%d resp=%+v, want the per-account concurrent cap", s, r)
	}
}

// ---------------------------------------------------------------------------
// Atomic bulk activation (BulkToggle)
// ---------------------------------------------------------------------------

// TestSchoolPoolConcurrentBulkToggleRace fires 4 simultaneous bulk activations
// of DIFFERENT started exams in a 1-concurrent-slot school pool. The pool lock
// (instansi rows FOR UPDATE) serializes them, so exactly ONE activation wins;
// without the atomic path each request counts the same free slot and the school
// ends up with 4 running exams in a 1-slot pool.
func TestSchoolPoolConcurrentBulkToggleRace(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-bulk", "SMK Pool Bulk", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 10, 50*1024*1024, 1, 500*1024*1024)

	subIDs := make([]int, 4)
	for i := range subIDs {
		subIDs[i] = createSchoolQuotaUser(t, pool, fmt.Sprintf("sub-pool-bulk-%d", i), "SMK Pool Bulk", []string{models.RoleGuru}, 10)
	}

	// Each sub-account uploads one exam; we then plant started-but-INACTIVE
	// state directly (activating such an exam is what makes it running). The
	// API start path cannot be used here: starting an exam makes it running
	// immediately, which the 1-slot pool would reject.
	runners := make([]struct {
		tc  *quotaTestClient
		req *http.Request
	}, 4)
	for i := 0; i < 4; i++ {
		tc, _ := newQuotaTestClient(t, pool)
		tc.login(t, subIDs[i])
		status, body := tc.upload(t, fmt.Sprintf("bulk-%d", i), schoolTestPDF(64))
		if status != http.StatusOK {
			t.Fatalf("bulk upload %d: status=%d body=%s", i, status, body)
		}
		var examID int
		if err := pool.QueryRow(ctx,
			`SELECT id FROM exams WHERE name = $1`, fmt.Sprintf("bulk-%d", i)).Scan(&examID); err != nil {
			t.Fatalf("find bulk exam %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE exams SET exam_started_at = now() WHERE id = $1`, examID); err != nil {
			t.Fatalf("plant started-but-inactive state %d: %v", i, err)
		}
		req, err := http.NewRequest(http.MethodPost,
			tc.srv.URL+"/admin/api/exams/bulk-toggle",
			strings.NewReader(fmt.Sprintf(`{"ids":[%d],"status":"active"}`, examID)))
		if err != nil {
			t.Fatalf("new bulk-toggle request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", "application/json")
		runners[i] = struct {
			tc  *quotaTestClient
			req *http.Request
		}{tc: tc, req: req}
	}

	start := make(chan struct{})
	results := make(chan int, len(runners))
	for i := range runners {
		go func(r *struct {
			tc  *quotaTestClient
			req *http.Request
		}) {
			<-start
			resp, err := r.tc.client.Do(r.req)
			if err != nil {
				results <- -1
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}(&runners[i])
	}
	close(start)

	ok, rejected, other := 0, 0, 0
	for range runners {
		switch code := <-results; code {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			rejected++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent bulk toggles: %d unexpected responses", other)
	}
	if ok != 1 || rejected != 3 {
		t.Fatalf("concurrent bulk toggles: want exactly 1 success + 3 rejects, got %d + %d", ok, rejected)
	}

	// The DB agrees: exactly one exam in the school is running.
	if n, err := models.CountRunningExamsByInstansi(ctx, pool, "SMK Pool Bulk", 0); err != nil || n != 1 {
		t.Fatalf("running exams after bulk race: got %d err=%v, want 1", n, err)
	}
}

// TestSchoolPoolConcurrentEditStorageRace fires 4 simultaneous PDF replacements
// of DIFFERENT exams in a 400-byte school storage pool. Each replacement grows
// its exam by 200 bytes and each request individually passes the pre-check
// (200 used - 50 old + 250 new = 400 ≤ 400), but all four committing would end
// the school at 1000 bytes. The atomic storage-delta gate (instansi rows
// FOR UPDATE + re-check + UPDATE in one tx, mirroring UploadExam) serializes
// the replacements so EXACTLY one wins and the school never exceeds the pool.
func TestSchoolPoolConcurrentEditStorageRace(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-editrace", "SMK Pool EditRace", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 10, 1024*1024, 10, 400)

	subIDs := make([]int, 4)
	for i := range subIDs {
		subIDs[i] = createSchoolQuotaUser(t, pool, fmt.Sprintf("sub-pool-editrace-%d", i), "SMK Pool EditRace", []string{models.RoleGuru}, 10)
	}

	// One shared router + stub so the R2 upload delay applies to every runner:
	// the delay widens the pre-check→commit window so ALL four pre-checks read
	// the stale 200-byte snapshot before any replacement commits — without it
	// the race window is too small for the mutation to fail reliably.
	r, stub := newSchoolQuotaTestRouter(pool)
	stub.uploadDelay = 150 * time.Millisecond
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// Each sub-account uploads a 50-byte exam (pool at 200/400), then we stage
	// its 50 → 250 byte replacement (delta +200; each alone fits the pre-check
	// 200-50+250 = 400 ≤ 400, all four together would blow the pool to 1000).
	runners := make([]struct {
		tc  *quotaTestClient
		req *http.Request
	}, 4)
	for i := 0; i < 4; i++ {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar %d: %v", i, err)
		}
		tc := &quotaTestClient{srv: srv, client: &http.Client{Jar: jar}}
		tc.login(t, subIDs[i])
		if status, body := tc.upload(t, fmt.Sprintf("editrace-%d", i), schoolTestPDF(50)); status != http.StatusOK {
			t.Fatalf("editrace upload %d: status=%d body=%s", i, status, body)
		}
		var examID int
		if err := pool.QueryRow(ctx,
			`SELECT id FROM exams WHERE name = $1`, fmt.Sprintf("editrace-%d", i)).Scan(&examID); err != nil {
			t.Fatalf("find editrace exam %d: %v", i, err)
		}
		body, contentType := editExamMultipart(t, fmt.Sprintf("editrace-%d", i), schoolTestPDF(250), fmt.Sprintf("editrace-%d.pdf", i))
		req, err := http.NewRequest(http.MethodPost,
			tc.srv.URL+"/admin/api/exams/"+strconv.Itoa(examID)+"/edit", body)
		if err != nil {
			t.Fatalf("new edit request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", contentType)
		runners[i] = struct {
			tc  *quotaTestClient
			req *http.Request
		}{tc: tc, req: req}
	}

	start := make(chan struct{})
	results := make(chan int, len(runners))
	for i := range runners {
		go func(r *struct {
			tc  *quotaTestClient
			req *http.Request
		}) {
			<-start
			resp, err := r.tc.client.Do(r.req)
			if err != nil {
				results <- -1
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}(&runners[i])
	}
	close(start)

	ok, rejected, other := 0, 0, 0
	for range runners {
		switch code := <-results; code {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			rejected++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent edits: %d unexpected responses", other)
	}
	if ok != 1 || rejected != 3 {
		t.Fatalf("concurrent edits: want exactly 1 success + 3 rejects, got %d + %d", ok, rejected)
	}

	// The DB agrees: the school's total storage never exceeds the 400-byte pool
	// (one 250-byte replacement + three 50-byte originals = exactly 400).
	var poolUsed int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(e.size_bytes), 0) FROM exams e JOIN admin_users u ON e.created_by = u.id WHERE u.instansi = $1`,
		"SMK Pool EditRace").Scan(&poolUsed); err != nil {
		t.Fatalf("sum school storage after edit race: %v", err)
	}
	if poolUsed != 400 {
		t.Fatalf("school storage after edit race: got %d, want 400 (pool cap)", poolUsed)
	}
}

// TestSchoolPoolConcurrentUploadRace fires 2 simultaneous uploads competing
// for the LAST free slot of a 2-exam school pool (the operator pre-planted the
// first exam). The atomic pool gate (instansi rows FOR UPDATE + count + INSERT
// in one tx) serializes them so EXACTLY one fills the slot and the other is
// rejected with the school-pool message; without it both count the same free
// slot and the school ends up with 3 exams in a 2-exam package.
func TestSchoolPoolConcurrentUploadRace(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pool-uploadrace", "SMK Pool UploadRace", []string{models.RoleGuru, models.RoleOperator}, 10)
	plantSchoolRedemption(t, pool, op, 2, 50*1024*1024, 10, 500*1024*1024)

	// Shared router + stub: the R2 upload delay widens the pre-commit window so
	// both requests reach the quota count with the same pre-commit snapshot
	// when the atomic gate is removed (the count check runs AFTER the R2
	// upload, so the delay makes the race deterministic instead of timing luck).
	r, stub := newSchoolQuotaTestRouter(pool)
	stub.uploadDelay = 150 * time.Millisecond
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	mkClient := func(userID int) *quotaTestClient {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		tc := &quotaTestClient{srv: srv, client: &http.Client{Jar: jar}}
		tc.login(t, userID)
		return tc
	}

	// The operator plants the first exam (pool at 1/2). The operator's own
	// upload bypasses the per-account column (pool active) but spends the pool.
	if status, body := mkClient(op).upload(t, "uploadrace-0", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("operator upload: status=%d body=%s", status, body)
	}

	// Two sub-accounts race for the last slot. Their own max_exams (10) never
	// binds below the pool, so the 2-exam school pool is the only gate.
	subIDs := []int{
		createSchoolQuotaUser(t, pool, "sub-pool-uploadrace-0", "SMK Pool UploadRace", []string{models.RoleGuru}, 10),
		createSchoolQuotaUser(t, pool, "sub-pool-uploadrace-1", "SMK Pool UploadRace", []string{models.RoleGuru}, 10),
	}
	runners := make([]struct {
		tc  *quotaTestClient
		req *http.Request
	}, 2)
	for i := range subIDs {
		tc := mkClient(subIDs[i])
		body, contentType := editExamMultipart(t, fmt.Sprintf("uploadrace-%d", i+1), schoolTestPDF(64), fmt.Sprintf("uploadrace-%d.pdf", i+1))
		req, err := http.NewRequest(http.MethodPost, tc.srv.URL+"/admin/api/upload", body)
		if err != nil {
			t.Fatalf("new upload request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", contentType)
		runners[i] = struct {
			tc  *quotaTestClient
			req *http.Request
		}{tc: tc, req: req}
	}

	start := make(chan struct{})
	results := make(chan int, len(runners))
	for i := range runners {
		go func(r *struct {
			tc  *quotaTestClient
			req *http.Request
		}) {
			<-start
			resp, err := r.tc.client.Do(r.req)
			if err != nil {
				results <- -1
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}(&runners[i])
	}
	close(start)

	ok, rejected, other := 0, 0, 0
	for range runners {
		switch code := <-results; code {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			rejected++
		default:
			other++
		}
	}
	if other > 0 {
		t.Fatalf("concurrent uploads: %d unexpected responses", other)
	}
	if ok != 1 || rejected != 1 {
		t.Fatalf("concurrent uploads: want exactly 1 success + 1 reject, got %d + %d", ok, rejected)
	}

	// The DB agrees: the school ends at exactly the 2-exam pool cap.
	if n, err := models.CountExamsByInstansi(ctx, pool, "SMK Pool UploadRace"); err != nil || n != 2 {
		t.Fatalf("school exams after upload race: got %d err=%v, want 2", n, err)
	}
}

// ---------------------------------------------------------------------------
// SaveQuestions pengawas roster scoping
// ---------------------------------------------------------------------------

// TestSaveQuestionsPengawasCrossTenantRejected locks in the SaveQuestions
// pengawas-validation fix: an operator (or SuperAdmin) could previously assign
// ANY user id as pengawas via SaveQuestions — including a user from ANOTHER
// school — granting that outsider supervision access (approvals, submissions)
// to an exam outside their tenant. PostDelegateExam already validated the
// roster; SaveQuestions now runs the same checks: each pengawas must exist, be
// active, hold the pengawas role, and belong to the exam creator's instansi.
func TestSaveQuestionsPengawasCrossTenantRejected(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	op := createSchoolQuotaUser(t, pool, "op-pengawas-a", "SMK Pengawas A", []string{models.RoleGuru, models.RoleOperator}, 10)
	ownPengawas := createSchoolQuotaUser(t, pool, "pengawas-a-1", "SMK Pengawas A", []string{models.RolePengawas}, 10)
	foreignPengawas := createSchoolQuotaUser(t, pool, "pengawas-b-1", "SMK Pengawas B", []string{models.RolePengawas}, 10)

	tc, _ := newQuotaTestClient(t, pool)
	tc.login(t, op)
	if status, body := tc.upload(t, "pengawas-exam", schoolTestPDF(64)); status != http.StatusOK {
		t.Fatalf("operator upload: status=%d body=%s", status, body)
	}
	var examID int
	if err := pool.QueryRow(ctx, `SELECT id FROM exams WHERE name = 'pengawas-exam'`).Scan(&examID); err != nil {
		t.Fatalf("find exam: %v", err)
	}

	save := func(pids []int) (int, apiResp) {
		t.Helper()
		return postJSON(t, tc.client, tc.srv, "/admin/api/exams/"+strconv.Itoa(examID)+"/questions",
			map[string]interface{}{"pengawas_ids": pids})
	}

	// Cross-tenant pengawas → 400, roster untouched (only the auto-assigned
	// creator remains — the rejected save never reached SetPengawasForExam).
	status, resp := save([]int{foreignPengawas})
	if status != http.StatusBadRequest || !strings.Contains(resp.Message, "tidak berada dalam instansi yang sama") {
		t.Fatalf("save foreign pengawas: status=%d resp=%+v, want the cross-instansi 400", status, resp)
	}
	if ids, _ := models.GetPengawasIDs(ctx, pool, examID); len(ids) != 1 || ids[0] != op {
		t.Fatalf("foreign pengawas must not be assigned; roster must stay [creator], got %v", ids)
	}

	// Same-school pengawas → 200 and assigned (SetPengawasForExam replaces
	// the roster with exactly the submitted ids).
	status, resp = save([]int{ownPengawas})
	if status != http.StatusOK || !resp.Success {
		t.Fatalf("save own-school pengawas: status=%d resp=%+v", status, resp)
	}
	ids, err := models.GetPengawasIDs(ctx, pool, examID)
	if err != nil || len(ids) != 1 || ids[0] != ownPengawas {
		t.Fatalf("own-school pengawas must be the sole assigned pengawas, got ids=%v err=%v", ids, err)
	}
}
