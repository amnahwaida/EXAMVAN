"""Kunci identitas siswa harus sama di client dan server.

Izin mengulang diberikan pengawas disimpan dengan kunci yang dihitung
server (`helpers.StudentKey` di Go), sedangkan client menghitungnya dengan
`build_student_key` di Python. Kalau urutan keduanya berbeda, izin yang
diberikan pengawas TIDAK akan berlaku -- dan gejalanya hanya "siswa
ditolak terus" tanpa sebab yang bisa ditunjuk.

Go-nya punya test dengan tabel kasus yang sama persis
(`internal/helpers/student_key_test.go`). Tabel di sini sengaja isinya
mengulang, supaya perubahan di satu sisi yang tidak ikuti sisi lain
tertangkap oleh test di sisi itu.

Salah satu yang paling rawan: client mengirim field identitas BERNAMA
KUSTOM (`nama`, `nomor_ujian`, `kelas`), bukan `student_name` /
`exam_number`. `map_identity_to_standard` yang menerjemahkannya, dan itu
sumber pergeseran urutan yang paling mudah luput.
"""

from __future__ import annotations

import unittest

from examvan.utils import build_student_key, map_identity_to_standard


class StudentKeyParityTest(unittest.TestCase):
    """Tabel yang harus identik dengan StudentKeyPrefersExamNumber di Go."""

    def test_precedence_matches_the_server(self):
        cases = [
            # (nama kasus, exam_number, nama, kelas, kunci yang diharapkan)
            ("nomor menang", "N01", "Andi", "9A", "n01"),
            ("tanpa nomor -> nama", "", "Andi", "9A", "andi"),
            ("tanpa nomor dan nama -> kelas", "", "", "9A", "9a"),
            ("semua kosong", "", "", "", ""),
            ("spasi dibersihkan", "  N01  ", "Andi", "9A", "n01"),
            ("huruf besar-kecil sama", "n01", "Andi", "9A", "n01"),
            ("nomor whitespace only -> nama", "   ", "Andi", "9A", "andi"),
        ]
        for label, number, name, klass, want in cases:
            with self.subTest(case=label):
                self.assertEqual(build_student_key({
                    "exam_number": number,
                    "student_name": name,
                    "student_class": klass,
                }), want)

    def test_the_same_student_gets_the_same_key(self):
        # Pengawas melihat "N01" di tabel; siswa mengetik "n01".
        from_table = build_student_key({
            "exam_number": "N01", "student_name": "Andi"})
        from_typed = build_student_key({
            "exam_number": "n01", "student_name": "andi"})
        self.assertEqual(from_table, from_typed)

    def test_different_students_get_different_keys(self):
        a = build_student_key({"exam_number": "N01", "student_name": "Andi"})
        b = build_student_key({"exam_number": "N02", "student_name": "Andi"})
        self.assertNotEqual(a, b)

    def test_custom_field_names_produce_the_expected_key(self):
        # Yang dikirim client sebenarnya: nama, nomor_ujian, kelas.
        # Salah baca di sini = izin pengawas tidak berlaku.
        key = build_student_key({
            "nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"})
        self.assertEqual(key, "n01")

    def test_custom_names_only_falls_back_to_name(self):
        key = build_student_key({"nama": "Andi", "kelas": "9A"})
        self.assertEqual(key, "andi")

    def test_whitespace_only_identity_produces_no_key(self):
        # Kunci kosong berarti "tidak bisa diidentifikasi". Server tidak
        # boleh memblokir berdasarkan itu — siswa yang terkunci tanpa ada
        # yang bisa melepas.
        self.assertEqual(build_student_key({"exam_number": "   "}), "")

    def test_standard_mapping_agrees_with_the_key(self):
        # Titik kegagalan yang paling mungkin: map_identity_to_standard
        # berhenti di tengah jalan, lalu kunci berubah diam-diam.
        custom = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
        std = map_identity_to_standard(custom)
        self.assertEqual(std.get("student_name"), "Andi")
        self.assertEqual(std.get("exam_number"), "N01")
        self.assertEqual(std.get("student_class"), "9A")
        self.assertEqual(
            build_student_key(custom),
            build_student_key({
                "student_name": "Andi", "exam_number": "N01",
                "student_class": "9A",
            }),
            "field kustom dan field standar harus menghasilkan kunci yang sama",
        )

    def test_missing_identity_data_is_tolerated(self):
        self.assertEqual(build_student_key(None), "")
        self.assertEqual(build_student_key({}), "")


if __name__ == "__main__":
    unittest.main()
