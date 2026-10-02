"""Ronde 6 (item 4) — lapis kunci kanonik tidak boleh bergantung urutan dict.

Bug
---
`utils.map_identity_to_standard` menghasilkan hasil BERBEDA
untuk data yang SAMA, hanya karena urutan key berbeda:

    map_identity_to_standard({"Student_Name": "A", "student_name": "B"})
        -> {"student_name": "A"}
    map_identity_to_standard({"student_name": "B", "Student_Name": "A"})
        -> {"student_name": "B"}

Lapis (a) berjalan di urutan sisipan dan menang-pertama pada kunci kanoniknya:

    for key, val in items:
        kl = str(key or "").lower()
        if kl == "student_name" and "student_name" not in result:

Lapis (b) dispatch kata sudah menyortir kandidat-nya, dan docstring modul
menyatakan "Pencocokan tiga lapis, deterministik terhadap urutan dict" —
yang tidak benar untuk (a).

Dampaknya jauh dari kosmetik. `map_identity_to_standard` adalah sumber
`student_name` untuk submit, untuk `build_student_key` (kunci marker
submit), untuk `build_attempt_key` (label perangkat) dan untuk
`get_device_label`. Config yang diurutkan berbeda — dan `IdentityDialog`
menyusun ulang field sesuai urutan respons API, sementara jalur recovery /
halaman hasil membaca ulang `identity_data` yang urutannya bisa berbeda —
maka seluruh kunci itu berubah:

* identitas yang sama menghasilkan DUA kunci siswa berbeda
  -> `answers_<id>.owner` tidak cocok -> recovery sah tidak pernah ditawarkan;
* label perangkat berubah -> gate unduhan PDF tidak match baris approval.

Perbaikan
---------
Lapis (a) menjadi deterministik:

* satu kandidat  -> dipakai, seperti sebelumnya;
* lebih dari satu (ambigu: key berbeda hanya dalam huruf besar/kecil) ->
  diselesaikan dengan urutan yang SELALU sama: ejaan persis kanonik
  lebih dulu, lalu key terpendek, lalu lexicografis. Ambiguitasnya di-LOG
  karena itu konfigurasi yang tidak akan disadari guru.

Semua slot kanonik diperlakukan sama, dan `items` diurutkan sekali di awal
sehingga lapis (b) tidak lagi bisa melihat urutan sisipan sama sekali.
"""

from __future__ import annotations

import unittest

from examvan.utils import map_identity_to_standard

LOGGER = "examvan.utils"


def _reversed_pairs(data: dict) -> list:
    """Semua permutasi urutan key untuk dict kecil (deterministik)."""
    keys = list(data)
    out = []

    def walk(prefix, rest):
        if not rest:
            out.append({k: data[k] for k in prefix})
            return
        for i, k in enumerate(rest):
            walk(prefix + [k], rest[:i] + rest[i + 1:])

    walk([], keys)
    return out


class CanonicalLayerIsOrderIndependentTest(unittest.TestCase):
    def test_key_case_ambiguity_reversed_gives_the_same_result(self):
        forward = map_identity_to_standard(
            {"Student_Name": "A", "student_name": "B"})
        backward = map_identity_to_standard(
            {"student_name": "B", "Student_Name": "A"})
        self.assertEqual(
            forward, backward,
            "data yang sama dengan urutan key dibalik menghasilkan "
            "pemetaan berbeda — student_name yang dipakai submit, kunci "
            "marker submit, dan label perangkat ikut berubah",
        )

    def test_every_permutation_of_the_ambiguous_pair_agrees(self):
        data = {"Student_Name": "A", "student_name": "B"}
        results = {
            repr(sorted(map_identity_to_standard(p).items()))
            for p in _reversed_pairs(data)
        }
        self.assertEqual(len(results), 1, results)

    def test_exactly_canonical_spelling_wins(self):
        """Ejaan kanonik menang atas variasinya, apa pun urutannya."""
        for perm in _reversed_pairs(
            {"Student_Name": "A", "student_name": "B"}
        ):
            out = map_identity_to_standard(perm)
            self.assertEqual(out.get("student_name"), "B", perm)

    def test_all_three_slots_are_order_independent(self):
        data = {
            "Student_Name": "A",
            "student_name": "B",
            "Exam_Number": "X",
            "exam_number": "N01",
            "Student_Class": "Y",
            "student_class": "9A",
        }
        results = [
            map_identity_to_standard(p) for p in _reversed_pairs(data)
        ]
        for out in results[1:]:
            self.assertEqual(out, results[0], out)
        self.assertEqual(results[0], {
            "student_name": "B",
            "exam_number": "N01",
            "student_class": "9A",
        })

    def test_unambiguous_canonical_keys_are_unchanged(self):
        out = map_identity_to_standard({
            "Student_Name": "A", "exam_number": "N01", "studentClass": "9A",
        })
        self.assertEqual(out.get("student_name"), "A")
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_class"), "9A")


class AmbiguityIsResolvedAndLoggedTest(unittest.TestCase):
    def test_pair_without_a_canonical_spelling_is_resolved_deterministically(self):
        data = {"Student_Name": "A", "STUDENT_NAME": "B"}
        forward = map_identity_to_standard(dict(data))
        backward = map_identity_to_standard(dict(reversed(list(data.items()))))
        self.assertEqual(forward, backward)
        # Tidak ada ejaan kanonik -> decide by (len, lexicographic).
        self.assertEqual(forward, {"student_name": "B"})

    def test_three_way_ambiguity_is_resolved_deterministically(self):
        data = {"STUDENT_NAME": "A", "Student_Name": "B", "student_name": "C"}
        results = [
            map_identity_to_standard(p) for p in _reversed_pairs(data)
        ]
        for out in results[1:]:
            self.assertEqual(out, results[0], out)
        self.assertEqual(results[0], {"student_name": "C"})

    def test_ambiguity_is_logged_with_both_keys(self):
        import logging

        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append(record.getMessage())

        logger = logging.getLogger(LOGGER)
        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.WARNING)
        try:
            map_identity_to_standard(
                {"Student_Name": "A", "STUDENT_NAME": "B"})
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        joined = " ".join(records)
        self.assertIn("Student_Name", joined)
        self.assertIn("STUDENT_NAME", joined)
        self.assertIn("student_name", joined.lower())

    def test_unambiguous_input_logs_nothing(self):
        import logging

        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append(record.getMessage())

        logger = logging.getLogger(LOGGER)
        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.WARNING)
        try:
            map_identity_to_standard({"student_name": "A", "kelas": "9A"})
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        self.assertEqual(records, [])


class WholeMapperIsOrderIndependentTest(unittest.TestCase):
    """Dokumen docstring: tiga lapis, deterministik terhadap urutan dict."""

    def test_full_config_every_permutation_agrees(self):
        data = {
            "nama": "Andi",
            "nomor_ujian": "N01",
            "kelas": "9A",
            "alamat": "Jl. Mawar",
            "Student_Name": "B",
        }
        results = [
            map_identity_to_standard(p) for p in _reversed_pairs(data)
        ]
        for out in results[1:]:
            self.assertEqual(out, results[0], out)

    def test_word_dispatch_layer_still_order_independent(self):
        data = {"kelasSiswa": "9A", "namaSiswa": "Budi", "nomorUjian": "N01"}
        results = [
            map_identity_to_standard(p) for p in _reversed_pairs(data)
        ]
        for out in results[1:]:
            self.assertEqual(out, results[0], out)
        self.assertEqual(results[0], {
            "student_name": "Budi",
            "exam_number": "N01",
            "student_class": "9A",
        })

    def test_canonical_layer_takes_priority_over_word_layer(self):
        """Key kanonik eksak tetap menang, meski ada kunci lain untuk slot itu.

        Ini perilaku yang diuji sejak R5 — pastikan perbaikan urutan ini
        tidak merusaknya. Value `student_name` yang eksak HARUS menang atas
        `nama`, apa pun urutan dict.
        """
        for perm in _reversed_pairs(
            {"student_name": "EXACT", "nama": "LAIN", "kelas": "9A"}
        ):
            out = map_identity_to_standard(perm)
            self.assertEqual(out.get("student_name"), "EXACT", perm)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()