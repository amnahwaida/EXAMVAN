"""Headless tests for ExamViewerWindow auto-submit flow.

Covers the desktop↔Android consistency fix (Agustus 2026):
- auto-submit (deadline / exam_terminated / focus-loss medium / close
  medium-strict) now mirrors Android autoSubmitAndExit: flush answers +
  sticky marker + release lock + close window IMMEDIATELY, then submit in
  the background;
- success → clear answers + complete presence + success notification
  (including the teacher's custom congrats_message);
- failure → answers stay on disk (recovery re-entry offers "Kirim Lagi");
- F1: empty memory must NOT overwrite the disk copy when flushing.
"""

from __future__ import annotations

import shutil
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtWidgets import QApplication

from examvan import api, config, notify
from examvan.models import Exam, SubmitResponse
from examvan.ui import exam_viewer
from examvan.ui.exam_viewer import ExamViewerWindow

APP = QApplication.instance() or QApplication([])


def _wait_until(predicate, timeout=5.0, interval=0.02):
    """Poll until predicate() is truthy (background thread completion)."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return True
        time.sleep(interval)
    return False


class AutoSubmitTestCase(unittest.TestCase):
    """Common setup: temp config dir + mocked network/security/ws/notify."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-autosubmit-test-")
        self._dir_patch = mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
        )
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None

        self._sec_patch = mock.patch.object(exam_viewer, "SecurityEnforcer")
        self._sec = self._sec_patch.start()
        self._ws_patch = mock.patch.object(exam_viewer, "ExamWebSocket")
        self._ws_patch.start()
        self._dl_patch = mock.patch.object(
            api, "download_pdf", return_value=None
        )
        self._dl_patch.start()
        self._al_patch = mock.patch.object(api, "send_access_log", return_value=False)
        self._al_patch.start()
        self._cm_patch = mock.patch.object(api, "complete_exam", return_value=True)
        self._cm_patch.start()
        self._notify_patch = mock.patch.object(notify, "send_notification", return_value=True)
        self._notify = self._notify_patch.start()

    def tearDown(self):
        self._notify_patch.stop()
        self._cm_patch.stop()
        self._al_patch.stop()
        self._dl_patch.stop()
        self._ws_patch.stop()
        self._sec_patch.stop()
        self._file_patch.stop()
        self._dir_patch.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)

    def _make_window(self, exam_id=7, answers=None):
        exam = Exam(
            id=exam_id,
            name="Ujian Matematika",
            status="active",
            security_level="low",
        )
        win = ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        if answers:
            win._answer_sheet.restore_answers(answers)
        return win

    def _wait_notify(self, fragment: str, timeout: float = 5.0) -> bool:
        """Wait until a notification whose MESSAGE contains `fragment` was sent.

        Robust against background-thread leakage: `_auto_submit_and_exit`
        finishes with a notify call, but a slow daemon thread from a PREVIOUS
        test can still be alive and land its notify on THIS test's mock
        (patch menggantikan atribut modul secara global). Mencocokkan isi
        pesan (bukan `call_args` terakhir / "any call") membuat tiap test
        deterministik, dan menunggu notifikasi sendiri memastikan thread
        selesai sebelum test berikutnya mulai.
        """
        return _wait_until(
            lambda: any(
                fragment in str(c.args[1]) for c in self._notify.call_args_list
            ),
            timeout,
        )


class AutoSubmitSuccessTest(AutoSubmitTestCase):
    def test_success_clears_answers_and_notifies_with_congrats(self):
        resp = SubmitResponse(
            success=True,
            message="ok",
            status="done",
            congrats_message="Selamat, Budi! Skor kamu 88.",
        )
        # Gate: tahan thread background di dalam submit_with_retry sampai
        # assertion flush selesai. Tanpa gate, mock submit instan → thread
        # bisa clear_answers SEBELUM `load_answers` di-assert (race internal
        # test — flaky di full suite).
        release = threading.Event()

        def _gated_submit(*args, **kwargs):
            release.wait(timeout=5)
            return resp

        sub_patch = mock.patch.object(
            api, "submit_with_retry", side_effect=_gated_submit
        )
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A", "2": "B"})
            win._auto_submit_and_exit()

            # Window ditutup SEGERA, sebelum hasil jaringan tiba.
            self.assertFalse(win.isVisible())
            self.assertTrue(win._submitted)
            # Sticky marker + jawaban ter-flush ke disk (thread masih diblokir).
            self.assertTrue(config.is_submitted(7, "ABCD1234"))
            self.assertEqual(config.load_answers(7), {"1": "A", "2": "B"})
            # Lock task dilepas (security.deactivate dipanggil).
            self._sec.return_value.deactivate.assert_called()

            # Lepas thread → sukses → clear + complete presence + notif.
            release.set()
            self.assertTrue(self._wait_notify("Selamat, Budi!"))
            self.assertIsNone(config.load_answers(7))
            api.complete_exam.assert_called()
            # Congrats custom guru dipakai sebagai isi notifikasi sukses.
            call = next(
                c for c in self._notify.call_args_list
                if "Selamat, Budi!" in str(c.args[1])
            )
            self.assertIn("Ujian Terkumpul", call.args[0])
        finally:
            release.set()
            sub_patch.stop()

    def test_success_without_congrats_falls_back_to_message(self):
        resp = SubmitResponse(success=True, message="Jawaban berhasil dikirim", status="done")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit_and_exit()
            self.assertTrue(self._wait_notify("Jawaban berhasil dikirim"))
        finally:
            sub_patch.stop()

    def test_queued_202_polls_before_success(self):
        # Server hanya mengantre (202) → polling /result sampai done.
        resp = SubmitResponse(
            success=True, message="queued", status="queued", job_id="j1",
        )
        polled = SubmitResponse(
            success=True, status="done", score=90.0, message="ok",
        )
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        poll_patch = mock.patch.object(api, "poll_queued_result", return_value=polled)
        sub_patch.start()
        poll_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit_and_exit()
            self.assertTrue(_wait_until(lambda: config.load_answers(7) is None))
            api.poll_queued_result.assert_called_once()
            # Drain: tunggu notifikasi thread ini selesai (message fallback
            # = "ok") supaya thread daemon tidak bocor ke test berikutnya
            # (tanpa drain, notify "ok" mendarat di mock test berikutnya).
            self.assertTrue(self._wait_notify("ok"))
        finally:
            poll_patch.stop()
            sub_patch.stop()


class AutoSubmitFailureTest(AutoSubmitTestCase):
    def test_failure_keeps_answers_and_notifies_critical(self):
        resp = SubmitResponse(success=False, message="jaringan mati")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit_and_exit()

            self.assertTrue(self._wait_notify("jaringan mati"))
            # Jawaban TIDAK dihapus — recovery re-entry mengirim ulang.
            self.assertEqual(config.load_answers(7), {"1": "A"})
            self.assertTrue(config.is_submitted(7, "ABCD1234"))
            call = next(
                c for c in self._notify.call_args_list
                if "jaringan mati" in str(c.args[1])
            )
            self.assertIn("Pengumpulan Gagal", call.args[0])
        finally:
            sub_patch.stop()

    def test_failure_preserves_disk_copy_after_exception(self):
        sub_patch = mock.patch.object(
            api, "submit_with_retry", side_effect=OSError("timeout")
        )
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit_and_exit()
            self.assertTrue(self._wait_notify("timeout"))
            self.assertEqual(config.load_answers(7), {"1": "A"})
            call = next(
                c for c in self._notify.call_args_list
                if "timeout" in str(c.args[1])
            )
            self.assertIn("Pengumpulan Gagal", call.args[0])
        finally:
            sub_patch.stop()


class AutoSubmitF1FlushTest(AutoSubmitTestCase):
    def test_empty_memory_does_not_overwrite_disk_copy(self):
        # F1: deadline menembak sebelum restore (memori kosong, disk punya
        # jawaban dari auto-save) → flush TIDAK boleh menimpa dengan {}.
        config.save_answers(7, {"1": "ASLI", "2": "JAWABAN"})
        win = self._make_window(answers=None)  # memori kosong
        resp = SubmitResponse(success=True, status="done", message="ok")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win._auto_submit_and_exit()
            # Flush menyimpan copy disk (bukan {}), dan submit memakainya.
            self.assertTrue(self._wait_notify("ok"))
            self.assertIsNone(config.load_answers(7))
            self.assertTrue(api.submit_with_retry.return_value.success)
            # Argumen answers yang dikirim = copy disk.
            args = api.submit_with_retry.call_args.args
            self.assertEqual(args[5], {"1": "ASLI", "2": "JAWABAN"})
        finally:
            sub_patch.stop()

    def test_nonempty_memory_is_flushed(self):
        win = self._make_window(answers={"1": "BARU"})
        config.save_answers(7, {"1": "LAMA"})
        resp = SubmitResponse(success=True, status="done", message="ok")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win._auto_submit_and_exit()
            self.assertTrue(self._wait_notify("ok"))
            self.assertIsNone(config.load_answers(7))
            args = api.submit_with_retry.call_args.args
            self.assertEqual(args[5], {"1": "BARU"})
        finally:
            sub_patch.stop()


class AutoSubmitDelegateTest(AutoSubmitTestCase):
    def test_auto_submit_delegates_to_exit_flow(self):
        # `_auto_submit` (time_up / exam_terminated / focus-loss / close
        # medium-strict) → jalur exit segera, bukan _do_submit interaktif.
        resp = SubmitResponse(success=True, status="done", message="ok")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit()
            self.assertTrue(win._submitted)
            self.assertFalse(win.isVisible())
            self.assertTrue(self._wait_notify("ok"))
            self.assertIsNone(config.load_answers(7))
        finally:
            sub_patch.stop()

    def test_gate_prevents_double_start(self):
        # Panggilan kedua saat jalur sudah berjalan → di-ignore (submit
        # tunggal — mirror SubmitFlowPolicy Android).
        resp = SubmitResponse(success=True, status="done", message="ok")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            win._auto_submit_and_exit()
            # Tunggu submit background pertama mulai.
            self.assertTrue(_wait_until(lambda: api.submit_with_retry.call_count >= 1))
            # `_submitted` sudah True → jalur exit kedua di-ignore.
            win._auto_submit_and_exit()
            win._auto_submit()
            self.assertTrue(self._wait_notify("ok"))
            self.assertIsNone(config.load_answers(7))
            self.assertEqual(api.submit_with_retry.call_count, 1)
        finally:
            sub_patch.stop()


if __name__ == "__main__":
    unittest.main()
