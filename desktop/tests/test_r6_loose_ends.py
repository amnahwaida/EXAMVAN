"""Ronde 6 — loose end yang tersisa dari-agEN lain.

Empat item yang mereka tandai "di luar edit list saya" tapi memang milik
project, jadi diselesaikan di sini:

1. `server_config._recovery_submit_thread` menghapus jawaban di disk tanpa
   owner guard yang dipakai jalur background (`exam_viewer`).
2. `config.clear_stale_identity_on_startup()` sudah ada tapi TIDAK PERNAH
   dipanggil — identitas siswa yang prosesnya dibunuh tertinggal untuk
   siswa berikutnya.
3. `AccessLog` di server memakai `truncate()` bukan `sanitize()`, jadi
   karakter bidi masih masuk ke dashboard monitoring pengawas.
4. Komentar di `exam_viewer` masih mengiklankan `is_submitted` sebagai
   pemblokir re-entry, padahal ia kini hanya menawarkan pilihan.
"""

from __future__ import annotations

import inspect
import pathlib
import tempfile
import unittest
from unittest import mock

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import QDialog, QMessageBox

APP_DIR = pathlib.Path(__file__).resolve().parents[1]


class _NoModal:
    """Jaga agar tidak ada dialog sungguhan yang memblokir runner."""

    @staticmethod
    def exec_(*_a, **_k):
        return QMessageBox.Yes

    @staticmethod
    def question(*_a, **_k):
        return QMessageBox.Yes


class RecoveryClearAnswersUsesOwnerGuardTest(unittest.TestCase):
    """1 — recovery tidak boleh menghapus jawaban percobaan lain."""

    def test_recovery_clears_only_when_the_disk_still_holds_its_own_payload(self):
        from examvan.ui import server_config

        src = inspect.getsource(server_config.ServerConfigDialog._recovery_submit_thread)
        self.assertIn(
            "answers_match_disk",
            src,
            "jalur recovery menghapus answers tanpa owner guard: jawaban "
            "percobaan yang lebih baru bisa terhapus (sudah 84 detik worker "
            "ini berjalan)",
        )
        # Dan import-nya harus ada, kalau tidak nama di atas tidak resolve.
        self.assertIn(
            "answers_match_disk",
            inspect.getsource(server_config),
            "answers_match_disk dihitung tapi tidak di-import",
        )


class StartupClearsStaleIdentityTest(unittest.TestCase):
    """2 — identitas dari proses yang dibunuh harus dibersihkan saat launch."""

    def test_main_calls_the_startup_identity_sweep(self):
        import examvan.__main__ as main_mod

        src = inspect.getsource(main_mod.main)
        self.assertIn(
            "clear_stale_identity_on_startup",
            src,
            "config.clear_stale_identity_on_startup() tidak pernah dipanggil: "
            "siswa yang prosesnya dibunuh meninggalkan identity_data, dan "
            "satu Enter cukup untuk tercatat atas namanya",
        )

    def test_the_sweep_actually_clears_a_stale_session(self):
        from examvan import config

        with tempfile.TemporaryDirectory() as tmp:
            d = pathlib.Path(tmp)
            with mock.patch.object(config, "_CONFIG_DIR", d), mock.patch.object(
                config, "_CONFIG_FILE", d / "config.json"
            ):
                config._cache = None
                config.set_identity_session(
                    {"nama": "Siswa A", "nomor_ujian": "01"},
                    {"exam_id": 42, "token": "ABCD1234"},
                )
                self.assertTrue(config.get("identity_data"))
                self.assertTrue(config.clear_stale_identity_on_startup())
                self.assertEqual(config.get("identity_data", {}), {})
                self.assertEqual(config.get("identity_context", {}), {})
                # Idempoten: panggilan kedua tidak apa-apa dan tidak melempar.
                self.assertFalse(config.clear_stale_identity_on_startup())
            config._cache = None


class AccessLogSanitizesIdentityTest(unittest.TestCase):
    """3 — AccessLog harus sanitize, bukan hanya truncate."""

    def test_access_log_uses_sanitize_not_bare_truncate(self):
        path = APP_DIR.parent / "webui" / "internal" / "handlers" / "api" / "exams.go"
        src = path.read_text(encoding="utf-8")
        self.assertNotIn(
            "studentName := truncate(body.StudentName, 200)",
            src,
            "AccessLog masih memakai truncate() sehingga karakter bidi "
            "(U+202E) masuk ke dashboard monitoring pengawas dan bisa "
            "menyamarkan nama di leaderboard",
        )
        for field in ("body.StudentName", "body.ExamNumber", "body.StudentClass"):
            self.assertIn(
                f"sanitize({field}",
                src,
                f"{field} di AccessLog harus lewat sanitize()",
            )


class StaleCommentTest(unittest.TestCase):
    """4 — komentar tidak boleh mengiklankan gate yang tidak ada."""

    def test_the_reentry_marker_comment_matches_reality(self):
        from examvan.ui import exam_viewer

        src = inspect.getsource(exam_viewer.ExamViewerWindow._cleanup_after_submit)
        self.assertNotIn(
            "mencegah re-entry",
            src,
            "komentar masih mengiklankan marker sebagai pemblokir re-entry; "
            "padahal server_config kini hanya MENAWARKAN pilihan eksplisit",
        )


if __name__ == "__main__":
    unittest.main()