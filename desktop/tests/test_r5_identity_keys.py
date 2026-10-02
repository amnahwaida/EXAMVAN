"""Ronde 5 — kunci identitas: camelCase, kata di dalam kunci, dan slot kanonik.

Tiga temuan review ronde 5 yang saling terkait di
`examvan/utils.py map_identity_to_standard`:

H3 — kunci camelCase kehilangan KETIGA slot
    `_first_word` melakukan `str(key).lower()` SEBELUM memecah kunci, jadi
    `namaSiswa` menjadi satu token `namasiswa`. Akibatnya `namaSiswa`,
    `nomorUjian`, `kelasSiswa` tidak cocok dengan kata mana pun, ketiga
    kolom standar kosong, dan `build_student_key` jatuh ke token ujian —
    kunci milik SELURUH KELAS. Gejalanya di client: "marker sudah
    dikumpulkan" milik siswa pertama memblokir siswa berikutnya di kelas
    yang sama, dan rekap guru tidak punya nama/nomor sama sekali.
    Ini regresi murni (pencocokan substring yang lama menerima kunci ini),
    jadi tidak butuh data legacy.

    Perbaikan: batas camelCase dipecah sebelum lowercasing, lalu
    dipecah pada non-alfanumerik.

H5 — dispatch kata PERTAMA membuang kunci secara diam-diam
    Komentar di kode mengklaim kunci yang tidak cocok dilewati "supaya
    server menolak dengan pesan yang jelas". Itu tidak benar: server
    membaca `body.IdentityData[field.Key]` dengan key yang tersimpan
    mentah, dan fallback kanonik hanya menyala untuk tiga ejaan kanonik.
    `id_kelas` -> `student_class` kosong, tanpa 400. Perbaikan: pencocokan
    SELURUH kata di dalam kunci (batas kata, bukan substring, bukan hanya
    kata pertama) dengan urutan yang tetap, sementara kata tanggal/jam/
    waktu tetap tidak pernah mengklaim slot.

M1 — kolom DB harus otoritatif untuk setiap konfigurasi
    `map_identity_to_standard` sengaja tidak menebak, jadi konfigurasi
    yang tidak lazim tetap submit sukses dengan kolom
    student_name/exam_number/student_class KOSONG padahal nilainya
    sudah diketik. `identity_data_with_canonical` menyisipkan ketiga kunci
    kanonik ke payload sehingga sisi server (yang sudah membaca
    `identity_data` lebih dulu) selalu punya kolom terisi.
"""

from __future__ import annotations

import unittest

from examvan.utils import (
    build_student_key,
    map_identity_to_standard,
)

# Token yang dipakai di bawah: student_key yang jatuh ke token berarti
# seluruh kelas berbagi satu kunci (bug H3).
TOKEN = "ABCD1234"


# ---------------------------------------------------------------------------
# H3 — kunci camelCase
# ---------------------------------------------------------------------------


class CamelCaseIdentityKeyTest(unittest.TestCase):
    """camelCase harus memenuhi ketiga slot standar, bukan nol."""

    def test_camel_case_keys_fill_all_three_slots(self):
        out = map_identity_to_standard({
            "kelasSiswa": "9A",
            "namaSiswa": "Budi",
            "nomorUjian": "N01",
        })
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")

    def test_english_camel_case_keys_fill_all_three_slots(self):
        out = map_identity_to_standard({
            "studentClass": "9A",
            "studentName": "Budi",
            "examNumber": "N01",
        })
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")

    def test_camel_case_keys_do_not_collapse_the_student_key(self):
        # Gejala yang dipakai report: student_key = token ujian.
        identity = {
            "kelasSiswa": "9A",
            "namaSiswa": "Budi",
            "nomorUjian": "N01",
        }
        key = build_student_key(identity, TOKEN)
        self.assertEqual(key, "n01")
        self.assertNotEqual(
            key, TOKEN.lower(),
            "student_key jatuh ke token ujian: satu kunci untuk seluruh kelas",
        )

    def test_english_camel_case_keys_do_not_collapse_the_student_key(self):
        identity = {
            "studentClass": "9A",
            "studentName": "Budi",
            "examNumber": "N01",
        }
        self.assertEqual(build_student_key(identity, TOKEN), "n01")

    def test_two_camel_case_students_get_different_keys(self):
        # Kalau kunci tetap jatuh ke token, kedua siswa ini blocked bersama.
        andi = build_student_key(
            {"namaSiswa": "Andi", "nomorUjian": "N01", "kelasSiswa": "9A"}, TOKEN)
        budi = build_student_key(
            {"namaSiswa": "Budi", "nomorUjian": "N02", "kelasSiswa": "9A"}, TOKEN)
        self.assertNotEqual(andi, budi)
        self.assertNotIn(TOKEN.lower(), (andi, budi))

    def test_camel_case_result_is_identical_to_snake_case(self):
        camel = {"namaSiswa": "Budi", "nomorUjian": "N01", "kelasSiswa": "9A"}
        snake = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
        self.assertEqual(
            map_identity_to_standard(camel), map_identity_to_standard(snake))
        self.assertEqual(
            build_student_key(camel, TOKEN), build_student_key(snake, TOKEN))


# ---------------------------------------------------------------------------
# H3 — kasus lama yang HARUS tetap benar (regresi nol)
# ---------------------------------------------------------------------------


class ExistingGoodCasesStillMapTest(unittest.TestCase):
    """Perbaikan camelCase tidak boleh merusak pemetaan yang sudah benar."""

    def test_canonical_keys_map_directly(self):
        out = map_identity_to_standard(
            {"student_name": "Budi", "exam_number": "N01", "student_class": "9A"})
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")

    def test_canonical_keys_are_case_insensitive(self):
        out = map_identity_to_standard(
            {"STUDENT_NAME": "Budi", "Exam_Number": "N01", "KELAS_SISWA": "9A"})
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")

    def test_snake_case_keys_still_map(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"})
        self.assertEqual(
            out,
            {"student_name": "Budi", "exam_number": "N01",
             "student_class": "9A"},
        )

    def test_nomor_peserta_and_kelas_siswa_still_map(self):
        out = map_identity_to_standard(
            {"nama_peserta": "Budi", "nomor_peserta": "N01", "kelas_siswa": "9A"})
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")

    def test_unknown_first_word_is_still_skipped(self):
        # Kata tak dikenal tidak boleh menebak slot mana pun.
        for key in ("gelombang", "myexam", "sekolah", "field_0", "field_1"):
            with self.subTest(key=key):
                out = map_identity_to_standard({key: "X1"})
                self.assertNotIn("X1", out.values())

    def test_date_and_time_words_still_claim_nothing(self):
        for key in ("tanggal_lahir", "exam_date", "waktu_mulai", "jam_ujian",
                    "kode_kelas"):
            with self.subTest(key=key):
                out = map_identity_to_standard(
                    {"nama": "Andi", key: "X1", "kelas": "9A"})
                self.assertNotIn("X1", out.values())

    def test_empty_values_are_still_dropped(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nomor_ujian": "", "kelas": "9A"})
        self.assertNotIn("exam_number", out)
        self.assertEqual(out.get("student_name"), "Budi")


# ---------------------------------------------------------------------------
# H5 — kata di dalam kunci, bukan hanya kata pertama
# ---------------------------------------------------------------------------


class WholeWordDispatchTest(unittest.TestCase):
    """Kunci yang tidak dikenal TIDAK boleh hilang begitu saja.

    Komentar lama di `map_identity_to_standard` mengklaim kunci yang tidak
    cocok dilewati "supaya server menolak dengan pesan yang jelas". Server
    tidak menolak: ia membaca `body.IdentityData[field.Key]` dengan
    key yang tersimpan mentah, dan fallback kanonik hanya menyala untuk
    tiga ejaan kanonik. Jadi kunci yang tidak cocok mendarat diam-diam di
    kolom DB kosong:

        {"id_kelas": "9A"} -> student_class kosong, HTTP 200.

    Perbaikannya: kata yang menentukan slot boleh berada di KAPAN saja di
    dalam kunci (batas kata, bukan substring, bukan hanya kata pertama),
    dengan urutan tetap supaya hasilnya tidak bergantung urutan dict.
    """

    POSITIVES = [
        # (kunci, slot)
        ("id_kelas", "student_class"),
        ("kode_ujian", "exam_number"),
        ("ujian", "exam_number"),
        ("nim", "exam_number"),
        ("nama_lengkap", "student_name"),
        ("no_peserta", "exam_number"),
        ("rombel", "student_class"),
        # kata pertama yang sudah bekerja sebelumnya TIDAK boleh bergeser
        ("nama_peserta", "student_name"),
        ("nomor_ujian", "exam_number"),
        ("nomor_peserta", "exam_number"),
        ("kelas_siswa", "student_class"),
        ("nis", "exam_number"),
        ("nisn", "exam_number"),
        ("kelompok", "student_class"),
        ("no_ujian", "exam_number"),
        ("namaSiswa", "student_name"),
        ("kelasSiswa", "student_class"),
        ("studentName", "student_name"),
        ("studentClass", "student_class"),
        ("examNumber", "exam_number"),
    ]

    NEGATIVES = [
        # Kunci yang TIDAK boleh mengklaim slot standar mana pun.
        "tanggal_lahir",
        "agama",
        "alamat",
        "nilai",
        "email",
        "angkatan",
        "jurusan",
        "kode_pos",
        "gelombang",
        "myexam",
        "sekolah",
        "exam_date",
        "waktu_mulai",
        "jam_ujian",
    ]

    def test_positive_table_maps_to_its_slot(self):
        for key, slot in self.POSITIVES:
            with self.subTest(key=key):
                out = map_identity_to_standard({key: "V"})
                self.assertEqual(out.get(slot), "V")
                # Tidak boleh bocor ke slot lain.
                self.assertEqual(
                    sorted(out), sorted([slot]),
                    f"{key} mengisi slot lain: {out}",
                )

    def test_negative_table_claims_no_slot(self):
        for key in self.NEGATIVES:
            with self.subTest(key=key):
                out = map_identity_to_standard({key: "V"})
                self.assertEqual(out, {}, f"{key} seharusnya tidak klaim slot")

    def test_negatives_never_displace_a_real_slot_in_a_full_config(self):
        # Config nyata guru: tanggal lahir + nama + nomor + kelas. Nilai
        # yang tidak dikenal tidak boleh masuk ke kolom mana pun.
        out = map_identity_to_standard({
            "nama": "Andi",
            "nomor_ujian": "N01",
            "kelas": "9A",
            "tanggal_lahir": "2010-05-05",
            "alamat": "Jl. Mawar 1",
            "agama": "Islam",
            "nilai": "95",
        })
        self.assertEqual(out, {
            "student_name": "Andi", "exam_number": "N01",
            "student_class": "9A",
        })

    def test_id_kelas_no_longer_lands_in_an_empty_column(self):
        out = map_identity_to_standard({"id_kelas": "9A", "nama": "Andi"})
        self.assertEqual(out.get("student_class"), "9A")

    def test_kode_ujian_no_longer_lands_in_an_empty_column(self):
        out = map_identity_to_standard({"kode_ujian": "U-7", "nama": "Andi"})
        self.assertEqual(out.get("exam_number"), "U-7")

    def test_kode_ujian_never_outranks_nomor_ujian(self):
        # `nomor_ujian` menyebut slot secara langsung; `kode_ujian` hanya
        # menyebut kata turunannya. Keduanya slot number, yang paling
        # spesifik harus menang supaya kunci tidak bergeser antar siswa.
        out = map_identity_to_standard({
            "kode_ujian": "U-7",
            "nomor_ujian": "N01",
            "nama": "Andi",
        })
        self.assertEqual(out.get("exam_number"), "N01")

    def test_dispatch_is_dict_order_independent(self):
        identity = {
            "id_kelas": "9A",
            "kode_ujian": "U-7",
            "nama_lengkap": "Andi",
            "tanggal_lahir": "2010-05-05",
            "no_peserta": "N01",
        }
        forward = map_identity_to_standard(identity)
        backward = map_identity_to_standard(dict(reversed(list(identity.items()))))
        self.assertEqual(forward, backward)
        self.assertEqual(forward, {
            "student_name": "Andi", "exam_number": "N01",
            "student_class": "9A",
        })

    def test_a_value_is_never_reused_for_two_slots(self):
        out = map_identity_to_standard({"nama": "Budi", "nim": "Budi", "kelas": "9A"})
        self.assertEqual(out.get("exam_number"), "Budi")
        self.assertIsNone(out.get("student_name"))


if __name__ == "__main__":
    unittest.main()