package main

import (
	"net/http"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
)

// ---------------------------------------------------------------------------
// Bug arsitektur: key limiter per-IP tidak berdimensi rute.
//
// Key Redis adalah `ratelimit:<ip>:<window_ms>` — tidak ada rute di dalamnya.
// Satu IP di belakang NAT sekolah karena itu hanya punya SATU counter untuk
// SETIAP rute siswa, sehingga budget terkecil yang dikonfigurasi menjadi plafon
// semua rute lain. Gejalanya yang dilaporkan: "GET /api/exams 429s 440 siswa"
// (traffic approval/polling ruang aktif ikut menguras bucket yang sama), dan
// sebaliknya.
//
// Kontrak: exhausting budget satu rute TIDAK boleh menyentuh rute lain dari IP
// yang sama, sedangkan rute itu sendiri tetap throttle pada plafonnya sendiri.
// Dikunci oleh TestStudentRoutesRateLimitPerIP (plafon per rute) dan tes ini
// (isolasi antar rute) — keduanya memakai registerRoutes sungguhan lewat
// miniredis, jadi gagal kalau key kembali kehilangan dimensi rute.
// ---------------------------------------------------------------------------

// newRouteIsolationTestRouter membangun router yang identik dengan main():
// session store + Redis di-inject sebelum registerRoutes.
func newRouteIsolationTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(gin.Recovery())
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, nil)
	return r
}

// TestStudentRoutesAreRateLimitedIndependently adalah regression test langsung
// dari laporan "GET /api/exams 429s 440 siswa": satu IP yang sudah menghabiskan
// SELURUH budget suatu rute tidak boleh langsung 429 pada rute lain.
//
// Rute yang dipakai sebagai "penguras" adalah POST /submit, budget terbesar
// yang masih murah diuji (2500/menit). Setelah itu teruras, SEMUA kelas budget
// lain sudah terlewati — presence (2000), /ws (1500), /hasil (1500) dan /api/exams
// (2000) semuanya < 2500 — jadi satu IP yang sama harus tetap bebas di
// semua rute itu. Result poll (15000/menit) ikut diperiksa sebagai kontrol.
func TestStudentRoutesAreRateLimitedIndependently(t *testing.T) {
	r := newRouteIsolationTestRouter(t)
	ip := "203.0.113.60:9999"

	// Habiskan budget POST /submit dari satu IP (burst deadline + retry).
	for n := 0; n < rateLimitBurstPerMinute; n++ {
		if rec := doRouteRateLimitRequest(r, http.MethodPost, "/api/exams/1/submit", ip); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("permintaan %d/%d ke /submit mendadak 429 — ruang 500 perangkat "+
				"tidak boleh saling memblokir", n+1, rateLimitBurstPerMinute)
		}
	}
	if rec := doRouteRateLimitRequest(r, http.MethodPost, "/api/exams/1/submit", ip); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("permintaan %d ke /submit = %d, want 429 (plafon rute sendiri)",
			rateLimitBurstPerMinute+1, rec.Code)
	}

	// Rute lain dari IP yang SAMA harus masih punya jatah utuh.
	other := []struct {
		method string
		path   string
		name   string
	}{
		{http.MethodGet, "/api/exams", "exams list"},
		{http.MethodPost, "/api/exams/1/access-log", "access-log"},
		{http.MethodPost, "/api/exams/1/complete", "complete"},
		{http.MethodGet, "/ws/1", "websocket"},
		{http.MethodGet, "/api/hasil/TOK12345", "hasil api"},
		{http.MethodGet, "/api/exams/1/result", "result poll (kontrol: budget 15000)"},
	}
	for _, tc := range other {
		for n := 0; n < 3; n++ {
			if rec := doRouteRateLimitRequest(r, tc.method, tc.path, ip); rec.Code == http.StatusTooManyRequests {
				t.Fatalf("%s: permintaan %d = 429 padahal bucket rute-nya sendiri baru dipakai — "+
					"key limiter tidak berdimensi rute", tc.name, n+1)
			}
		}
	}
}
