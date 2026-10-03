"""MEDIUM — fullscreen tidak ditegaskan ulang setelah MOVE atau RESIZE polos.

Bug
---
`ExamViewerWindow._enforce_fullscreen` hanya disambungkan ke
`changeEvent` dengan `WindowStateChange` dan `ActivationChange`, dan
`covers_fullscreen()` — yang menjawab pertanyaan "sudah fullscreen?" dari
GEOMETRI, bukan dari flag — tidak dikonsultasikan di tempat lain. Jadi dua hal yang TIDAK mengubah
window state sama sekali tidak pernah diperbaiki:

  * Pindah jendela (Alt+Space → Move, atau drag title bar);
  * Resize polos (Alt+Space → Size).

Auditor mengukur dengan viewer sungguhan:

    sesudah move    : frameGeometry QRect(0,0,800,600) → QRect(40,30,1024,700)
                      isFullScreen() False, _enforce_fullscreen dipanggil 0×
    sesudah resize  : sama, 0×
    minimize/restore: 3× (StateChange benar-benar terjadi)

Di medium/low Alt+Space juga tersedia karena `keyPressEvent` hanya
menelan `Qt.Key_Space` dengan Alt di strict, dan tidak ada keyboard hook di
bawah strict — jadi Move/Size bukan hanya masalah strict.

Test di bawah memakai viewer nyata + event loop nyata (`app.exec_()` dengan
stop `QTimer.singleShot`): yang diperiksa adalah GEOMETRI akhirnya, bukan
sekali panggil handler-nya. Panggilan event secara manual tidak akan
menangkap apa pun — dua bug topologi di proyek ini tidak terlihat dari
event loop yang dipompa tangan.
"""

from __future__ import annotations

import contextlib
import importlib
import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, QTimer, pyqtSignal
from PyQt5.QtWidgets import QApplication

from examvan import api, config
from examvan.models import Exam
from examvan.ui.fullscreen import apply_fullscreen, covers_fullscreen

APP = QApplication.instance() or QApplication([])

EXAM_ID = 614


class _FakeSecurity(QObject):
    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()
        self.reasserts = 0

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        pass

    @contextlib.contextmanager
    def pause_focus_guard(self):
        yield

    def reassert_capture_protection(self) -> None:
        self.reasserts += 1

    def clear_clipboard_now(self) -> None:
        pass

    def protect_window(self, window) -> bool:
        return True


def _pump(ms: int = 320) -> None:
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


class FullscreenRepairTestCase(unittest.TestCase):
    def setUp(self) -> None:
        module = importlib.import_module("examvan.ui.exam_viewer")
        for patcher in (
            mock.patch.object(module, "SecurityEnforcer", _FakeSecurity),
            mock.patch.object(module, "ExamWebSocket"),
            mock.patch.object(api, "download_pdf", side_effect=OSError("offline")),
            mock.patch.object(api, "send_access_log", return_value=False),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low",
            "questions": [{"number": 1, "type": "single_choice",
                           "choices": ["A", "B"]}],
        })
        self.viewer = module.ExamViewerWindow(
            exam=exam, server_url="https://exam.example", token="T",
            identity_data={"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(self.viewer.hide)
        self.addCleanup(self.viewer.deleteLater)
        apply_fullscreen(self.viewer)
        _pump(120)
        self.assertTrue(
            covers_fullscreen(self.viewer, self.viewer.screen()),
            "setup gagal: jendela belum menutupi layar — test tidak "
            "menguji apa pun",
        )
        # HitungPemanggilan perbaikan yang sebenarnya terjadi.
        self.calls = {"n": 0}
        original = self.viewer._enforce_fullscreen

        def _counting():
            self.calls["n"] += 1
            original()

        self.viewer._enforce_fullscreen = _counting

    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def test_a_plain_move_is_repaired(self):
        before = self.calls["n"]
        self.viewer.move(40, 30)
        _pump()
        screen = self.viewer.screen()
        self.assertGreater(
            self.calls["n"], before,
            "Pindah jendela tidak memicu perbaikan sama sekali: "
            "`_enforce_fullscreen` hanya disambungkan ke WindowStateChange/"
            "ActivationChange, dan PINDACH tidak mengubah state apa pun",
        )
        self.assertTrue(
            covers_fullscreen(self.viewer, screen),
            f"jendela tidak dikembalikan ke layar setelah dipindah: "
            f"{self.viewer.frameGeometry()}",
        )

    def test_a_plain_resize_is_repaired(self):
        before = self.calls["n"]
        self.viewer.resize(800, 600)
        _pump()
        screen = self.viewer.screen()
        self.assertGreater(
            self.calls["n"], before,
            "Resize polos tidak memicu perbaikan sama sekali",
        )
        self.assertTrue(
            covers_fullscreen(self.viewer, screen),
            f"jendela tidak dikembalikan ke layar setelah di-resize: "
            f"{self.viewer.frameGeometry()}",
        )

    def test_the_repair_survives_a_minimise_restore_cycle(self):
        # Jalur yang SUDAH bekerja — harus tetap bekerja (kontrol positif).
        before = self.calls["n"]
        self.viewer.showMinimized()
        _pump()
        self.viewer.showNormal()
        apply_fullscreen(self.viewer)
        _pump()
        self.assertGreater(self.calls["n"], before)
        self.assertTrue(covers_fullscreen(self.viewer, self.viewer.screen()))

    def test_the_window_state_is_actually_still_fullscreen(self):
        # `isFullScreen()` dilaporkan False sesudah dipindah pada kondisi
        # rusak; kalau state-nya ikut hilang oleh perbaikannya, jendela akan
        # muncul sebagai window biasa (bukan fullscreen) sampai kejadian
        # berikutnya.
        self.viewer.move(40, 30)
        _pump()
        self.assertTrue(
            self.viewer.isFullScreen(),
            "state fullscreen hilang bersama perbaikannya — jendela akan "
            "muncul sebagai window biasa, dan langkah berikutnya tidak lagi "
            "menutupi layar",
        )


if __name__ == "__main__":
    unittest.main()