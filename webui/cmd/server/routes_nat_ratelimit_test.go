package main

import (
	"context"
	"time"

	"fmt"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
)

// TestStudentRoutesRateLimitPerIP locks in the per-IP middleware budget of
// every student-facing route: the first N requests per minute from one IP
// pass the middleware (the handler may still reject them for unrelated
// reasons — the pool is nil here — which is fine), and request N+1 MUST be
// rejected with 429 by the middleware itself.
//
// This is the contract behind the "satu NAT = satu ruangan" capacity: the
// whole school shares one per-IP bucket behind NAT, so the budgets are sized
// for a full 500-device room — see the sizing arithmetic on the constants in
// main.go and TestRateLimitBudgetsCoverOneNATRoom. Presence
// (access-log/complete) has its own budget (2000/menit = room × 4, above the
// room × 2 lockstep at t=0) and is checked separately, end to end, by
// TestPresenceBucketIsSeparateFromSubmit.
//
// The test drives the real registerRoutes — the exact function main() calls —
// with a fresh miniredis injected into the request context (like main() does),
// so it fails at registration time if a route's middleware budget is lowered,
// removed, or the route itself is dropped.
func TestStudentRoutesRateLimitPerIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	// Jam DIBEKUKAN (review 2026-11-13): limiter Redis-nya sliding window yang
	// memangkas entri lebih tua dari window. Burst sekuensial 15.000 request
	// butuh 60-100 detik di mesin lambat — entri awal terpangkas sebelum
	// probe ke-limit+1 tiba, 429 yang wajib tidak pernah terjadi (flaky,
	// ~6 menit per run). Dengan jam beku seluruh burst TERBUKTI jatuh di satu
	// window pada kecepatan mesin apa pun: request ke-k melihat tepat k entri,
	// jadi probe ke-limit+1 WAJIB 429 (kontrak "tepat N pertama lewat").
	// Paralel di sini justru merusak kontrak — urutan pipeline tak bisa
	// dijamin, request burst terakhir bisa melihat >N entri dan kena 429
	// prematur — karena itu burst kembali sekuensial.
	frozen := time.Unix(1_700_000_000, 0)
	middleware.RateLimitNow = func() time.Time { return frozen }
	t.Cleanup(func() { middleware.RateLimitNow = time.Now })

	r := gin.New()
	r.Use(gin.Recovery())
	// Mirror main(): session store (beberapa handler memanggil
	// sessions.Default) + Redis di-inject sebelum rute didaftarkan.
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	// Mirror main(): DatabaseMiddleware ada di main(), BUKAN di
	// registerRoutes — tanpa ini getPool MustGet panic per request (biaya
	// format stack ~50ms menjadikan burst 68k request CPU-bound).
	dbPool := lazyFailPool(t)
	r.Use(middleware.DatabaseMiddleware(dbPool))
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, dbPool)

	// Engine kedua TANPA Redis di context — lihat komentar noRedis pada cases.
	// Memory limiter membaca RateLimitNow yang sama (jam beku) sehingga
	// kontrak budget-nya deterministik dengan cara yang sama.
	rNoRedis := gin.New()
	rNoRedis.Use(gin.Recovery())
	store2 := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store2.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	rNoRedis.Use(sessions.Sessions("examvan_session", store2))
	rNoRedis.Use(middleware.DatabaseMiddleware(dbPool))
	registerRoutes(rNoRedis, &config.Config{Version: "test", AdminUser: "superadmin"}, dbPool)

	cases := []struct {
		name   string
		method string
		path   string
		limit  int
		// perToken rotates the token per request (see below): the /hasil
		// routes enforce a per-TOKEN bucket (60/menit, public package) ON
		// TOP of the per-IP middleware ceiling this test locks. A fixed
		// token would 429 from the handler at request 61 — indistinguishable
		// from a middleware 429 — so token-bearing hasil cases vary the
		// token per request to isolate the middleware dimension.
		perToken bool
		// noRedis routes the burst through a second engine WITHOUT a Redis
		// client in the request context. Three student handlers carry their
		// OWN anti-flood buckets (Inc-based, real-clock) whose budgets are
		// SMALLER than the middleware ceiling this test locks: join 600/token,
		// pdf 10/device, result 60/device + 12000/exam-global. A fast frozen-
		// clock burst inevitably trips them (e.g. join tripped at request 601)
		// and the failure would masquerade as a middleware bug. On the second
		// engine checkRateLimit/getRedis see nil Redis → fail-open (dokumen
		// di internal/handlers/api) → hanya layer middleware yang diuji,
		// dijalankan oleh MEMORY limiter yang membaca RateLimitNow yang sama.
		// Redis wiring untuk layer middleware tetap diuji oleh semua case lain.
		noRedis bool
	}{
		{"exams list", http.MethodGet, "/api/exams", rateLimitExamsPerMinute, false, false},
		{"request-approval", http.MethodPost, "/api/exams/request-approval", rateLimitWavePerMinute, false, false},
		{"token join", http.MethodGet, "/api/exams/token/TOK12345", rateLimitWavePerMinute, false, true},
		{"pdf download", http.MethodGet, "/api/exams/1/pdf", rateLimitWavePerMinute, false, true},
		{"submit", http.MethodPost, "/api/exams/1/submit", rateLimitBurstPerMinute, false, false},
		{"result poll", http.MethodGet, "/api/exams/1/result", rateLimitWavePerMinute, false, true},
		{"access-log", http.MethodPost, "/api/exams/1/access-log", rateLimitPresencePerMinute, false, false},
		{"complete", http.MethodPost, "/api/exams/1/complete", rateLimitPresencePerMinute, false, false},
		{"websocket", http.MethodGet, "/ws/1", rateLimitWSPerMinute, false, false},
		{"hasil api", http.MethodGet, "/api/hasil/TOK12345", rateLimitHasilPerMinute, true, false},
		// M1: the HTML result pages share the API route's anti-brute-force
		// budget — the comment on rateLimitHasilPerMinute always claimed it,
		// but the middleware was only wired on /api/hasil/:token.
		{"hasil page", http.MethodGet, "/hasil/TOK12345", rateLimitHasilPerMinute, true, false},
		{"cek hasil page", http.MethodGet, "/hasil", rateLimitHasilPerMinute, false, false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// IP unik per kasus agar bucket Redis tidak saling terkontaminasi.
			remoteAddr := fmt.Sprintf("203.0.113.%d:9999", 10+i)
			engine := r
			if tc.noRedis {
				engine = rNoRedis
			}

			// Burst SEKUENSIAL di atas jam beku: request ke-k melihat tepat k
			// entri dalam window (setiap pipeline ZAdd→ZCard miliknya sendiri),
			// jadi tidak ada 429 prematur di dalam burst, dan probe ke-limit+1
			// wajib 429. Kontrak identik versi lama — hanya tidak lagi
			// bergantung pada kecepatan mesin.
			for n := 0; n < tc.limit; n++ {
				path := tc.path
				if tc.perToken {
					path = fmt.Sprintf("%s-%06d", tc.path, n)
				}
				rec := doRouteRateLimitRequest(engine, tc.method, path, remoteAddr)
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d/%d unexpectedly rate-limited by middleware", n+1, tc.limit)
				}
			}
			// Request limit+1 MUST be 429 from the middleware.
			rec := doRouteRateLimitRequest(engine, tc.method, tc.path, remoteAddr)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d status = %d, want 429 (per-IP middleware budget must be %d/min)", tc.limit+1, rec.Code, tc.limit)
			}
		})
	}
}

// lazyFailPool gives registerRoutes a REAL *pgxpool.Pool that fails fast:
// pgxpool dials lazily, and 127.0.0.1:1 refuses instantly on loopback, so
// handlers take the graceful DB-error path (500/400) instead of panicking in
// getPool's MustGet. Without it, EVERY one of the ~68k burst requests paid
// for gin.Recovery's panic+stack formatting — the test was CPU-bound at
// ~19 req/s and could not outrun the limiter's 60s sliding window on slow
// machines (the flake this suite is fixing). Cleanup closes the pool.
func lazyFailPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://examvan:examvan@127.0.0.1:1/examvan?sslmode=disable")
	if err != nil {
		t.Fatalf("lazy fail pool: %v", err)
	}
	pool.Config().MaxConns = 2
	pool.Config().MinConns = 0
	pool.Config().HealthCheckPeriod = time.Hour // jangan dial di latar belakang
	t.Cleanup(pool.Close)
	return pool
}

func doRouteRateLimitRequest(r *gin.Engine, method, path, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
