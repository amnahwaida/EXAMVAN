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

Ditambah review alur submit (Oktober 2026):
- clear lintas sesi: kesuksesan TERLAMBAT dari thread background percobaan
  LAMA tidak boleh menghapus jawaban milik percobaan BARU yang sudah
  mulai menulis autosave-nya sendiri ke disk (BackgroundSuccessClearGuardTest);
- lembar jawaban TERKUNCI selama submit berjalan (payload sudah snapshot;
  edit selama itu tidak pernah terkirim dan hilang dua tempat saat sukses),
  dan terbuka lagi di jalur gagal (ManualSubmitSheetLockTest).
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

# Kunci marker "sudah dikumpulkan" sekarang per (ujian, siswa), bukan per
# token. Di lab satu token dipakai bersama seluruh kelas, jadi token sebagai
# kunci akan membuat siswa-siswa saling memblokir. Yang dipakai adalah nomor
# ujian, jadi nilainya "n01" -- lihat utils.build_student_key.
STUDENT_KEY = "n01"


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
        # C1 drain: thread background mengirim `_sig_auto_submit_done`
        # (queued) yang baru tiba SETELAH wait selesai — tanpa pump,
        # halaman selamatnya pop-up di test BERIKUTNYA. Pump lalu
        # sembunyikan + jadwalkan hapus semua top-level yang tersisa.
        #
        # SENGAJA `hide()` + `deleteLater()`, BUKAN `close()`: close() pada
        # viewer yang belum submit menjalankan closeEvent → dialog
        # konfirmasi low-mode yang menunggu klik yang tidak pernah datang
        # (gantung selamanya di offscreen).
        APP.processEvents()
        for w in QApplication.topLevelWidgets():
            try:
                w.hide()
                w.deleteLater()
            except Exception:
                pass
        APP.processEvents()

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

    def _pump_until(self, predicate, timeout: float = 5.0) -> bool:
        """Poll predicate sambil memompa event loop (sinyal queued).

        `_sig_auto_submit_done` dikirim dari worker thread → slot GUI
        (halaman selamat / all_done) baru jalan kalau event loop dipompa;
        `_wait_until` biasa tidak memompa sehingga halaman tidak pernah
        tampil selama wait.
        """
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.02)
        return False

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

            # Jendela ujian disembunyikan SEGERA, sebelum hasil jaringan tiba.
            self.assertFalse(win.isVisible())
            self.assertTrue(win._submitted)
            # Sticky marker + jawaban ter-flush ke disk (thread masih diblokir).
            self.assertTrue(config.is_submitted(7, STUDENT_KEY))
            self.assertEqual(config.load_answers(7), {"1": "A", "2": "B"})
            # M1: lockdown TIDAK dilepas selama menunggu. `deactivate()`
            # membebaskan hook keyboard/WDA/ClipCursor untuk SELURUH budget
            # submit (retry ~7 dtk + polling 202 ~77 dtk); sekarang ia
            # dipanggil dari `_on_auto_submit_done`, yaitu setelah hasil tiba.
            self._sec.return_value.deactivate.assert_not_called()
            self.assertIsNotNone(
                getattr(win, "_progress_ref", None),
                "layar pengumpulan harus tetap terlihat sebagai top-level "
                "penyangga (C1)",
            )

            # Lepas thread → sukses → clear + complete presence + notif.
            release.set()
            self.assertTrue(self._wait_notify("Selamat, Budi!"))
            self.assertIsNone(config.load_answers(7))
            api.complete_exam.assert_called()
            # Lockdown dilepas begitu hasil tiba.
            self.assertTrue(
                self._pump_until(
                    lambda: self._sec.return_value.deactivate.called, 5.0
                ),
                "lockdown tidak pernah dilepas setelah hasil background tiba",
            )
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
            self.assertTrue(config.is_submitted(7, STUDENT_KEY))
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


class BackgroundSuccessClearGuardTest(AutoSubmitTestCase):
    """Kesuksesan TERLAMBAT thread lama tidak boleh menghapus jawaban baru.

    Timeline kejadian di lapangan: deadline → auto-submit → jendela ditutup
    segera → thread background mengirim (jaringan mati, retry 7 dtk + poll
    sampai ~77 dtk). Siswa re-entry DALAM rentang itu, mulai mengerjakan
    lagi, autosave-nya menimpa disk. Jaringan pulih → thread LAMA sukses →
    dulu `clear_answers(exam_id)` tanpa syarat: jawaban sesi baru hilang
    dari disk, dan bila sesi baru mati mendadak, tidak ada yang bisa
    dipulihkan.

    Invariant sekarang: thread hanya menghapus disk bila isinya MASIH
    persis payload yang baru dikonfirmasi server. Isi berbeda berarti
    milik percobaan lain.
    """

    def _run_background_thread(self, answers):
        resp = SubmitResponse(success=True, status="done", message="ok")
        with mock.patch.object(api, "submit_with_retry", return_value=resp):
            ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", 7, "ABCD1234",
                "Budi", "N01", "9A", answers,
                "2026-08-16T07:00:00Z", "DESKTOP:m", {"nama": "Budi"},
            )

    def test_a_late_success_does_not_clear_a_new_sessions_answers(self):
        # Sesi baru sudah menulis autosave-nya sendiri sebelum thread lama
        # sukses.
        config.save_answers(7, {"1": "JAWABAN-SESI-BARU", "2": "B"})
        self._run_background_thread({"1": "JAWABAN-SESI-LAMA"})
        self.assertEqual(
            config.load_answers(7), {"1": "JAWABAN-SESI-BARU", "2": "B"},
            "thread percobaan LAMA menghapus jawaban percobaan BARU dari "
            "disk -- recovery berikutnya tidak punya apa pun",
        )

    def test_the_same_payload_is_still_cleared_as_before(self):
        # Negative control: tanpa race, clear berjalan seperti biasa --
        # perbaikan ini tidak boleh membuat disk menumpuk selamanya.
        config.save_answers(7, {"1": "A"})
        self._run_background_thread({"1": "A"})
        self.assertIsNone(config.load_answers(7))

    def test_success_without_any_disk_copy_does_not_crash(self):
        # Disk sudah tidak ada (mis. sesi baru belum pernah menyimpan).
        # Guard harus memperlakukannya sebagai "bukan milik kita", bukan
        # melempar dari thread daemon.
        self._run_background_thread({"1": "A"})
        self.assertIsNone(config.load_answers(7))


class ManualSubmitSheetLockTest(AutoSubmitTestCase):
    """Lembar jawaban terkunci selama submit manual berjalan.

    Dulu hanya tombolnya yang dimatikan: siswa bisa terus mengetik sampai
    77+ detik (retry + polling 202) padahal payload sudah snapshot -- edit
    itu tidak pernah terkirim, dan saat sukses `clear_answers` menghapusnya
    dari disk sementara `_save_answers` no-op karena `_submitted`. Hilang
    dari DUA tempat sekaligus, padahal layar masih menampilkannya.
    """

    def test_sheet_locks_and_payload_flushes_when_submit_starts(self):
        win = self._make_window(answers={"1": "A"})
        # Patch lewat type(win), BUKAN exam_viewer.ExamViewerWindow: modul
        # exam_viewer bisa saja di-reload oleh test lain (test_admin_password
        # me-reload-nya untuk menguji pembacaan password saat import), jadi
        # atribut module menunjuk KELAS BARU yang bukan kelas `win` -- patch
        # di kelas baru tidak pernah menyentuh metode yang dipakai `win`,
        # dan thread submit jalan sungguhan ke jaringan.
        with mock.patch.object(type(win), "_submit_thread") as st:
            win._do_submit()
            self.assertFalse(
                win._answer_sheet.isEnabled(),
                "lembar masih bisa diedit setelah payload dikirim",
            )
            self.assertFalse(win._btn_submit.isEnabled())
            # Payload persis yang dikirim tersimpan ke disk: bila proses
            # mati di tengah polling, "Kirim Lagi" mengirim ulang payload
            # yang SAMA, bukan copy autosave yang sedikit lebih lama.
            self.assertEqual(config.load_answers(7), {"1": "A"})
            self.assertTrue(st.called)

    def test_failure_reopens_the_sheet_and_the_button(self):
        win = self._make_window(answers={"1": "A"})
        # type(win), bukan exam_viewer.ExamViewerWindow -- alasan di atas.
        with mock.patch.object(type(win), "_submit_thread"):
            win._do_submit()
        self.assertFalse(win._answer_sheet.isEnabled())

        # exec_: dialog kegagalan adalah instance yang di-exec (PlainText),
        # bukan static warning() — mock static saja menggantung di offscreen.
        with mock.patch.object(exam_viewer.QMessageBox, "exec_",
                               return_value=exam_viewer.QMessageBox.Ok):
            win._on_submit_result(False, "jaringan mati")

        self.assertTrue(
            win._answer_sheet.isEnabled(),
            "submit gagal tapi lembar tetap terkunci -- siswa tidak bisa "
            "memperbaiki jawaban sebelum mencoba lagi",
        )
        self.assertTrue(win._btn_submit.isEnabled())
        self.assertEqual(win._btn_submit.text(), " Kumpulkan Jawaban")

    def test_auto_submit_also_locks_the_sheet(self):
        win = self._make_window(answers={"1": "A"})
        resp = SubmitResponse(success=True, status="done", message="ok")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win._auto_submit_and_exit()
            self.assertFalse(win._answer_sheet.isEnabled())
            self.assertTrue(self._wait_notify("ok"))
        finally:
            sub_patch.stop()


class AutoSubmitDoneFlowTest(AutoSubmitTestCase):
    """Alur selesai C1: hidden-bukan-destroyed + wiring all_done.

    - submit manual sukses → viewer hide (closed BELUM menembak), halaman
      tampil; halaman ditutup → viewer close → closed;
    - auto-submit sukses → halaman tampil; halaman ditutup → all_done;
    - auto-submit gagal → all_done langsung TANPA halaman (recovery
      re-entry menawarkan "Kirim Lagi");
    - antre TANPA job_id → gagal (M7), halaman hijau tidak pernah tampil.
    """

    def test_manual_submit_hides_viewer_until_page_closed(self):
        win = self._make_window(answers={"1": "A"})
        closed = []
        win.closed.connect(lambda: closed.append(True))
        win._on_submit_result(True, "Hebat!")
        APP.processEvents()
        page = win._congrats_ref
        self.assertTrue(page.isVisible(), "halaman selamat tidak tampil")
        self.assertFalse(win.isVisible(), "viewer harus hide, bukan close")
        self.assertEqual(
            closed, [],
            "closed menembak padahal halaman belum ditutup — dialog "
            "konfigurasi akan menimpanya (bug C1)",
        )
        self.assertEqual(page.congrats_text(), "Hebat!")
        page.close()
        self.assertEqual(
            closed, [True],
            "menutup halaman harus menutup viewer (page_closed)",
        )

    def test_auto_success_shows_page_then_all_done_on_page_close(self):
        resp = SubmitResponse(
            success=True, message="ok", status="done",
            congrats_message="Halo!",
        )
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            fired = []
            win.all_done.connect(lambda: fired.append(True))
            win._auto_submit_and_exit()
            self.assertTrue(self._wait_notify("Halo!"))
            self.assertTrue(
                self._pump_until(lambda: hasattr(win, "_congrats_ref")),
                "halaman selamat tidak tampil setelah sukses background",
            )
            page = win._congrats_ref
            self.assertTrue(page.isVisible())
            self.assertEqual(fired, [])
            page.close()
            self.assertEqual(
                fired, [True],
                "menutup halaman sukses-background harus menembak all_done",
            )
            self.assertIsNone(config.load_answers(7))
        finally:
            sub_patch.stop()

    def test_auto_failure_emits_all_done_without_page(self):
        resp = SubmitResponse(success=False, message="jaringan mati")
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        sub_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            fired = []
            win.all_done.connect(lambda: fired.append(True))
            win._auto_submit_and_exit()
            self.assertTrue(self._wait_notify("jaringan mati"))
            self.assertTrue(
                self._pump_until(lambda: fired),
                "all_done tidak menembak setelah gagal background",
            )
            self.assertFalse(hasattr(win, "_congrats_ref"))
            # Jawaban tetap di disk untuk "Kirim Lagi".
            self.assertEqual(config.load_answers(7), {"1": "A"})
        finally:
            sub_patch.stop()

    def test_queued_without_job_id_is_failure_never_green(self):
        resp = SubmitResponse(
            success=True, status="queued", job_id="", message="queued",
        )
        sub_patch = mock.patch.object(api, "submit_with_retry", return_value=resp)
        poll_patch = mock.patch.object(api, "poll_queued_result")
        sub_patch.start()
        poll_patch.start()
        try:
            win = self._make_window(answers={"1": "A"})
            fired = []
            win.all_done.connect(lambda: fired.append(True))
            win._auto_submit_and_exit()
            self.assertTrue(
                self._pump_until(lambda: fired),
                "all_done tidak menembak untuk antre tanpa job_id",
            )
            api.poll_queued_result.assert_not_called()
            self.assertFalse(
                hasattr(win, "_congrats_ref"),
                "halaman hijau tampil tanpa konfirmasi durable (M7)",
            )
            self.assertEqual(config.load_answers(7), {"1": "A"})
        finally:
            poll_patch.stop()
            sub_patch.stop()


if __name__ == "__main__":
    unittest.main()
