package models

import (
	"testing"
)

/**
 * Mengunci kontrak normalisasi versi aplikasi sistem (fix review Aplikasi
 * Sistem #3: "kolom Versi menerima teks bebas — 'V2.7.3' vs '2.7.3'
 * dianggap rilis berbeda dan label halaman download menampilkan persis
 * teks yang diketik").
 *
 * Kontrak NormalizeAppVersion:
 *  - Trim spasi; buang prefiks 'v'/'V' opsional.
 *  - Valid: 1–4 segmen angka ber titik (mis. "2", "2.7", "2.7.3").
 *  - Invalid → string kosong (caller menolak unggah): huruf, segmen
 *    kosong ("2..3"), sufiks "-beta", dsb.
 */
func TestNormalizeAppVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2.7.3", "2.7.3"},
		{"2.7", "2.7"},
		{"2", "2"},
		{"  2.7.3  ", "2.7.3"},
		{"v2.7.3", "2.7.3"},
		{"V2.7.3", "2.7.3"},
		{"", ""},
		{"   ", ""},
		{"abc", ""},
		{"v", ""},
		{"2..3", ""},
		{"2.7.3-beta", ""},
		{"2.7.3.4.5", ""},
	}
	for _, c := range cases {
		if got := NormalizeAppVersion(c.in); got != c.want {
			t.Errorf("NormalizeAppVersion(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

/**
 * Kontrak tambahan: versi ternormalisasi HARUS tetap comparable oleh
 * CompareVersions (urutan benar setelah normalisasi).
 */
func TestNormalizedVersion_stillComparable(t *testing.T) {
	v := NormalizeAppVersion(" V2.10.0 ")
	if v != "2.10.0" {
		t.Fatalf("normalize salah: %q", v)
	}
	if CompareVersions(v, "2.9.9") != 1 {
		t.Error("2.10.0 harus > 2.9.9")
	}
}
