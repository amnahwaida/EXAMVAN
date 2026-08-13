package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	redis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// End-to-end tests: GET /admin/api/pengawas/exams/:exam_id/submissions
// ---------------------------------------------------------------------------
//
// The monitoring table + per-device modal render entirely from this JSON, so
// the two fields that came from the N+1 batch rework get pinned here against a
// real database AND a real (in-process miniredis) Redis: is_online (heartbeat
// presence, batch-fetched via one pipeline) and submission_history (every
// attempt per device, oldest first). The list itself dedupes per device
// (DISTINCT ON mac_address, latest row), matching the production query.

// newSubmissionsRedisRouter mirrors the production wiring for the pengawas
// detail submissions endpoint with BOTH db and redis injected, so the
// heartbeat-based is_online branch actually runs. When rdb is nil it exercises
// the no-Redis path (everything offline).
func newSubmissionsRedisRouter(pool *pgxpool.Pool, rdb *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("redis", rdb)
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

	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	adminAPI.GET("/pengawas/exams/:exam_id/submissions", PengawasExamSubmissions())
	return r
}

func TestPengawasExamSubmissionsOnlineAndHistory(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	// Realistic: the exam is started (the monitoring page is only used for
	// running exams).
	if _, err := pool.Exec(ctx, `UPDATE exams SET exam_started_at = CURRENT_TIMESTAMP WHERE id = $1`, fx.ExamID); err != nil {
		t.Fatalf("start exam: %v", err)
	}

	macOnline := "AA:BB:CC:DD:EE:C1"
	macOffline := "AA:BB:CC:DD:EE:C2"
	older := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 3, 9, 10, 0, 0, time.UTC)

	// Two attempts for the online device (open then completed), one for the
	// other — the list must dedupe to one row per device while history keeps
	// every attempt, oldest first.
	score := 80.0
	startOpen := "2026-08-03 09:00:00"
	startDone := "2026-08-03 09:05:00"
	for _, row := range []struct {
		mac, start string
		answers    *string
		score      *float64
		at         time.Time
	}{
		{macOnline, startOpen, nil, nil, older},                // in-progress attempt
		{macOnline, startDone, spT(`{"1":"a"}`), &score, newer}, // completed attempt
		{macOffline, startDone, spT(`{"1":"b"}`), &score, newer},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
			VALUES ($1, $2, 'Siswa E2E', '01', 'XII A', $3, $4, $5, $6)`,
			fx.ExamID, row.mac, row.answers, row.score, row.start, row.at); err != nil {
			t.Fatalf("insert submission: %v", err)
		}
	}

	// Access logs for the online device (login then heartbeat) — the modal's
	// timeline comes from these.
	for _, row := range []struct{ event, at string }{
		{"login", "2026-08-03T09:00:00Z"},
		{"heartbeat", "2026-08-03T09:05:00Z"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO student_access_logs (exam_id, student_identifier, student_name, event, created_at)
			VALUES ($1, $2, 'Siswa E2E', $3, $4)`,
			fx.ExamID, macOnline, row.event, row.at); err != nil {
			t.Fatalf("insert access log: %v", err)
		}
	}

	// Redis heartbeat: only the first device is online.
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Set(ctx, fmt.Sprintf("heartbeat:%d:%s", fx.ExamID, macOnline),
		`{"last_seen":"2026-08-03T09:05:00Z"}`, 0).Err(); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	srv := httptest.NewServer(newSubmissionsRedisRouter(pool, rdb))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.PwID) // assigned pengawas

	code, out := client.do(http.MethodGet,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/submissions?per_page=20", fx.ExamID), nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("GET status=%d out=%v, want 200 success", code, out)
	}

	subs, ok := out["submissions"].([]interface{})
	if !ok {
		t.Fatalf("submissions is not an array: %T", out["submissions"])
	}
	if len(subs) != 2 {
		t.Fatalf("submissions = %d, want 2 (deduped per device)", len(subs))
	}

	byMAC := map[string]map[string]interface{}{}
	for _, raw := range subs {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		mac, _ := m["mac_address"].(string)
		byMAC[mac] = m
	}

	// --- Online device: heartbeat key present → is_online true ---
	online := byMAC[macOnline]
	if online == nil {
		t.Fatalf("device %s missing from response", macOnline)
	}
	if online["is_online"] != true {
		t.Errorf("is_online = %v, want true (heartbeat key present)", online["is_online"])
	}
	// The main row is the latest attempt (completed) — the monitoring table
	// shows it as submitted.
	if online["submitted"] != true {
		t.Errorf("submitted = %v, want true (latest row is the completed attempt)", online["submitted"])
	}
	// Full history: both attempts, oldest first.
	hist, ok := online["submission_history"].([]interface{})
	if !ok || len(hist) != 2 {
		t.Fatalf("submission_history = %v, want 2 attempts", online["submission_history"])
	}
	first := hist[0].(map[string]interface{})
	second := hist[1].(map[string]interface{})
	if first["answers_json"] != nil || first["score"] != nil {
		t.Errorf("first attempt answers/score = %v/%v, want open row (nil)", first["answers_json"], first["score"])
	}
	if second["answers_json"] == nil || second["score"] == nil {
		t.Errorf("second attempt = %v, want completed row with answers + score", second)
	}
	fStr, _ := first["created_at"].(string)
	sStr, _ := second["created_at"].(string)
	if fStr == "" || sStr == "" || fStr >= sStr {
		t.Errorf("history not ordered oldest-first: first=%q second=%q", fStr, sStr)
	}
	if online["attempt_count"] != float64(2) {
		t.Errorf("attempt_count = %v, want 2", online["attempt_count"])
	}
	// Access logs ride along (login then heartbeat) for the detail modal.
	logs, ok := online["access_logs"].([]interface{})
	if !ok || len(logs) != 2 {
		t.Fatalf("access_logs = %v, want 2 entries", online["access_logs"])
	}
	if logs[0].(map[string]interface{})["event"] != "login" || logs[1].(map[string]interface{})["event"] != "heartbeat" {
		t.Errorf("access log order = [%v, %v], want [login, heartbeat]",
			logs[0].(map[string]interface{})["event"], logs[1].(map[string]interface{})["event"])
	}

	// --- Offline device: no heartbeat key → is_online false ---
	offline := byMAC[macOffline]
	if offline == nil {
		t.Fatalf("device %s missing from response", macOffline)
	}
	if offline["is_online"] != false {
		t.Errorf("is_online = %v, want false (no heartbeat key)", offline["is_online"])
	}
	if hist, ok := offline["submission_history"].([]interface{}); !ok || len(hist) != 1 {
		t.Errorf("submission_history = %v, want 1 attempt", offline["submission_history"])
	}
}

// Same exam-scoped authorization as the other pengawas endpoints: an
// unassigned pengawas (from another instansi) must be denied even on the read.
func TestPengawasExamSubmissionsUnauthorizedDenied(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	srv := httptest.NewServer(newSubmissionsRedisRouter(pool, nil))
	defer srv.Close()

	client := newAutoApproveClient(t, srv)
	client.login(fx.OtherID) // unassigned pengawas from another instansi

	code, out := client.do(http.MethodGet,
		fmt.Sprintf("/admin/api/pengawas/exams/%d/submissions", fx.ExamID), nil)
	if code != http.StatusForbidden {
		t.Fatalf("GET status=%d out=%v, want 403 for unassigned pengawas", code, out)
	}
}
