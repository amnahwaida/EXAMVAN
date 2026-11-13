package helpers

import (
	"strings"
	"testing"
)

// TestGenerateExamTokenContract locks the exam-token shape and the fail-closed
// error path (review 14 Nov 2026): the exam token is the credential students
// log in with, so a degraded draw must NEVER be issued. The old fallback
// produced time.Now().UnixNano()%99999999 (≤32 bits, predictable) when
// crypto/rand failed — the same class of defect generateOTP had; both now
// return the error instead of minting a weak credential.
func TestGenerateExamTokenContract(t *testing.T) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		tok, err := GenerateExamToken()
		if err != nil {
			t.Fatalf("happy path returned error: %v", err)
		}
		if len(tok) != 8 {
			t.Fatalf("len=%d want 8 (token=%q)", len(tok), tok)
		}
		if strings.Trim(tok, charset) != "" {
			t.Fatalf("token outside [A-Z0-9]: %q", tok)
		}
		seen[tok] = true
	}
	// 200 draws from 36^8 ≈ 2.8e12 space: collisions are astronomically
	// unlikely, so far fewer than 200 distinct tokens signals a degenerate
	// source (e.g. a constant/time-based fallback).
	if len(seen) < 195 {
		t.Fatalf("only %d distinct tokens in 200 draws — entropy source looks degenerate", len(seen))
	}
}
