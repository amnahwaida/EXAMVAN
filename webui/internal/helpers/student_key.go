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

import (
	"sort"
	"strings"
	"unicode"
)

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

// ---------------------------------------------------------------------------
// Pemetaan identity_data mentah -> slot standar.
//
// WAJIB identik dengan examvan/utils.py map_identity_to_standard (R5 H3/H5):
// kata penentu slot boleh berada di kata mana pun di dalam kunci (batas
// kata, bukan substring), tier-1 mengalahkan tier-2, dan kunci yang memuat
// kata tanggal/jam/waktu tidak pernah mengklaim slot.
//
// Dua cacat yang ditutup di sini:
//
//  1. NONDETERMINISTIK — versi lama meng-iterasi map Go (`for k, v := range`),
//     dan urutan iterasi map Go sengaja diacak. Config dengan beberapa field
//     yang bisa mengisi slot yang sama mengembalikan kunci BERBEDA antar
//     proses: izin pengawas berlaku di satu run dan tidak di run berikutnya.
//     Sekarang kandidat diurutkan (tier, posisi kata, nama kunci).
//  2. SUBSTRING — `strings.Contains(lowerKey, kw)` dengan kata bersama
//     ("no_", "no.", "exam") membuat kunci tak terduga ikut mengklaim slot,
//     dan tidak bisa mengurai `id_kelas`/`nim` seperti client.
// ---------------------------------------------------------------------------

// Slot standar, dalam urutan prioritas.
var identitySlots = []string{"exam_number", "student_name", "student_class"}

// Tier-1 = spesifik (menyebut slot itu sendiri), tier-2 = umum (menyebut
// entitas atau turunan saja). Tier-1 menang di mana pun posisinya:
// `studentClass` adalah kelas walau `student` ada, dan `nomor_ujian`
// mengalahkan `kode_ujian` untuk slot number.
var (
	numberTier1 = map[string]bool{"nomor": true, "number": true, "nis": true, "nisn": true, "nip": true, "nim": true}
	nameTier1   = map[string]bool{"nama": true, "name": true}
	classTier1  = map[string]bool{"kelas": true, "class": true, "rombel": true, "kelompok": true}
	numberTier2 = map[string]bool{"ujian": true, "exam": true}
	nameTier2   = map[string]bool{"siswa": true, "student": true, "peserta": true}

	// `no` TIDAK ada di numberTier1 tanpa syarat. Dia hanya berarti "nomor"
	// kalau berdiri sendiri atau kalau kata berikutnya benar-benar menyebut
	// identitas (lihat noFollowers). Alasannya, tier-1 bisa berada di
	// posisi 0 dan perebutnya diputuskan lexicografis: `'_' (0x5F) < 'm'`,
	// jadi `no_hp` SELALU mengalahkan `nomor_ujian`.
	//
	// Dulu `no` ada di numberTier1 tanpa syarat dan di SEMUA posisi kata,
	// jadi client (yang sudah punya gate sejak ronde 7) dan server
	// menghitung kunci berbeda untuk config yang sama:
	//
	//	Python: {"nama":"Andi","no_hp":"0812","kelas":"9A"} -> "andi"
	//	Go    : map yang sama                                -> "0812"
	//
	// Nomor telepon siswa menjadi nomor ujiannya — kolom yang dibaca
	// `repeat_grant.go`, dicetak di halaman selamat, dan ditampilkan di
	// tabel hasil publik — dan `StudentKey` ikut menjadi "0812". Gerbang
	// repeat yang diberikan pengawas lalu tidak berlaku, dan tidak ada yang
	// melaporkannya karena dari sisi client kelihatannya hanya "ditolak
	// terus".
	genericTier1 = map[string]bool{"no": true}

	// Kata yang membuat `no` tetap berarti "nomor". Kalau kata berikutnya
	// TIDAK ada di sini, `no` bukan kata nomor: `no_hp`, `no_telp`,
	// `no_telpon`, `no_wa`, `no_hp_siswa`, `no_hp_ortu` semuanya berarti
	// nomor kontak. Daftar ini berisi kata yang benar-benar menyebut
	// identitas — `no_induk`, `no_nis`, `no_absen`, `no_ujian`,
	// `no_peserta`, `no_siswa`, `no_kelas` — sehingga tidak ada nomor ujian
	// yang ikut kehilangan slotnya.
	noFollowers = map[string]bool{
		"absen": true, "exam": true, "induk": true, "kelas": true,
		"kelompok": true, "murid": true, "name": true, "nama": true,
		"nim": true, "nip": true, "nis": true, "nisn": true, "nomor": true,
		"number": true, "peserta": true, "rombel": true, "siswa": true,
		"student": true, "ujian": true,
	}

	// Kunci yang memuat salah satunya TIDAK PERNAH mengklaim slot meski
	// kata slot-nya ikut ada: `jam_ujian` bukan nomor ujian, `exam_date`
	// bukan nomor ujian. Ini yang menjaga `tanggal_lahir` tidak pernah
	// tercatat sebagai nomor ujian.
	nonIdentityWords = map[string]bool{
		"tanggal": true, "date": true, "lahir": true, "birth": true, "birthday": true,
		"waktu": true, "jam": true, "time": true,
		"mulai": true, "start": true, "selesai": true, "end": true,
	}
)

var (
	tier1Slot = map[string]string{}
	tier2Slot = map[string]string{}
)

func init() {
	for w := range numberTier1 {
		tier1Slot[w] = "exam_number"
	}
	for w := range nameTier1 {
		tier1Slot[w] = "student_name"
	}
	for w := range classTier1 {
		tier1Slot[w] = "student_class"
	}
	for w := range numberTier2 {
		tier2Slot[w] = "exam_number"
	}
	for w := range nameTier2 {
		tier2Slot[w] = "student_name"
	}
}

// identityKeyWords memecah kunci menjadi kata lowercase. Batas camelCase
// (`namaSiswa` -> `nama_Siswa`) dipecah lebih dulu — Go RE2 tidak mendukung
// lookbehind, jadi batasnya dicari manual.
func identityKeyWords(key string) []string {
	var b strings.Builder
	rs := []rune(key)
	for i, r := range rs {
		if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])) && unicode.IsUpper(r) {
			b.WriteRune('_')
		}
		b.WriteRune(r)
	}
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(b.String()), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		words = append(words, w)
	}
	return words
}

// identityDispatch mengembalikan (slot, tier, posisi_kata, generic) penentu
// slot untuk satu kunci. ok=false berarti kunci itu bukan identitas — kata
// tanggal/jam, atau tidak ada kata slot yang sah.
//
// `generic` menandai kata tier-1 yang cuma singkatan dan menempel pada kata
// apa saja (`no`). Ia NECADARI pemutus antara dua kunci yang sama-sama
// tier-1 di posisi yang sama: `nomor_ujian` (kata yang menyebut slot)
// harus mengalahkan `no_ujian` (singkatan umum), sedangkan urutan
// lexicografis justru mengarah ke sebaliknya karena `'_' (0x5F) < 'm'`. Tanpa
// generic itu, `{"no_ujian":"N01","nomor_ujian":"N02"}` jadi "n01" di Go
// dan "n02" di client.
//
// Gate `no` (lihat noFollowers) dan generic ini WAJIB identik dengan
// `_slot_candidates`/`_offer` di examvan/utils.py; tabel kasusnya dibaca dari
// file test Go ini oleh `desktop/tests/test_r8_identity_two_slots.py`, jadi
// kedua sisi tidak bisa berbeda tanpa salah satu test merah.
func identityDispatch(key string) (slot string, tier, pos int, generic bool, ok bool) {
	words := identityKeyWords(key)
	tier1At, tier2At := -1, -1
	tier1SlotAt, tier2SlotAt := "", ""
	tier1Generic := false
	for i, w := range words {
		if nonIdentityWords[w] {
			return "", 0, 0, false, false
		}
		switch {
		case w == "no":
			// `no` hanya sah sebagai kata nomor di awal kunci, dan hanya
			// kalau kata berikutnya (kalau ada) menyebut identitas.
			// Slot-nya ditulis langsung, bukan lewat `tier1Slot`: `no` TIDAK
			// ada di map tier-1, supaya ia tidak pernah diklaim tanpa gate.
			if i == 0 && (len(words) == 1 || noFollowers[words[1]]) {
				if tier1At < 0 {
					tier1At, tier1SlotAt, tier1Generic = i, "exam_number", true
				}
			}
		case tier1Slot[w] != "":
			if tier1At < 0 {
				tier1At, tier1SlotAt, tier1Generic = i, tier1Slot[w], false
			} else if tier2At < 0 {
				tier2At = i
			}
		case tier2Slot[w] != "":
			if tier2At < 0 {
				tier2At, tier2SlotAt = i, tier2Slot[w]
			}
		}
	}
	if tier1At >= 0 {
		return tier1SlotAt, 0, tier1At, tier1Generic, true
	}
	if tier2At >= 0 {
		return tier2SlotAt, 1, tier2At, false, true
	}
	return "", 0, 0, false, false
}

// identityKeyValue adalah kandidat slot: nilai yang sudah dinormalkan.
type identityKeyValue struct {
	tier    int
	pos     int
	generic bool
	key     string
	val     string
}

// StudentKeyFromIdentityData membaca kunci dari map identity_data mentah
// (JSON dari client): kunci kanonik lebih dulu, lalu field kustom memakai
// pemetaan whole-word yang sama dengan client.
//
// Dipakai oleh halaman pengawas, yang sering hanya punya identity_data.
func StudentKeyFromIdentityData(identity map[string]interface{}, examNumber, studentName, studentClass string) string {
	if identity != nil {
		for _, k := range identitySlots {
			if v, ok := identity[k].(string); ok {
				if t := strings.ToLower(strings.TrimSpace(v)); t != "" {
					return t
				}
			}
		}
		// Field kustom. Kunci diurutkan supaya hasilnya tidak bergantung
		// urutan iterasi map Go (lihat catatan di atas file ini).
		keys := make([]string, 0, len(identity))
		for k := range identity {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		assigned := map[string]string{}
		for _, slot := range identitySlots {
			var cands []identityKeyValue
			for _, k := range keys {
				s, tier, pos, generic, ok := identityDispatch(k)
				if !ok || s != slot {
					continue
				}
				v, isStr := identity[k].(string)
				if !isStr {
					continue
				}
				t := strings.ToLower(strings.TrimSpace(v))
				if t == "" {
					continue
				}
				cands = append(cands, identityKeyValue{tier: tier, pos: pos, generic: generic, key: k, val: t})
			}
			// Tier-1 lebih dulu, lalu posisi kata paling depan, lalu kata
			// yang BUKAN singkatan generic, lalu nama kunci — urutan tetap,
			// sama seperti utils.py.
			sort.Slice(cands, func(i, j int) bool {
				if cands[i].tier != cands[j].tier {
					return cands[i].tier < cands[j].tier
				}
				if cands[i].pos != cands[j].pos {
					return cands[i].pos < cands[j].pos
				}
				if cands[i].generic != cands[j].generic {
					return !cands[i].generic
				}
				return cands[i].key < cands[j].key
			})
			for _, c := range cands {
				// Nilai yang sudah diklaim KUNCI LAIN tidak dipakai ulang.
				// Satu kunci boleh memakai nilainya untuk dua slot — itulah
				// aturan utils.py yang membuat `nama_kelas` mengisi kolom
				// kelas juga. Di sini tidak perlu apa pun untuk itu: fungsi
				// ini mengembalikan SATU kunci dan langsung berhenti di slot
				// pertama yang terisi, jadi fill-ganda tidak pernah
				// mengubah hasilnya.
				if owner, taken := assigned[c.val]; taken && owner != c.key {
					continue
				}
				assigned[c.val] = c.key
				return c.val
			}
		}
	}
	return StudentKey(examNumber, studentName, studentClass)
}
