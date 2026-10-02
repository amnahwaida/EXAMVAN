package api

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// sanitize tidak boleh membelah rune UTF-8: potongan byte di tengah rune
// multi-byte menghasilkan string invalid yang ditolak Postgres ("invalid
// byte sequence") → 500 permanen saat submit.
func TestSanitizeRuneSafe(t *testing.T) {
	// 150 emoji × 4 byte = 600 byte, 150 rune: potongan byte [:200] akan
	// membelah — hasilnya harus tetap valid dan ≤ 200 rune.
	long := strings.Repeat("😀", 150)
	got := sanitize(long)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitize returned invalid UTF-8")
	}
	if n := len([]rune(got)); n != 150 {
		t.Fatalf("sanitize cut a short-rune string: got %d runes, want 150", n)
	}

	// 300 ASCII (> 200 byte dan > 200 rune): harus dipotong tepat 200 rune.
	ascii := strings.Repeat("a", 300)
	got = sanitize(ascii)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitize ASCII returned invalid UTF-8")
	}
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("sanitize ASCII: got %d runes, want 200", n)
	}

	// 250 emoji (> 200 byte dan > 200 rune): potong 200 rune, tetap valid.
	multi := strings.Repeat("é", 250) // 2 byte per rune
	got = sanitize(multi)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitize multi-byte returned invalid UTF-8")
	}
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("sanitize multi-byte: got %d runes, want 200", n)
	}

	// TrimSpace tetap berlaku.
	if got := sanitize("  halo  "); got != "halo" {
		t.Fatalf("sanitize trim: got %q, want %q", got, "halo")
	}
}
