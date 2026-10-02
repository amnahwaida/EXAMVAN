package public

import (
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// checkHasilTokenRateLimit tanpa DB: 60 request pertama untuk satu token
// lolos, request ke-61 ditolak, token lain tidak terpengaruh (satu ruangan
// di belakang NAT tidak saling memblokir), dan tanpa Redis fail-open.
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
		t.Fatalf("request 61 unexpectedly allowed (per-token bucket must be %d/min)", hasilTokenRateLimitMax)
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
