package public

import (
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// Halaman HTML hasil dan API JSON-nya harus punya bucket yang TERPISAH,
// di kedua dimensi limiter (client dan token).
//
// Kenapa: satu tampilan hasil = DUA permintaan untuk token yang SAMA —
// GET /hasil/<token> (halaman) lalu GET /api/hasil/<token> yang dipanggil
// halaman itu sendiri untuk mengisi tabelnya. Dengan satu key per scope saja,
// satu tampilan memakan DUA unit dari kuota yang sama; pada mode
// static-token seluruh satu ruangan BERBAGI token yang sama, jadi memakai
// satu bucket bersama membuat siswa-siswa pertama yang membuka link membuat
// yang lain dapat 429 pada halaman yang sama.
//
// Bucket client diuji terpisah di hasil_client_rate_limit_test.go
// (TestHasilPageAndAPIClientBudgetsAreSeparate); tes ini mengunci bucket
// token/backstop.
//
// Tes ini menjalankan kedua rute lewat engine sungguhan (bukan context
// buatan), supaya `c.FullPath()` yang dipakai untuk memilih bucket
// benar-benar berisi rute yang dipanggil.
func TestHasilPageAndAPIIsSeparateTokenBudgets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	// 204 = lolos throttle, 429 = bucket token habis.
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	probe := func(c *gin.Context) {
		if checkHasilTokenRateLimit(c, c.Param("token")) {
			c.Status(204)
			return
		}
		c.Status(429)
	}
	r.GET("/hasil/:token", probe)
	r.GET("/api/hasil/:token", probe)

	call := func(path, token string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path+token, nil)
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	const token = "TOK12345"

	// Halaman HTML: kuota penuh terpakai.
	for n := 1; n <= hasilTokenRateLimitMax; n++ {
		if code := call("/hasil/", token); code != 204 {
			t.Fatalf("halaman request %d = %d, want 204 (bucket halaman harus "+
				"independent %d/menit)", n, code, hasilTokenRateLimitMax)
		}
	}
	if code := call("/hasil/", token); code != 429 {
		t.Fatalf("halaman request %d = %d, want 429", hasilTokenRateLimitMax+1, code)
	}

	// API JSON: bucket sendiri, jadi MASIH fresh — inilah yang rusak
	// sebelumnya (satu bucket dipakai kedua rute).
	if code := call("/api/hasil/", token); code != 204 {
		t.Fatalf("API = %d setelah bucket halaman habis — kedua rute masih "+
			"berbagi satu bucket, sehingga satu tampilan hasil memakan dua "+
			"unit dari kuota yang sama", code)
	}
	// Sisa kuota bucket API (request pertama sudah dipakai di atas).
	for n := 2; n <= hasilTokenRateLimitMax; n++ {
		if code := call("/api/hasil/", token); code != 204 {
			t.Fatalf("API request %d = %d, want 204", n, code)
		}
	}
	if code := call("/api/hasil/", token); code != 429 {
		t.Fatalf("API request %d = %d, want 429", hasilTokenRateLimitMax+1, code)
	}

	// Bucket halaman yang habis tidak boleh mematikan bucket API kelas lain
	// maupun rute lain untuk token yang sama.
	if code := call("/api/hasil/", "TOK67890"); code != 204 {
		t.Fatalf("token lain = %d, want 204 (satu token tidak boleh mematikan token lain)", code)
	}
}
