package public

import (
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// checkHasilTokenRateLimit tanpa DB: backstop per-token melepas request
// pertama..backstop, menolak yang berikutnya, token lain tidak terpengaruh,
// dan tanpa Redis fail-open.
//
// CATATAN H7: ini BACKSTOP runaway, bukan lagi limit anti-brute utama.
// Plafon yang menahan penyerang yang menebak-nebak token lives di
// checkHasilClientRateLimit dan di-key pada client-nya (lihat
// hasil_client_rate_limit_test.go).
func TestCheckHasilTokenRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	newCtx := func() *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("redis", rdb)
		c.Request = httptest.NewRequest("GET", "/api/hasil/TOK12345", nil)
		return c
	}

	for n := 1; n <= hasilTokenRateLimitMax; n++ {
		if !checkHasilTokenRateLimit(newCtx(), "TOK12345") {
			t.Fatalf("request %d unexpectedly limited", n)
		}
	}
	if checkHasilTokenRateLimit(newCtx(), "TOK12345") {
		t.Fatalf("request %d unexpectedly allowed (per-token backstop must be %d/min)",
			hasilTokenRateLimitMax+1, hasilTokenRateLimitMax)
	}
	// Token lain dari IP/ruangan yang sama tidak ikut terblokir.
	if !checkHasilTokenRateLimit(newCtx(), "TOK67890") {
		t.Fatalf("different token unexpectedly limited")
	}

	// Tanpa Redis: fail-open (pola yang sama dengan checkRateLimit di api).
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/hasil/TOK12345", nil)
	if !checkHasilTokenRateLimit(c, "TOK12345") {
		t.Fatalf("nil redis must fail open")
	}
}
