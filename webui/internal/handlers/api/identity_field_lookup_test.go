package api

import "testing"

// ---------------------------------------------------------------------------
// H4 — field identitas dengan key KOSONG harus dibaca POSISIONAL.
//
// Namespace `field_<i>` BUKAN synthetic-only: `static/js/admin.js:1013`
// menuliskannya untuk setiap label yang normalisasi ke string kosong, jadi
// guru bisa (dan sudah) menyimpan config seperti ini:
//
//	[{"key":"","label":"Alamat"},
//	 {"key":"field_0","label":"Kelas","required":true},
//	 {"key":"field_1","label":"No Ujian","required":true}]
//
// Client (identity_dialog.py) menamai field tanpa key dengan `field_<index>`
// — index = posisinya di exam.identity_fields — lalu mengirim dict itu apa
// adanya. Dulu server mencari `body.IdentityData[field.Key]`, yaitu
// `identity_data[""]`, yang HANYA diisi client kalau TEPAT SATU field tanpa
// key. Dua key kosong atau lebih = kunci tidak pernah ditemukan = 400
// "Identitas 'Kelas' wajib diisi" selamanya, dan `field_0` yang benar-benar
// terisi diabaikan.
// ---------------------------------------------------------------------------

func TestIdentityFieldValueEmptyKeyResolvesPositionally(t *testing.T) {
	fields := []identityField{
		{Key: "", Label: "Alamat"},
		{Key: "field_0", Label: "Kelas", Required: true},
		{Key: "field_1", Label: "No Ujian", Required: true},
	}
	// Tanpa alias warisan `""` sama sekali.
	data := map[string]interface{}{
		"field_0": "9A",
		"field_1": "U-7",
	}
	for idx, f := range fields {
		if !f.Required {
			continue
		}
		if got := identityFieldValue(data, f, idx); got == "" {
			t.Fatalf("field #%d (%s) tidak ditemukan dari %v", idx, f.Label, data)
		}
	}
}

func TestIdentityFieldValueEmptyKeyWithSeveralEmptyKeys(t *testing.T) {
	// Dua field tanpa key: alias `""` tidak mungkin menutup keduanya.
	fields := []identityField{
		{Key: "", Label: "Kelas", Required: true},
		{Key: "", Label: "No Ujian", Required: true},
	}
	data := map[string]interface{}{
		"field_0": "9A",
		"field_1": "U-7",
	}
	if got := identityFieldValue(data, fields[0], 0); got != "9A" {
		t.Fatalf("field #0 dapat %q, want 9A", got)
	}
	if got := identityFieldValue(data, fields[1], 1); got != "U-7" {
		t.Fatalf("field #1 dapat %q, want U-7", got)
	}
}

func TestIdentityFieldValueNonEmptyKeyIsUnaffected(t *testing.T) {
	cases := []struct {
		key  string
		data map[string]interface{}
		want string
	}{
		{"nama", map[string]interface{}{"nama": "Andi"}, "Andi"},
		{"nomor_ujian", map[string]interface{}{"nomor_ujian": "N01"}, "N01"},
		// Key yang tidak ada di payload -> kosong (tidak menebak).
		{"kelas", map[string]interface{}{"nama": "Andi"}, ""},
		// Nilai non-string diabaikan, bukan dipaksa jadi teks.
		{"nama", map[string]interface{}{"nama": 42}, ""},
		// Nilai whitespace-only = belum diisi.
		{"nama", map[string]interface{}{"nama": "   "}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			f := identityField{Key: tc.key, Label: "x", Required: true}
			if got := identityFieldValue(tc.data, f, 0); got != tc.want {
				t.Fatalf("identityFieldValue = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFirstMissingRequiredIdentityEmptyKeyResolvesPositionally(t *testing.T) {
	fields := []identityField{
		{Key: "", Label: "Alamat"},
		{Key: "field_0", Label: "Kelas", Required: true},
		{Key: "field_1", Label: "No Ujian", Required: true},
	}
	data := map[string]interface{}{"field_0": "9A", "field_1": "U-7"}
	if msg := firstMissingRequiredIdentity(data, fields, nil); msg != "" {
		t.Fatalf("config dengan field_0/field_1 harus lolos, dapat %q", msg)
	}
}

func TestFirstMissingRequiredIdentityLegacyAliasIsRedundant(t *testing.T) {
	// Client tetap mengirim alias `""` untuk tepat satu field tanpa key;
	// jalur positional harus bekerja TANPA alias itu, dan tetap sama
	// hasilnya ketika alias ikut ada.
	fields := []identityField{
		{Key: "", Label: "Kelas", Required: true},
		{Key: "field_1", Label: "No Ujian", Required: true},
	}
	without := map[string]interface{}{"field_0": "9A", "field_1": "U-7"}
	withAlias := map[string]interface{}{
		"field_0": "9A", "field_1": "U-7", "": "9A",
	}
	if a := firstMissingRequiredIdentity(without, fields, nil); a != "" {
		t.Fatalf("tanpa alias harus lolos, dapat %q", a)
	}
	if b := firstMissingRequiredIdentity(withAlias, fields, nil); b != "" {
		t.Fatalf("dengan alias harus lolos, dapat %q", b)
	}
}

func TestFirstMissingRequiredIdentityStillRejectsGenuinelyEmpty(t *testing.T) {
	// Key kosong yang TIDAK punya field_<idx> tetap harus 400 — jangan
	// sampai lookup positional menerima sembarang field.
	fields := []identityField{
		{Key: "", Label: "Kelas", Required: true},
		{Key: "", Label: "No Ujian", Required: true},
	}
	data := map[string]interface{}{"field_1": "U-7"}
	if msg := firstMissingRequiredIdentity(data, fields, nil); msg != "Kelas" {
		t.Fatalf("harus menolak field #0 (Kelas), dapat %q", msg)
	}
}

func TestFirstMissingRequiredIdentitySkipsOptionalAndUsesCanonical(t *testing.T) {
	fields := []identityField{
		{Key: "nama", Label: "Nama"},
		{Key: "student_name", Label: "Nama Siswa", Required: true},
		{Key: "exam_number", Label: "Nomor Ujian", Required: true},
	}
	// Kolom top-level dipakai sebagai fallback untuk key kanonik (perilaku
	// lama), dan field optional tidak boleh memblokir submit.
	canonical := map[string]string{"student_name": "Andi", "exam_number": "N01"}
	if msg := firstMissingRequiredIdentity(nil, fields, canonical); msg != "" {
		t.Fatalf("harus lolos lewat kolom kanonik, dapat %q", msg)
	}
	if msg := firstMissingRequiredIdentity(nil, fields, nil); msg != "Nama Siswa" {
		t.Fatalf("tanpa kolom kanonik harus menolak, dapat %q", msg)
	}
}
