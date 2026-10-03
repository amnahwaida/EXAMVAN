package api

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// M8 — kolom identity_fields yang RUSAK tidak boleh mematikan validasi.
//
// Bug
// ---
// Jalur submit dulu menelan error parse:
//
//	if exam.IdentityFields != nil && *exam.IdentityFields != "" && *exam.IdentityFields != "[]" {
//	    _ = json.Unmarshal([]byte(*exam.IdentityFields), &expectedFields)
//	}
//
// `_ =` membuang errornya, jadi kolom yang ADA tapi tidak bisa di-parse
// (`"{"`, `null`, angka, JSON bentuk lain) meninggalkan `expectedFields`
// KOSONG. `firstMissingRequiredIdentity` lalu mengiterasi tidak ada apa pun dan
// submit DITERIMA tanpa satu pun syarat identitas.
//
// Yang paling buruk: jalur LIST (`helpers.ParseIdentityFields`, dipakai
// `HasilAPI`) gagal-TERBUKA ke tiga default kanonik. Jadi client dan server
// tidak sepakat tentang kolom mana yang wajib — client mewajibkan tiga kolom,
// server tidak mewajibkan apa pun.
//
// Fix: error parse dicek dan jatuh ke default yang SAMA dengan jalur list.
// ---------------------------------------------------------------------------

// TestExpectedIdentityFieldsFallsBackOnMalformedColumn mengunci kata
// "malformed" yang harus jatuh ke tiga default kanonik.
func TestExpectedIdentityFieldsFallsBackOnMalformedColumn(t *testing.T) {
	want := canonicalDefaultIdentityFields()
	if len(want) != 3 {
		t.Fatalf("default kanonik = %d kolom, want 3", len(want))
	}

	malformed := []struct {
		name string
		raw  string
	}{
		// Tidak bisa di-parse sama sekali.
		{"objek terpotong", "{"},
		{"teks bebas", "bukan json sama sekali"},
		{"array angka", "[1,2]"},
		{"array object kosong", "[{}]"},
		// Parse OK tapi tidak menghasilkan field yang bisa dipakai:
		// `null` dan `[]` membuat slice kosong, `[{},{}]` cuma menghasilkan
		// field tanpa key DAN tanpa label.
		{"null", "null"},
		{"array kosong", "[]"},
		{"beberapa object kosong", "[{},{}]"},
		// Bentuk lain yang bukan array of object.
		{"object berisi object", `{"key":"nama"}`},
		{"string", `"nama"`},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			got := expectedIdentityFields(&tc.raw)
			if len(got) != len(want) {
				t.Fatalf("kolom %q -> %d field, want %d default kanonik (%s)",
					tc.raw, len(got), len(want), describeFields(got))
			}
			for i := range want {
				if got[i].Key != want[i].Key || got[i].Label != want[i].Label || got[i].Required != want[i].Required {
					t.Fatalf("kolom %q -> field #%d = %+v, want %+v",
						tc.raw, i+1, got[i], want[i])
				}
			}
		})
	}
}

// Kolom kosong / NULL juga harus jatuh ke default (perilaku lama, dipertahankan).
func TestExpectedIdentityFieldsFallsBackOnEmptyColumn(t *testing.T) {
	want := canonicalDefaultIdentityFields()
	for _, raw := range []*string{nil} {
		got := expectedIdentityFields(raw)
		if len(got) != len(want) {
			t.Fatalf("kolom NULL -> %d field, want %d", len(got), len(want))
		}
	}
	for _, s := range []string{"", "   "} {
		v := s
		if got := expectedIdentityFields(&v); len(got) != len(want) {
			t.Fatalf("kolom %q -> %d field, want %d", s, len(got), len(want))
		}
	}
}

// Kolom yang SAH tidak boleh tersentuh: label, required, dan URUTAN asli
// tetap apa adanya.
func TestExpectedIdentityFieldsKeepsValidColumnUnchanged(t *testing.T) {
	raw := `[
		{"key":"nama","label":"Nama","required":true},
		{"key":"kelas","label":"Kelas","required":false},
		{"key":"no_absen","label":"No Absen","required":true}
	]`
	got := expectedIdentityFields(&raw)
	want := []identityField{
		{Key: "nama", Label: "Nama", Required: true},
		{Key: "kelas", Label: "Kelas", Required: false},
		{Key: "no_absen", Label: "No Absen", Required: true},
	}
	if len(got) != len(want) {
		t.Fatalf("jumlah field = %d, want %d (%s)", len(got), len(want), describeFields(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field #%d = %+v, want %+v", i+1, got[i], want[i])
		}
	}
}

// Field tanpa `key` tapi dengan `label` sah (jalur positional H4) TIDAK boleh
// dianggap malformed — itu config yang sah.
func TestExpectedIdentityFieldsKeepsPositionalFields(t *testing.T) {
	raw := `[{"key":"","label":"Alamat"},{"key":"","label":"Kelas","required":true}]`
	got := expectedIdentityFields(&raw)
	if len(got) != 2 {
		t.Fatalf("config field-positional dianggap malformed (%s)", describeFields(got))
	}
	if got[1].Key != "" || got[1].Label != "Kelas" || !got[1].Required {
		t.Fatalf("field positional berubah: %+v", got[1])
	}
}

// Yang paling penting secara operasional: kolom rusak tidak boleh membuat
// submit lolos tanpa identitas sama sekali.
func TestMalformedColumnStillRequiresTheCanonicalIdentity(t *testing.T) {
	raw := "{"
	fields := expectedIdentityFields(&raw)
	if msg := firstMissingRequiredIdentity(nil, fields, nil); msg == "" {
		t.Fatalf("kolom rusak %q membuat submit tanpa identitas apa pun "+
			"DITERIMA — validasi identitas mati diam-diam", raw)
	}
	// Dan kolom rusak harus tetap bisa ditutup lewat kolom kanonik top-level,
	// persis seperti default lama.
	canonical := map[string]string{
		"student_name":  "Andi",
		"exam_number":   "N01",
		"student_class": "9A",
	}
	if msg := firstMissingRequiredIdentity(nil, fields, canonical); msg != "" {
		t.Fatalf("default kanonik harus bisa ditutup lewat kolom top-level, dapat %q", msg)
	}
}

// Kolom rusak TIDAK boleh membuat validasi lebih longgar dari default lama:
// `parseIdentityFieldsDefault` harus benar-benar sama dengan default lama.
func TestCanonicalDefaultIdentityFieldsMatchesSubmitFallback(t *testing.T) {
	raw := "[1,2]"
	got := expectedIdentityFields(&raw)
	// Default lama di blok submit: student_name / exam_number / student_class.
	wantKeys := []string{"student_name", "exam_number", "student_class"}
	for i, want := range wantKeys {
		if got[i].Key != want {
			t.Fatalf("field #%d key = %q, want %q — fallback harus memakai key "+
				"kanonik yang sama dengan kolom top-level", i+1, got[i].Key, want)
		}
		if !got[i].Required {
			t.Fatalf("field #%d harus wajib", i+1)
		}
	}
}

func describeFields(fields []identityField) string {
	b, _ := json.Marshal(fields)
	if len(b) > 200 {
		b = append(b[:200], []byte("...")...)
	}
	return string(b)
}

func strptr(s string) *string { return &s }

// TestExpectedIdentityFieldsFailsClosed adalah pengaman lapis kedua: helper
// yang dipanggil SubmitExam harus mengembalikan tiga kolom kanonik untuk
// SEMUA masukan rusak, bukan hanya yang diuji.
func TestExpectedIdentityFieldsFailsClosed(t *testing.T) {
	bad := []struct {
		nama string
		raw  *string
	}{
		{"bukan JSON sama sekali", strptr(`{`)},
		{"JSON null", strptr(`null`)},
		{"JSON array angka", strptr(`[1,2]`)},
		{"array berisi objek tanpa key", strptr(`[{}]`)},
		{"array kosong", strptr(`[]`)},
		{"string JSON, bukan array", strptr(`"nama"`)},
		{"objek, bukan array", strptr(`{"key":"nama"}`)},
		{"key bukan string", strptr(`[{"key":123,"label":"Nama","required":true}]`)},
		// BUKAN kasus gagal-tertutup: `[{"key":"nama","label":"Nama"}]` adalah
		// JSON yang valid yang mendeklarasikan satu field OPSIONAL. `required`
		// yang hilang di-unmarshal ke `false`, jadi mengembalikannya apa adanya
		// benar — admin memang tidak mewajibkan apa pun. Kalau ini dikembalikan
		// ke default kanonik, config yang sah akan diam-diam berubah jadi
		// mewajibkan nama/nomor/kelas dan siswa tidak bisa masuk sama sekali.
	}
	want := canonicalDefaultIdentityFields()

	for _, tc := range bad {
		t.Run(tc.nama, func(t *testing.T) {
			got := expectedIdentityFields(tc.raw)
			if len(got) != len(want) {
				t.Fatalf("expectedIdentityFields(%s) = %d field, want %d "+
					"(harus jatuh ke default kanonik)",
					*tc.raw, len(got), len(want))
			}
			for i := range want {
				if got[i].Key != want[i].Key || got[i].Label != want[i].Label ||
					got[i].Required != want[i].Required {
					t.Errorf("field #%d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}
