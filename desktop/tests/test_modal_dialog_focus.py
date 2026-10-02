"""Dialog modal di window ujian tidak boleh memicu auto-submit (#1, #7).

Bug yang menutup file ini
-------------------------
Commit sebelumnya menghentikan COUNTDOWN saat dialog konfirmasi
`_on_submit` terbuka. Tapi ada timer kedua yang tidak dihentikan:
focus monitor milik `SecurityEnforcer`.

`QMessageBox.question()` menjalankan nested event loop. Selama itu
berjalan, dialog adalah window aktif dan window UJIAN menjadi tidak
aktif. Diverifikasi di PyQt 5.15:

    sebelum dialog : isActiveWindow = True
    SAAT dialog    : isActiveWindow = False   activeWindow = QMessageBox

`_poll_focus()` (enforcer.py) persis menanyakan
`self._window.isActiveWindow()` setiap 500 ms dari level `medium` ke
atas, dan `security_levels.DEFAULT_LEVEL` adalah `medium`. Jadi:

    dialog modal terbuka
      -> _poll_focus melihat isActiveWindow() == False
      -> _focus_timer.start()  (3000 ms, single shot)
      -> _on_focus_timeout()
      -> auto_submit.emit()
      -> ExamViewerWindow._auto_submit_and_exit()

Jawaban terkirim dan jendela tertutup sementara siswa masih membaca
dialog "Yakin ingin mengumpulkan?".

`_on_window_active_changed` -- yang terpasang di
`windowHandle().activeChanged` -- memanggil handler yang sama, jadi
jalur kedua memicu lebih cepat lagi. Di strict, `_poll_focus` juga
memanggil `raise_()` + `activateWindow()` pada window induk tiap 500 ms,
sehingga jendela ujian bertarung dengan dialog soal z-order dulu.

`_cleanup_after_submit` sudah menyelesaikan ini dengan benar
(`deactivate()` sebelum `information()`), jadi mekanismenya jelas
diketahui; hanya tidak diterapkan di dialog yang muncul SAAT ujian
berjalan.

Test di sini membuktikannya lewat perilaku, bukan hanya membaca sumber:
dialog sungguhan dibuka di platform offscreen, event loop sempat
memproses timer enforcer, lalu auto-submit harus TIDAK pernah sampai.
"""

from __future__ import annotations

import time
import unittest
from unittest import mock

from PyQt5.QtCore import QTimer
from PyQt5.QtWidgets import QApplication, QMessageBox, QWidget

from examvan.models import Exam
from examvan.security.enforcer import SecurityEnforcer
from examvan.ui.exam_viewer import ExamViewerWindow


def _exam(level: str = "medium") -> Exam:
    return Exam.from_json(
        {
            "id": 9,
            "name": "Ujian",
            "status": "active",
            "security_level": level,
        }
    )


class ModalDialogDoesNotAutoSubmitTestCase(unittest.TestCase):
    """Dialog utama di window ujian tidak boleh memicu auto-submit."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _viewer(self, level="medium"):
        viewer = ExamViewerWindow(
            exam=_exam(level),
            server_url="https://exam.example",
            token="T0KEN01",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(viewer.deleteLater)
        return viewer

    def _fake_modal_dialog(self, ms=900, on_tick=None):
        """Pengganti QMessageBox.exec_ yang menjalankan nested loop.

        Dialog modal sungguhan memblokir dengan `exec()`, yang tidak
        pernah kembali ke test. Yang penting untuk bug ini adalah
        event loop-nya tetap BERJALAN selama "dialog" terbuka -- itu yang
        membuat timer enforcer sempat memutuskan. Fungsi ini meniru itu dengan
        batas waktu supaya test tidak menggantung.
        """
        def _question(*args, **kwargs):
            self.fired = False
            deadline = time.monotonic() + ms / 1000.0
            while time.monotonic() < deadline:
                self.app.processEvents()
                if on_tick is not None:
                    on_tick()
            return QMessageBox.No
        return _question

    def _run_with_modal_dialog(self, viewer, ms=900, on_tick=None):
        """Buka 'dialog' lewat jalur produksi, lalu apa yang terjadi."""
        fired = []
        viewer._security.auto_submit.connect(lambda: fired.append("submit"))
        enforcer = viewer._security
        # Dialog nyata membekukan countdown 3 detik focus-loss; dengan
        # timer diperpendek, 900 ms sudah cukup untuk deciding.
        original = enforcer._focus_timer.interval()
        enforcer._focus_timer.setInterval(300)

        def question(*args, **kwargs):
            self.assertTrue(
                enforcer._focus_guard_paused,
                "focus guard tidak ditangguhkan di sekitar dialog modal",
            )
            return self._fake_modal_dialog(ms, on_tick)(*args, **kwargs)

        # Dialog konfirmasi sekarang dibangun eksplisit lalu
        # `exec_()`-kan (teks server tidak boleh dirender sebagai
        # rich text), jadi yang perlu diganti adalah `exec_()`.
        with mock.patch("examvan.ui.exam_viewer.QMessageBox.exec_", question):
            self.seen = {}
            try:
                viewer._on_submit()
            finally:
                enforcer._focus_timer.setInterval(original)
        return fired

    # -- countdown harus ikut, bukan cuma focus guard ------------------

    def test_countdown_timer_is_frozen_around_the_dialog(self):
        viewer = self._viewer("medium")

        def sample():
            self.seen["countdown"] = viewer._timer_widget._timer.isActive()

        self._run_with_modal_dialog(viewer, ms=400, on_tick=sample)
        self.assertFalse(
            self.seen.get("countdown", True),
            "countdown QTimer masih aktif di dalam dialog modal",
        )

    def test_focus_guard_is_paused_while_the_dialog_is_open(self):
        viewer = self._viewer("medium")
        # test helper di dalam sudah memeriksa _focus_guard_paused;
        # kalau guard tidak ada, test ini gagal di sana.
        self._run_with_modal_dialog(viewer, ms=400)

    def test_focus_guard_is_restored_after_the_dialog(self):
        viewer = self._viewer("medium")
        enforcer = viewer._security
        with mock.patch(
            "examvan.ui.exam_viewer.QMessageBox.exec_",
            self._fake_modal_dialog(200),
        ):
            viewer._on_submit()
        self.app.processEvents()
        self.assertFalse(
            enforcer._focus_guard_paused,
            "focus guard tidak dipulihkan setelah dialog ditutup -- "
            "selama itu,_poll_focus buta dan fokus tidak pernah dihitung lagi",
        )

    def test_no_auto_submit_reaches_the_viewer_from_the_dialog(self):
        viewer = self._viewer("medium")
        fired = self._run_with_modal_dialog(viewer, ms=900)
        self.assertEqual(
            fired, [],
            "auto-submit terpicu dari dalam dialog modal",
        )


class FocusGuardPausedTestCase(unittest.TestCase):
    """`SecurityEnforcer` harus bisa menangguhkan deteksi focus-loss."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _enforcer(self, level="medium"):
        window = QWidget()
        window.show()
        self.app.processEvents()
        enforcer = SecurityEnforcer(security_level=level, window=window)
        enforcer.activate()
        self.addCleanup(enforcer.deactivate)
        self.addCleanup(window.deleteLater)
        return enforcer

    def test_paused_enforcer_ignores_inactive_window(self):
        enforcer = self._enforcer()
        with enforcer.pause_focus_guard():
            enforcer._window.hide()
            enforcer._poll_focus()
            enforcer._on_app_state_changed(2)  # ApplicationInactive
            self.assertFalse(
                enforcer._focus_timer.isActive(),
                "timer focus mulai meski guard ditangguhkan",
            )

    def test_resuming_restores_normal_enforcement(self):
        enforcer = self._enforcer()
        with enforcer.pause_focus_guard():
            pass
        enforcer._window.hide()
        enforcer._poll_focus()
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "setelah guard dipulihkan, focus-loss harus dihitung lagi",
        )

    def test_nested_guards_do_not_unpause_early(self):
        enforcer = self._enforcer()
        with enforcer.pause_focus_guard():
            with enforcer.pause_focus_guard():
                pass
            self.assertTrue(
                enforcer._focus_guard_paused,
                "guard dalamBlindly meng-unpause guard luar",
            )
        self.assertFalse(enforcer._focus_guard_paused)

    def test_a_timer_running_before_the_pause_resumes_afterwards(self):
        enforcer = self._enforcer()
        enforcer._window.hide()
        enforcer._poll_focus()
        self.assertTrue(enforcer._focus_timer.isActive())

        with enforcer.pause_focus_guard():
            self.assertFalse(
                enforcer._focus_timer.isActive(),
                "guard harus membekukan countdown yang sedang berjalan",
            )
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "countdown yang tertunda harus dilanjutkan setelah dialog",
        )

    def test_low_level_exam_has_no_focus_guard_to_pause(self):
        enforcer = self._enforcer("low")
        with enforcer.pause_focus_guard():
            self.assertFalse(enforcer._focus_guard_paused)
        enforcer.deactivate()


if __name__ == "__main__":
    unittest.main()