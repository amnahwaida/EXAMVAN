"""Ronde 5 (M1) — payload identitas harus selalu memuat kunci kanonik.

`map_identity_to_standard` sengaja tidak menebak kunci yang tidak dikenal,
supaya tidak ada tanggal lahir yang tercatat sebagai nomor ujian. Konsekuensinya
kolom DB bisa kosong padahal siswa sudah mengetik nilainya: konfigurasi dengan
kunci `alamat`/`kode_pos` saja submit dengan HTTP 200 dan
student_name/exam_number/student_class kosong, sehingga kunci siswa jatuh ke
token ujian (satu kunci untuk seluruh kelas).

Sisi server sudah membaca `identity_data` lebih dulu sebelum kolom top-level
(api/exams.go), jadi menyisipkan ketiga kunci kanonik ke payload membuat kolom
DB otoritatif untuk SETIAP konfigurasi tanpa perubahan server sama sekali.

CATATAN call site (tidak diubah di ronde ini — satu baris per call site):

    # desktop/examvan/ui/identity_dialog.py — IdentityDialog.get_identity_data()
    return identity_data_with_canonical(data)

    # atau di call site submit, sebelum api.submit_exam(...):
    identity_data = identity_data_with_canonical(identity_data)
"""

from __future__ import annotations

import unittest

from examvan.utils import (
    build_student_key,
    identity_data_with_canonical,
)

TOKEN = "ABCD1234"


# ---------------------------------------------------------------------------
# M1 — kunci kanonik ikut dikirim supaya kolom DB otoritatif
# ---------------------------------------------------------------------------


class CanonicalPayloadTest(unittest.TestCase):
    """Payload identitas + kunci kanonik, tanpa kehilangan key asli."""

    def test_canonical_keys_are_added_for_a_degenerate_config(self):
        data = {"alamat": "Jl. Mawar 1", "kode_pos": "12345"}
        out = identity_data_with_canonical(data)
        self.assertEqual(out["alamat"], "Jl. Mawar 1")
        self.assertEqual(out["kode_pos"], "12345")
        # Tidak ada kata yang cocok -> tidak ada yang boleh dikarang.
        self.assertNotIn("student_name", out)
        self.assertNotIn("exam_number", out)
        self.assertNotIn("student_class", out)

    def test_canonical_keys_are_filled_from_the_mapper(self):
        out = identity_data_with_canonical({"nama": "Andi", "kelas": "9A"})
        self.assertEqual(out["student_name"], "Andi")
        self.assertEqual(out["student_class"], "9A")
        self.assertNotIn("exam_number", out)

    def test_camel_case_config_gets_three_canonical_columns(self):
        out = identity_data_with_canonical(
            {"namaSiswa": "Andi", "nomorUjian": "N01", "kelasSiswa": "9A"})
        self.assertEqual(out["student_name"], "Andi")
        self.assertEqual(out["exam_number"], "N01")
        self.assertEqual(out["student_class"], "9A")

    def test_original_keys_are_never_dropped(self):
        # Server memvalidasi terhadap key yang TERSIMPAN, jadi payload asli
        # harus utuh; kunci kanonik hanya tambahan.
        original = {"nama_peserta": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
        out = identity_data_with_canonical(original)
        for key, val in original.items():
            self.assertEqual(out[key], val)

    def test_input_dict_is_not_mutated(self):
        original = {"nama": "Andi"}
        identity_data_with_canonical(original)
        self.assertEqual(original, {"nama": "Andi"})

    def test_existing_canonical_value_is_never_overwritten(self):
        out = identity_data_with_canonical(
            {"student_name": "Asli", "nama": "Lain"})
        self.assertEqual(out["student_name"], "Asli")

    def test_empty_identity_is_tolerated(self):
        self.assertEqual(identity_data_with_canonical(None), {})
        self.assertEqual(identity_data_with_canonical({}), {})

    def test_blank_values_never_reach_the_payload(self):
        # Nilai whitespace-only tidak boleh jadi kolom isian: server akan
        # trimming dan menganggapnya kosong (dan kolom DB jadi whitespace).
        out = identity_data_with_canonical({"nama": "   ", "kelas": "9A"})
        self.assertNotIn("student_name", out)
        self.assertEqual(out["student_class"], "9A")

    def test_injected_keys_do_not_change_the_student_key(self):
        identity = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
        enriched = identity_data_with_canonical(identity)
        self.assertEqual(
            build_student_key(identity, TOKEN),
            build_student_key(enriched, TOKEN),
        )


if __name__ == "__main__":
    unittest.main()
