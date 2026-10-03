"""H12 (HIGH): kredensial kelas + PII siswa tidak boleh duduk telanjang di disk.

Tiga hal yang diverifikasi dengan modul aslinya:

    config.json       "exam_token": "ABCD1234"                  <- polos
    answers_7.owner   {"student_key": "0812",
                       "label": "exam_number=0812"}             <- polos

`exam_token` adalah kredensial SELURUH KELAS pada mode static, dan
`utils.build_student_key` mengembalikan nilai identitas apa adanya
(`value.lower()`), BUKAN hash — jadi docstring `save_answers_owner` yang
bilang "hash identitas+token, bukan data pribadi" tidak benar. Ironisnya
`exam_token_history` dan marker `submitted_*` di berkas yang sama sudah
ter-obfuscate.

Yang diuji di sini:

*  tidak ada satu pun byte plaintext di `config.json` / `.owner`;
*  nilai yang tersimpan tetap bisa dibaca balik (round-trip), termasuk
   token yang secara kebetulan "ter-decode" jadi sampah kalau
   ditebak tanpa penanda — ini alasan bentuk simpanannya memakai kunci
   terpisah, bukan tebakan `base64` pada nilai mentah;
*  `answers_*.dat` versi lama (kunci dicampur token) tetap terbaca
   setelah rotasi token DAN setelah app direstart;
*  konsumen `load_answers_owner` (`exam_viewer`, `server_config`) tetap
   dapat kunci yang benar;
*  sidecar yatim (jawaban sudah hilang) disapu saat start, sidecar yang
   masih menopangi jawaban yang ADA tidak boleh disapu.
"""

from __future__ import annotations

import base64
import json
import logging
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._base = tempfile.mkdtemp(prefix="examvan-r8-secret-")
        self.addCleanup(shutil.rmtree, self._base, ignore_errors=True)
        self.dir = Path(self._base) / "examvan"
        self.dir.mkdir()
        for attr, value in (
            ("_CONFIG_DIR", self.dir),
            ("_CONFIG_FILE", self.dir / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        self._old_cache = config._cache
        config._cache = None
        self.addCleanup(self._restore_cache)

    def _restore_cache(self) -> None:
        config._cache = self._old_cache

    def restart(self) -> None:
        """Buang cache = proses baru yang baca config.json dari disk."""
        config._cache = None

    def raw_config_bytes(self) -> bytes:
        return (self.dir / "config.json").read_bytes()

    def owner_bytes(self, exam_id: int = 7) -> bytes:
        return (self.dir / f"answers_{exam_id}.owner").read_bytes()


class TokenNotOnDiskInPlaintextTest(_ConfigSandbox):
    """exam_token tidak boleh tersimpan apa adanya."""

    TOKEN = "TOKENSECRET01"

    def test_plaintext_token_is_absent_from_config_json(self):
        config.set("exam_token", self.TOKEN)
        raw = self.raw_config_bytes()
        self.assertNotIn(
            self.TOKEN.encode("utf-8"), raw,
            "kredensial seluruh kelas masih bisa dibaca dengan satu "
            "`grep` di akun lain PC lab",
        )

    def test_plaintext_token_is_absent_after_rotation_too(self):
        for token in ("AAAA0001", "BBBB0002", "CCCC0003", self.TOKEN):
            config.set("exam_token", token)
            self.assertNotIn(token.encode("utf-8"), self.raw_config_bytes())

    def test_the_credential_is_still_there_after_a_restart(self):
        config.set("exam_token", self.TOKEN)
        self.restart()
        self.assertEqual(config.get("exam_token"), self.TOKEN)
        self.assertEqual(config.get("exam_token", "").strip(), self.TOKEN)

    def test_round_trip_is_unambiguous(self):
        # Token ini kebetulan "base64 + XOR" valid menjadi sampah kalau
        # nilai mentah ditebak-decode tanpa penanda (lihat
        # `_decode_secret`). Kalau config memakai tebakan seperti itu,
        # kredensial kelas berubah jadi sampah tanpa satu pun error.
        self.assertNotEqual(
            config._decode_secret("NG5BY1A2"), "NG5BY1A2",
            "token uji ini tidak lagi_ixor-ambigu; ganti skenario",
        )
        config.set("exam_token", "NG5BY1A2")
        self.restart()
        self.assertEqual(config.get("exam_token"), "NG5BY1A2")

    def test_empty_token_is_not_written_as_a_secret(self):
        config.set("exam_token", "")
        self.restart()
        self.assertEqual(config.get("exam_token", ""), "")

    def test_plaintext_config_from_an_older_version_is_read_verbatim(self):
        # Config yang sudah ada di PC lab tidak boleh berubah artinya saat
        # di-upgrade: token mentah yang ditulis versi lama dibaca apa
        # adanya, tidak boleh "diperbaiki" jadi hasil tebakan decode.
        (self.dir / "config.json").write_text(
            json.dumps({"exam_token": "NG5BY1A2", "server_url": "http://x/"}),
            encoding="utf-8",
        )
        self.restart()
        self.assertEqual(config.get("exam_token"), "NG5BY1A2")

    def test_sanitized_token_is_still_a_plain_string(self):
        # `_sanitize` dipanggil untuk SEMUA config di disk; nilai bukan
        # string harus tetap jadi string (bukan None / crash).
        out = config._sanitize({"exam_token": 12345, "identity_data": {}})
        self.assertIsInstance(out["exam_token"], str)


class LegacyAnswersStillDecodeTest(_ConfigSandbox):
    """`answers_*.dat` versi lama hanya bisa dibaca lewat token history."""

    @staticmethod
    def _write_legacy_answers(exam_id: int, answers: dict, token: str) -> None:
        raw = json.dumps(answers, separators=(",", ":")).encode("utf-8")
        blob = config._legacy_xor_obfuscate(raw, token)
        (Path(config.__dict__["_CONFIG_DIR"]) / f"answers_{exam_id}.dat").write_text(
            base64.urlsafe_b64encode(blob).decode("ascii"), encoding="ascii",
        )

    def test_pre_round2_answers_survive_a_rotation_after_restart(self):
        answers = {"1": "A", "2": "B", "3": "C"}
        self._write_legacy_answers(7, answers, "OLDTOKEN")
        config.set("exam_token", "OLDTOKEN")
        config.set("exam_token", "NEWTOKEN")     # rotasi dynamic-token
        self.restart()                          # app direstart
        self.assertEqual(config.get("exam_token"), "NEWTOKEN")
        self.assertEqual(
            config.load_answers(7), answers,
            "jawaban yang ditulis sebelum update tidak terbaca: tidak ada "
            "prompt 'Kirim Lagi', autosave berikutnya menimpanya",
        )

    def test_history_of_rotated_tokens_is_not_readable_in_plaintext(self):
        for token in ("AAAA0001", "BBBB0002", "CCCC0003"):
            config.set("exam_token", token)
        raw = self.raw_config_bytes()
        for token in ("AAAA0001", "BBBB0002", "CCCC0003"):
            self.assertNotIn(token.encode("utf-8"), raw)


class OwnerSidecarIsNotPlaintextTest(_ConfigSandbox):
    """`.owner` menyimpan kunci siswa apa adanya — itu PII, bukan hash."""

    KEY = "0812"
    LABEL = "exam_number=0812"
    NAME = "Ahmad Fauzi"

    def test_plaintext_key_and_label_are_absent_from_the_sidecar(self):
        config.save_answers_owner(7, self.KEY, label=self.LABEL)
        raw = self.owner_bytes()
        self.assertNotIn(self.KEY.encode("utf-8"), raw)
        self.assertNotIn(self.LABEL.encode("utf-8"), raw)
        self.assertNotIn(self.NAME.encode("utf-8"), raw)

    def test_consumers_still_get_the_real_key_and_label(self):
        config.save_answers_owner(7, self.KEY, label=self.LABEL)
        owner = config.load_answers_owner(7)
        self.assertIsNotNone(owner)
        self.assertEqual(owner["student_key"], self.KEY)
        self.assertEqual(owner["label"], self.LABEL)

    def test_the_gate_comparison_still_works_after_a_restart(self):
        # `resolve_submit_answers` membandingkan `student_key` dengan kunci
        # yang baru saja dihitung (`utils.build_student_key`) — nilai yang
        # dikembalikan harus persis sama, bukan hash.
        config.save_answers(7, {"1": "MILIK-A"})
        config.save_answers_owner(7, self.KEY, label=self.LABEL)
        self.restart()
        self.assertEqual(
            config.resolve_submit_answers({}, 7, attempt_key=self.KEY),
            {"1": "MILIK-A"},
        )
        self.assertEqual(
            config.resolve_submit_answers({}, 7, attempt_key="9999"),
            {},
            "gerbang pemilik jadi gagal-TERBUKA: jawaban siswa lain "
            "dikembalikan ke percobaan yang bukan miliknya",
        )

    def test_sidecar_written_by_an_older_version_is_still_readable(self):
        (self.dir / "answers_7.owner").write_text(
            json.dumps({"student_key": self.KEY, "label": self.LABEL}),
            encoding="utf-8",
        )
        owner = config.load_answers_owner(7)
        self.assertEqual(owner["student_key"], self.KEY)
        self.assertEqual(owner["label"], self.LABEL)

    def test_a_mangled_sidecar_is_reported_not_guessed(self):
        # Sidecar versi baru (ada penanda `v`) yang isinya rusak: karena
        # jelas ter-encode, decode yang gagal berarti isinya berubah —
        # ditebak begitu saja berarti mengarang kunci siswa.
        (self.dir / "answers_7.owner").write_text(
            json.dumps({
                "v": config._OWNER_FORMAT,
                "student_key": "bukan-base64-yang-invalid",
                "label": "",
            }),
            encoding="utf-8",
        )
        with self.assertLogs(config._log, level=logging.WARNING):
            got = config.load_answers_owner(7)
        self.assertIsNone(
            got,
            "isi sidecar yang rusak harus dilaporkan sebagai 'tidak "
            "diketahui', bukan diterjemahkan jadi tebakan",
        )

    def test_owner_sidecar_is_created_private_from_the_start(self):
        # Dulu `open(tmp, "w")` memakai mode umask (0644) dan baru
        # di-chmod setelah selesai menulis — ada jendela ketika isinya
        # world-readable.
        os.umask(0o022)
        try:
            config.save_answers_owner(7, self.KEY, label=self.LABEL)
        finally:
            os.umask(0o022)
        mode = (self.dir / "answers_7.owner").stat().st_mode & 0o777
        self.assertEqual(mode & 0o077, 0, f"mode sidecar {mode:o}")


class OrphanOwnerSweepTest(_ConfigSandbox):
    """Sidecar yatim = PII siswa yang tidak pernah dihapus."""

    def _owner(self, exam_id: int) -> Path:
        return self.dir / f"answers_{exam_id}.owner"

    def test_owner_left_by_a_finished_exam_is_swept_at_startup(self):
        # Sidecar tanpa berkas jawaban = sisa proses sebelumnya. Tidak ada
        # yang bisa memakainya lagi, dan isinya identitas siswa.
        self._owner(7).write_text(json.dumps({"student_key": "0812"}))
        config.clear_stale_identity_on_startup()
        self.assertFalse(
            self._owner(7).exists(),
            "sidecar yatim tidak disapu saat start: identitas siswa "
            "yang ujiannya sudah selesai menetap di disk",
        )

    def test_owner_of_a_killed_exam_keeps_its_answers_protected(self):
        # Siswa dibunuh di tengah ujian: jawabannya masih di disk, jadi
        # sidecar WAJIB bertahan — justru sidecar itu yang mencegah siswa
        # berikutnya mengirim jawaban orang lain atas namanya.
        config.save_answers(7, {"1": "A"})
        config.save_answers_owner(7, "0812", label="exam_number=0812")
        config.clear_stale_identity_on_startup()
        self.assertTrue(
            self._owner(7).exists(),
            "sidecar dihapus padahal jawabannya masih ada — gerbang "
            "pemilik jadi gagal-TERBUKA (kebocoran lintas siswa)",
        )
        self.assertEqual(config.load_answers(7), {"1": "A"})

    def test_closing_the_window_does_not_delete_a_live_sidecar(self):
        # `clear_identity()` juga dipanggil saat jendela ujian ditutup,
        # yaitu saat ujian masih berjalan: sidecar yang sedang dipakai
        # tidak boleh ikut terhapus.
        config.save_answers(7, {"1": "A"})
        config.save_answers_owner(7, "0812", label="exam_number=0812")
        config.set_identity_session({"nama": "Ahmad"}, {"exam_id": 7})
        config.clear_identity()
        self.assertTrue(self._owner(7).exists())
        self.assertEqual(config.load_answers(7), {"1": "A"})

    def test_clear_answers_still_removes_owner_last(self):
        # Urutan dari `clear_answers` tidak boleh dibalik: owner hanya
        # boleh hilang kalau berkasnya benar-benar tidak ada.
        config.save_answers(7, {"1": "A"})
        config.save_answers_owner(7, "0812", label="exam_number=0812")
        config.clear_answers(7)
        self.assertFalse(self._owner(7).exists())
        self.assertFalse((self.dir / "answers_7.dat").exists())


if __name__ == "__main__":
    unittest.main()
