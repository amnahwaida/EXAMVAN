"""Admin exit (Ctrl+Shift+Alt+Q) harus tetap bisa dipakai, tidak lebih mudah.

Bug — syarat modifier menuntut KECOCOKAN PERSIS
----------------------------------------------
`keyPressEvent` mensyaratkan

    event.modifiers() == (Qt.ControlModifier | Qt.ShiftModifier | Qt.AltModifier)

Kecocokan persis itu rapuh di lapangan: modifier tambahan apa pun yang
dilaporkan Qt — CapsLock/NumLock sebagai group-switch di beberapa layout,
IME aktif, atau keyboard yang mengirim `KeypadModifier` — membuat backdoor
tidak aktif. Worse, keyboard hook Windows yang dipasang strict menelan
keydown Alt, jadi supervisor yang menekan tiga tombol itu pun bisa tidak
sampai ke Qt dengan susunan modifier yang lengkap.

Perbaikannya: tiga modifier itu harus HADIR (subset), bukan sama persis.
Sifat keamanan yang WAJIB dikunci: tanpa tiga modifier itu, backdoor tetap
tidak bisa dicapai — hanya satu atau dua dari them yang tidak cukup, dan
tombol lain selain Q juga tidak cukup.
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

from PyQt5.QtCore import Qt
from PyQt5.QtGui import QKeyEvent
from PyQt5.QtWidgets import QApplication, QMainWindow

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer as ev_mod

APP = QApplication.instance() or QApplication([])

THREE = Qt.ControlModifier | Qt.ShiftModifier | Qt.AltModifier


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


class _ViewerStub(ev_mod.ExamViewerWindow):
    """ExamViewerWindow tanpa UI-nya, tapi dengan basis QWidget yang utuh.

    `keyPressEvent` memanggil `super().keyPressEvent()`, jadi kelas dasar
    QMainWindow harus benar-benar diinisialisasi; sisanya (state exam
    strict, penghitung backdoor, security non-None) yang diuji di sini.
    """

    def __init__(self, level: str = "high") -> None:
        QMainWindow.__init__(self)
        self._submitted = False
        self._exam = Exam(id=1, name="Ujian", status="active",
                          security_level=level)
        self._security = _NoopSecurity()
        self._admin_exit_count = 0
        self._admin_exit_prompts = 0
        self._admin_exit_timer = self._timer()

    def _timer(self):
        from PyQt5.QtCore import QTimer

        t = QTimer(self)
        t.setSingleShot(True)
        t.setInterval(2000)
        return t

    def _admin_exit_prompt(self) -> None:
        self._admin_exit_prompts += 1


class AdminExitModifiersTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r6-admin-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
            mock.patch.object(ev_mod, "SecurityEnforcer", _NoopSecurity),
            mock.patch.object(ev_mod, "ExamWebSocket"),
            mock.patch.object(ev_mod.api, "download_pdf",
                              side_effect=OSError("offline")),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self.viewer = _ViewerStub()
        self.addCleanup(self.viewer.hide)
        self.addCleanup(self.viewer.deleteLater)

    def _press(self, modifiers, key=Qt.Key_Q) -> None:
        self.viewer.keyPressEvent(QKeyEvent(QKeyEvent.KeyPress, key,
                                            modifiers))

    def _press_thrice(self, modifiers, key=Qt.Key_Q) -> None:
        for _ in range(3):
            self._press(modifiers, key=key)
            self.viewer._admin_exit_timer.stop()

    # -- harus tetap bisa dipakai --------------------------------------

    def test_the_exact_trio_still_opens_the_backdoor(self):
        self._press_thrice(THREE)
        self.assertEqual(
            self.viewer._admin_exit_prompts, 1,
            "backdoor admin exit tidak aktif dengan tiga modifier yang tepat",
        )

    def test_an_extra_modifier_does_not_disable_the_backdoor(self):
        # Ini inti perbaikannya: `==` yang menyebalkan.
        for extra in (
            Qt.MetaModifier,
            Qt.KeypadModifier,
            Qt.GroupSwitchModifier,
            Qt.AltModifier | Qt.ShiftModifier | Qt.ControlModifier
            | Qt.KeyboardModifierMask,
        ):
            self.setUp()
            self._press_thrice(THREE | extra)
            self.assertEqual(
                self.viewer._admin_exit_prompts, 1,
                "modifier tambahan (%s) mematikan backdoor admin exit — "
                "supervisor terkunci di dalam ujian yang harus ia tutup"
                % extra,
            )

    # -- sifat keamanan: tidak boleh lebih mudah ------------------------

    def test_two_of_the_three_modifiers_are_not_enough(self):
        for partial in (
            Qt.ControlModifier | Qt.ShiftModifier,
            Qt.ControlModifier | Qt.AltModifier,
            Qt.ShiftModifier | Qt.AltModifier,
        ):
            self.setUp()
            self._press_thrice(partial)
            self.assertEqual(
                self.viewer._admin_exit_prompts, 0,
                "backdoor terbuka dengan HANYA dua dari tiga modifier (%s) — "
                "siswa bisa menutup ujian strict dengan tekanan tombol biasa"
                % partial,
            )
            self.assertEqual(
                self.viewer._admin_exit_count, 0,
                "penghitung backdoor jalan untuk dua modifier — tekan tiga "
                "kali sudah cukup untuk keluar dari ujian",
            )

    def test_one_modifier_alone_is_not_enough(self):
        for solo in (
            Qt.ControlModifier,
            Qt.ShiftModifier,
            Qt.AltModifier,
            Qt.NoModifier,
        ):
            self.setUp()
            self._press_thrice(solo)
            self.assertEqual(
                self.viewer._admin_exit_prompts, 0,
                "backdoor terbuka dengan satu modifier saja (%s)" % solo,
            )

    def test_the_wrong_key_is_not_enough(self):
        for key in (Qt.Key_W, Qt.Key_O, Qt.Key_Escape, Qt.Key_Space):
            self.setUp()
            self._press_thrice(THREE, key=key)
            self.assertEqual(
                self.viewer._admin_exit_prompts, 0,
                "backdoor terbuka dengan tombol %s selama tiga modifier "
                "ditahan" % key,
            )

    def test_the_counter_must_still_reach_three_presses(self):
        self._press(THREE)
        self.viewer._admin_exit_timer.stop()
        self._press(THREE)
        self.viewer._admin_exit_timer.stop()
        self.assertEqual(
            self.viewer._admin_exit_prompts, 0,
            "backdoor terbuka setelah dua tekanan — ini jalur yang membuat "
            "ketidak sengaja (tiga tombol sekaligus) berarti emergency exit",
        )
        self._press(THREE)
        self.viewer._admin_exit_timer.stop()
        self.assertEqual(self.viewer._admin_exit_prompts, 1)

    def test_a_low_level_exam_has_no_admin_exit(self):
        viewer = _ViewerStub(level="low")
        self.addCleanup(viewer.hide)
        self.addCleanup(viewer.deleteLater)
        for _ in range(3):
            viewer.keyPressEvent(
                QKeyEvent(QKeyEvent.KeyPress, Qt.Key_Q, THREE))
            viewer._admin_exit_timer.stop()
        self.assertEqual(
            viewer._admin_exit_prompts, 0,
            "low_level mendapat backdoor admin exit",
        )

    def test_a_submitted_window_enforces_nothing(self):
        self.viewer._submitted = True
        self._press_thrice(THREE)
        self.assertEqual(
            self.viewer._admin_exit_prompts, 0,
            "backdoor masih aktif di jendela yang sudah mengumpulkan "
            "jawaban — Escape/PrintScreen ikut ditelan di atas halaman "
            "selamat",
        )


if __name__ == "__main__":
    unittest.main()