package helpers

import "testing"

// ---------------------------------------------------------------------------
// M3 — StudentKeyFromIdentityData harus DETERMINISTIK dan SEPAKAT dengan
// examvan/utils.py map_identity_to_standard (H5).
//
// Dua cacat lama:
//
//  1. NONDETERMINISTIK — ia meng-iterasi map Go (`for k, v := range identity`),
//     dan urutan iterasi map Go sengaja diacak. Config dengan dua field yang
//     bisa menjelaskan slot yang sama bisa menghasilkan kunci BERBEDA antar
//     proses: izin yang diberikan pengawas pada satu run tidak berlaku di run
//     berikutnya, dan tidak ada yang melaporkannya.
//  2. SUBSTRING — `strings.Contains(lowerKey, kw)` dengan kata bersama
//     ("no_", "no.", "exam") membuat kunci tak terduga claiming slot, dan
//     tidak bisa_parse `kode_ujian`/`ujian` seperti client (lihat H5).
//
// Kata penentu slot dan urutannya harus sama persis dengan utils.py:
// tier-1 menang atas tier-2, kunci kosong/tanggal tidak pernah mengklaim
// slot, dan nama slot diproses nomor -> nama -> kelas.
// ---------------------------------------------------------------------------

// studentKeyDeterminism runs mirrors the client-side identity tables. The
// expected value is what examvan/utils.py produces for the same input.
func TestStudentKeyFromIdentityDataMatchesClientMapping(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{"nama/nomor_ujian/kelas", map[string]interface{}{
			"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}, "n01"},
		{"id_kelas saja", map[string]interface{}{
			"id_kelas": "9A"}, "9a"},
		{"id_kelas + nama -> nama menang", map[string]interface{}{
			"nama": "Andi", "id_kelas": "9A"}, "andi"},
		{"kode_ujian", map[string]interface{}{
			"nama": "Andi", "kode_ujian": "U-7"}, "u-7"},
		{"ujian saja", map[string]interface{}{
			"nama": "Andi", "ujian": "U-8"}, "u-8"},
		{"nim", map[string]interface{}{
			"nama": "Andi", "nim": "12345"}, "12345"},
		{"nama_lengkap", map[string]interface{}{
			"nama_lengkap": "Andi Pratama"}, "andi pratama"},
		{"no_peserta", map[string]interface{}{
			"no_peserta": "N02"}, "n02"},
		// Gate `no`: `no` hanya berarti "nomor" kalau berdiri sendiri atau
		// diikuti kata yang benar-benar menyebut identitas. Tanpa gate ini
		// nomor telepon siswa menjadi nomor ujiannya di server, padahal
		// client (examvan/utils.py `_NO_FOLLOWERS`) sudah menolaknya sejak
		// ronde 7 — dua sisi tidak sepakat soal kunci yang sama.
		{"no_hp bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "no_hp": "0812", "kelas": "9A"}, "andi"},
		{"no_telp bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "no_telp": "0812"}, "andi"},
		{"no_wa bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "no_wa": "0812"}, "andi"},
		{"no_hp_siswa bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "no_hp_siswa": "0812"}, "andi"},
		{"hp_no bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "hp_no": "0812"}, "andi"},
		{"telp_no bukan nomor ujian", map[string]interface{}{
			"nama": "Andi", "telp_no": "0812"}, "andi"},
		// Tapi `no` yang sah tetap memetakan exam_number.
		{"no sendirian", map[string]interface{}{
			"nama": "Andi", "no": "7"}, "7"},
		{"no_absen", map[string]interface{}{
			"nama": "Andi", "no_absen": "12"}, "12"},
		{"no_ujian", map[string]interface{}{
			"nama": "Andi", "no_ujian": "N01"}, "n01"},
		{"no_nomor", map[string]interface{}{
			"nama": "Andi", "no_nomor": "N3"}, "n3"},
		// Kata yang menyebut slot mengalahkan singkatan `no` di posisi dan
		// tier yang sama — urutan lexicografis justru mengarah ke
		// sebaliknya (`no_ujian` < `nomor_ujian`).
		{"nomor_ujian menang atas no_ujian", map[string]interface{}{
			"no_ujian": "N01", "nomor_ujian": "N02", "nama": "Andi"}, "n02"},
		{"nomor_peserta menang atas no_peserta", map[string]interface{}{
			"no_peserta": "N01", "nomor_peserta": "N02", "nama": "Andi"}, "n02"},
		{"nomor_kursi tetap nomor ujian", map[string]interface{}{
			"nama": "Andi", "nomor_kursi": "K3"}, "k3"},
		// Satu kunci yang menyebut DUA slot mengisi keduanya (H11).
		{"nama_kelas mengisi kelas juga", map[string]interface{}{
			"nama_kelas": "9A"}, "9a"},
		{"rombel_nama mengisi kelas juga", map[string]interface{}{
			"rombel_nama": "9A"}, "9a"},
		{"nama_kelas + kelas khusus", map[string]interface{}{
			"nama": "Andi", "nama_kelas": "9A", "kelas": "9B"}, "andi"},
		{"nama_ujian adalah nama", map[string]interface{}{
			"nama_ujian": "01"}, "01"},
		{"rombel", map[string]interface{}{
			"rombel": "9B"}, "9b"},
		{"tanggal_lahir bukan nomor", map[string]interface{}{
			"nama": "Andi", "tanggal_lahir": "2010-05-05"}, "andi"},
		{"exam_date bukan nomor", map[string]interface{}{
			"nama": "Andi", "exam_date": "2026-10-02"}, "andi"},
		{"jam_ujian bukan nomor", map[string]interface{}{
			"nama": "Andi", "jam_ujian": "09:00"}, "andi"},
		{"alamat bukan nama", map[string]interface{}{
			"alamat": "Jl. Mawar 1"}, ""},
		{"nilai bukan nama", map[string]interface{}{
			"nilai": "95"}, ""},
		{"email bukan nama", map[string]interface{}{
			"email": "a@b.c"}, ""},
		{"kode_pos bukan nomor", map[string]interface{}{
			"nama": "Andi", "kode_pos": "12345"}, "andi"},
		{"jurusan bukan kelas", map[string]interface{}{
			"nama": "Andi", "jurusan": "IPA"}, "andi"},
		{"angkatan bukan kelas", map[string]interface{}{
			"nama": "Andi", "angkatan": "2026"}, "andi"},
		{"agama bukan nama", map[string]interface{}{
			"nama": "Andi", "agama": "Islam"}, "andi"},
		// Field yang bisa menjelaskan SLOT SAMA harus memilih yang paling
		// spesifik dan stabil — urutan map Go tidak boleh memengaruhi.
		{"nomor_ujian menang atas kode_ujian", map[string]interface{}{
			"kode_ujian": "U-7", "nomor_ujian": "N01", "nama": "Andi"}, "n01"},
		{"studentName/studentClass/examNumber", map[string]interface{}{
			"studentName": "Andi", "studentClass": "9A", "examNumber": "N03"}, "n03"},
		{"namaSiswa/nomorUjian/kelasSiswa", map[string]interface{}{
			"namaSiswa": "Andi", "nomorUjian": "N04", "kelasSiswa": "9A"}, "n04"},
		// Nilai non-string harus diabaikan, bukan dipaksa jadi teks.
		{"nilai non-string", map[string]interface{}{
			"nomor_ujian": float64(1), "nama": "Andi"}, "andi"},
		{"whitespace only", map[string]interface{}{
			"nama": "   ", "kelas": "9A"}, "9a"},
		{"semua tak dikenal", map[string]interface{}{
			"alamat": "Jl. Mawar", "kode_pos": "12345"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StudentKeyFromIdentityData(tc.data, "", "", "")
			if got != tc.want {
				t.Fatalf("got %q, want %q (data=%v)", got, tc.want, tc.data)
			}
		})
	}
}

func TestStudentKeyFromIdentityDataIsDeterministic(t *testing.T) {
	// Dulu: `for k, v := range identity` mengacak urutan pemindaian, jadi
	// dua field yang sama-sama bisa mengisi slot bisa mengembalikan kunci
	// yang BERBEDA antar proses. Izin yang diberikan pengawas lalu berlaku
	// atau tidak secara acak.
	data := map[string]interface{}{
		"nomor_ujian": "N01",
		"kode_ujian":  "U-7",
		"ujian":       "U-8",
		"nama":        "Andi",
		"kelas":       "9A",
		"id_kelas":    "9B",
		"rombel":      "9C",
	}
	first := StudentKeyFromIdentityData(data, "", "", "")
	for i := 0; i < 50; i++ {
		if got := StudentKeyFromIdentityData(data, "", "", ""); got != first {
			t.Fatalf("panggilan #%d dapat %q, sebelumnya %q — tidak deterministik", i, got, first)
		}
	}
	if first != "n01" {
		t.Fatalf("kunci paling spesifik harus menang, dapat %q", first)
	}
}

func TestStudentKeyFromIdentityDataCanonicalKeysStillWin(t *testing.T) {
	// Slot kanonik yang tersimpan di DB harus tetap menang atas tebakan.
	data := map[string]interface{}{
		"student_name": "Siti", "nama": "Andi",
		"exam_number": "N09", "nomor_ujian": "N01",
	}
	if got := StudentKeyFromIdentityData(data, "", "", ""); got != "n09" {
		t.Fatalf("got %q, want n09 (kunci kanonik menang)", got)
	}
}
