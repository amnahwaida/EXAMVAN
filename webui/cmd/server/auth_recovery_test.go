package main

import (
	"strings"
	"testing"
)

// TestGenerateOTPContract locks the OTP shape and the error path:
//   - exactly 6 numeric digits (the email UI and the confirm input both
//     assume it),
//   - successive draws differ (crypto/rand, not a counter),
//   - no error in the happy path.
//
// The signature is (string, error) as of the 2026-11-13 review: the old
// fallback silently degraded to time.Now().UnixNano()%10 digits when
// crypto/rand failed — an OTP that a remote attacker could predict if the
// entropy pool were broken. Callers now fail closed (neutral response / log)
// instead of issuing a predictable code.
func TestGenerateOTPContract(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		code, err := generateOTP()
		if err != nil {
			t.Fatalf("happy path returned error: %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("len=%d want 6 (code=%q)", len(code), code)
		}
		if strings.Trim(code, "0123456789") != "" {
			t.Fatalf("non-numeric OTP: %q", code)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Fatalf("only %d distinct OTPs in 50 draws — entropy source looks degenerate", len(seen))
	}
}
