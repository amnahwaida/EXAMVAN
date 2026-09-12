package public

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
// Fixtures
// ---------------------------------------------------------------------------

// insertHasilFixture creates a guru account plus one exam with questions_json
// and the given privacy flags, and returns the exam id + token.
func insertHasilFixture(t *testing.T, pool *pgxpool.Pool, publicResults, showAnswers bool) (examID int, token string) {
	t.Helper()
	ctx := context.Background()

	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: fmt.Sprintf("hasil-guru-%d", time.Now().UnixNano()), Name: "Guru Hasil",
		PasswordHash: "pass", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	token = fmt.Sprintf("H%07d", time.Now().UnixNano()%10000000)
	questionsJSON := `[
		{"number": 1, "type": "single_choice", "label": "Ibu kota RI?", "weight": 1, "key": "Jakarta", "options": ["Jakarta", "Bogor"]},
		{"number": 2, "type": "short_answer", "label": "Ibu kota Jabar?", "weight": 2, "answer": "Bandung"}
	]`

	var publicResultsVal, showAnswersVal int
	if publicResults {
		publicResultsVal = 1
	}
	if showAnswers {
		showAnswersVal = 1
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, public_results, show_answers)
		VALUES ('Ujian Hasil', 'hasil.pdf', 1024, $1, $1, 'active', 'medium', $2, $3, $4, $5)
		RETURNING id`, token, questionsJSON, owner.ID, publicResultsVal, showAnswersVal).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return examID, token
}

// insertHasilSubmission inserts one submission row. When answersJSON is "" the
// row mimics a heartbeat-only row (device opened the exam but never submitted)
// that the public hasil API must EXCLUDE.
func insertHasilSubmission(t *testing.T, pool *pgxpool.Pool, examID int, studentName string, answersJSON string, score *float64) {
	t.Helper()
	ctx := context.Background()

	startTime := "2026-08-09 08:00:00"
	_, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'DEVICE:hasil', $8)`,
		examID, studentName, "01", "XII-A", answersJSON, score, startTime,
		`{"student_name":"`+studentName+`","exam_number":"01","student_class":"XII-A"}`)
	if err != nil {
		t.Fatalf("insert submission %s: %v", studentName, err)
	}
}

// newHasilAPITestRouter wires GET /api/hasil/:token → HasilAPI with the db
// context value (no auth, no rate limit — production adds RateLimitIP).
func newHasilAPITestRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("db", pool) })
	r.GET("/api/hasil/:token", HasilAPI())
	return r
}

func getHasilAPI(t *testing.T, router *gin.Engine, path string) (int, map[string]interface{}) {
	t.Helper()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode %s body %q: %v", path, body, err)
	}
	return resp.StatusCode, parsed
}

// ---------------------------------------------------------------------------
// HasilAPI: submitted-only filter, stats, search, evaluated_answers
// ---------------------------------------------------------------------------

// TestHasilAPIFiltersUnsubmittedHeartbeat locks in that a heartbeat-only row
// (empty answers_json) is excluded from submissions AND from the stats count,
// so a student who merely opened the exam no longer appears as a "—" row.
func TestHasilAPIFiltersUnsubmittedHeartbeat(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	examID, token := insertHasilFixture(t, pool, true, true)

	score := 80.0
	insertHasilSubmission(t, pool, examID, "Siti", `{"1":"Jakarta","2":"Bandung"}`, &score)
	// Heartbeat-only row: answers_json empty → must NOT be counted/shown.
	insertHasilSubmission(t, pool, examID, "Budi", "", nil)

	router := newHasilAPITestRouter(pool)
	status, body := getHasilAPI(t, router, "/api/hasil/"+token)

	if status != http.StatusOK {
		t.Fatalf("status=%d, want 200", status)
	}
	subs := body["submissions"].([]interface{})
	if len(subs) != 1 {
		t.Fatalf("submissions len=%d, want 1 (heartbeat row must be excluded)", len(subs))
	}
	sub := subs[0].(map[string]interface{})
	if sub["student_name"] != "Siti" {
		t.Errorf("student_name=%v, want Siti", sub["student_name"])
	}

	pagination := body["pagination"].(map[string]interface{})
	if pagination["total"].(float64) != 1 {
		t.Errorf("pagination.total=%v, want 1", pagination["total"])
	}

	stats := body["stats"].(map[string]interface{})
	if stats["count"].(float64) != 1 {
		t.Errorf("stats.count=%v, want 1", stats["count"])
	}
	if stats["average"].(float64) != 80.0 {
		t.Errorf("stats.average=%v, want 80", stats["average"])
	}
}

// TestHasilAPISearchServerSide locks in the server-side search param: only
// submissions whose student_name matches are returned, and pagination.total
// reflects the filtered set.
func TestHasilAPISearchServerSide(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	examID, token := insertHasilFixture(t, pool, true, true)

	s1, s2 := 90.0, 70.0
	insertHasilSubmission(t, pool, examID, "Ani", `{"1":"Jakarta","2":"Bandung"}`, &s1)
	insertHasilSubmission(t, pool, examID, "Budi", `{"1":"Bogor","2":"Bandung"}`, &s2)

	router := newHasilAPITestRouter(pool)

	status, body := getHasilAPI(t, router, "/api/hasil/"+token+"?search=ani")
	if status != http.StatusOK {
		t.Fatalf("status=%d, want 200", status)
	}
	subs := body["submissions"].([]interface{})
	if len(subs) != 1 {
		t.Fatalf("search 'ani': submissions len=%d, want 1", len(subs))
	}
	if subs[0].(map[string]interface{})["student_name"] != "Ani" {
		t.Errorf("search 'ani': got %v, want Ani", subs[0].(map[string]interface{})["student_name"])
	}
	pagination := body["pagination"].(map[string]interface{})
	if pagination["total"].(float64) != 1 {
		t.Errorf("search 'ani': pagination.total=%v, want 1", pagination["total"])
	}

	// No match → empty submissions, total 0, but stats stay exam-wide.
	_, body = getHasilAPI(t, router, "/api/hasil/"+token+"?search=zzz")
	if len(body["submissions"].([]interface{})) != 0 {
		t.Error("search 'zzz': submissions must be empty")
	}
	stats := body["stats"].(map[string]interface{})
	if stats["count"].(float64) != 2 {
		t.Errorf("search 'zzz': stats.count=%v, want 2 (stats are exam-wide)", stats["count"])
	}
}

// TestHasilAPIEvaluatedAnswersPresent locks in that every submission carries a
// per-question evaluated_answers map (earned/statusText/statusClass) so the
// frontend detail modal no longer shows everything as "Belum Dijawab".
func TestHasilAPIEvaluatedAnswersPresent(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	examID, token := insertHasilFixture(t, pool, true, true)

	score := 3.0
	insertHasilSubmission(t, pool, examID, "Siti", `{"1":"Jakarta","2":"Bandung"}`, &score)

	router := newHasilAPITestRouter(pool)
	_, body := getHasilAPI(t, router, "/api/hasil/"+token)

	// max_score must equal the sum of effective GetWeight()s (1 + 2 = 3) —
	// the same semantics the evaluation engine uses, so the modal's earned /max
	// ratio can never exceed 1.0.
	if ms := body["max_score"]; ms == nil || ms.(float64) != 3.0 {
		t.Errorf("max_score=%v, want 3.0 (sum of effective weights)", ms)
	}

	sub := body["submissions"].([]interface{})[0].(map[string]interface{})
	eval := sub["evaluated_answers"].(map[string]interface{})
	if len(eval) != 2 {
		t.Fatalf("evaluated_answers len=%d, want 2", len(eval))
	}
	q1 := eval["1"].(map[string]interface{})
	if q1["statusClass"] != "correct" {
		t.Errorf("q1 statusClass=%v, want correct", q1["statusClass"])
	}
	if q1["earned"].(float64) != 1.0 {
		t.Errorf("q1 earned=%v, want 1", q1["earned"])
	}
	q2 := eval["2"].(map[string]interface{})
	if q2["statusClass"] != "correct" {
		t.Errorf("q2 statusClass=%v, want correct", q2["statusClass"])
	}
	if q2["earned"].(float64) != 2.0 {
		t.Errorf("q2 earned=%v, want 2", q2["earned"])
	}
}

// TestHasilAPIAnswersPrivacy locks in the entitlement model: raw student
// answers are sent only when the visitor is entitled (show_answers enabled or
// logged in). evaluated_answers (statuses) is always present so the modal
// works, but the answer TEXT stays hidden for anonymous visitors when the
// teacher disabled show_answers.
func TestHasilAPIAnswersPrivacy(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")

	// show_answers = 1 → anonymous visitor is entitled to raw answers.
	examID, token := insertHasilFixture(t, pool, true, true)
	score := 3.0
	insertHasilSubmission(t, pool, examID, "Siti", `{"1":"Jakarta","2":"Bandung"}`, &score)
	router := newHasilAPITestRouter(pool)
	_, body := getHasilAPI(t, router, "/api/hasil/"+token)
	sub := body["submissions"].([]interface{})[0].(map[string]interface{})
	if sub["answers"] == nil {
		t.Error("show_answers=1: answers must be included for anonymous visitors")
	}

	// show_answers = 0 → raw answers omitted, evaluated_answers still present.
	examID2, token2 := insertHasilFixture(t, pool, true, false)
	insertHasilSubmission(t, pool, examID2, "Siti", `{"1":"Jakarta","2":"Bandung"}`, &score)
	router2 := newHasilAPITestRouter(pool)
	_, body2 := getHasilAPI(t, router2, "/api/hasil/"+token2)
	sub2 := body2["submissions"].([]interface{})[0].(map[string]interface{})
	if sub2["answers"] != nil {
		t.Error("show_answers=0: answers must be omitted for anonymous visitors")
	}
	if sub2["evaluated_answers"] == nil {
		t.Error("show_answers=0: evaluated_answers must still be present")
	}
}

// TestHasilAPIServerSidePagination locks in that pagination is fully
// server-side: page 1 returns the top-per_page, page 2 the next, and the
// frontend no longer needs to fetch every page.
func TestHasilAPIServerSidePagination(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	examID, token := insertHasilFixture(t, pool, true, true)

	for i := 0; i < 25; i++ {
		score := float64(100 - i)
		insertHasilSubmission(t, pool, examID, fmt.Sprintf("Siswa%02d", i), `{"1":"Jakarta","2":"Bandung"}`, &score)
	}

	router := newHasilAPITestRouter(pool)

	_, page1 := getHasilAPI(t, router, "/api/hasil/"+token+"?per_page=10&page=1")
	subs1 := page1["submissions"].([]interface{})
	if len(subs1) != 10 {
		t.Fatalf("page1 len=%d, want 10", len(subs1))
	}
	if subs1[0].(map[string]interface{})["student_name"] != "Siswa00" {
		t.Errorf("page1 top=%v, want Siswa00 (highest score first)", subs1[0].(map[string]interface{})["student_name"])
	}
	pag1 := page1["pagination"].(map[string]interface{})
	if pag1["total_pages"].(float64) != 3 {
		t.Errorf("page1 total_pages=%v, want 3", pag1["total_pages"])
	}

	_, page3 := getHasilAPI(t, router, "/api/hasil/"+token+"?per_page=10&page=3")
	subs3 := page3["submissions"].([]interface{})
	if len(subs3) != 5 {
		t.Errorf("page3 len=%d, want 5", len(subs3))
	}
}

// ---------------------------------------------------------------------------
// HasilPage: headers
// ---------------------------------------------------------------------------

// loadPublicHasilTemplatesForTest registers the real public hasil templates on
// the gin engine. hasil.html pulls in the public_fonts define from shared.html
// and (sejak M26) the admin sprite partial svg-symbols.html; no custom funcs.
func loadPublicHasilTemplatesForTest(t *testing.T, r *gin.Engine) {
	t.Helper()
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	if _, err := os.Stat(templatesDir); err != nil {
		t.Fatalf("resolve templates dir: %v", err)
	}

	tmpl := template.New("").Funcs(template.FuncMap{})
	for _, name := range []string{"admin/partials/svg-symbols.html", "public/shared.html", "public/hasil.html"} {
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read template %s: %v", name, err)
		}
		if _, err := tmpl.New(name).Parse(string(data)); err != nil {
			t.Fatalf("parse template %s: %v", name, err)
		}
	}
	r.SetHTMLTemplate(tmpl)
}

// TestHasilPageNoIndexHeaders locks in the SEO/privacy hardening: the shared
// results page must declare X-Robots-Tag: noindex (so student names + scores
// never appear in search engines, including via the /<token> short URL that
// redirects here) and Cache-Control: no-store (results change as students
// submit). The template also carries a <meta name=robots content=noindex>.
func TestHasilPageNoIndexHeaders(t *testing.T) {
	pool := database.NewPackageTestPool(t, "public")
	_, token := insertHasilFixture(t, pool, true, true)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-hasil-test-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })
	r.GET("/hasil/:token", HasilPage())
	loadPublicHasilTemplatesForTest(t, r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/hasil/" + token)
	if err != nil {
		t.Fatalf("GET page: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag=%q, want to contain 'noindex'", got)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control=%q, want to contain 'no-store'", cc)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read page body: %v", err)
	}
	if !strings.Contains(string(body), `name="robots" content="noindex`) {
		t.Error("page must carry a <meta name=robots content=noindex, nofollow> tag")
	}
}
