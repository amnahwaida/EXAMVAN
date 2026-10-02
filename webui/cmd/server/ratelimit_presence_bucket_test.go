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

// Presence (access-log + complete) harus punya bucket KHAS, di atas
// 500 × 2 — bukan ikut plafon submit yang dipakai sebelumnya.
//
// Kenapa "di atas", bukan "tepat 500 × 2": pada t=0 seluruh 500 perangkat
// melakukan login + heartbeat pertama lockstep, itu sudah 1000 permintaan
// dalam hitungan detik. Plafon 1000 akan menerima semuanya TANPA sisa
// sedikit pun, dan 500 `complete` yang menyusul lockstep di deadline akan
// menjadi 429 untuk semua orang — heartbeat siswa menentukan siapa yang
// terlihat OFFLINE di layar pengawas.
//
// Tes ini mengikat dua hal sekaligus: angkanya (harus > 500 × 2) dan
// PENEMPATANNYA — rute access-log & complete harus benar-benar memakai plafon
// itu. Untuk penempatan, rutenya dijalankan sungguhan (registerRoutes +
// miniredis) seperti routes_nat_ratelimit_test.go: N permintaan pertama dari
// satu IP harus lolos, permintaan ke-(N+1) harus 429 dari middleware.
func TestPresenceBucketIsSeparateFromSubmit(t *testing.T) {
	if rateLimitPresencePerMinute <= natRoomSizePinned*2 {
		t.Errorf("rateLimitPresencePerMinute = %d/menit, harus DI ATAS %d/menit "+
			"(500 login + 500 heartbeat lockstep di t=0 sudah memakai 100%% dari angka itu)",
			rateLimitPresencePerMinute, natRoomSizePinned*2)
	}

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

	cases := []struct {
		name string
		path string
		ip   string
	}{
		// IP berbeda per rute: mengukur presence dan submit di bucket yang sama
		// tidak akan membuktikan apa pun sebelum key limiter diberi dimensi
		// rute, jadi pisahkan saja supaya failure-nya menunjuk tepat ke rute
		// yang salah pasang plafonnya.
		{"access-log", "/api/exams/1/access-log", "203.0.113.41:9999"},
		{"complete", "/api/exams/1/complete", "203.0.113.42:9999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for n := 0; n < rateLimitPresencePerMinute; n++ {
				rec := doRouteRateLimitRequest(r, http.MethodPost, tc.path, tc.ip)
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d/%d mendadak 429 — ruang 500 perangkat di "+
						"belakang satu NAT tidak boleh saling memblokir di presence",
						n+1, rateLimitPresencePerMinute)
				}
			}
			rec := doRouteRateLimitRequest(r, http.MethodPost, tc.path, tc.ip)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d status = %d, want 429 (plafon presence %d/menit)",
					rateLimitPresencePerMinute+1, rec.Code, rateLimitPresencePerMinute)
			}
		})
	}
}

// Dua konstanta, dua perhitungan: heartbeat dan burst submit datang di detik
// yang sama pada satu IP yang sama, dan ukuran jendela keduanya memang
// berbeda. Kalau keduanya jadi satu variabel, mengubah salah satunya diam-diam
// mengubah yang lain.
func TestPresenceBudgetIsNotAliasedToSubmitBudget(t *testing.T) {
	if rateLimitPresencePerMinute == rateLimitBurstPerMinute {
		t.Errorf("rateLimitPresencePerMinute == rateLimitBurstPerMinute (%d): "+
			"presence harus punya konstanta sendiri supaya ukurannya bisa "+
			"dihitung terpisah dari burst submit", rateLimitBurstPerMinute)
	}
	if natRoomSize < natRoomSizePinned {
		t.Errorf("natRoomSize = %d, di bawah ukuran ruangan yang dijaga tes (%d)",
			natRoomSize, natRoomSizePinned)
	}
}
