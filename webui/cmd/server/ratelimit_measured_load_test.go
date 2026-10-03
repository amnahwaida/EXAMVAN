package main

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
)

// ---------------------------------------------------------------------------
// M9 — aritmetika pada komentar sizing di main.go harus cocok dengan kode
// client yang sebenarnya, bukan dengan pengali tebakan.
//
// Dua klaim lama di blok komentar itu SALAH:
//
//  1. "polling hasil tiap ~2,5 dtk di semua perangkat = 500×24 = 12000".
//     Klien tidak polling tiap 2,5 dtk. `poll_queued_result`
//     (desktop/examvan/api.py) memakai `_sleep_or_give_up` yang jeda-nya
//     MENUMPUH dan dibatasi total 77,5 detik, jadi satu perangkat hanya
//     mengirim 8 permintaan (1 + 7 retry) per bursts. Dihitung ulang dengan
//     kode client sungguhan: 8 permintaan / 77,5 dtk = 6,2 req/menit per
//     perangkat, jadi satu ruangan ≈ 3100 req/menit terhadap plafon 15000 —
//     headroom 4,8×, bukan 1,25×. Angka 15000 tetap BENAR; alasannya yang
//     salah, dan alasan yang salah membuat orang memotongnya di angka yang
//     tidak perlu dipotong.
//  2. "presence (access-log + complete): bucket KHAS". Karena keduanya
//     dipasang lewat `RateLimitIPPerRoute`, di belakang NAT sekolah ada DUA
//     bucket 2000/menit yang terpisah — bukan satu. Klaim "satu bucket" di
//     komentar membuat hitungan 500×2 seolah terlindungi satu plafon padahal
//     tidak.
//
// Test pertama mengunci klaim di komentar (bug M9 itu sendiri ada di
// teksnya); dua test lain mengunci angka dan bentuk bucket-nya supaya
// komentar tidak bisa "benar" tanpa rater realities-nya ikut benar.
// ---------------------------------------------------------------------------

// Beban NYATA satu perangkat, diukur dari kode client (lihat header):
//
//	result polling : 8 permintaan per bursts 77,5 dtk (jeda menumpuk,
//	                 bukan 24 permintaan/menit)
//	presence       : 1 login + 1 heartbeat per menit + 1 complete di
//	                 deadline = 3 permintaan lockstep
const (
	measuredResultPollBurstPerDevice = 8
	measuredPresenceBurstPerDevice   = 3
)

// TestRateLimitSizingCommentMatchesMeasuredClientLoad mengunci isi komentar
// sizing. Bug M9 ada di teksnya, jadi ini satu-satunya tempat yang bisa
// gagal: komentar tidak boleh lagi mengklaim 500×24 = 12000 untuk polling,
// dan harus menyebut jumlah percobaan yang benar-benar diukur.
func TestRateLimitSizingCommentMatchesMeasuredClientLoad(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("baca main.go: %v", err)
	}
	src := string(raw)

	for _, wrong := range []string{"500×24 = 12000", "500 × 24 = 12000"} {
		if strings.Contains(src, wrong) {
			t.Errorf("main.go masih mengklaim polling hasil %s — angka itu "+
				"berasal dari interval tetap 2,5 dtk, sedangkan client's "+
				"_sleep_or_give_up memakai jeda yang MENUMPUH di dalam deadline "+
				"77,5 dtk: %d permintaan per perangkat per bursts, bukan 24",
				wrong, measuredResultPollBurstPerDevice)
		}
	}

	// Klaim presence sebagai SATU bucket juga salah: /access-log dan
	// /complete dua rute BERDIMENSI RUTE, jadi dua bucket terpisah.
	if strings.Contains(src, "presence (access-log + complete): bucket KHAS") &&
		!strings.Contains(src, "DUA bucket") {
		t.Errorf("main.go menyebut presence sebagai satu bucket; /access-log dan " +
			"/complete dipasang lewat RateLimitIPPerRoute, jadi di belakang satu " +
			"NAT ada dua bucket 2000/menit yang terpisah")
	}

	// Angka hasil pengukuran harus tertulis, supaya alasan di balik 15000
	// bisa diaudit dan bukan tebakan.
	for _, required := range []string{
		"8 permintaan",
		"77,5 detik",
	} {
		if !strings.Contains(src, required) {
			t.Errorf("main.go tidak menyebut %q — komentar harus memuat hasil "+
				"pengukuran client yang jadi alasan angka ini, bukan pengali tebakan",
				required)
		}
	}
}

// TestResultPollBudgetCoversMeasuredBurstPerDevice mengunci angkanya: satu
// ruangan yang SETIAP perangkatnya melakukan satu bursts polling penuh
// (8 permintaan) harus muat di bawah plafon result poll.
//
// CATATAN: test ini SUDAH LOLOS sebelum perbaikan M9 — M9 memperbaiki
// ALASAN yang tertulis di komentar, bukan angkanya. Yang dijaga di sini
// hanya supaya alasan baru itu tidak someday wiped out angka yang benar.
func TestResultPollBudgetCoversMeasuredBurstPerDevice(t *testing.T) {
	want := natRoomSizePinned * measuredResultPollBurstPerDevice
	if rateLimitWavePerMinute < want {
		t.Errorf("rateLimitWavePerMinute = %d/menit < %d/menit (%d perangkat x "+
			"%d permintaan per bursts polling) — satu deadline lockstep akan "+
			"membuat sebagian kelas dapat 429",
			rateLimitWavePerMinute, want, natRoomSizePinned,
			measuredResultPollBurstPerDevice)
	}
	t.Logf("bukti: %d/menit vs beban terukur %d/menit -> headroom %.1fx",
		rateLimitWavePerMinute, want,
		float64(rateLimitWavePerMinute)/float64(want))
}

// TestResultPollHeadroomIsNotTheOldOnePointTwoFiveClaim mengunciImplikasi
// utama koreksi: ruang yang tersisa jauh lebih besar dari yang dulu
// diklaim, jadi 15000/menit tidak boleh diperlukar dengan angka "tight"
// hanya karena komentar lamanya lama begitu.
//
// CATATAN: test ini SUDAH LOLOS sebelum perbaikan M9 juga.
func TestResultPollHeadroomIsNotTheOldOnePointTwoFiveClaim(t *testing.T) {
	load := natRoomSizePinned * measuredResultPollBurstPerDevice // 4000
	headroom := float64(rateLimitWavePerMinute) / float64(load)
	if headroom <= 1.25 {
		t.Errorf("headroom polling hasil %.2fx — klaim lama 1,25x (500×24 = "+
			"12000 terhadap 15000) ternyata tidak berlaku untuk client's jeda "+
			"yang menumpuk; headroom sebenarnya %.2fx",
			headroom, headroom)
	}
}

// TestPresenceIsTwoSeparatePerRouteBuckets menjalankan rute aslinya
// (registerRoutes + miniredis) untuk mengunciKEPUTUSAN desain presence:
// /access-log dan /complete TIDAK dilipat ke satu bucket, dan memang tidak
// boleh dilipat — key route-less `ratelimit:<ip>:<window>` yang dipakai
// middleware.RateLimitIP BENTROK dengan bucket 10/menit di /login pada
// key yang sama, jadi memakainya di Presence akan membuatPresence diam-diam
// mengambil alih plafon login.
//
// CATATAN: test ini SUDAH LOLOS sebelum perbaikan M9 (memang begitu
// kodenya sekarang); yang dikunci di sini adalah kebenarannya supaya
// lipatan tidak dilakukan diam-diam sambil "memperbaiki" komentar.
func TestPresenceIsTwoSeparatePerRouteBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	// Handler asli /access-log memanggil getPool(c) yang butuh "db" di context;
	// test ini hanya mengukur bucket middleware, jadi panic di handler wajar
	// terjadi dan tidak menarik perhatian. Recovery senyap tetap menulis 500,
	// yang tetap "bukan 429" — assert test ini tidak berubah.
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	registerRoutes(r, &config.Config{Version: "test", AdminUser: "superadmin"}, nil)

	const ip = "203.0.113.77:9999"

	// Habiskan budget presence /access-log dari SATU IP.
	for n := 0; n < rateLimitPresencePerMinute; n++ {
		if rec := doRouteRateLimitRequest(r, http.MethodPost, "/api/exams/1/access-log", ip); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("access-log request %d/%d mendadak 429 — ruang 500 perangkat "+
				"di belakang NAT tidak boleh saling memblokir",
				n+1, rateLimitPresencePerMinute)
		}
	}
	if rec := doRouteRateLimitRequest(r, http.MethodPost, "/api/exams/1/access-log", ip); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("access-log request %d = %d, want 429 (plafonnya sendiri)",
			rateLimitPresencePerMinute+1, rec.Code)
	}

	// /complete dari IP yang SAMA harus masih punya jatah utuh: dua rute,
	// dua bucket 2000/menit.
	for n := 1; n <= 3; n++ {
		if rec := doRouteRateLimitRequest(r, http.MethodPost, "/api/exams/1/complete", ip); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("complete request %d = 429 setelah /access-log kehabisan "+
				"budget — jadi presence DUA rute ini sebenarnya SATU bucket; "+
				"komentar di main.go dan bucket route-nya tidak lagi mengomitekan "+
				"hal yang sama", n)
		}
	}
}
