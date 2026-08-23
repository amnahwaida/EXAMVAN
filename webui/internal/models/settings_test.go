package models

import (
	"context"
	"testing"

	"github.com/examvan/webui/internal/database"
)

func TestEmailDomainAllowed(t *testing.T) {
	const wl = "gmail.com, sch.id, ac.id"

	cases := []struct {
		name  string
		wl    string
		email string
		want  bool
	}{
		{"empty whitelist allows anything", "", "anyone@whatever.io", true},
		{"exact match", wl, "budi@gmail.com", true},
		{"exact match case-insensitive", wl, "Budi@GMail.Com", true},
		{"subdomain of sch.id allowed", wl, "guru@smanegeri1.sch.id", true},
		{"deep subdomain allowed", wl, "a@mail.kampus.ac.id", true},
		{"not in whitelist rejected", wl, "user@outlook.com", false},
		// Security: suffix must be anchored on a dot — these must NOT match.
		{"lookalike suffix rejected", wl, "user@notgmail.com", false},
		{"lookalike tld rejected", wl, "user@evilsch.id", false},
		{"bare entry not matched as suffix", wl, "user@id", false},
		{"no @ rejected", wl, "notanemail", false},
		{"empty domain rejected", wl, "user@", false},
		{"whitespace/format-tolerant whitelist", "  GMAIL.COM ;\n @yahoo.com , *.sch.id ", "x@yahoo.com", true},
		{"wildcard-style entry matches subdomain", "*.sch.id", "x@a.sch.id", true},
	}
	for _, c := range cases {
		if got := EmailDomainAllowed(c.wl, c.email); got != c.want {
			t.Errorf("%s: EmailDomainAllowed(%q, %q) = %v, want %v", c.name, c.wl, c.email, got, c.want)
		}
	}
}

func TestParseDomainList(t *testing.T) {
	got := ParseDomainList("Gmail.com, @Yahoo.com;\n *.sch.id\t, , ac.id.")
	want := []string{"gmail.com", "yahoo.com", "sch.id", "ac.id"}
	if len(got) != len(want) {
		t.Fatalf("ParseDomainList len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseDomainList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Cache baca saas_settings (Lapis 1 — kapasitas)
// ---------------------------------------------------------------------------
//
// GetSaasSetting berada di jalur panas setiap request siswa (middleware versi
// Android + handler). Karena kini dibungkus cache TTL 30 detik, dua kontrak
// lama harus tetap terjaga dan dikunci test ini:
//
//  1. Tulisan terlihat SEGERA di proses yang sama — SetSaasSetting wajib
//     meng-invalidasi entri cache-nya; kalau tidak, panel admin menyimpan
//     nilai baru tapi handler masih membaca nilai lama sampai TTL habis.
//  2. Key yang belum ada di DB tetap konsisten ("", nil), termasuk pada
//     pembacaan kedua yang dilayani dari cache miss persisten.
func TestSaasSettingCacheWriteVisibility(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	const key = "cache_write_visibility_test_key"

	// Bersihkan sisa run sebelumnya, lalu pastikan miss terbaca konsisten.
	if err := SetSaasSetting(ctx, pool, key, ""); err != nil {
		t.Fatalf("reset key: %v", err)
	}
	invalidateSaasSettingCache(pool, key)

	for i := 0; i < 2; i++ {
		val, err := GetSaasSetting(ctx, pool, key)
		if err != nil || val != "" {
			t.Fatalf("read #%d of unset key: val=%q err=%v, want \"\",nil", i+1, val, err)
		}
	}

	// Tulis pertama → terlihat segera.
	if err := SetSaasSetting(ctx, pool, key, "nilai-pertama"); err != nil {
		t.Fatalf("set first value: %v", err)
	}
	if val, _ := GetSaasSetting(ctx, pool, key); val != "nilai-pertama" {
		t.Fatalf("after first Set: got %q, want %q", val, "nilai-pertama")
	}

	// Timpa saat entri masih hangat di cache → tetap harus terlihat segera
	// (inilah kontrak invalidasi; tanpa itu pembacaan kedua akan stale).
	if err := SetSaasSetting(ctx, pool, key, "nilai-kedua"); err != nil {
		t.Fatalf("set second value: %v", err)
	}
	if val, _ := GetSaasSetting(ctx, pool, key); val != "nilai-kedua" {
		t.Fatalf("after overwrite: got %q, want %q (cache not invalidated?)", val, "nilai-kedua")
	}
}
