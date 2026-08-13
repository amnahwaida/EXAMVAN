package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Unit tests: stripAnswerKeys
// ---------------------------------------------------------------------------

// TestStripAnswerKeys_GenericShape covers the shape ExamByToken produces:
// json.Unmarshal into interface{} yields []interface{} of map[string]interface{}.
func TestStripAnswerKeys_GenericShape(t *testing.T) {
	raw := `[
		{"number": 1, "type": "multiple_choice", "label": "Ibu kota RI?", "weight": 1, "key": "jakarta", "options": ["A", "B", "C"]},
		{"number": 2, "type": "essay", "label": "Jelaskan?", "answer": "ini jawaban", "weight": 2, "min_chars": 20}
	]`
	var questions interface{}
	if err := json.Unmarshal([]byte(raw), &questions); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	stripAnswerKeys(questions)

	list := questions.([]interface{})
	checkStripped(t, list[0].(map[string]interface{}), map[string]interface{}{
		"number":  float64(1),
		"type":    "multiple_choice",
		"label":   "Ibu kota RI?",
		"weight":  float64(1),
		"options": []interface{}{"A", "B", "C"},
	})
	checkStripped(t, list[1].(map[string]interface{}), map[string]interface{}{
		"number":    float64(2),
		"type":      "essay",
		"label":     "Jelaskan?",
		"weight":    float64(2),
		"min_chars": float64(20),
	})
}

// TestStripAnswerKeys_TypedShape covers the []map[string]interface{} shape used
// by other callers (e.g. public hasil) if it is ever routed through the helper.
func TestStripAnswerKeys_TypedShape(t *testing.T) {
	questions := []map[string]interface{}{
		{"number": float64(1), "type": "multiple_choice", "key": "a", "answer": "b"},
		{"number": float64(2), "type": "essay", "label": "Essay", "key": "rahasia"},
	}
	stripAnswerKeys(questions)
	for i, q := range questions {
		if _, ok := q["key"]; ok {
			t.Errorf("question %d: 'key' not stripped", i)
		}
		if _, ok := q["answer"]; ok {
			t.Errorf("question %d: 'answer' not stripped", i)
		}
	}
}

// TestStripAnswerKeys_EdgeInputs locks in the no-op behaviour for payloads that
// are not the expected question-list shape: nil, scalar, object, and empty.
func TestStripAnswerKeys_EdgeInputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input interface{}
	}{
		{"nil", nil},
		{"scalar", "hello"},
		{"object", map[string]interface{}{"key": "a", "answer": "b"}},
		{"empty slice", []interface{}{}},
		{"nil slice", []interface{}(nil)},
		{"non-question elements", []interface{}{"x", float64(1), nil, []interface{}{"nested"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := fmt.Sprintf("%v", tc.input)
			stripAnswerKeys(tc.input) // must not panic
			if got := fmt.Sprintf("%v", tc.input); got != before {
				t.Errorf("stripAnswerKeys mutated input %v → %v", before, got)
			}
		})
	}
}

// checkStripped asserts every key in want is present with an equal value, and
// that the "key"/"answer" fields are gone (a map field is present when its
// value is non-nil).
func checkStripped(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	if _, ok := got["key"]; ok {
		t.Errorf("'key' not stripped: got %v", got)
	}
	if _, ok := got["answer"]; ok {
		t.Errorf("'answer' not stripped: got %v", got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stripped question = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Integration tests: GET /api/exams/token/:token (ExamByToken)
// ---------------------------------------------------------------------------

// newExamByTokenRouter mirrors the production wiring for the public exam-token
// endpoint: sessions + "db" injected into the context, then the handler.
func newExamByTokenRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })
	r.GET("/api/exams/token/:token", ExamByToken())
	r.GET("/api/exams/:exam_id/result", ExamResult())
	return r
}

// createExamByTokenFixture inserts an active, started exam whose questions_json
// carries answer keys, and returns its active token.
func createExamByTokenFixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	// The exams.created_by column references admin_users, so create a minimal
	// owner account and reuse its generated ID for the exam row.
	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "api-token-guru", Name: "Guru API",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	// 8-char token matching the app's ^[A-Z0-9]{8}$ format.
	token := fmt.Sprintf("T%07d", time.Now().UnixNano()%10000000)
	questionsJSON := `[
		{"number": 1, "type": "multiple_choice", "label": "Ibu kota RI?", "weight": 1, "key": "jakarta", "options": ["Jakarta", "Bogor", "Bandung"]},
		{"number": 2, "type": "essay", "label": "Jelaskan", "weight": 2, "answer": "kunci rahasia"}
	]`
	// exam_started_at is set so the "Ujian belum dimulai" gate passes.
	var id int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, exam_started_at)
		VALUES ('Ujian Token API', 'ujian.pdf', 2048, $1, $1, 'active', 'medium', $2, $3, CURRENT_TIMESTAMP)
		RETURNING id`, token, questionsJSON, owner.ID).Scan(&id); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return token
}

// TestExamByTokenStripsAnswerKeys is the end-to-end regression test for the fix:
// a public client calling GET /api/exams/token/:token must NOT receive the
// "key"/"answer" fields of the stored questions, while every other question
// field (number, type, label, weight, options) must be preserved.
func TestExamByTokenStripsAnswerKeys(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	token := createExamByTokenFixture(t, pool)

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/exams/token/" + token)
	if err != nil {
		t.Fatalf("GET /api/exams/token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %v)", resp.StatusCode, readBody(t, resp))
	}

	var body struct {
		Success bool `json:"success"`
		Exam    struct {
			ID        int                      `json:"id"`
			Name      string                   `json:"name"`
			Questions []map[string]interface{} `json:"questions"`
		} `json:"exam"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatalf("success = false")
	}
	if len(body.Exam.Questions) != 2 {
		t.Fatalf("len(questions) = %d, want 2", len(body.Exam.Questions))
	}

	for i, q := range body.Exam.Questions {
		if _, ok := q["key"]; ok {
			t.Errorf("question %d: leaked 'key' field: %v", i, q)
		}
		if _, ok := q["answer"]; ok {
			t.Errorf("question %d: leaked 'answer' field: %v", i, q)
		}
	}
	// Sanity: non-key fields survive.
	q0 := body.Exam.Questions[0]
	if q0["type"] != "multiple_choice" || q0["label"] != "Ibu kota RI?" {
		t.Errorf("question 0 lost non-key fields: %v", q0)
	}
}

// TestExamByTokenInvalidToken covers the public 404 path, so a bad token does
// not turn into a 500.
func TestExamByTokenInvalidToken(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/exams/token/NOTAVALID")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestExamByTokenNotStarted covers the "Ujian belum dimulai" gate: a valid
// active token for an exam that has never been started must be rejected, not
// leak the exam payload.
func TestExamByTokenNotStarted(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()
	if _, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "api-token-guru2", Name: "Guru API",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	}); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("U%07d", time.Now().UnixNano()%10000000)
	if _, err := pool.Exec(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by)
		VALUES ('Ujian Belum Mulai', 'belum.pdf', 1024, $1, $1, 'active', 'medium', 1)`, token); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/exams/token/" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: models.ExamScheduleEnded (Fix #3 — end_time enforcement)
// ---------------------------------------------------------------------------

// TestExamScheduleEnded covers the end_time window logic: a nil end_time means
// "no schedule limit" (manual start/stop governs), a past end_time (beyond the
// 60s grace) means ended, and a recent end_time inside the grace window is
// still accepted for a borderline submit.
func TestExamScheduleEnded(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		end    *time.Time
		now    time.Time
		wantEnd bool
	}{
		{
			name:    "nil end_time = no schedule limit",
			end:     nil,
			now:     now,
			wantEnd: false,
		},
		{
			name:    "future end_time = open",
			end:     ptrTime(now.Add(2 * time.Hour)),
			now:     now,
			wantEnd: false,
		},
		{
			name:    "just-past end_time inside 60s grace = still open",
			end:     ptrTime(now.Add(-30 * time.Second)),
			now:     now,
			wantEnd: false,
		},
		{
			name:    "well-past end_time = ended",
			end:     ptrTime(now.Add(-5 * time.Minute)),
			now:     now,
			wantEnd: true,
		},
		{
			name:    "thousands past = ended",
			end:     ptrTime(now.Add(-48 * time.Hour)),
			now:     now,
			wantEnd: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exam := &models.Exam{EndTime: tc.end}
			if got := models.ExamScheduleEnded(exam, tc.now); got != tc.wantEnd {
				t.Errorf("ExamScheduleEnded(end=%v) = %v, want %v", tc.end, got, tc.wantEnd)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------------------
// Integration tests: GET /api/exams/:exam_id/result (Fix #2 — async result)
// ---------------------------------------------------------------------------

// TestExamResultReturnsScoreFromDB verifies the result endpoint's DB fallback:
// when the Redis result key has expired, a poll that supplies the device
// identity (mac_address + identity_data used on submit) finds the persisted
// submission and returns its score with status "done".
func TestExamResultReturnsScoreFromDB(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "api-result-guru", Name: "Guru Hasil",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("R%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at, questions_json)
		VALUES ('Ujian Hasil', 'hasil.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP, $3)
		RETURNING id`, token, owner.ID, `[{"number":1,"type":"multiple_choice","label":"S","weight":1,"key":"jakarta"}]`).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	// Simulate the durable row the async worker would have written.
	answers := `{"1":"jakarta"}`
	score := 100.0
	identity := `{"student_name":"Siti","exam_number":"E1","student_class":"XII-A"}`
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1,'Siti','E1','XII-A',$2,$3,'2026-08-09 08:00:00','DEVICE:result1',$4)`,
		examID, answers, score, identity); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	// Poll with the same device + identity (non-expired Redis result not needed —
	// this exercises the durable fallback). The exam token is required since the
	// result endpoint became credential-gated (14 Agustus 2026).
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/exams/%d/result?mac_address=%s&identity_data=%s",
		srv.URL, examID, "DEVICE:result1", url.QueryEscape(identity)), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Exam-Token", token)
	req.Header.Set("X-App-Version", "2.5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET result: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, readBody(t, resp))
	}
	var body struct {
		Success bool     `json:"success"`
		Status  string   `json:"status"`
		Score   *float64 `json:"score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success || body.Status != "done" {
		t.Fatalf("got success=%v status=%q, want true/done", body.Success, body.Status)
	}
	if body.Score == nil || *body.Score != 100 {
		t.Fatalf("score = %v, want 100", body.Score)
	}
}

// TestExamResultPending confirms a poll that cannot find any persisted row (job
// still queued/processing or already failed with no DB row) reports "pending",
// never a hard error.
func TestExamResultPending(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()
	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "api-result-gur2", Name: "Guru API",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("R%07d", time.Now().UnixNano()%10000000)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ('Ujian Hasil B', 'hasilb.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
		RETURNING id`, token, owner.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	// Unknown job_id, no matching DB row → "pending". The token is required
	// since the result endpoint became credential-gated (14 Agustus 2026).
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/exams/%d/result?job_id=nonexistent&mac_address=DEVICE:xyz",
		srv.URL, examID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Exam-Token", token)
	req.Header.Set("X-App-Version", "2.5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET result: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, readBody(t, resp))
	}
	var body struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success || body.Status != "pending" {
		t.Fatalf("got success=%v status=%q, want true/pending", body.Success, body.Status)
	}
}

// ---------------------------------------------------------------------------
// Integration tests: end_time enforcement (Fix #3)
// ---------------------------------------------------------------------------

// createExamWithEndTime inserts an active, started exam whose questions_json
// carries answer keys, with an optional end_time (nil = no schedule). Returns
// its active token and exam id.
func createExamWithEndTime(t *testing.T, pool *pgxpool.Pool, end *time.Time) (string, int) {
	t.Helper()
	ctx := context.Background()
	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "api-sched-guru", Name: "Guru Jadwal",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	token := fmt.Sprintf("S%07d", time.Now().UnixNano()%10000000)
	var examID int
	if end != nil {
		err = pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
			                   security_level, created_by, exam_started_at, end_time)
			VALUES ('Ujian Berakhir', 's.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP, $3)
			RETURNING id`, token, owner.ID, end).Scan(&examID)
	} else {
		err = pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
			                   security_level, created_by, exam_started_at)
			VALUES ('Ujian Berakhir', 's.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
			RETURNING id`, token, owner.ID).Scan(&examID)
	}
	if err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return token, examID
}

// TestExamByTokenRejectsPastEndTime locks in Fix #3 for the join path: joining
// an exam whose end_time has long passed must be denied (403), not silently
// allowed as it was before.
func TestExamByTokenRejectsPastEndTime(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	past := time.Now().UTC().Add(-2 * time.Hour)
	tok, _ := createExamWithEndTime(t, pool, &past)

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/exams/token/" + tok)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (end_time enforcement)", resp.StatusCode)
	}
}

// TestExamByTokenAllowsInsideWindow verifies a future end_time does not block
// joining an otherwise-active exam.
func TestExamByTokenAllowsInsideWindow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	future := time.Now().UTC().Add(2 * time.Hour)
	tok, _ := createExamWithEndTime(t, pool, &future)

	srv := httptest.NewServer(newExamByTokenRouter(pool))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/exams/token/" + tok)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, readBody(t, resp))
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Logf("read body: %v", err)
	}
	return string(b)
}
