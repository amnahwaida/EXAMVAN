package database

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// H5 (lanjutan) — migrasi data identity_fields harus ada di schema.sql dan
// harus benar.
//
// Kenapa butuh migrasi kalau jalur baca sudah menormalkan: jalur baca
// (`identityFieldValue`: key persis -> trimmed -> case-folded) menutup
// submitnya, tapi kolom yang tersimpan tetap TIDAK kanonik — jadi yang tampil
// di UI pengawas tidak sama dengan yang dibaca server, dan setiap request
// bergantung pada fallback. Migrasi menutup sisi datanya.
//
// Skema.sql di repo ini kumulatif dan idempoten (`IF NOT EXISTS` + backfill
// `ON CONFLICT DO NOTHING` untuk exam_token_history), jadi pola migrasi data
// seperti ini memang tersedia di sini.
// ---------------------------------------------------------------------------

// TestSchemaHasIdempotentIdentityFieldsMigration mengunci bahwa migrasi ada
// dan tidak merusak baris yang tidak perlu disentuh. Dijalankan sebagai test
// DB-backed supaya SQL-nya benar-benar dieksekusi, bukan cuma dicek sebagai teks.
func TestSchemaHasIdempotentIdentityFieldsMigration(t *testing.T) {
	ctx := context.Background()
	// NewPackageTestPool: schema sendiri untuk package ini, ApplySchema, lalu
	// TRUNCATE tabel data — semua baris di bawah dibuat oleh test ini, tidak
	// ada sisa dari run sebelumnya.
	pool := NewPackageTestPool(t, "database")

	// --- baris legacy yang harus dinormalkan ------------------------------
	legacy := `[
		{"key":" nama ","label":"Nama","required":true},
		{"key":"Kelas","label":"Kelas","required":true},
		{"key":"kelas","label":"Kelas (duplikat warisan)","required":true},
		{"key":"nomor_ujian","label":"Nomor Ujian","required":false}
	]`
	// Baris yang sudah kanonik harus TIDAK berubah (kecuali bentuk JSON-nya,
	// karena migrasi menulis ulang lewat jsonb).
	canonical := `[
		{"key":"student_name","label":"Nama","required":true},
		{"key":"exam_number","label":"Nomor Ujian","required":true}
	]`

	ids := make([]int, 0, 2)
	for n, raw := range []string{legacy, canonical} {
		var id int
		err := pool.QueryRow(ctx,
			`INSERT INTO exams (name, file_path, token, identity_fields, status)
			 VALUES ($1, 'x.pdf', $2, $3, 'active') RETURNING id`,
			fmt.Sprintf("Migration Test %d", n),
			fmt.Sprintf("MIGRTOKEN%05d", n), raw).Scan(&id)
		if err != nil {
			t.Fatalf("insert exam: %v", err)
		}
		ids = append(ids, id)
	}

	// Jalankan schema (dan dengan begitu migrasi) dua kali: idempoten.
	var afterFirst string
	for pass := 1; pass <= 2; pass++ {
		if err := ApplySchema(ctx, pool); err != nil {
			t.Fatalf("ApplySchema pass %d: %v", pass, err)
		}
		got := readIdentityFields(t, ctx, pool, ids[0])
		if pass == 1 {
			assertLegacyKeysCanonical(t, got)
			afterFirst = got
			continue
		}
		// Pass 2: hasil harus SAMA persis (tidak berosilasi, tidak damage).
		if got != afterFirst {
			t.Fatalf("migrasi tidak idempoten:\n pass 1: %s\n pass 2: %s", afterFirst, got)
		}
	}

	// Baris kanonik tetap kanonik: key, label, required, dan URUTAN utuh.
	var canonicalRaw string
	if err := pool.QueryRow(ctx, `SELECT identity_fields FROM exams WHERE id = $1`, ids[1]).Scan(&canonicalRaw); err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	var gotFields []map[string]interface{}
	if err := json.Unmarshal([]byte(canonicalRaw), &gotFields); err != nil {
		t.Fatalf("unmarshal canonical: %v", err)
	}
	if len(gotFields) != 2 {
		t.Fatalf("baris kanonik berubah jumlah kolom: %d, want 2 (%s)", len(gotFields), canonicalRaw)
	}
	if gotFields[0]["key"] != "student_name" || gotFields[1]["key"] != "exam_number" {
		t.Fatalf("baris kanonik berubah: %s", canonicalRaw)
	}
	if gotFields[0]["label"] != "Nama" || gotFields[1]["label"] != "Nomor Ujian" {
		t.Fatalf("label ikut berubah: %s", canonicalRaw)
	}
	if gotFields[0]["required"] != true || gotFields[1]["required"] != true {
		t.Fatalf("required ikut berubah: %s", canonicalRaw)
	}
}

func readIdentityFields(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int) string {
	t.Helper()
	var raw string
	if err := pool.QueryRow(ctx, `SELECT identity_fields FROM exams WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read identity_fields: %v", err)
	}
	return raw
}

// assertLegacyKeysCanonical: key berspasi ter-trim, duplikat case hilang, dan
// sisanya (termasuk kolom optional) tidak tersentuh.
func assertLegacyKeysCanonical(t *testing.T, raw string) {
	t.Helper()
	var fields []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("unmarshal hasil migrasi: %v (%s)", err, raw)
	}
	wantKeys := []string{"nama", "Kelas", "nomor_ujian"}
	if len(fields) != len(wantKeys) {
		t.Fatalf("jumlah kolom = %d, want %d (%s)", len(fields), len(wantKeys), raw)
	}
	for i, want := range wantKeys {
		if got, _ := fields[i]["key"].(string); got != want {
			t.Fatalf("kolom #%d key = %q, want %q (%s)", i+1, got, want, raw)
		}
	}
	// Label & required kolom pertama harus utuh (kita hanya menormalkan key).
	if fields[0]["label"] != "Nama" || fields[0]["required"] != true {
		t.Fatalf("kolom pertama berubah: %s", raw)
	}
	if fields[2]["required"] != false {
		t.Fatalf("kolom optional berubah: %s", raw)
	}
}
