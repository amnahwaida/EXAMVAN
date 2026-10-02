"""M4 (ronde 5) — semua timer harus berhenti di setiap jalur keluar.

Tiga timer milik viewer:

  * `_timer_widget._timer` — countdown 1 Hz;
  * `_save_timer` — debounce autosave 500 ms;
  * `_flush_timer` — flush autosave 2 detik.

`_stop_autosave()` menghentikan dua yang terakhir SAJA, dan di jalur
admin-exit (`_admin_exit_prompt`) ia tidak dipanggil sama sekali. Jadi
ketika siswa keluar tanpa submit atau pengawas menutup ujian dari sisi
mereka, ketiga timer tetap aktif di objek yang sudah tidak terlihat:

  * countdown 1 Hz menembak `time_up` → `_auto_submit` → jawaban terkirim
    pada waktu yang SALAH (batas waktu jadi tak bermakna di log
    lapangan, dan siswa yang sudah keluar masih "dikerjakan");
  * flush autosave menulis jawaban ke disk SETELAH lockdown dilepas —
    dan menimpa jawaban percobaan BERIKUTNYA di PC lab yang sama
    (`_background_submit_thread` hanyasdkip clear kalau payload cocok).

Timer yang berjalan di atas jendela yang sudah selesai adalah kebohongan
lapangan: batas waktu yang tercatat bukan lagi batas waktu sebenarnya.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer
from examvan.ui.exam_viewer import ExamViewerWindow

APP = QApplication.instance() or QApplication([])


class _NoopSecurity:
    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()
        self.deactivate_calls = 0

    def activate(self):
        pass

    def deactivate(self):
        self.deactivate_calls += 1

    def pause_focus_guard(self):
        import contextlib

        return contextlib.nullcontext()

    def protect_window(self, window):
        return True

    def reassert_capture_protection(self):
        pass

    def clear_clipboard_now(self):
        pass


def _purge_top_levels() -> None:
    for _ in range(3):
        for w in APP.topLevelWidgets():
            try:
                w.hide()
            except Exception:
                pass
        APP.processEvents()
    for w in APP.topLevelWidgets():
        try:
            w.deleteLater()
        except Exception:
            pass
    APP.processEvents()


class TimerStopBase(unittest.TestCase):
    def setUp(self) -> None:
        _purge_top_levels()
        self._tmp = tempfile.mkdtemp(prefix="examvan-r5-timer-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp)),
            mock.patch.object(
                config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
            ),
            mock.patch.object(exam_viewer, "SecurityEnforcer", _NoopSecurity),
            mock.patch.object(exam_viewer, "ExamWebSocket"),
            mock.patch.object(
                exam_viewer.api, "download_pdf", side_effect=OSError("offline")
            ),
            mock.patch.object(exam_viewer.api, "send_access_log",
                              return_value=False),
            mock.patch.object(exam_viewer.api, "complete_exam", return_value=True),
            mock.patch.object(exam_viewer.api, "submit_with_retry"),
            mock.patch.object(exam_viewer.notify, "send_notification",
                              return_value=True),
        ]
        for p in self._patches:
            p.start()
        config._cache = None

    def tearDown(self) -> None:
        _purge_top_levels()
        for p in reversed(self._patches):
            p.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)
        _purge_top_levels()

    def _viewer(self, level="low", strict=False, end_time=None):
        exam = Exam.from_json({
            "id": 51, "name": "Ujian", "status": "active",
            "security_level": level, "strict_mode": strict,
            "end_time": end_time,
            "questions": [{"number": 1, "type": "single_choice",
                           "choices": ["A", "B"]}],
        })
        win = ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        # Viewer WARUH ditunjukkan: `_modal_dialog_guard` hanya me-restart
        # countdown pada jendela yang terlihat, jadi tanpa `show()` test
        # ini akan lulus karena artefak harness, bukan karena perbaikan.
        win.show()
        APP.processEvents()
        self.addCleanup(win.hide)
        return win

    def _assert_all_timers_stopped(self, win, context=""):
        suffix = f" ({context})" if context else ""
        self.assertFalse(
            win._timer_widget._timer.isActive(),
            "countdown 1 Hz masih berjalan setelah jalur keluar" + suffix,
        )
        self.assertFalse(
            win._save_timer.isActive(),
            "debounce autosave masih berjalan setelah jalur keluar" + suffix,
        )
        self.assertFalse(
            win._flush_timer.isActive(),
            "flush autosave masih berjalan setelah jalur keluar" + suffix,
        )


class TimerStopOnLowExitTest(TimerStopBase):
    """Siswa menekan "Keluar Ujian → Ya"."""

    def test_countdown_and_autosave_stop_on_confirmed_exit(self):
        win = self._viewer(level="low", end_time="2099-01-01T23:59:59Z")
        # Countdown benar-benar hidup (uji tidak boleh lulus karena timer
        # memang belum pernah mulai).
        win._timer_widget.refresh_deadline()
        self.assertTrue(
            win._timer_widget._timer.isActive(),
            "countdown tidak berjalan — test tidak menguji apa pun",
        )
        self.assertTrue(win._flush_timer.isActive())

        event = mock.Mock(spec=["accept", "ignore"])
        with mock.patch.object(
            # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
            exam_viewer.QMessageBox, "exec_",
            return_value=exam_viewer.QMessageBox.Yes,
        ):
            win.closeEvent(event)
        event.accept.assert_called_once()
        self._assert_all_timers_stopped(win, "closeEvent low-level")


class TimerStopOnAdminExitTest(TimerStopBase):
    """Pengawas menutup ujian dari sisi mereka (strict + password)."""

    def _admin_exit(self, win, password="rahasia-pengawas"):
        # `_ADMIN_PASSWORD` dibaca saat import modul, dan `QInputDialog`
        # di-import di dalam fungsi — patch keduanya di tempat yang benar.
        with mock.patch(
            "PyQt5.QtWidgets.QInputDialog.getText",
            return_value=(password, True),
        ), mock.patch.object(exam_viewer, "_ADMIN_PASSWORD", password):
            win._admin_exit_prompt()

    def test_countdown_and_autosave_stop_after_admin_exit(self):
        win = self._viewer(level="high", strict=True,
                           end_time="2099-01-01T23:59:59Z")
        win._timer_widget.refresh_deadline()
        self.assertTrue(
            win._timer_widget._timer.isActive(),
            "countdown tidak berjalan — test tidak menguji apa pun",
        )
        self.assertTrue(win._save_timer.isActive() or win._flush_timer.isActive())

        self._admin_exit(win)

        self._assert_all_timers_stopped(win, "admin exit")


class TimerStopOnAdminExitFailureTest(TimerStopBase):
    """Password salah / belum dikonfigurasi: TIDAK boleh keluar — timer
    boleh tetap hidup (siswa masih boleh mengerjakan), jadi ini kontrol
    negatif agar perbaikan tidak mematikan countdown secara buta."""

    def test_wrong_password_keeps_timers_running(self):
        win = self._viewer(level="high", strict=True,
                           end_time="2099-01-01T23:59:59Z")
        win._timer_widget.refresh_deadline()
        with mock.patch(
            "PyQt5.QtWidgets.QInputDialog.getText",
            return_value=("salah", True),
        ), mock.patch.object(
            exam_viewer, "_ADMIN_PASSWORD", "rahasia-pengawas"
        ), mock.patch.object(
            exam_viewer.QMessageBox, "warning", return_value=mock.Mock()
        ):
            win._admin_exit_prompt()
        self.assertTrue(
            win._timer_widget._timer.isActive(),
            "countdown dimatikan padahal password salah — ujian yang masih "
            "berjalan kehilangan hitung mundurnya",
        )


if __name__ == "__main__":
    unittest.main()