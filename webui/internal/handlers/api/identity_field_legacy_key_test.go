package api

import "testing"

// ---------------------------------------------------------------------------
// H5 — key identitas warisan harus tetap RESOLVE di jalur baca.
//
// Bug
// ---
// `validateIdentityFields` (internal/handlers/admin) sekarang menolak key yang
// tidak kanonik (berspasi) dan key duplikat case-insensitive — tapi HANYA saat
// MENYIMPAN. Tidak ada migrasi untuk baris yang sudah ada di database, dan
// tidak ada yang pernah membaca ulang dan menormalkan. Sementara itu client
// (`identity_dialog.py:107`) menormalkan key sebelum mengirim:
//
//	stored key : " nama "  →  wire key "nama"
//	submit     : identity_data["nama"]
//	lookup     : identity_data[" nama "]  → miss → 400
//	             "Identitas 'Nama' wajib diisi"  (selamanya)
//
// Akibatnya satu baris config warisan membuat SETIAP siswa di kelas itu 400
// permanen, dan tidak ada apa pun yang bisa diperbaiki dari sisi siswa.
//
// Dua bentuk warisan yang nyata:
//
//   - key berspasi: " nama " (ditolak sejak H6, tapi baris lama masih ada).
//   - key duplikat case: config lama menyimpan ["Nama"] lalu guru menambahkan
//     ["nama"] (belum ada cek duplikat waktu itu). Client sekarang
//     case-folded-menggabungkannya jadi satu kolom dan mengirim "nama" saja,
//     sedangkan server masih punya DUA field wajib: `Nama` (cocok) dan `nama`
//     (tidak cocok, karena payload-nya cuma punya satu). Tanpa fallback
//     case-folded, field kedua 400 selamanya.
//
// Fix: `identityFieldValue` mencoba `field.Key`, lalu bentuk TRIMMED-nya, lalu
// bentuk CASE-FOLDED-nya. Bentuk persis selalu menang, jadi config yang sudah
// kanonik tidak berubah sama sekali.
// ---------------------------------------------------------------------------

// keyTrimResolvesLegacyUntrimmedKey: config warisan dengan spasi di key harus
// tetap dibaca.
func TestIdentityFieldValueResolvesLegacyUntrimmedKey(t *testing.T) {
	field := identityField{Key: " nama ", Label: "Nama", Required: true}
	// Client sudah strip sebelum mengirim.
	if got := identityFieldValue(map[string]interface{}{"nama": "Andi"}, field, 0); got != "Andi" {
		t.Fatalf("identityFieldValue = %q, want \"Andi\" — key tersimpan %q tidak "+
			"pernah cocok dengan payload client %q, jadi setiap submit di kelas "+
			"ini dijawab 400 selamanya", got, field.Key, "nama")
	}
	// Bentuk mentahnya harus tetap menang kalau memang ada (kita tidak menebak).
	if got := identityFieldValue(map[string]interface{}{" nama ": "Awal", "nama": "Andi"}, field, 0); got != "Awal" {
		t.Fatalf("identityFieldValue = %q, want \"Awal\" — key yang tersimpan "+
			"harus SELALU menang sebelum bentuk turunannya dicoba", got)
	}
}

// keyCaseDuplicateResolvesThroughCaseFoldedKey: config lama dengan pasangan
// duplikat case harus dua-duanya resolve dari payload yang hanya memuat satu.
func TestIdentityFieldValueResolvesCaseDuplicatedKey(t *testing.T) {
	// Config warisan: guru menambah "nama" tanpa sadar "Nama" sudah ada.
	fields := []identityField{
		{Key: "Nama", Label: "Nama", Required: true},
		{Key: "nama", Label: "Nama (duplikat)", Required: true},
	}
	// Client menggabungkan case-insensitive lalu mengirim HANYA satu: `nama`.
	data := map[string]interface{}{"nama": "Andi"}
	for idx, f := range fields {
		if got := identityFieldValue(data, f, idx); got != "Andi" {
			t.Fatalf("field #%d (%s) = %q, want \"Andi\" — config lama punya key "+
				"duplikat case (%q), client mengirim satu saja, jadi tanpa "+
				"fallback case-folded field yang satu ini 400 selamanya",
				idx, f.Key, got, f.Key)
		}
	}
	// firstMissingRequiredIdentity harus meloloskan seluruh config.
	if msg := firstMissingRequiredIdentity(data, fields, nil); msg != "" {
		t.Fatalf("config duplikat case harus lolos, dapat %q", msg)
	}
}

// keyPersis selalu menang: payload yang punya BOTH bentuk harus memakai yang
// persis sama dengan yang tersimpan, bukan bentuk turunannya.
func TestIdentityFieldValuePrefersExactKeyOverNormalised(t *testing.T) {
	field := identityField{Key: "  Kelas  ", Label: "Kelas", Required: true}
	data := map[string]interface{}{
		"  Kelas  ": "9A-mentah",
		"kelas":     "9B-trim",
		"KELAS":     "9C-casefold",
	}
	if got := identityFieldValue(data, field, 0); got != "9A-mentah" {
		t.Fatalf("identityFieldValue = %q, want \"9A-mentah\" — urutan percobaan "+
			"harus key persis → trimmed → case-folded", got)
	}
	// Case-folded hanya dipakai kalau dua bentuk pertama tidak ada.
	fieldTrimmed := identityField{Key: "Kelas", Label: "Kelas", Required: true}
	data2 := map[string]interface{}{"kelas": "9B-trim", "KELAS": "9C-casefold"}
	if got := identityFieldValue(data2, fieldTrimmed, 0); got != "9B-trim" {
		t.Fatalf("identityFieldValue = %q, want \"9B-trim\"", got)
	}
	fieldUpper := identityField{Key: "KELAS", Label: "Kelas", Required: true}
	if got := identityFieldValue(data2, fieldUpper, 0); got != "9C-casefold" {
		t.Fatalf("identityFieldValue = %q, want \"9C-casefold\" — client boleh "+
			"mengirim huruf besar-kecil yang berbeda dari config warisan", got)
	}
}

// Nilai yang benar-benar tidak ada tetap 400 — normalisasi tidak boleh
// menerima sembarang key.
func TestFirstMissingRequiredIdentityStillRejectsGenuinelyMissing(t *testing.T) {
	fields := []identityField{
		{Key: " nama ", Label: "Nama", Required: true},
		{Key: "Kelas", Label: "Kelas", Required: true},
		{Key: "nama_ortu", Label: "Nama Orang Tua", Required: true},
	}
	// `nama` ada; `Kelas` tidak ada dalam bentuk apa pun; `nama_ortu` tidak ada.
	msg := firstMissingRequiredIdentity(map[string]interface{}{"nama": "Andi"}, fields, nil)
	if msg != "Kelas" {
		t.Fatalf("harus menolak field kedua, dapat %q — fallback normalisasi "+
			"justru membuat key yang tidak ada dianggap terisi", msg)
	}
}

// Nilai non-string dan whitespace-only tetap dianggap belum diisi pada semua
// bentuk kunci — normalisasi tidak boleh melemahkan aturan isi.
func TestIdentityFieldValueNormalisationKeepsValueRules(t *testing.T) {
	field := identityField{Key: " Nama ", Label: "Nama", Required: true}
	cases := []struct {
		name string
		data map[string]interface{}
	}{
		{"tidak ada sama sekali", map[string]interface{}{"lain": "x"}},
		{"angka, bukan teks", map[string]interface{}{"nama": 42}},
		{"hanya spasi", map[string]interface{}{"nama": "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := identityFieldValue(tc.data, field, 0); got != "" {
				t.Fatalf("identityFieldValue = %q, want kosong", got)
			}
		})
	}
}
