"""Audit file yang bisa ditulis siswa — pola exception dari ronde kedua.

Konteks: review ronde kedua menemukan `admin_password.txt` yang korup
melempar ValueError di jalur dialog supervisor. Audit ini membedah file
LAIN yang juga berada di dalam jangkauan tulis siswa pada akun lab yang
sama (`config.json`, `answers_*.dat`, `answers_*.json`) dan menemukan
pola yang sama — semuanya direproduksi dulu sebelum ditulis testnya:

A. `config.json` berisi JSON valid tapi bukan object (`[]`, `123`,
   `null`, `"str"`) → `{**_defaults, **json.load(f)}` melempar TypeError
   di `_load()`. Karena `_cache` hanya terisi SETELAH baris itu sukses,
   cache tidak pernah terisi dan crash BERULANG di setiap launch —
   sekelas dengan "app tidak bisa dibuka sampai folder dihapus manual",
   di PC lab tanpa siapa pun yang tahu harus menghapus apa.

B. Migrasi legacy `answers_<id>.json` menerima array/nilai tanpa cek
   dict → dikembalikan sebagai jawaban sah, mengalir ke payload submit.
   Format `.dat` sudah punya `_looks_like_answers()`; jalur migrasinya
   yang terlewat.

C. `submitted_<id>` dengan value campur tipe ({k: 123}) →
   `sorted()` di `submitted_labels()` melempar TypeError → dialog
   re-entry "Kirim Lagi" mati — persis saat siswa membutuhkannya.

D. `identity_data` berupa list → `["x"] or {}` lolos dan mengalir ke
   dialog identitas (AttributeError saat `.get("nama")`).

Kontrak yang ditegakkan file ini: file yang bisa ditulis siswa adalah
INPUT MUSUH — bentuk tidak sah = nilai default + file dibiarkan (akan
ditimpa saat save berikutnya), bukan exception.
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = Path(self._tmp.name)
        for attr, value in (
            ("_CONFIG_DIR", self.dir),
            ("_CONFIG_FILE", self.dir / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    def _write_config(self, text: str) -> None:
        (self.dir / "config.json").write_text(text, encoding="utf-8")


# ---------------------------------------------------------------------------
# A. config.json bukan object — tidak boleh crash, apalagi berulang
# ---------------------------------------------------------------------------


class NonObjectConfigTestCase(_ConfigSandbox):
    def test_array_config_falls_back_to_defaults(self):
        self._write_config("[]")
        # Default exam_token adalah string kosong (bukan None).
        self.assertEqual(config._load().get("exam_token"), "")

    def test_number_config_falls_back_to_defaults(self):
        self._write_config("123")
        self.assertEqual(config.get("server_url", ""), "")

    def test_null_config_falls_back_to_defaults(self):
        self._write_config("null")
        self.assertFalse(config.get_all().get("remember_url", False) is None)

    def test_string_config_falls_back_to_defaults(self):
        self._write_config('"siswa"')
        self.assertEqual(config.get_all()["exam_token"], "")

    def test_corrupt_config_still_recovers_after_repair(self):
        # Dua peluncuran beruntun: keduanya harus hidup, dan SETELAH file
        # diperbaiki (ditulis ulang oleh app), config benar-benar terpakai.
        self._write_config("[]")
        self.assertEqual(config.get_all()["exam_token"], "")
        config._cache = None
        config.set("exam_token", "ABCD1234")
        config._cache = None
        self.assertEqual(config.get("exam_token"), "ABCD1234")


# ---------------------------------------------------------------------------
# B. migrasi legacy answers_<id>.json tanpa validasi
# ---------------------------------------------------------------------------


class LegacyAnswersMigrationTestCase(_ConfigSandbox):
    def test_legacy_array_is_not_returned_as_answers(self):
        (self.dir / "answers_7.json").write_text('["jawaban-palsu"]')
        self.assertIsNone(
            config.load_answers(7),
            "array dari file legacy dikembalikan sebagai jawaban sah — "
            "mengalir ke payload submit",
        )

    def test_legacy_number_is_not_returned_as_answers(self):
        (self.dir / "answers_7.json").write_text("42")
        self.assertIsNone(config.load_answers(7))

    def test_legacy_object_is_still_migrated(self):
        # Jangan buang air out with the bathwater: migrasi yang sah tetap
        # jalan — file .json dihapus dan isinya pindah ke .dat.
        (self.dir / "answers_7.json").write_text('{"1": "A"}')
        self.assertEqual(config.load_answers(7), {"1": "A"})
        self.assertFalse((self.dir / "answers_7.json").exists())
        self.assertTrue((self.dir / "answers_7.dat").exists())

    def test_new_format_rejects_non_dict_too(self):
        # Kontrak yang sama untuk format .dat saat ini.
        (self.dir / "answers_7.dat").write_text("dGhlbg==", encoding="ascii")
        # decode gagal / hasilnya bukan dict -> None. Yang penting TIDAK
        # melempar dan TIDAK mengembalikan non-dict.
        result = config.load_answers(7)
        self.assertTrue(result is None or isinstance(result, dict))


# ---------------------------------------------------------------------------
# C. submitted map dengan isi tidak sah
# ---------------------------------------------------------------------------


class SubmittedMapRobustnessTestCase(_ConfigSandbox):
    def test_mixed_type_values_do_not_break_labels(self):
        self._write_config(json.dumps({"submitted_9": {"k1": 123, "k2": "budi"}}))
        # Entri yang labelnya korup TETAP dihitung (marker sticky tidak
        # boleh hilang karena tampering — server idempoten, tapi dialog
        # re-entry harus tetap menyebut percobaan itu), labelnya jatuh ke
        # fallback agar sorted() tidak pernah mencampur str dan int.
        self.assertEqual(
            config.submitted_labels(9),
            ["budi", "identitas tidak diketahui"],
        )

    def test_non_dict_submitted_map_is_ignored(self):
        self._write_config(json.dumps({"submitted_9": ["x"]}))
        self.assertEqual(config.submitted_labels(9), [])
        self.assertFalse(config.is_submitted(9, "key"))

    def test_non_string_labels_are_coerced_not_crash(self):
        self._write_config(json.dumps({"submitted_9": {"k1": None, "k2": 5}}))
        # sorted() tidak pernah melempar; marker tetap dikenali lewat
        # kunci yang di-hash (lihat _submitted_key).
        self.assertEqual(
            config.submitted_labels(9),
            ["identitas tidak diketahui", "identitas tidak diketahui"],
        )


# ---------------------------------------------------------------------------
# D. identity_data tidak boleh berupa non-dict
# ---------------------------------------------------------------------------


class IdentityDataShapeTestCase(_ConfigSandbox):
    def test_list_identity_data_is_replaced_by_empty_dict(self):
        self._write_config(json.dumps({"identity_data": ["siswa-palsu"]}))
        self.assertEqual(config.get("identity_data"), {})

    def test_string_identity_data_is_replaced_by_empty_dict(self):
        self._write_config(json.dumps({"identity_data": "budi"}))
        self.assertEqual(config.get("identity_data"), {})

    def test_valid_identity_data_is_kept(self):
        self._write_config(
            json.dumps({"identity_data": {"nama": "Budi", "kelas": "X-A"}})
        )
        self.assertEqual(
            config.get("identity_data"), {"nama": "Budi", "kelas": "X-A"}
        )

    def test_known_broken_keys_are_sanitized_but_unknown_keys_are_kept(self):
        # Sanitasi terbatas pada kunci yang DIPAHAMI app; kunci lain tidak
        # boleh dibuang (mis. start_time_9, submitted_8 versi lama).
        self._write_config(
            json.dumps({"identity_data": 5, "start_time_9": "2026-09-30T08:00:00"})
        )
        self.assertEqual(config.get("identity_data"), {})
        self.assertEqual(config.get("start_time_9"), "2026-09-30T08:00:00")


if __name__ == "__main__":
    unittest.main()
