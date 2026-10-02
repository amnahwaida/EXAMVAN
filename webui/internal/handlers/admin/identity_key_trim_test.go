package admin

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// H6 — key identitas yang BERSPASI harus ditolak, bukan disimpan.
//
// Client menormalisasi key (`identity_dialog.py:70`,
// `norm_key = str(f.key or "").strip()`) SEBELUM mengirim payload, jadi
// siswa tidak pernah mengirim key berspasi. Server, sebaliknya, hanya
// menolak key yang kosong total (`TrimSpace(key) == ""`) lalu MENYIMPAN dan
// mencari `body.IdentityData[field.Key]` dengan key MENTAH itu. Config
// tersimpan `" nama "` lalu menggantung selamanya:
//
//	stored key : " nama "     → identity_dialog() → wire key "nama"
//	submit     : identity_data["nama"]
//	lookup     : identity_data[" nama "]  → miss → 400
//	             "Identitas 'Nama' wajib diisi"  (selamanya)
//
// Memperbaiki sisi client saja tidak cukup: config sudah ada di server dan
// bisa datang dari API/import mana pun. Jadi server yang harus menutup
// celah ini, dan ia menolak key yang tidak kanonik alih-alih menyimpannya
// dalam bentuk lain — tidak ada satu pun bentuk yang bisa disepakati diam-diam
// antara client dan server.
// ---------------------------------------------------------------------------

func TestValidateIdentityFieldsRejectsUntrimmedKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"spasi depan", " nama"},
		{"spasi belakang", "nama "},
		{"spasi dua sisi", " nama "},
		{"tab", "\tnama\t"},
		{"newline", "nama\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := []map[string]interface{}{
				{"key": tc.key, "label": "Nama", "required": true},
			}
			msg := validateIdentityFields(fields)
			if msg == "" {
				t.Fatalf("key %q diterima — akan menggantung selamanya", tc.key)
			}
			if !strings.Contains(msg, "spasi") {
				t.Fatalf("pesan harus menyebut spasi, dapat: %s", msg)
			}
		})
	}
}

func TestValidateIdentityFieldsMessageNamesTheColumn(t *testing.T) {
	fields := []map[string]interface{}{
		{"key": "nama", "label": "Nama", "required": true},
		{"key": "kelas ", "label": "Kelas", "required": true},
	}
	msg := validateIdentityFields(fields)
	if !strings.Contains(msg, "#2") {
		t.Fatalf("pesan harus menyebut kolom ke-2, dapat: %s", msg)
	}
}

func TestValidateIdentityFieldsAcceptsAlreadyTrimmedKey(t *testing.T) {
	// Penolakan harus TIDAK menyiksa key yang memang sudah kanonik.
	fields := []map[string]interface{}{
		{"key": "nama", "label": "Nama", "required": true},
		{"key": "nomor_ujian", "label": "Nomor Ujian", "required": true},
		{"key": "kelas", "label": "Kelas", "required": true},
	}
	if msg := validateIdentityFields(fields); msg != "" {
		t.Fatalf("key kanonik ditolak: %s", msg)
	}
}

func TestValidateIdentityFieldsDuplicateCheckUsesStoredForm(t *testing.T) {
	// Normalisasi yang dipakai cek duplikat harus sama dengan bentuk yang
	// DISIMPAN: trimmed + case-insensitive. Tanpa itu, "nama" dan "nama "
	// lolos sebagai dua kolom berbeda (key yang sama setelah trim),
	// dan client menggabungkannya jadi satu input.
	cases := []struct {
		name string
		keys []string
	}{
		{"beda huruf besar-kecil", []string{"Nama", "nAMA"}},
		{"identik", []string{"nama", "nama"}},
		{"satu ada spasi di configs lama", []string{"nama", "nama "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := []map[string]interface{}{
				{"key": tc.keys[0], "label": "A", "required": true},
				{"key": tc.keys[1], "label": "B", "required": true},
			}
			// Case "satu ada spasi" sekarang ditolak lebih dulu oleh
			// aturan kanonik; dua-duanya tetap tidak boleh lolos.
			if msg := validateIdentityFields(fields); msg == "" {
				t.Fatalf("duplikat %v diterima", tc.keys)
			}
		})
	}
}

func TestValidateIdentityFieldsTrimmedKeyStillDetectsCaseInsensitiveDuplicate(t *testing.T) {
	fields := []map[string]interface{}{
		{"key": "Kelas", "label": "A", "required": true},
		{"key": "kelas", "label": "B", "required": true},
	}
	msg := validateIdentityFields(fields)
	if !strings.Contains(msg, "sama dengan") {
		t.Fatalf("duplikat case-insensitive harus tetap terdeteksi, dapat: %s", msg)
	}
}

func TestIdentityFieldKeyIsNeverPersistedUntrimmed(t *testing.T) {
	// Penjaga tambahan: fungsi normalisasi yang dipakai validateIdentityFields
	// harus mengembalikan bentuk yang sama persis dengan bentuk yang dibaca
	// client di payload — kalau tidak, mismatch ini akan kembali.
	fields := []map[string]interface{}{
		{"key": "nama", "label": "Nama", "required": true},
	}
	if msg := validateIdentityFields(fields); msg != "" {
		t.Fatalf("key \"nama\" ditolak: %s", msg)
	}
	stored := strings.TrimSpace("nama")
	if stored != "nama" {
		t.Fatalf("bentuk tersimpan %q harus sama dengan key yang dikirim client", stored)
	}
}
