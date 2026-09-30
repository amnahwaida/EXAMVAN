// Kunci identitas siswa — harus PERSIS sama dengan
// examvan/utils.py build_student_key() di client.
//
// Urutannya: nomor ujian, lalu nama, lalu kelas. Kalau urutannya berbeda
// di satu sisi, izin mengulang yang diberikan pengawas tidak akan berlaku
// — dan tidak akan ada yang melaporkannya, karena dari sisi client
// kelihatannya hanya "ditolak terus".
//
// Bedanya dengan device: ini identitas orang, bukan identitas mesin. Label
// perangkat di client per-percobaan, jadi mengunci izin pada mac_address
// akan kehilangan izin begitu siswa memakai PC lain.
package helpers

import "strings"

// StudentKey mengembalikan kunci stabil untuk identitas siswa, atau "" kalau
// tidak ada satu pun field yang terisi.
//
// Nilai dikembalikan dalam bentuk lowercase + trim supaya "N01", " n01 ",
// dan "n01" dianggap siswa yang sama.
func StudentKey(examNumber, studentName, studentClass string) string {
	for _, v := range []string{examNumber, studentName, studentClass} {
		if s := strings.ToLower(strings.TrimSpace(v)); s != "" {
			return s
		}
	}
	return ""
}

// StudentKeyFromIdentityData membaca kunci dari map identity_data mentah
// (JSON dari client), memakai nama field standar lebih dulu lalu fallback ke
// field kustom yang namanya mengandung kata kunci.
//
// Dipakai oleh halaman pengawas, yang sering hanya punya identity_data.
func StudentKeyFromIdentityData(identity map[string]interface{}, examNumber, studentName, studentClass string) string {
	if identity != nil {
		for _, k := range []string{"exam_number", "student_name", "student_class"} {
			if v, ok := identity[k]; ok {
				if s, ok := v.(string); ok {
					if t := strings.ToLower(strings.TrimSpace(s)); t != "" {
						return t
					}
				}
			}
		}
		// Field kustom: cari yang paling mendekati, urutan dinormalkan.
		groups := [][]string{
			{"nomor", "number", "ujian", "exam", "nis", "nip", "no_", "no."},
			{"nama", "name", "siswa", "student", "peserta"},
			{"kelas", "class", "rombel", "kelompok"},
		}
		for _, group := range groups {
			for k, v := range identity {
				s, ok := v.(string)
				if !ok {
					continue
				}
				lowerKey := strings.ToLower(k)
				for _, kw := range group {
					if strings.Contains(lowerKey, kw) {
						if t := strings.ToLower(strings.TrimSpace(s)); t != "" {
							return t
						}
					}
				}
			}
		}
	}
	return StudentKey(examNumber, studentName, studentClass)
}
