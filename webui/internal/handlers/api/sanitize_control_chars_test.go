package api

import (
	"strings"
	"testing"
	"unicode"
)

// ---------------------------------------------------------------------------
// sanitize() hanya TrimSpace + potong 200 rune. Karakter kontrol/bidi lolos
// utuh ke Postgres lalu dirender di tabel hasil publik yang dilihat SELURUH
// kelas: html/template meng-escape HTML, bukan karakter kontrol.
//
// U+202E (RIGHT-TO-LEFT OVERRIDE) adalah contoh paling berbahaya: ia membalik
// urutan tampilan, jadi "Budi" bisa tampil sebagai "idub" dan menyamar
// sebagai nama lain. Client desktop sudah membersihkan string tampilannya, tapi
// server tidak boleh menyimpan apa pun yang ia terima mentah.
//
// Yang dibersihkan: seluruh format/bidi control (Cf) yang bisa dipakai
// menyamar — minimal U+200E, U+200F, U+202A–U+202E, U+2066–U+2069, U+FEFF —
// plus karakter kontrol ASCII kecuali tab dan newline. Tab/newline tetap
// dipertahankan karena identitas yang di-enter multiline (alamat) sah.
// ---------------------------------------------------------------------------

func TestSanitizeStripsBidiAndControlCharacters(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Bidi override/embedding — pemalsuan nama.
		{"U+202E RTL override", "‮evil‬", "evil"},
		{"U+202D LTR override", "‭abc‬", "abc"},
		{"U+202A LTR embedding", "‪Andi‬", "Andi"},
		{"U+202B RLE embedding", "‫Andi‬", "Andi"},
		{"U+202C pop bidi", "Andi‬", "Andi"},
		// Directional mark tersembunyi.
		{"U+200E LRM", "And‎i", "Andi"},
		{"U+200F RLM", "And‏i", "Andi"},
		// Isolates (dirilis Unicode 6.3, sekarang sering dipakai untuk
		// menyembunyikan teks di believiewer).
		{"U+2066 LRI", "⁦Andi⁩", "Andi"},
		{"U+2067 RLI", "⁧Andi⁩", "Andi"},
		{"U+2068 FSI", "⁨Andi⁩", "Andi"},
		{"U+2069 PDI", "Andi⁩", "Andi"},
		// BOM/zero-width.
		{"U+FEFF BOM", "\ufeffAndi", "Andi"},
		// Kontrol ASCII (kecuali tab/newline).
		{"NUL", "An\x00di", "Andi"},
		{"BEL", "An\x07di", "Andi"},
		{"backspace", "An\bdi", "Andi"},
		{"vertical tab", "An\vdi", "Andi"},
		{"form feed", "An\fdi", "Andi"},
		{"CR", "An\rdi", "Andi"},
		{"DEL", "An\x7Fdi", "Andi"},
		// Nol lebar yang bukan bidi juga menipu.
		{"U+200B zero width space", "An\u200Bdi", "Andi"},
		{"U+200C ZWNJ", "An\u200Cdi", "Andi"},
		{"U+200D ZWJ", "An\u200Ddi", "Andi"},
		// Gabungan: penghalang di tengah nama Indonesia.
		{"kombinasi", "‮Siti‏\x00 Aminah‬", "Siti Aminah"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitize(tc.in)
			if got != tc.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Regression guard: karakter yang TIDAK boleh ikut terbuang — tab dan newline adalah
// isi identitas yang sah (alamat multibaris), dan huruf non-ASCII harus utuh.
func TestSanitizeKeepsLegitimateCharacters(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tab dipertahankan", "Kelas\t1", "Kelas\t1"},
		{"newline dipertahankan", "Jl. Merdeka\nNo. 5", "Jl. Merdeka\nNo. 5"},
		// CR ikut terbuang (kategori kontrol ASCII) tapi newline tetap ada,
		// jadi paste alamat dari Windows tidak kehilangan pemisah baris.
		{"CRLF jadi LF", "a\r\nb", "a\nb"},
		{"huruf latin & angka", "Andi 123", "Andi 123"},
		{"beraksen & non-Latin", "Andi Ångström 张", "Andi Ångström 张"},
		{"emoji", "Andi 😀", "Andi 😀"},
		{"nama Indonesia", "Abdul Rahman", "Abdul Rahman"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitize(tc.in); got != tc.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Regression guard: hasil sanitize tidak boleh mengandung format/control apa pun, untuk
// setiap rune input — ini yang dijamin ke tabel hasil publik.
func TestSanitizeOutputHasNoFormatOrControlRunes(t *testing.T) {
	inputs := []string{
		"‮evil‬",
		"Andi\x00\x07\r\x7f",
		"\ufeffAndi",
		"⁦Andi⁩",
		strings.Repeat("‮", 50) + "Andi",
	}
	for _, in := range inputs {
		for _, r := range sanitize(in) {
			if unicode.Is(unicode.Cf, r) {
				t.Fatalf("sanitize(%q) masih memuat U+%04X (kategori Cf)", in, r)
			}
			if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
				t.Fatalf("sanitize(%q) masih memuat kontrol U+%04X", in, r)
			}
		}
	}
}

// Regression guard: urutan pemotongan 200 rune harus tetap setelah pembersihan, dan
// pemotongan terjadi SETELAH karakter terbuang dibuang — supaya string 300 rune
// yang 100-nya tersembunyi tetap menghasilkan 200 rune yang berguna.
func TestSanitizeTruncatesAfterStripping(t *testing.T) {
	// 300 rune 'a' + 100 bidi tersembunyi = 400 rune; setelah dibuang tersisa
	// 300 rune, jadi pemotongan ke 200 tetap berlaku dan tidak menghasilkan
	// string kosong.
	got := sanitize(strings.Repeat("a", 300) + strings.Repeat("‮", 100))
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("sanitize menghasilkan %d rune, want 200", n)
	}

	// Sebaliknya: 100 rune berguna + 500 tersembunyi → 100 rune, tidak 0.
	got = sanitize(strings.Repeat("a", 100) + strings.Repeat("‏", 500))
	if got != strings.Repeat("a", 100) {
		t.Fatalf("sanitize = %q, want 100 'a'", got)
	}
}
