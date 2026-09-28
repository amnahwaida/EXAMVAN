"""_maximize_window tidak boleh membatalkan fullscreen mode strict.

Gejala lapangan (Windows, 2026-09-29): mode strict "tidak terasa strict".
Urutan pemanggilan di __main__.main():

    ExamViewerWindow(...)      # -> SecurityEnforcer.activate() ->
                               #    enforcer._activate_strict() ->
                               #    showFullScreen()
    _maximize_window(viewer)   # -> setGeometry(availableGeometry)
                               #    show()
                               #    showMaximized()   <-- membatalkan fullscreen

showMaximized() menimpa state fullscreen yang baru dipasang, jadi jendela
strict berakhir sebagai maximized biasa (masih frameless + always on top,
tapi bukan fullscreen) — kiosk mode kehilangan fokus.

Kontrak yang dikunci di sini: pemanggil menyatakan niatnya lewat
`fullscreen=`, dan helper ini tidak pernah melakukan maximize padawndow
fullscreen. Aspek "window_state bocor diperbaiki ulang" diuji di
tests/test_exam_mode_enforcement.py (butuh QWidget sungguhan).
"""

from __future__ import annotations

import unittest
from unittest import mock

from examvan import __main__ as main_mod


class _FakeWidget:
    def __init__(self):
        self.calls = []
        self.shown = False
        self.maximized = False
        self.fullscreen = False

    def setGeometry(self, geo):
        self.calls.append("setGeometry")

    def show(self):
        self.calls.append("show")
        self.shown = True

    def showMaximized(self):
        self.calls.append("showMaximized")
        self.maximized = True
        self.fullscreen = False

    def showFullScreen(self):
        self.calls.append("showFullScreen")
        self.fullscreen = True
        self.maximized = False

    def isFullScreen(self):
        return self.fullscreen and not self.maximized


class MaximizeWindowTestCase(unittest.TestCase):
    def setUp(self):
        # _maximize_window does `from PyQt5.QtWidgets import QApplication`
        # inside the function, so the class is patched at its definition
        # site. primaryScreen() -> None skips the geometry call entirely.
        patcher = mock.patch(
            "PyQt5.QtWidgets.QApplication.primaryScreen", return_value=None
        )
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_fullscreen_does_not_maximize(self):
        w = _FakeWidget()
        main_mod._maximize_window(w, fullscreen=True)
        self.assertTrue(w.fullscreen)
        self.assertNotIn("showMaximized", w.calls)

    def test_default_still_maximizes(self):
        # The config dialog and the waiting-approval dialog are not exams,
        # so they keep the existing maximized behaviour.
        w = _FakeWidget()
        main_mod._maximize_window(w)
        self.assertTrue(w.maximized)
        self.assertFalse(w.fullscreen)

    def test_fullscreen_still_shows_the_window(self):
        w = _FakeWidget()
        main_mod._maximize_window(w, fullscreen=True)
        self.assertIn("show", w.calls)
        self.assertTrue(w.shown)

    def test_show_comes_before_the_state_call(self):
        # Qt ignores a state request on a hidden window, so show() has to
        # precede showMaximized()/showFullScreen().
        w = _FakeWidget()
        main_mod._maximize_window(w, fullscreen=True)
        self.assertLess(w.calls.index("show"), w.calls.index("showFullScreen"))


if __name__ == "__main__":
    unittest.main()
