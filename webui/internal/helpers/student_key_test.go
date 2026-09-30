package helpers

import "testing"

// Kunci ini harus PERSIS sama dengan examvan/utils.py build_student_key().
// Kalau berbeda, izin yang diberikan pengawas tidak akan berlaku, dan dari
// sisi client kelihatannya hanya "siswa ditolak terus" tanpa sebabnya.
func TestStudentKeyPrefersExamNumber(t *testing.T) {
	cases := []struct {
		name                          string
		examNumber, stuName, stuClass string
		want                          string
	}{
		{"nomor menang", "N01", "Andi", "9A", "n01"},
		{"tanpa nomor -> nama", "", "Andi", "9A", "andi"},
		{"tanpa nomor dan nama -> kelas", "", "", "9A", "9a"},
		{"semua kosong", "", "", "", ""},
		{"spasi dibersihkan", "  N01  ", "Andi", "9A", "n01"},
		{"huruf besar-kecil sama", "n01", "Andi", "9A", "n01"},
		{"nomor whitespace only -> nama", "   ", "Andi", "9A", "andi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StudentKey(tc.examNumber, tc.stuName, tc.stuClass)
			if got != tc.want {
				t.Fatalf("StudentKey(%q,%q,%q) = %q, want %q",
					tc.examNumber, tc.stuName, tc.stuClass, got, tc.want)
			}
		})
	}
}

func TestStudentKeyIsStableForTheSameStudent(t *testing.T) {
	// Pengawas melihat nomor "N01" di tabel, client mengetik "n01".
	// Keduanya harus menjadi satu kunci yang sama.
	fromTable := StudentKey("N01", "Andi", "9A")
	fromTyped := StudentKey("n01", "andi", "9A")
	if fromTable != fromTyped {
		t.Fatalf("kunci berbeda: %q vs %q", fromTable, fromTyped)
	}
}

func TestStudentKeyDiffersBetweenStudents(t *testing.T) {
	if StudentKey("N01", "Andi", "9A") == StudentKey("N02", "Andi", "9A") {
		t.Fatal("dua siswa berbeda harus punya kunci berbeda")
	}
}

func TestStudentKeyFromIdentityData(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{
			"field standar",
			map[string]interface{}{"exam_number": "N01", "student_name": "Andi"},
			"n01",
		},
		{
			"field kustom ala client (nama/nomor_ujian)",
			map[string]interface{}{"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
			"n01",
		},
		{
			"hanya nama kustom",
			map[string]interface{}{"nama": "Andi", "kelas": "9A"},
			"andi",
		},
		{
			"kosong -> kolom",
			map[string]interface{}{},
			"",
		},
		{
			"nilai non-string diabaikan",
			map[string]interface{}{"exam_number": 42, "nama": "Andi"},
			"andi",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StudentKeyFromIdentityData(tc.data, "", "", "")
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStudentKeyFromIdentityDataFallsBackToColumns(t *testing.T) {
	got := StudentKeyFromIdentityData(nil, "N07", "Andi", "9A")
	if got != "n07" {
		t.Fatalf("harus jatuh ke kolom, dapat %q", got)
	}
}

func TestEmptyIdentityNeverProducesAKey(t *testing.T) {
	// Kunci kosong berarti "tidak bisa diidentifikasi". Server TIDAK boleh
	// memblokir berdasarkan itu -- siswa yang tidak bisa diidentifikasi akan
	// terkunci dengan tidak ada yang bisa melepas.
	if StudentKeyFromIdentityData(
		map[string]interface{}{"nama": "   "}, "", "", "",
	) != "" {
		t.Fatal("identitas berisi spasi saja harus menghasilkan kunci kosong")
	}
}
