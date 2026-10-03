"""MEDIUM — `_pdf_viewer.cleanup()` satu-satunya panggilan viewer tanpa penjaga.

Bug
---
`pdf_viewer.py:373-376` adalah `self._doc.close()` telanjang — tanpa
try/except. Sementara itu `cleanup()` dipanggil dari LIMA tempat di
`ExamViewerWindow`, dan tidak satu pun dibungkus penjaga:

  * `_cleanup_after_submit`  (submit manual sukses)
  * `_auto_submit_and_exit` (auto-submit)
  * `closeEvent` cabang `already_done`
  * `closeEvent` cabang keluar low-tier
  * `_admin_exit_prompt` (keluar supervisor)

Akibat yang diukur auditor dengan raises disisipkan, setelah submit yang
SUKSES:

  * exception escaping `_cleanup_after_submit` → viewer tetap terlihat,
    tombol sudah bertulis "✅ Terkumpul" tapi disabled, tidak ada halaman
    selamat, `all_done` tidak pernah PANCAKAN — padahal jawaban sudah
    durable dan `clear_answers` sudah jalan;
  * cabang `already_done` di `closeEvent` memanggil `cleanup()` SEBELUM
    `self.closed.emit()`, jadi kegagalan yang terus-menerus membuat jendela
    tidak bisa ditutup sama sekali (setiap close di-`ignore()`kan) dan
    `_discard_pdf()` tidak pernah jalan → naskah ujian tertinggal di
    `%TEMP%` untuk pupil berikutnya.

Event loop-nya sungguhan (`app.exec_()` dengan stop `QTimer.singleShot`),
karena yang diperiksa adalah apa yang terlihat dan sinyal apa yang
benar-benar ditembakkan, bukan nilai balik fungsi.
"""

from __future__ import annotations

import contextlib
import importlib
import os
import shutil
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, QTimer, pyqtSignal
from PyQt5.QtWidgets import QApplication, QLabel, QVBoxLayout, QWidget

from examvan import api, config
from examvan.models import Exam

APP = QApplication.instance() or QApplication([])

EXAM_ID = 613


class _FakeSecurity(QObject):
    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()
        self.deactivate_calls = 0

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        self.deactivate_calls += 1

    @contextlib.contextmanager
    def pause_focus_guard(self):
        yield

    def reassert_capture_protection(self) -> None:
        pass

    def clear_clipboard_now(self) -> None:
        pass

    def protect_window(self, window) -> bool:
        return True


def _pump_until(done, timeout_ms: int = 6000) -> bool:
    """Event loop sungguhan; berhenti saat `done()` atau batas keras."""
    watcher = QTimer()
    watcher.setInterval(20)
    state = {"ok": False}

    def _tick():
        if done():
            state["ok"] = True
            watcher.stop()
            APP.quit()

    watcher.timeout.connect(_tick)
    hard_stop = QTimer()
    hard_stop.setSingleShot(True)
    hard_stop.timeout.connect(APP.quit)
    hard_stop.setInterval(timeout_ms)
    watcher.start()
    hard_stop.start()
    try:
        APP.exec_()
    finally:
        watcher.stop()
        hard_stop.stop()
    return state["ok"]


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-pdfcleanup-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
            mock.patch.object(api, "download_pdf", side_effect=OSError("offline")),
            mock.patch.object(api, "send_access_log", return_value=False),
            mock.patch.object(api, "complete_exam", return_value=True),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self.module = importlib.import_module("examvan.ui.exam_viewer")
        patcher = mock.patch.object(self.module, "SecurityEnforcer",
                                   _FakeSecurity)
        patcher.start()
        self.addCleanup(patcher.stop)
        patcher = mock.patch.object(self.module, "ExamWebSocket")
        patcher.start()
        self.addCleanup(patcher.stop)

    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def _viewer(self, level="low"):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": level,
            "questions": [{"number": 1, "type": "single_choice",
                           "choices": ["A", "B"]}],
        })
        viewer = self.module.ExamViewerWindow(
            exam=exam, server_url="https://exam.example", token="T",
            identity_data={"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(viewer.hide)
        self.addCleanup(viewer.deleteLater)
        viewer.show()
        APP.processEvents()
        return viewer


class _ExplodingPdfViewer:
    """`cleanup()` yang selalu melempar —iru `QPdfDocument.close()` gagal."""

    def __init__(self):
        self.cleanup_calls = 0

    def cleanup(self):
        self.cleanup_calls += 1
        raise RuntimeError("PDF document sudah tertutup / handle rusak")

    def load_pdf(self, path):
        return True


class CleanupAfterSubmitSurvivesTestCase(_Sandbox):
    def test_a_failing_pdf_cleanup_does_not_swallow_the_result(self):
        viewer = self._viewer("low")
        viewer._pdf_viewer = _ExplodingPdfViewer()
        # Jawaban yang SEDANG dikirim harus ada di disk; kalau tidak, isi
        # disk dianggap "percobaan lain" dan halaman memang tidak boleh
        # tampil — test ini tidak menguji apa pun kalau begitu.
        config.save_answers(EXAM_ID, {"1": "A"})
        viewer._submitted_payload = {"1": "A"}
        viewer._submitted = True
        all_done = []
        # `all_done` yang ditembakkan `__main__` setelah `closed`; di sini
        # rantaunya dirakit sendiri supaya yang diuji benar-benar langkah
        # viewer, bukan isi `__main__`.
        viewer.closed.connect(lambda: viewer.all_done.emit())
        viewer.all_done.connect(lambda: all_done.append(True))

        with mock.patch.object(config, "mark_submitted"), \
             mock.patch.object(config, "load_start_time", return_value=""):
            viewer._cleanup_after_submit("Selesai")

        page = getattr(viewer, "_congrats_ref", None)
        self.assertIsNotNone(
            page,
            "halaman selamat tidak pernah dibuat karena exception dari "
            "`cleanup()` escaping: siswa menekan 'Kumpulkan', jawaban sudah "
            "durable, lalu tidak ada apa-apa yang terjadi",
        )
        self.assertEqual(viewer._btn_submit.text(), "✅ Terkumpul")
        self.assertFalse(
            viewer._btn_submit.isEnabled(),
            "tombol harus mati setelah terkumpul",
        )
        self.assertEqual(
            config.load_answers(EXAM_ID), None,
            "kontrol: jawaban milik sendiri tetap harus dihapus — test "
            "tidak boleh gagal karena alasan lain",
        )
        # `closed` harus menyala saat halaman ditutup → itu yang membuka
        # jalan ke `all_done`.
        page.close()
        self.assertTrue(
            _pump_until(lambda: bool(all_done)),
            "`all_done` tidak pernah PANCAKAN — alur selesai buntu di layar "
            "yang tidak bisa ditutup",
        )


class CloseEventSurvivesFailingCleanupTestCase(_Sandbox):
    def test_the_window_can_still_be_closed_after_a_failing_cleanup(self):
        viewer = self._viewer("low")
        viewer._pdf_viewer = _ExplodingPdfViewer()
        viewer._submitted = True
        pdf_path = Path(tempfile.gettempdir()) / f"examvan_exam_{EXAM_ID}.pdf"
        pdf_path.write_bytes(b"%PDF-1.4\n")
        viewer._pdf_path = str(pdf_path)
        closed = []
        viewer.closed.connect(lambda: closed.append(True))

        viewer.close()
        self.assertTrue(
            closed,
            "`closed` tidak pernah ditembakkan: `cleanup()` dipanggil "
            "SEBELUM emit dan lempar, jadi setiap close di-ignore()kan dan "
            "jendela tidak bisa ditutup sama sekali",
        )
        self.assertFalse(
            viewer.isVisible(),
            "jendela masih terlihat setelah close yang diterima",
        )
        self.assertFalse(
            pdf_path.exists(),
            "berkas naskah ujian tertinggal di %TEMP% karena `_discard_pdf()` "
            "dilewati — PC lab mewarisi naskah ujian ke pupil berikutnya",
        )

    def test_a_persistent_cleanup_failure_never_blocks_closing_again(self):
        viewer = self._viewer("low")
        viewer._pdf_viewer = _ExplodingPdfViewer()
        viewer._submitted = True
        viewer._pdf_path = None
        closed = []
        viewer.closed.connect(lambda: closed.append(True))

        for _ in range(3):
            viewer.close()
            APP.processEvents()

        self.assertEqual(
            len(closed), 3,
            "setiap close berikutnya kembali di-ignore(): jendela menjadi "
            "tidak bisa ditutup dan satu-satunya jalan keluar Task Manager",
        )


class CleanExitPathGuardTestCase(_Sandbox):
    def test_low_level_exit_survives_a_failing_cleanup(self):
        from PyQt5.QtWidgets import QMessageBox

        viewer = self._viewer("low")
        viewer._pdf_viewer = _ExplodingPdfViewer()
        viewer._pdf_path = None
        closed = []
        viewer.closed.connect(lambda: closed.append(True))

        box_patch = mock.patch("examvan.ui.exam_viewer.QMessageBox")
        box = box_patch.start()
        self.addCleanup(box_patch.stop)
        # Enum asli harus dibiarkan apa adanya: `closeEvent` membandingkan
        # `reply != QMessageBox.No`, jadi kalau `No` ikut jadi MagicMock,
        # perbandingannya selalu "berbeda" danjalur keluar tidak pernah
        # tercapai.
        box.Yes = QMessageBox.Yes
        box.No = QMessageBox.No
        box.return_value.exec_.return_value = QMessageBox.Yes

        viewer.close()

        self.assertTrue(
            closed,
            "jalur keluar low-tier gagal hanya karena `cleanup()` melempar "
            "— siswa menekan 'Ya' lalu tidak terjadi apa-apa",
        )
        self.assertTrue(viewer._submitted)


if __name__ == "__main__":
    unittest.main()