"""M3 (ronde 5) — `_close_in_progress` tidak boleh mengunci selamanya.

`closeEvent` memasang latch anti-reentrancy (`_close_in_progress = True`)
DAN mengembalikan flag itu kembali False hanya di tiga tempat. Satu saja yang
lolos — `QMessageBox.exec_` melempar, atau `self.closed.emit()` →
`_after_viewer_gone` → `_maximize_window(dialog)` melempar — flag itu
latch True untuk sisa umur proses.

Akibatnya setiap `closeEvent` berikutnya mengembalikan `event.ignore()`.
Pada medium/strict itu juga menutup gate close → auto-submit, dan kalau
`end_time=None` tidak ada auto-submit berbasis timer: satu-satunya jalan
keluar adalah Task Manager — dengan jawaban yang belum terkirim.

Dua perbaikan yang diuji di sini:

  1. Cabang `blocks_free_exit` jalan SEBELUM latch.Latch itu ada untuk
     menahan re-entrancy, bukan untuk mem-veto auto-submit: menaruhnya
     lebih dulu membuat latch yang salah-benar menggantung ikut memblokir
     jalan keluar yang sah.
  2. Seluruh badan `closeEvent` dibungkus `try/finally`, jadi latch
     kembali False apa pun yang terjadi di dalamnya.
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

    def activate(self):
        pass

    def deactivate(self):
        pass

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


class CloseLatchBase(unittest.TestCase):
    def setUp(self) -> None:
        _purge_top_levels()
        self._tmp = tempfile.mkdtemp(prefix="examvan-r5-close-")
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

    def _viewer(self, level="low", strict=False):
        exam = Exam.from_json({
            "id": 41, "name": "Ujian", "status": "active",
            "security_level": level, "strict_mode": strict,
            "questions": [],
        })
        win = ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(win.hide)
        return win


class CloseLatchUnlatchesTest(CloseLatchBase):
    """(1) latch harus kembali False walau dialog melempar."""

    def test_closed_emit_exception_does_not_latch(self):
        # Jalur lain yang bisa melempar di dalam closeEvent: `closed.emit()`
        # → `_after_viewer_gone` → `_maximize_window(dialog)`.
        #
        # Di PyQt5 exception yang lolos dari virtual override berakhir
        # sebagai qFatal()/SIGABRT, jadi kegagalan ini HARUS ditangkap:
        # jendela tetap terbuka (siswa boleh mencoba lagi) dan latch lepas.
        win = self._viewer(level="low")
        boom = RuntimeError("maximize gagal")
        event = mock.Mock(spec=["accept", "ignore"])
        with mock.patch.object(
            # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
            exam_viewer.QMessageBox, "exec_",
            return_value=exam_viewer.QMessageBox.Yes,
        ), mock.patch.object(
            type(win.closed), "emit", side_effect=boom
        ):
            win.closeEvent(event)
        self.assertFalse(
            win._close_in_progress,
            "latch close menggantung True setelah closed.emit() melempar — "
            "siswa tidak bisa menutup/mengumpulkan jawaban lagi",
        )
        self.assertTrue(
            event.ignore.called,
            "ujian ditutup diam-diam meski langkah keluarnya gagal",
        )

    def test_confirm_dialog_exception_does_not_latch(self):
        win = self._viewer(level="low")
        # Dialog konfirmasi melempar = kegagalan widget/parent, bukan hal
        # yang boleh membekukan seluruh jendela (dan tidak boleh jadi
        # qFatal di PyQt5).
        event = mock.Mock(spec=["accept", "ignore"])
        with mock.patch.object(
            # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
            exam_viewer.QMessageBox, "exec_",
            side_effect=RuntimeError("dialog gagal"),
        ):
            win.closeEvent(event)
        self.assertFalse(
            win._close_in_progress,
            "latch close menggantung True setelah dialog melempar — setiap "
            "closeEvent berikutnya akan di-ignore() selamanya",
        )
        self.assertTrue(event.ignore.called)
        self.assertFalse(event.accept.called)

    def test_next_close_still_works_after_the_exception(self):
        win = self._viewer(level="low")
        with mock.patch.object(
            # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
            exam_viewer.QMessageBox, "exec_",
            side_effect=RuntimeError("dialog gagal"),
        ):
            win.closeEvent(mock.Mock(spec=["accept", "ignore"]))
        # Percobaan kedua: dialog normal dan menjawab "Ya" → tutup diterima.
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
        self.assertFalse(win._close_in_progress)

    def test_no_confirm_still_latches_off(self):
        # Kontrol negatif: jawaban "Tidak" juga harus melepas latch.
        win = self._viewer(level="low")
        event = mock.Mock(spec=["accept", "ignore"])
        with mock.patch.object(
            # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
            exam_viewer.QMessageBox, "exec_",
            return_value=exam_viewer.QMessageBox.No,
        ):
            win.closeEvent(event)
        event.ignore.assert_called_once()
        self.assertFalse(win._close_in_progress)


class CloseLatchDoesNotBlockAutoSubmitTest(CloseLatchBase):
    """(2) latch yang menggantung tidak boleh menutup gate auto-submit."""

    def test_medium_close_still_auto_submits_after_a_raise(self):
        # medium/strict: `closeEvent` = auto-submit, bukan keluar. Kalau
        # latch dicek lebih dulu, latch yang menggantung (dari percobaan
        # sebelumnya) mematikan satu-satunya jalan keluar tanpa dialog.
        win = self._viewer(level="medium")
        win._close_in_progress = True      # latch menggantung dari exception
        auto_submit = mock.Mock()
        win._auto_submit = auto_submit
        event = mock.Mock(spec=["accept", "ignore"])

        win.closeEvent(event)

        self.assertTrue(
            auto_submit.called,
            "latch yang menggantung memblokir gate close→auto-submit: "
            "siswa di medium/strict tidak punya jalan keluar selain "
            "Task Manager",
        )
        event.ignore.assert_called_once()

    def test_medium_close_is_ignored_while_a_submit_is_already_running(self):
        # Kontrol: gate submit tunggal tetap berlaku. `_submitted`/
        # `_submitting` (bukan latch close) yang menahan submit kedua,
        # jadi memindahkan latch ke belakang TIDAK boleh membuka double
        # submit.
        win = self._viewer(level="medium")
        win._submitting = True
        auto_submit = mock.Mock()
        win._auto_submit = auto_submit
        event = mock.Mock(spec=["accept", "ignore"])

        win.closeEvent(event)

        self.assertFalse(auto_submit.called, "submit kedua dijalankan")
        event.ignore.assert_called_once()
        self.assertFalse(win._close_in_progress)

    def test_latch_still_guards_reentrancy_inside_the_dialog(self):
        # Kontrol: latch TETAP berguna di jalur low-level. closeEvent kedua
        # yang masuk selagi dialog konfirmasi terbuka harus di-ignore, bukan
        # menjalankan dialog kedua di atasnya (atau menutup jendela dua kali).
        win = self._viewer(level="low")
        nested = mock.Mock(spec=["accept", "ignore"])
        seen = []

        def _question(*args, **kwargs):
            seen.append(1)
            win.closeEvent(nested)
            return exam_viewer.QMessageBox.No

        event = mock.Mock(spec=["accept", "ignore"])
        # Dialog konfirmasi dibangun eksplisit + `setTextFormat`
            # (teks server tidak boleh jadi rich text), jadi yang perlu
            # diblokir sekarang adalah `exec_()` — bukan lagi
            # `QMessageBox.question()` yang tidak pernah dipanggil.
        with mock.patch.object(exam_viewer.QMessageBox, "exec_",
                               side_effect=_question):
            win.closeEvent(event)

        self.assertEqual(
            len(seen), 1, "dialog konfirmasi dijalankan dua kali (re-entrancy)"
        )
        nested.ignore.assert_called_once()
        event.ignore.assert_called_once()
        self.assertFalse(win._close_in_progress)


if __name__ == "__main__":
    unittest.main()