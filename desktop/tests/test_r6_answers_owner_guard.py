"""Penjagaan pemilik jawaban di disk: satu helper untuk SEMUA jalur clear.

Bug (MEDIUM) — jalur recovery menghapus jawaban tanpa penjagaan pemilik
---------------------------------------------------------------------
`_background_submit_thread` (auto-submit) tidak menghapus jawaban begitu saja:

    current = config.load_answers(exam_id)
    if current == answers:
        config.clear_answers(exam_id)

`ServerConfigDialog._recovery_submit_thread` (jalur "Kirim Lagi") melakukan
`config.clear_answers(exam.id)` tanpa perbandingan itu. Worker recovery bisa
berjalan ~84 detik (retry + polling 202); dalam rentang itu siswa bisa
re-entry, memulai percobaan baru, dan autosave-nya menimpa disk. Sukses yang
terlambat dari percobaan LAMA lalu menghapus jawaban percobaan BARU — dan
kalau sesi baru itu mati mendadak sesudahnya, tidak ada yang tersisa untuk
dipulihkan.

`server_config.py` tidak boleh diedit ronde ini, jadi yang dilakukan di sini:

  * perbandingan itu difaktorkan keluar jadi `answers_match_disk()` di
    `exam_viewer.py` — satu fungsi yang dipakai kedua jalur;
  * test di bawah mengunci INVARIAN yang diinginkan lewat helper itu, jadi
    whoever memasang penjagaan di `server_config` hanya butuh satu baris
    (lihat pesan kegagalan `test_recovery_path_still_needs_the_guard`).
"""

from __future__ import annotations

import contextlib
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import config
from examvan.models import Exam, SubmitResponse
from examvan.ui import exam_viewer as ev_mod

APP = QApplication.instance() or QApplication([])

EXAM_ID = 99
_EXAM = Exam(id=EXAM_ID, name="Ujian Matematika", status="active")
_IDENTITY = {"nama": "SITI", "nomor_ujian": "N02", "kelas": "9B"}

OLD_PAYLOAD = {"1": "A", "2": "B"}
NEW_ATTEMPT = {"1": "C", "2": "C", "3": "D"}


class _Sandbox(unittest.TestCase):
    """config diarahkan ke direktori sementara supaya config.json asli utuh."""

    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r6-owner-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


# ---------------------------------------------------------------------------
# Helper-nya sendiri
# ---------------------------------------------------------------------------


class AnswersMatchDiskTestCase(_Sandbox):
    """`answers_match_disk(exam_id, answers)` — boleh HANYA kalau sama persis."""

    def test_identical_disk_content_may_be_cleared(self):
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        self.assertTrue(
            ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD),
            "payload yang barusan dikonfirmasi server dianggap bukan "
            "miliknya sendiri — jawaban lama tidak pernah terhapus dan "
            "recovery milik siswa berikutnya akan mengirim ulang jawaban "
            "terkirim",
        )

    def test_a_newer_attempt_on_disk_blocks_the_clear(self):
        # Percobaan baru menulis autosave-nya di tengah worker lama.
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        config.save_answers(EXAM_ID, dict(NEW_ATTEMPT))

        self.assertFalse(
            ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD),
            "sukses yang TERLAMBAT dari percobaan lama menghapus jawaban "
            "percobaan baru dari disk — kalau proses mati sesudahnya, "
            "tidak ada yang tersisa untuk dipulihkan",
        )

    def test_an_empty_or_missing_answer_file_blocks_the_clear(self):
        self.assertFalse(
            ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD),
            "disk kosong dianggap 'sama' dengan payload — clear berikutnya "
            "justru menyembunyikan bahwa tidak ada apa pun yang dihapus",
        )

    def test_an_unreadable_disk_blocks_the_clear(self):
        # Arah fail-safe: kalau tidak bisa dipastikan, JANGAN menghapus.
        with mock.patch.object(config, "load_answers",
                               side_effect=OSError("file terkunci")):
            self.assertFalse(
                ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD),
                "kegagalan membaca disk dianggap 'sama' — helper yang "
                "mengeceplosikan kesalahan menghapus jawaban orang lain",
            )

    def test_the_helper_does_not_mutate_anything_on_disk(self):
        config.save_answers(EXAM_ID, dict(NEW_ATTEMPT))
        ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD)
        self.assertEqual(config.load_answers(EXAM_ID), NEW_ATTEMPT)


# ---------------------------------------------------------------------------
# Jalur background auto-submit: memakai helper, bukan pembanding sendiri
# ---------------------------------------------------------------------------


class _NoopSecurity:
    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()

    def activate(self):
        pass

    def deactivate(self):
        pass

    def pause_focus_guard(self):
        return contextlib.nullcontext()

    def reassert_capture_protection(self):
        pass

    def clear_clipboard_now(self):
        pass


class BackgroundSubmitOwnerGuardTestCase(_Sandbox):
    def _run_background(self):
        """Jalankan `_background_submit_thread` (unbound, tanpa Qt) sukses."""
        config.save_answers(EXAM_ID, dict(NEW_ATTEMPT))
        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", message="ok", congrats_message="Selesai")
        with mock.patch.object(ev_mod, "api", fake_api), \
             mock.patch.object(ev_mod, "notify") as notify:
            notify.send_notification.return_value = True
            ev_mod.ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", EXAM_ID, "ABCD1234",
                "SITI", "N02", "9B", dict(OLD_PAYLOAD),
                "2026-01-01T00:00:00Z", "DESKTOP:h", dict(_IDENTITY),
            )
        return config.load_answers(EXAM_ID)

    def test_the_background_path_delegates_to_the_shared_helper(self):
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", message="ok")
        with mock.patch.object(ev_mod, "api", fake_api), \
             mock.patch.object(ev_mod, "notify"), \
             mock.patch.object(
                 ev_mod, "answers_match_disk",
                 wraps=ev_mod.answers_match_disk) as guard:
            ev_mod.ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", EXAM_ID, "ABCD1234",
                "SITI", "N02", "9B", dict(OLD_PAYLOAD),
                "2026-01-01T00:00:00Z", "DESKTOP:h", dict(_IDENTITY),
            )
        guard.assert_called_once_with(EXAM_ID, OLD_PAYLOAD)

    def test_a_late_success_does_not_wipe_a_newer_attempt(self):
        self.assertEqual(
            self._run_background(), NEW_ATTEMPT,
            "worker auto-submit yang sukses terlambat menghapus jawaban "
            "percobaan yang lebih baru",
        )

    def test_own_payload_is_still_cleared(self):
        # Penjagaan tidak boleh membekukan jawaban yang benar-benar sudah
        # terkirim (file membengkak & recovery salah bagi siswa berikutnya).
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", message="ok")
        with mock.patch.object(ev_mod, "api", fake_api), \
             mock.patch.object(ev_mod, "notify"):
            ev_mod.ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", EXAM_ID, "ABCD1234",
                "SITI", "N02", "9B", dict(OLD_PAYLOAD),
                "2026-01-01T00:00:00Z", "DESKTOP:h", dict(_IDENTITY),
            )
        self.assertIsNone(config.load_answers(EXAM_ID))


# ---------------------------------------------------------------------------
# Jalur recovery: helper-nya sudah siap, call site-nya belum
# ---------------------------------------------------------------------------


class RecoveryPathOwnerGuardTestCase(_Sandbox):
    """Invarian yang harus dipegang `server_config._recovery_submit_thread`."""

    def _run_recovery(self, race=False):
        """Jalankan worker recovery; `race` menyimulasikan percobaan baru.

        recovery bisa berjalan sampai ~84 detik (retry 7 + polling 202).
        Selama itu siswa bisa re-entry dan memulai percobaan BARU yang
        menulis autosave-nya sendiri ke `answers_<id>.dat`. Kalau worker
        yang sudah lama itu lalu menghapus file tanpa syarat, jawaban
        percobaan baru ikut hilang — dan tidak ada yang tersisa untuk
        dipulihkan.
        """
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg._validated_token = "ABCD1234"
        dlg._recovery_identity = dict(_IDENTITY)
        config.set("exam_token", "ABCD1234")
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))

        ok = SubmitResponse(
            success=True, status="ok", message="ok", congrats_message="Selesai")

        def _submit(*_a, **_k):
            # Titik balapan: payload lama sudah terkirim, dan TEPAT sekarang
            # percobaan baru menimpa disk sebelum worker menyentuh clear.
            if race:
                config.save_answers(EXAM_ID, dict(NEW_ATTEMPT))
            return ok

        fake_api = mock.Mock()
        fake_api.submit_with_retry.side_effect = _submit
        with mock.patch("examvan.ui.server_config.api", fake_api), \
             mock.patch("examvan.security.enforcer.get_backend",
                        lambda: _RecordingBackend()):
            dlg._recovery_submit_thread(_EXAM, dict(_IDENTITY))
        return config.load_answers(EXAM_ID)

    def test_the_guard_reports_the_right_answer_for_the_recovery_scenario(self):
        # Inilah yang harus dipakai `_recovery_submit_thread`: hapus HANYA
        # kalau isi disk masih persis payload yang barusan terkirim.
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        self.assertTrue(ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD))
        config.save_answers(EXAM_ID, dict(NEW_ATTEMPT))
        self.assertFalse(ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD))

    def test_recovery_clears_its_own_payload_when_nothing_else_touched_it(self):
        # Tanpa balapan, isi disk masih persis payload yang terkirim —
        # jadi HAPUS benar, supaya file tidak jadi yatim.
        self.assertEqual(self._run_recovery(race=False), None)

    def test_recovery_never_deletes_a_newer_attempts_answers(self):
        """Celah yang ditutup: sukses terlambat tidak boleh menghapus
        jawaban percobaan yang lebih baru."""
        self.assertEqual(
            self._run_recovery(race=True), NEW_ATTEMPT,
            "worker recovery menghapus jawaban percobaan yang lebih baru — "
            "jaga `answers_match_disk()` tetap membungkus `clear_answers()` "
            "di `_recovery_submit_thread`",
        )


class _RecordingBackend:
    """Backend minimal untuk halaman hasil recovery."""

    def set_capture_protection(self, window) -> None:
        pass

    def __getattr__(self, name):
        raise AssertionError(f"backend dipanggil untuk {name!r}, tak terduga")


if __name__ == "__main__":
    unittest.main()