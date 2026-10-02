package admin

import "testing"

// validateIdentityFields murni (tanpa DB): key non-string (angka dari JSON),
// key duplikat case-insensitive, dan > 30 field harus ditolak saat simpan
// agar ujian tidak menjadi unsubmittable.
func TestValidateIdentityFields(t *testing.T) {
	valid := []map[string]interface{}{
		{"key": "student_name", "label": "Nama", "required": true},
		{"key": "exam_number", "label": "Nomor Ujian", "required": true},
	}
	if msg := validateIdentityFields(valid); msg != "" {
		t.Fatalf("valid fields rejected: %s", msg)
	}
	if msg := validateIdentityFields(nil); msg != "" {
		t.Fatalf("nil fields rejected: %s", msg)
	}

	// Key numerik (decode JSON → float64) harus DITOLAK — di path submit ia
	// masuk ke struct `Key string` yang error-nya di-swallow sehingga field
	// tak pernah cocok dan ujian tak bisa di-submit.
	numeric := []map[string]interface{}{
		{"key": float64(123), "label": "Angka", "required": true},
	}
	if msg := validateIdentityFields(numeric); msg == "" {
		t.Fatalf("numeric key accepted, want rejection")
	}

	nonString := []map[string]interface{}{
		{"key": true, "label": "Bool", "required": true},
	}
	if msg := validateIdentityFields(nonString); msg == "" {
		t.Fatalf("bool key accepted, want rejection")
	}

	empty := []map[string]interface{}{
		{"key": "   ", "label": "Kosong", "required": true},
	}
	if msg := validateIdentityFields(empty); msg == "" {
		t.Fatalf("empty key accepted, want rejection")
	}

	missing := []map[string]interface{}{
		{"label": "Tanpa Key", "required": true},
	}
	if msg := validateIdentityFields(missing); msg == "" {
		t.Fatalf("missing key accepted, want rejection")
	}

	dup := []map[string]interface{}{
		{"key": "Nama", "label": "A", "required": true},
		{"key": "nAMA", "label": "B", "required": true},
	}
	if msg := validateIdentityFields(dup); msg == "" {
		t.Fatalf("case-insensitive duplicate key accepted, want rejection")
	}

	many := make([]map[string]interface{}, 31)
	for i := range many {
		many[i] = map[string]interface{}{"key": "k"}
	}
	// Buat key unik agar yang diuji murni batas jumlah.
	for i := range many {
		many[i]["key"] = string(rune('a'+i%26)) + string(rune('0'+i/26))
	}
	if msg := validateIdentityFields(many); msg == "" {
		t.Fatalf("31 fields accepted, want rejection (>30)")
	}
}
