package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Dimensi rute pada key rate-limit.
//
// Key limiter lama adalah `ratelimit:<ip>:<window_ms>` — TIDAK ada rute di
// dalamnya. Akibatnya satu IP punya SATU counter untuk semua rute, jadi
// budget terkecil yang dikonfigurasi berlaku sebagai plafon SEMUA rute:
// traffic approval/polling satu ruang menguras plafon /hasil (dan sebaliknya),
// dan angka warisan 60/menit pada GET /api/exams membuat 440 dari 500 siswa di
// belakang satu NAT langsung menerima 429 (lihat blok konstanta di
// cmd/server/main.go).
//
// Kontrak yang diikat tes ini: identitas bucket = (IP/user, RUTE, window)
// untuk limiter ber-scope rute, yaitu
//   - RUTE A yang exhausting budget-nya tidak boleh men-throttle RUTE B dari IP
//     yang sama;
//   - RUTE A tetap throttle dirinya sendiri pada budget SENDIRNYA.
//
// Konstruktor lama (RateLimitIP/RateLimit) sengaja TIDAK diberi dimensi rute:
// keluarga login anti-brute-force dan limiter tingkat grup di admin memakai
// satu bucket bersama dengan sengaja. See TestRateLimitIP_WithoutRouteScope-
// KeepsSharingOneBucket.
// ---------------------------------------------------------------------------

// newRouteScopeTestRouter membangun router dengan beberapa rute yang berbagi
// angka budget yang sama, di depan miniredis sungguhan — jalur produksi
// (newRedisRateLimit) yang berjalan setiap kali "redis" ada di context, sama
// seperti main() menyuntikkannya.
func newRouteScopeTestRouter(t *testing.T, maxPerWindow int, window time.Duration) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true}) }
	r.GET("/alpha", RateLimitIPPerRoute(maxPerWindow, window), ok)
	r.GET("/alpha/:id", RateLimitIPPerRoute(maxPerWindow, window), ok)
	r.GET("/beta", RateLimitIPPerRoute(maxPerWindow, window), ok)
	// Tanpa scope rute: dua rute yang harus saling memblokir (satu bucket).
	r.GET("/shared-one", RateLimitIP(maxPerWindow, window), ok)
	r.GET("/shared-two", RateLimitIP(maxPerWindow, window), ok)
	return r
}

func doRouteScopeRequest(r *gin.Engine, path, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// TestRateLimitPerRoute_DifferentRoutesDoNotShareBucket adalah regression test
// bug "GET /api/exams 429s 440 siswa": exhausting budget RUTE A tidak boleh
// menyentuh RUTE B dari IP yang sama.
func TestRateLimitPerRoute_DifferentRoutesDoNotShareBucket(t *testing.T) {
	const limit = 5
	r := newRouteScopeTestRouter(t, limit, time.Minute)
	ip := "203.0.113.77:9999"

	// 1. RUTE A (/alpha) dibanjiri lewat budget-nya.
	for n := 0; n < limit; n++ {
		if code := doRouteScopeRequest(r, "/alpha", ip); code == http.StatusTooManyRequests {
			t.Fatalf("permintaan %d/%d ke /alpha mendadak 429", n+1, limit)
		}
	}
	if code := doRouteScopeRequest(r, "/alpha", ip); code != http.StatusTooManyRequests {
		t.Fatalf("permintaan %d ke /alpha = %d, want 429 (plafon rute sendiri)", limit+1, code)
	}

	// 2. RUTE B (/beta) dari IP yang SAMA harus tetap bisa lewat utuh.
	for n := 0; n < limit; n++ {
		if code := doRouteScopeRequest(r, "/beta", ip); code == http.StatusTooManyRequests {
			t.Fatalf("permintaan %d/%d ke /beta = 429 padahal bucket-nya sendiri masih kosong — "+
				"key limiter tidak berdimensi rute", n+1, limit)
		}
	}
}

// TestRateLimitPerRoute_SameRouteStillThrottlesAtItsOwnBudget adalah separuh
// lain dari kontrak: menambah dimensi rute tidak boleh melemahkan proteksi
// per-rute. Dua template rute berbeda (/alpha dan /alpha/:id) adalah dua
// bucket, masing-masing harus throttling pada plafonny sendiri.
func TestRateLimitPerRoute_SameRouteStillThrottlesAtItsOwnBudget(t *testing.T) {
	const limit = 5
	r := newRouteScopeTestRouter(t, limit, time.Minute)

	for _, path := range []string{"/alpha", "/alpha/42"} {
		for n := 0; n < limit; n++ {
			if code := doRouteScopeRequest(r, path, "198.51.100.3:1111"); code == http.StatusTooManyRequests {
				t.Fatalf("%s permintaan %d/%d = 429 lebih awal", path, n+1, limit)
			}
		}
		if code := doRouteScopeRequest(r, path, "198.51.100.3:1111"); code != http.StatusTooManyRequests {
			t.Fatalf("%s permintaan %d = %d, want 429", path, limit+1, code)
		}
	}
}

// TestRateLimitIP_WithoutRouteScopeKeepsSharingOneBucket mengunci sisi
// kebalikannya supaya penambahan scope tidak mengubah semantics diam-diam:
// konstruktor lama tetap memakai SATU counter untuk semua rute — itulah yang
// membuat 10/menit di /login dan /admin/login tetap 10/menit total, bukan
// 20/menit.
func TestRateLimitIP_WithoutRouteScopeKeepsSharingOneBucket(t *testing.T) {
	const limit = 5
	r := newRouteScopeTestRouter(t, limit, time.Minute)
	ip := "192.0.2.10:2222"

	for n := 0; n < limit; n++ {
		if code := doRouteScopeRequest(r, "/shared-one", ip); code == http.StatusTooManyRequests {
			t.Fatalf("/shared-one permintaan %d/%d = 429 lebih awal", n+1, limit)
		}
	}
	if code := doRouteScopeRequest(r, "/shared-one", ip); code != http.StatusTooManyRequests {
		t.Fatalf("/shared-one permintaan %d = %d, want 429", limit+1, code)
	}
	if code := doRouteScopeRequest(r, "/shared-two", ip); code != http.StatusTooManyRequests {
		t.Fatalf("/shared-two = %d, want 429 — limiter tanpa scope rute harus tetap satu bucket bersama", code)
	}
}

// TestRateLimitRouteScopeUsesMatchedRouteTemplate mengunci nilai scope-nya:
// route TEMPLATE gin's, bukan path konkret — supaya /hasil/AAA11111 dan
// /hasil/BBB22222 benar-benar berbagi bucket (ayah/IBU yang sama di depan
// NAT harus ikut ter-throttle bersama).
func TestRateLimitRouteScopeUsesMatchedRouteTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var scopes []string
	record := func(c *gin.Context) {
		scopes = append(scopes, rateLimitRouteScope(c))
		c.Status(http.StatusOK)
	}
	r := gin.New()
	r.GET("/hasil/:token", record)
	r.GET("/api/exams/:exam_id/result", record)

	for _, path := range []string{"/hasil/AAA11111", "/hasil/BBB22222", "/api/exams/17/result"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	want := []string{"/hasil/:token", "/hasil/:token", "/api/exams/:exam_id/result"}
	for i, w := range want {
		if scopes[i] != w {
			t.Fatalf("scope[%d] = %q, want %q", i, scopes[i], w)
		}
	}
	if scopes[2] == scopes[0] {
		t.Fatalf("dua rute berbeda harus punya scope berbeda, keduanya %q", scopes[0])
	}
}

// TestRateLimitRouteScopeUnmatchedFallsBackToStableName: permintaan yang tidak
// cocok rute apa pun (404) tidak boleh mendapat scope kosong — semua 404 akan
// bertumpuk pada satu key, jadi fallback-nya harus stabil dan eksplisit.
func TestRateLimitRouteScopeUnmatchedFallsBackToStableName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var got string
	r.Use(func(c *gin.Context) {
		got = rateLimitRouteScope(c)
		c.Status(http.StatusNotFound)
	})
	r.GET("/known", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/tidak-ada", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got == "" {
		t.Fatal("scope tanpa rute tidak boleh kosong")
	}
	if got != rateLimitRouteScopeNoRoute {
		t.Fatalf("scope 404 = %q, want %q", got, rateLimitRouteScopeNoRoute)
	}
}
