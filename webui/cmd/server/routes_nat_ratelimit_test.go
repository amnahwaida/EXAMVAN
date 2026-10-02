package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
)

// TestStudentRoutesRateLimitPerIP locks in the per-IP middleware budget of
// every student-facing route: the first N requests per minute from one IP
// pass the middleware (the handler may still reject them for unrelated
// reasons — the pool is nil here — which is fine), and request N+1 MUST be
// rejected with 429 by the middleware itself.
//
// This is the contract behind the "satu NAT = satu ruangan" capacity: the
// whole school shares one per-IP bucket behind NAT, so the budgets are sized
// for a full 500-device room — the WS budget (600/menit, koneksi gelombang
// pertama + headroom reconnect) is the smallest of all. The higher budgets
// (15000/menit for join/download/poll waves — result polling alone is
// 500×24=12000/menit at the deadline — 1500/menit for deadline bursts) exist
// precisely so those routes never become the bottleneck. See README → "Kapasitas Satu NAT".
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

	r := gin.New()
	r.Use(gin.Recovery())
	// Mirror main(): session store (beberapa handler memanggil
	// sessions.Default) + Redis di-inject sebelum rute didaftarkan.
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, nil)

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
	}{
		{"exams list", http.MethodGet, "/api/exams", rateLimitExamsPerMinute, false},
		{"request-approval", http.MethodPost, "/api/exams/request-approval", rateLimitWavePerMinute, false},
		{"token join", http.MethodGet, "/api/exams/token/TOK12345", rateLimitWavePerMinute, false},
		{"pdf download", http.MethodGet, "/api/exams/1/pdf", rateLimitWavePerMinute, false},
		{"submit", http.MethodPost, "/api/exams/1/submit", rateLimitBurstPerMinute, false},
		{"result poll", http.MethodGet, "/api/exams/1/result", rateLimitWavePerMinute, false},
		{"access-log", http.MethodPost, "/api/exams/1/access-log", rateLimitBurstPerMinute, false},
		{"complete", http.MethodPost, "/api/exams/1/complete", rateLimitBurstPerMinute, false},
		{"websocket", http.MethodGet, "/ws/1", rateLimitWSPerMinute, false},
		{"hasil api", http.MethodGet, "/api/hasil/TOK12345", rateLimitHasilPerMinute, true},
		// M1: the HTML result pages share the API route's anti-brute-force
		// budget — the comment on rateLimitHasilPerMinute always claimed it,
		// but the middleware was only wired on /api/hasil/:token.
		{"hasil page", http.MethodGet, "/hasil/TOK12345", rateLimitHasilPerMinute, true},
		{"cek hasil page", http.MethodGet, "/hasil", rateLimitHasilPerMinute, false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// IP unik per kasus agar bucket Redis tidak saling terkontaminasi.
			remoteAddr := fmt.Sprintf("203.0.113.%d:9999", 10+i)

			for n := 0; n < tc.limit; n++ {
				path := tc.path
				if tc.perToken {
					path = fmt.Sprintf("%s-%06d", tc.path, n)
				}
				rec := doRouteRateLimitRequest(r, tc.method, path, remoteAddr)
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d/%d unexpectedly rate-limited by middleware", n+1, tc.limit)
				}
			}
			// Request limit+1 MUST be 429 from the middleware.
			rec := doRouteRateLimitRequest(r, tc.method, tc.path, remoteAddr)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d status = %d, want 429 (per-IP middleware budget must be %d/min)", tc.limit+1, rec.Code, tc.limit)
			}
		})
	}
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
