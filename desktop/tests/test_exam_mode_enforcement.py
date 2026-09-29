"""Regresi: mode "Tinggi" (server) tidak boleh diperlakukan sebagai "low".

Replay bug lapangan|Windows low-end, 2026-09-29. Server mengirim
security_level="high" (schema.sql CHECK, admin/exams.go:1524), sedangkan
client hanya mengenal "strict". Akibatnya tiga titik membandingkan literal:

  - Exam.is_strict            (models.py:77)
  - SecurityEnforcer.activate (security/enforcer.py:74)
  - ExamViewerWindow.closeEvent (ui/exam_viewer.py:747)

Ujian "Tinggi" yang dijanjikan "TIDAK BISA Keluar" berakhir sebagai
keluar bebas + tanpa submit. Test di bawah mengunci keempat tempat
yang memakai vocabulary itu setelah disatukan di examvan.security_levels.
"""

from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtWidgets import QApplication

from examvan import api, config, notify
from examvan.models import Exam
from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_HIGH, LEVEL_LOW, LEVEL_MEDIUM, LEVEL_STRICT
from examvan.ui import exam_viewer
from examvan.ui.exam_viewer import ExamViewerWindow
from examvan.ui.styles import SECURITY_COLORS

APP = QApplication.instance() or QApplication([])


# ---------------------------------------------------------------------------
# Cycle 2 — Exam model
# ---------------------------------------------------------------------------


class ExamModelTestCase(unittest.TestCase):
    def test_high_level_exam_is_strict(self):
        exam = Exam.from_json({"id": 1, "security_level": "high", "strict_mode": False})
        self.assertTrue(exam.is_strict)

    def test_high_level_exam_blocks_free_exit(self):
        # The exact reported symptom: a "Tinggi" exam let the student walk.
        exam = Exam.from_json({"id": 1, "security_level": "high", "strict_mode": False})
        self.assertTrue(exam.blocks_free_exit)

    def test_level_is_canonicalised(self):
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        self.assertEqual(exam.level, LEVEL_STRICT)

    def test_raw_level_is_preserved_for_logging(self):
        # Keep what the server actually said, so a support log can show the
        # discrepancy — but never branch on it.
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        self.assertEqual(exam.raw_security_level, "high")

    def test_medium_blocks_free_exit_but_is_not_strict(self):
        exam = Exam.from_json({"id": 1, "security_level": "medium", "strict_mode": False})
        self.assertFalse(exam.is_strict)
        self.assertTrue(exam.blocks_free_exit)

    def test_low_allows_free_exit(self):
        exam = Exam.from_json({"id": 1, "security_level": "low", "strict_mode": False})
        self.assertFalse(exam.is_strict)
        self.assertFalse(exam.blocks_free_exit)

    def test_missing_level_defaults_to_medium_not_low(self):
        # Fail-secure. A response with no security_level used to default to
        # "low" on the desktop, the most permissive tier.
        exam = Exam.from_json({"id": 1})
        self.assertEqual(exam.level, LEVEL_MEDIUM)
        self.assertTrue(exam.blocks_free_exit)

    def test_unparseable_level_defaults_to_medium(self):
        exam = Exam.from_json({"id": 1, "security_level": 42})
        self.assertEqual(exam.level, LEVEL_MEDIUM)

    def test_strict_flag_alone_still_works(self):
        # Combination test_capture_protection.py already relies on.
        exam = Exam.from_json({"id": 1, "security_level": "low", "strict_mode": True})
        self.assertTrue(exam.is_strict)
        self.assertTrue(exam.blocks_free_exit)

    def test_display_level_never_leaks_unknown_tier(self):
        for raw in ("low", "medium", "high", "strict", "zzz", None):
            exam = Exam.from_json({"id": 1, "security_level": raw})
            self.assertIn(
                exam.display_level, (LEVEL_LOW, LEVEL_MEDIUM, LEVEL_STRICT)
            )


# ---------------------------------------------------------------------------
# Cycle 3 — SecurityEnforcer
# ---------------------------------------------------------------------------


class _FakeBackend:
    def __init__(self):
        self.calls = []

    def activate(self):
        self.calls.append("activate")

    def deactivate(self):
        self.calls.append("deactivate")

    def allow_sleep(self):
        self.calls.append("allow_sleep")

    def prevent_sleep(self):
        self.calls.append("prevent_sleep")

    def clear_clipboard(self):
        self.calls.append("clear_clipboard")

    def set_capture_protection(self, w):
        self.calls.append("set_capture_protection")

    def release_capture_protection(self, w):
        self.calls.append("release_capture_protection")

    def set_strict_mode(self, w):
        self.calls.append("set_strict_mode")

    def release_strict_mode(self, w):
        self.calls.append("release_strict_mode")

    def has_multiple_monitors(self):
        return False

    def is_system_dark(self):
        return True


class _FakeWindow:
    def windowFlags(self):
        return 0

    def setWindowFlags(self, f):
        pass

    def showFullScreen(self):
        pass

    def isActiveWindow(self):
        return True

    def windowHandle(self):
        return None


class EnforcerLevelGatingTestCase(unittest.TestCase):
    def setUp(self):
        self.backend = _FakeBackend()
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=self.backend
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        self.window = _FakeWindow()

    def _enforcer(self, level, strict=False):
        return SecurityEnforcer(
            security_level=level, strict_mode=strict, window=self.window
        )

    def test_high_activates_strict_features(self):
        # Was the core bug: 'high' matched neither ("medium",) nor == "strict",
        # so a "Tinggi" exam ran with low-tier lockdown only.
        e = self._enforcer(LEVEL_HIGH, strict=False)
        e.activate()
        self.assertIn("set_capture_protection", self.backend.calls)
        self.assertIn("set_strict_mode", self.backend.calls)

    def test_high_activates_medium_features(self):
        e = self._enforcer(LEVEL_HIGH, strict=False)
        e.activate()
        # Capture resistance is the medium-tier marker.
        self.assertIn("set_capture_protection", self.backend.calls)

    def test_medium_activates_medium_but_not_strict(self):
        e = self._enforcer(LEVEL_MEDIUM, strict=False)
        e.activate()
        self.assertIn("set_capture_protection", self.backend.calls)
        self.assertNotIn("set_strict_mode", self.backend.calls)

    def test_low_activates_neither(self):
        e = self._enforcer(LEVEL_LOW, strict=False)
        e.activate()
        self.assertNotIn("set_capture_protection", self.backend.calls)
        self.assertNotIn("set_strict_mode", self.backend.calls)

    def test_canonical_level_property(self):
        e = self._enforcer(LEVEL_HIGH, strict=False)
        self.assertEqual(e.level, LEVEL_STRICT)
        self.assertTrue(e.strict)


# ---------------------------------------------------------------------------
# Cycle 4 — ExamViewerWindow: the "can I just leave?" gate
# ---------------------------------------------------------------------------


class _FakeCloseEvent:
    """Stands in for QCloseEvent, which closeEvent only accept()s/ignore()s."""

    def __init__(self):
        self._accepted = False

    def accept(self):
        self._accepted = True

    def ignore(self):
        self._accepted = False

    def isAccepted(self):
        return self._accepted


class ViewerCloseGateTestCase(unittest.TestCase):
    """A close attempt on medium/strict must auto-submit, not let go.

    `closeEvent` compared `mode in ("medium",)` against the raw server
    string, so a "high" exam reached the low branch: confirm dialog, then
    event.accept() — the student walks out of a "TIDAK BISA Keluar" exam.
    """

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-mode-test-")
        self.addCleanup(shutil.rmtree, self._tmp, True)
        self._dir_patch = mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
        )
        self._dir_patch.start()
        self._file_patch.start()
        self.addCleanup(self._dir_patch.stop)
        self.addCleanup(self._file_patch.stop)
        config._cache = None

        self._sec_patch = mock.patch.object(exam_viewer, "SecurityEnforcer")
        self._sec_patch.start()
        self.addCleanup(self._sec_patch.stop)
        for name, target, ret in (
            ("_ws", exam_viewer, "ExamWebSocket"),
            ("_dl", api, "download_pdf"),
            ("_al", api, "send_access_log"),
        ):
            p = mock.patch.object(target, ret)
            p.start()
            self.addCleanup(p.stop)
        self._notify_patch = mock.patch.object(
            notify, "send_notification", return_value=True
        )
        self._notify_patch.start()
        self.addCleanup(self._notify_patch.stop)

        # Neutralise the low-mode confirm dialog for every test in this class.
        #
        # Without this, a REGRESSION in the level gate is not a clean failure:
        # a "high" exam that wrongly reaches the low branch opens a MODAL
        # QMessageBox inside closeEvent and the test run deadlocks, because
        # the test never gets to answer it. Answering "No" up front means the
        # regression shows up as an assertion failure instead of a hang, and
        # each test still asserts on the dialog via its own patch.
        self._confirm_patch = mock.patch.object(
            exam_viewer.QMessageBox, "question", return_value=exam_viewer.QMessageBox.No
        )
        self._confirm_patch.start()
        self.addCleanup(self._confirm_patch.stop)

    def _make_window(self, level):
        exam = Exam.from_json(
            {
                "id": 9,
                "name": "Ujian",
                "status": "active",
                "security_level": level,
            }
        )
        win = ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token="T0KEN01",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(win.deleteLater)
        # Record auto-submit calls without running the real submit flow.
        win._auto_submit = mock.Mock()
        return win

    def test_high_exam_close_is_refused_and_auto_submits(self):
        win = self._make_window(LEVEL_HIGH)
        ev = _FakeCloseEvent()
        win.closeEvent(ev)
        self.assertFalse(ev.isAccepted(), '"high" exam must not close freely')
        win._auto_submit.assert_called_once()

    def test_medium_exam_close_is_refused_and_auto_submits(self):
        win = self._make_window(LEVEL_MEDIUM)
        ev = _FakeCloseEvent()
        win.closeEvent(ev)
        self.assertFalse(ev.isAccepted())
        win._auto_submit.assert_called_once()

    def test_low_exam_close_shows_confirm_and_stays_open(self):
        win = self._make_window(LEVEL_LOW)
        ev = _FakeCloseEvent()
        with mock.patch.object(
            exam_viewer.QMessageBox, "question", return_value=exam_viewer.QMessageBox.No
        ):
            win.closeEvent(ev)
        self.assertFalse(ev.isAccepted())
        win._auto_submit.assert_not_called()

    def test_low_exam_close_confirmed_is_allowed(self):
        win = self._make_window(LEVEL_LOW)
        ev = _FakeCloseEvent()
        with mock.patch.object(
            exam_viewer.QMessageBox,
            "question",
            return_value=exam_viewer.QMessageBox.Yes,
        ):
            win.closeEvent(ev)
        self.assertTrue(ev.isAccepted())

    # ---- the banner ---------------------------------------------------

    def test_high_exam_banner_reads_strict(self):
        win = self._make_window(LEVEL_HIGH)
        self.assertIn("STRICT", win._lbl_security.text())

    def test_high_exam_banner_uses_strict_colour(self):
        # Regression detail: SECURITY_COLORS had no "high" key, so the
        # banner fell back to the low-tier slate colour and the locked-down
        # exam looked like the permissive one.
        win = self._make_window(LEVEL_HIGH)
        self.assertIn(SECURITY_COLORS[LEVEL_STRICT][0].lower(), win._lbl_security.styleSheet().lower())

    def test_every_level_renders_a_banner(self):
        for level in (LEVEL_LOW, LEVEL_MEDIUM, LEVEL_HIGH, LEVEL_STRICT, "zzz", None):
            with self.subTest(level=level):
                win = self._make_window(level)
                self.assertTrue(win._lbl_security.text())

    # ---- strict presentation ------------------------------------------

    def test_viewer_reports_strict_for_high_exam(self):
        # __main__ uses this to choose showFullScreen() over showMaximized().
        self.assertTrue(self._make_window(LEVEL_HIGH).is_strict)
        self.assertFalse(self._make_window(LEVEL_MEDIUM).is_strict)
        self.assertFalse(self._make_window(LEVEL_LOW).is_strict)

    def test_strict_exam_reasserts_fullscreen_when_state_leaks(self):
        # Not enough to get the ordering right in one place: a stray
        # showMaximized()/restore anywhere would silently drop a strict
        # exam out of fullscreen. The viewer re-asserts it instead of
        # trusting every future caller.
        #
        # Asserted via a spy on showFullScreen, not via the real window
        # state: under QT_QPA_PLATFORM=offscreen the platform reports a
        # maximized window as isFullScreen()==True, so reading the state
        # back would test the QPA plugin instead of this logic.
        win = self._make_window(LEVEL_HIGH)
        with mock.patch.object(
            type(win), "isFullScreen", return_value=False
        ), mock.patch.object(win, "showFullScreen") as fs:
            self._send_window_state_change(win)
        fs.assert_called_once()

    def test_exam_already_covering_the_screen_is_left_alone(self):
        # Guards against a showFullScreen() -> WindowStateChange ->
        # showFullScreen() feedback loop.
        #
        # BOTH conditions must hold: the fullscreen state AND the real
        # geometry. Testing only isFullScreen() is what let the broken
        # layout look healthy — the state stayed True while the window sat
        # in the work area with the taskbar showing. See
        # examvan.ui.fullscreen.
        win = self._make_window(LEVEL_HIGH)
        app = QApplication.instance()
        with mock.patch.object(
            type(win), "isFullScreen", return_value=True
        ), mock.patch.object(
            type(win), "frameGeometry", return_value=app.primaryScreen().geometry()
        ), mock.patch.object(win, "showFullScreen") as fs:
            self._send_window_state_change(win)
        fs.assert_not_called()

    def test_state_flag_alone_does_not_count_as_covered(self):
        # The exact field failure: isFullScreen() says True, the window does
        # not fill the screen, and nothing repairs it.
        app = QApplication.instance()
        work_area = app.primaryScreen().availableGeometry()
        work_area.setHeight(work_area.height() - 40)
        win = self._make_window(LEVEL_HIGH)
        with mock.patch.object(
            type(win), "isFullScreen", return_value=True
        ), mock.patch.object(
            type(win), "frameGeometry", return_value=work_area
        ), mock.patch.object(win, "showFullScreen") as fs:
            self._send_window_state_change(win)
        fs.assert_called_once()

    def test_every_level_keeps_the_window_on_the_whole_screen(self):
        # Changed on purpose: the exam window used to be fullscreen only for
        # strict, so a medium or low exam was merely maximized with the
        # taskbar visible. Reported as "semua mode bermasalah".
        for level in (LEVEL_LOW, LEVEL_MEDIUM, LEVEL_HIGH):
            with self.subTest(level=level):
                win = self._make_window(level)
                with mock.patch.object(
                    type(win), "isFullScreen", return_value=False
                ), mock.patch.object(win, "showFullScreen") as fs:
                    self._send_window_state_change(win)
                fs.assert_called_once()

    @staticmethod
    def _send_window_state_change(win):
        from PyQt5.QtCore import QEvent

        ev = QEvent(QEvent.WindowStateChange)
        win.changeEvent(ev)

    # ---- PrintScreen ---------------------------------------------------

    def test_print_screen_wipes_clipboard_through_the_enforcer(self):
        # The platform half of a clipboard wipe must not run inside the
        # keystroke handler: on Linux that forks xsel/xclip, and on Windows
        # EmptyClipboard can block while OLE data is serialised. The
        # enforcer is what keeps that off the GUI thread.
        from PyQt5.QtGui import QKeyEvent
        from PyQt5.QtCore import Qt as _Qt

        win = self._make_window(LEVEL_HIGH)
        ev = QKeyEvent(QKeyEvent.KeyPress, _Qt.Key_Print, _Qt.NoModifier)
        win.keyPressEvent(ev)
        win._security.clear_clipboard_now.assert_called_once()

    def test_print_screen_key_is_swallowed(self):
        from PyQt5.QtGui import QKeyEvent
        from PyQt5.QtCore import Qt as _Qt

        win = self._make_window(LEVEL_HIGH)
        ev = QKeyEvent(QKeyEvent.KeyPress, _Qt.Key_Print, _Qt.NoModifier)
        win.keyPressEvent(ev)
        self.assertTrue(ev.isAccepted() is False, "PrintScreen must not reach the app")


if __name__ == "__main__":
    unittest.main()
