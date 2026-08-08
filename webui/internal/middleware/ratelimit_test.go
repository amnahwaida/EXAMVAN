package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func sha256SumHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// testContextWithIPAndFP builds a gin.Context with a real request so ClientIP
// and GetHeader behave like a live request. The header is plain HTTP (no TLS)
// so gin's ClientIP falls back to RemoteAddr — which we control.
func testContextWithIPAndFP(ip, fp string) *gin.Context {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = ip + ":1234"
	if fp != "" {
		req.Header.Set("X-Device-Fingerprint", fp)
	}
	c.Request = req
	return c
}

func TestGetRateLimitKeys_IPOnly(t *testing.T) {
	c := testContextWithIPAndFP("203.0.113.9", "")
	keys := getRateLimitKeys(c, true)
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d: %v", len(keys), keys)
	}
	if keys[0] != "203.0.113.9" {
		t.Fatalf("expected IP key, got %q", keys[0])
	}
}

func TestGetRateLimitKeys_IPPlusFingerprint(t *testing.T) {
	c := testContextWithIPAndFP("203.0.113.9", "some-device-fp")
	keys := getRateLimitKeys(c, true)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "203.0.113.9" {
		t.Fatalf("expected IP first, got %q", keys[0])
	}
	if len(keys[1]) != len("fp:")+64 {
		t.Fatalf("fingerprint key should be hashed to hex (fp: + 64 hex chars), got %q", keys[1])
	}
}

func TestGetRateLimitKeys_StableFingerprintHash(t *testing.T) {
	// Same raw fingerprint must hash to the same key regardless of IP.
	c1 := testContextWithIPAndFP("1.1.1.1", "device-abc")
	c2 := testContextWithIPAndFP("2.2.2.2", "device-abc")
	k1, k2 := getRateLimitKeys(c1, true), getRateLimitKeys(c2, true)
	if k1[1] != k2[1] {
		t.Fatalf("same fingerprint hashed differently: %q vs %q", k1[1], k2[1])
	}
	if k1[0] == k2[0] {
		t.Fatalf("different IPs should produce different IP keys")
	}
}

func TestGetRateLimitKeys_AuthenticatedUsesUserID(t *testing.T) {
	c := testContextWithIPAndFP("203.0.113.9", "device-abc")
	c.Set("user_id", 42)
	keys := getRateLimitKeys(c, true)
	if keys[0] != "user_42" {
		t.Fatalf("expected user_42 as first key, got %q", keys[0])
	}
}

func TestGetRateLimitKeys_SpoofedFingerprintDoesNotResetIP(t *testing.T) {
	// The whole point of two parallel counters: an attacker who varies the
	// fingerprint must still accumulate on the IP dimension.
	fps := []string{"fp-A", "fp-B", "fp-C", ""}
	for _, fp := range fps {
		c := testContextWithIPAndFP("203.0.113.9", fp)
		keys := getRateLimitKeys(c, true)
		if keys[0] != "203.0.113.9" {
			t.Fatalf("IP key must always be present, got %q", keys[0])
		}
	}
}

func TestGetRateLimitKeys_FormFieldFallsBackWhenNoHeader(t *testing.T) {
	// The public web forms submit the fingerprint as a hidden form field rather
	// than a header; it must be picked up when the header is absent.
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := "csrf_token=x&device_fingerprint=web-fp-123"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.7:5678"
	c.Request = req

	keys := getRateLimitKeys(c, true)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys (IP + form fp), got %d: %v", len(keys), keys)
	}
	if keys[0] != "198.51.100.7" {
		t.Fatalf("expected IP key first, got %q", keys[0])
	}
	if len(keys[1]) != len("fp:")+64 {
		t.Fatalf("form fingerprint should be hashed to hex, got %q", keys[1])
	}
}

func TestGetRateLimitKeys_IPOnlyModeIgnoresFingerprint(t *testing.T) {
	// Exam routes use includeFP=false: a supplied fingerprint must NOT add a
	// dimension, so a mid-exam device_id change can never retarget the limiter.
	c := testContextWithIPAndFP("203.0.113.9", "some-device-fp")
	keys := getRateLimitKeys(c, false)
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 key in IP-only mode, got %d: %v", len(keys), keys)
	}
	if keys[0] != "203.0.113.9" {
		t.Fatalf("expected IP key, got %q", keys[0])
	}
}

func TestGetRateLimitKeys_HeaderTakesPrecedenceOverForm(t *testing.T) {
	// When both are present the header wins (Android app path).
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := "device_fingerprint=form-fp"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Device-Fingerprint", "header-fp")
	req.RemoteAddr = "198.51.100.7:5678"
	c.Request = req

	keys := getRateLimitKeys(c, true)
	hashedHeader := sha256SumHex("header-fp")
	if keys[1] != "fp:"+hashedHeader {
		t.Fatalf("expected header fp to win, got %q, want fp:%s", keys[1], hashedHeader)
	}
}
