"""Berkas kredensial harus tertutup, dan kunci jawaban tidak boleh ikut hilang (#8, #9).

Bug #8 -- config.json world-readable
-----------------------------------
`_save()` menulis `config.json` dengan mode bawaan umask (biasanya 0o664
pada umask 002, dan 0o644 padaumask 022). Isinya:

    {"exam_token": "...", "identity_data": {"nama": "..."}}

Di mode static, `exam_token` adalah kredensial SELURUH KELAS: siapa pun
akun lain di PC lab bisa membacanya, memanggil `request-approval` dengan
baris persetujuan yang sudah ada, mengunduh PDF, dan mengirim jawaban
sebagai siapa saja.

Yang memperburuk: `answers_<id>.dat` di sebelahnya XOR-nya memakai kunci
yang dicampur dari token yang SAMA persis. Jadi siapa pun yang bisa
membaca `config.json` sekaligus bisa mendekripsi jawaban yang tersimpan
-- obfuscation-nya tidak menambah apa pun.

Bug #9 -- ganti token membuat jawaban lama tidak terbaca selamanya
-----------------------------------------------------------------
`_xor_obfuscate()` mengambil `get("exam_token")` sebagai material kunci.
Jadi kunci enkripsi jawaban adalah nilai yang bisa diedit pengguna:

    simpan jawaban dengan token ABCD1234 -> load_answers OK
    config.set("exam_token", "WXYZ5678")  -> load_answers() == None

Pada ujian dynamic-token, token berputar. Kalau mesin siswa mati atau
jaringan putus di menit 50, token sudah berganti, siswa mengetik token
BARU, `_connect_thread` menimpanya SEBELUM pemeriksaan recovery, lalu
`load_answers` mengembalikan None: tidak ada prompt "Kirim Lagi", tidak
ada yang dipulihkan, dan autosave berikutnya menimpa berkas dengan
jawaban yang berbeda. Pekerjaan siswa sebelumnya hilang tanpa pesan.

Yang diuji di sini: kunci enkripsi TIDAK BOLEH bergantung pada nilai
yang bisa diedit, dan berkas kredensial hanya boleh dibaca pemiliknya.
"""

from __future__ import annotations

import json
import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = Path(self._tmp.name) / "examvan"
        self.dir.mkdir(parents=True, exist_ok=True)
        for attr, value in (
            ("_CONFIG_DIR", self.dir),
            ("_CONFIG_FILE", self.dir / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    @staticmethod
    def _mode(path: Path) -> int:
        return stat.S_IMODE(path.stat().st_mode)


class CredentialFilePermissionsTestCase(_ConfigSandbox):
    """#8 — config.json hanya boleh dibaca oleh pemiliknya."""

    def test_config_file_is_not_world_readable(self):
        config.set("exam_token", "ABCD1234")
        config.set("identity_data", {"nama": "Ahmad"})
        mode = self._mode(self.dir / "config.json")
        self.assertFalse(
            mode & (stat.S_IROTH | stat.S_IWOTH),
            f"config.json punya mode {mode:o}; efeknya file kredensial "
            "bisa dibaca atau ditulis oleh user lain di PC yang sama.",
        )

    def test_config_file_is_not_group_readable(self):
        config.set("exam_token", "ABCD1234")
        mode = self._mode(self.dir / "config.json")
        self.assertFalse(
            mode & stat.S_IRGRP,
            f"config.json punya mode {mode:o}; token kelas + identitas "
            "siswa terbaca oleh group lain.",
        )

    def test_permissions_survive_a_later_rewrite(self):
        # `set()` menulis ulang berkasnya berkali-kali selama satu sesi.
        # Kalau chmod hanya dilakukan sekali, penulisan berikutnya
        # melawan umask dan mengembalikan file ke mode terbuka.
        config.set("exam_token", "ABCD1234")
        config.set("exam_token", "WXYZ5678")
        config.set("remember_url", False)
        mode = self._mode(self.dir / "config.json")
        self.assertFalse(mode & (stat.S_IRGRP | stat.S_IROTH | stat.S_IWOTH),
                         f"mode kembali terbuka setelah tulis ulang: {mode:o}")

    def test_pre_existing_loose_permissions_are_tightened(self):
        # File bisa sudah ada dari versi lama, atau user yang membuatnya
        # sendiri. Membuka app harus memperbaiki, bukan melanggarkan.
        loose = self.dir / "config.json"
        loose.write_text("{}", encoding="utf-8")
        os.chmod(loose, 0o644)
        config._cache = None
        config.set("exam_token", "ABCD1234")
        self.assertFalse(
            self._mode(loose) & (stat.S_IRGRP | stat.S_IROTH),
            "file yang sudah ada dengan mode longgar tidak diketatkan",
        )

    def test_answers_file_is_also_not_world_readable(self):
        config.set("exam_token", "ABCD1234")
        config.save_answers(9, {"1": "A"})
        answers = self.dir / "answers_9.dat"
        if answers.exists():
            self.assertFalse(
                self._mode(answers) & stat.S_IROTH,
                "berkas jawaban bisa dibaca user lain",
            )

    def test_the_stored_payload_really_is_the_credential(self):
        # Jaga supaya test izin file tidak jadi tak bermakna: pastikan
        # yang ditulis memang rahasianya.
        config.set("exam_token", "ABCD1234")
        config.set("identity_data", {"nama": "Ahmad"})
        data = json.loads((self.dir / "config.json").read_text(encoding="utf-8"))
        self.assertEqual(data["exam_token"], "ABCD1234")
        self.assertEqual(data["identity_data"], {"nama": "Ahmad"})


class AnswersSurviveATokenChangeTestCase(_ConfigSandbox):
    """#9 — kunci jawaban tidak boleh mengikuti token yang bisa diedit."""

    def setUp(self) -> None:
        super().setUp()
        config.set("exam_token", "ABCD1234")
        config.save_answers(9, {"1": "A", "2": "B", "3": "C"})

    def test_answers_load_back_with_the_original_token(self):
        self.assertEqual(config.load_answers(9), {"1": "A", "2": "B", "3": "C"})

    def test_answers_load_back_after_the_token_rotates(self):
        # Dynamic-token exam memutar token; siswa boleh mengetik yang baru.
        config.set("exam_token", "WXYZ5678")
        self.assertEqual(
            config.load_answers(9),
            {"1": "A", "2": "B", "3": "C"},
            "ganti token membuat jawaban tersimpan tidak terbaca; recovery "
            "menolak menawarkan 'Kirim Lagi' dan autosave berikutnya "
            "menimpanya",
        )

    def test_answers_survive_two_rotations(self):
        for token in ("WXYZ5678", "QQQQ1111", "ABCD1234"):
            config.set("exam_token", token)
            self.assertEqual(config.load_answers(9), {"1": "A", "2": "B", "3": "C"})

    def test_answers_do_not_become_readable_without_any_token(self):
        # Tanpa token sama sekali, kunci harus tetap sama -- bukan
        # jatuh ke konstanta yang berbeda dan jadi tidak bisa decode.
        config.set("exam_token", "")
        self.assertEqual(config.load_answers(9), {"1": "A", "2": "B", "3": "C"})

    def test_the_obfuscation_key_does_not_read_the_token(self):
        # Bukti mekanismenya: kunci tidak boleh tergantung pada config.
        source = Path(config.__file__).read_text(encoding="utf-8")
        # Potong HANYA badan `_xor_obfuscate`. Ada `_legacy_xor_obfuscate`
        # yang fungsinya memang membaca token -- itu disengaja untuk
        # migrasi, dan menyertakannya di sini akan membuat test ini salah
        # membaca tujuannya.
        start = source.index("def _xor_obfuscate")
        rest = source[start + 1:]
        end = rest.index("\ndef ")
        body = rest[:end]
        self.assertNotIn(
            'get("exam_token")', body,
            "_xor_obfuscate masih mengambil exam_token sebagai kunci",
        )

    def test_an_answers_file_written_before_the_fix_still_loads(self):
        # Backward compatibility: berkas lama SELALU ditulis dengan
        # key+token. Kalau drop token tanpa migrasi, semua jawaban
        # yang tersimpan sebelum update hilang.
        legacy = self.dir / "answers_9.dat"
        self.assertTrue(legacy.exists())
        legacy.write_text(
            config._encode_answers({"1": "A", "2": "B", "3": "C"}),
            encoding="utf-8",
        )
        config.set("exam_token", "WXYZ5678")
        self.assertEqual(config.load_answers(9), {"1": "A", "2": "B", "3": "C"})


if __name__ == "__main__":
    unittest.main()