package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// L10 (review_web_flow_dan_dead_code.md 4.1): RegenerateToken membuat token
// baru TANPA pre-check uniqueness + retry (kontras UploadExam yang retry 5×),
// dan unique violation 23505 tidak dipetakan ke 400 ramah. Saat token acar
// menabrak token ujian lain, admin menerima 500 mentah tanpa penjelasan.
//
// Kontrak:
//   - token generator adalah seam (generateExamToken) sehingga tabrakan bisa
//     dibuat deterministik di test;
//   - endpoint melakukan pre-check + retry (pola UploadExam) sehingga tabrakan
//     tunggal tetap berhasil;
//   - 23505 pada exams.token yang lolos dari retry dipetakan ke 400 ramah,
//     bukan 500 mentah.
// ---------------------------------------------------------------------------

func newRegenTokenTestRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })

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
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	adminAPI.POST("/exams/:exam_id/regenerate-token", RegenerateToken())
	return r
}

func TestRegenerateTokenRetriesOnCollision(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fx := createAutoApproveFixture(t, pool)
	ctx := context.Background()

	// Ujian kedua (pemilik lain) dengan token tetap — sumber tabrakan nyata
	// pada constraint UNIQUE exams.token.
	otherToken := "ZZ999999"
	if _, err := pool.Exec(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian Tabrak', 'tabrak.pdf', 1024, $1, $1, 'active', 'medium', $2)`,
		otherToken, fx.GuruID); err != nil {
		t.Fatalf("insert colliding exam: %v", err)
	}

	prev := generateExamToken
	calls := 0
	generateExamToken = func() string {
		calls++
		if calls == 1 {
			return otherToken // tabrakan: sudah dimiliki ujian lain
		}
		return fmt.Sprintf("RT%06d", calls) // segar, 8 char alfanumerik
	}
	t.Cleanup(func() { generateExamToken = prev })

	srv := httptest.NewServer(newRegenTokenTestRouter(pool))
	defer srv.Close()
	client := newAutoApproveClient(t, srv)
	client.login(fx.GuruID)

	code, out := client.do(http.MethodPost, fmt.Sprintf("/admin/api/exams/%d/regenerate-token", fx.ExamID), nil)
	if code != http.StatusOK || out["success"] != true {
		t.Fatalf("status=%d out=%v, want 200 — tabrakan tunggal harus tertangani retry pre-check", code, out)
	}
	if calls < 2 {
		t.Fatalf("generator dipanggil %d×, want ≥2 (retry setelah tabrakan)", calls)
	}
	want := "RT000002"
	if out["token"] != want {
		t.Fatalf("token=%v, want %q (token tabrakan tidak boleh dipakai)", out["token"], want)
	}

	var dbToken string
	if err := pool.QueryRow(ctx, `SELECT token FROM exams WHERE id = $1`, fx.ExamID).Scan(&dbToken); err != nil {
		t.Fatalf("read token: %v", err)
	}
	if dbToken != want {
		t.Fatalf("token DB=%q, want %q", dbToken, want)
	}
}

func TestRegenerateTokenUniqueViolationIsFriendly400(t *testing.T) {
	pgErr := func(code, constraint string) *pgconn.PgError {
		return &pgconn.PgError{Code: code, ConstraintName: constraint}
	}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"token unique violation", fmt.Errorf("update: %w", pgErr("23505", "exams_token_key")), "Token bentrok dengan ujian lain — silakan coba lagi"},
		{"unique violation di constraint lain bukan token", fmt.Errorf("update: %w", pgErr("23505", "exams_other_key")), ""},
		{"error DB nyata bukan duplicate", fmt.Errorf("update: %w", pgErr("08006", "")), ""},
		{"error biasa", fmt.Errorf("boom"), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := regenerateTokenUniqueViolationMessage(tc.err); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// L11 (review_web_flow_dan_dead_code.md 4.1): `go recalculateScores(
// context.Background(), ...)` tidak dilacak dan tidak dibatalkan saat graceful
// shutdown — shutdown di tengah proses menyisakan skor parsial sampai rescore
// berikutnya. Kontrak: goroutine berjalan di bawah job context aplikasi
// (dibatalkan main.go sebelum DB pool ditutup).
// ---------------------------------------------------------------------------

func TestRecalcGoContextHonorsJobContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	prev := jobContext
	jobContext = ctx
	t.Cleanup(func() { jobContext = prev; cancel() })

	if got := recalcGoContext(); got != ctx {
		t.Fatal("recalcGoContext harus mengembalikan job context yang di-wire main.go")
	}
}

func TestRecalcGoContextFallsBackToBackground(t *testing.T) {
	prev := jobContext
	jobContext = nil
	t.Cleanup(func() { jobContext = prev })

	if got := recalcGoContext(); got == nil {
		t.Fatal("recalcGoContext tidak boleh mengembalikan nil (goroutine mati instan)")
	}
}

func TestSetJobContextWiring(t *testing.T) {
	prev := jobContext
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { jobContext = prev; cancel() })

	SetJobContext(ctx)
	if jobContext != ctx {
		t.Fatal("SetJobContext tidak menyimpan context — wiring main.go jadi no-op")
	}
}
