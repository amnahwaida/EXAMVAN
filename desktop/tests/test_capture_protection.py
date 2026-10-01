"""Unit tests for the security enforcer's level-based feature gating.

Specifically the screen-capture protection split (issue C): it used to live
inside set_strict_mode(), so only strict exams got any capture resistance and
a medium exam had none. It is now its own backend method, called from
_activate_medium(), and released in deactivate().

These tests use a fake backend rather than the real one so the gating logic
can be checked on any platform — the real Windows backend cannot even be
imported off Windows, which is why this logic had no coverage before.
"""

from __future__ import annotations

import unittest
from unittest import mock

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer


class FakeSignal:
    def __init__(self):
        self.connected = 0

    def connect(self, *_args, **_kwargs):
        self.connected += 1


class FakeWindowHandle:
    def __init__(self):
        self.activeChanged = FakeSignal()


class FakeWindow:
    """Minimal stand-in for the QMainWindow the enforcer expects."""

    def __init__(self):
        self._handle = FakeWindowHandle()
        self.raised = 0
        self.activated = 0
        self.flags = None
        self.shown_fullscreen = False
        self.windowFlags_called = 0

    def windowHandle(self):
        return self._handle

    def setWindowFlags(self, flags):
        self.flags = flags
        self.windowFlags_called += 1

    def windowFlags(self):
        return 0

    def showFullScreen(self):
        self.shown_fullscreen = True

    def raise_(self):
        self.raised += 1

    def activateWindow(self):
        self.activated += 1

    def isActiveWindow(self):
        return True


class FakeBackend:
    """Records what the enforcer asked for, at each security level."""

    def __init__(self):
        self.calls: list[str] = []

    def activate(self):
        self.calls.append("activate")

    def deactivate(self):
        self.calls.append("deactivate")

    def set_capture_protection(self, window):
        self.calls.append("set_capture_protection")

    def release_capture_protection(self, window):
        self.calls.append("release_capture_protection")

    def set_strict_mode(self, window):
        self.calls.append("set_strict_mode")

    def release_strict_mode(self, window):
        self.calls.append("release_strict_mode")

    def confine_pointer(self, window):
        self.calls.append("confine_pointer")

    def release_pointer(self):
        self.calls.append("release_pointer")

    def clear_clipboard(self):
        self.calls.append("clear_clipboard")

    def prevent_sleep(self):
        self.calls.append("prevent_sleep")

    def allow_sleep(self):
        self.calls.append("allow_sleep")

    def has_multiple_monitors(self):
        return False

    def is_system_dark(self):
        return True


class CaptureProtectionGatingTestCase(unittest.TestCase):
    def setUp(self):
        # No QApplication is created on purpose. Creating a QCoreApplication
        # here makes the module fail when run standalone (another test module
        # in the full suite already owns a QApplication, and a second one is
        # rejected). Without any application object Qt prints a harmless
        # "QObject::startTimer" warning per test, which is the better trade.
        self.backend = FakeBackend()
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=self.backend
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        self.window = FakeWindow()

    def _enforcer(self, level: str, strict: bool = False) -> SecurityEnforcer:
        return SecurityEnforcer(
            security_level=level, strict_mode=strict, window=self.window
        )

    # ---- the actual fix -------------------------------------------------

    def test_medium_enables_capture_protection(self):
        e = self._enforcer("medium")
        e.activate()
        self.assertIn("set_capture_protection", self.backend.calls)

    def test_strict_enables_capture_protection(self):
        e = self._enforcer("strict")
        e.activate()
        self.assertIn("set_capture_protection", self.backend.calls)

    def test_strict_mode_flag_alone_enables_capture_protection(self):
        # security_level="low" with strict_mode=True is how the viewer sets
        # strict exams; it must still get capture resistance.
        e = self._enforcer("low", strict=True)
        e.activate()
        self.assertIn("set_capture_protection", self.backend.calls)

    def test_low_does_not_enable_capture_protection(self):
        # Low mode is the "no lockdown" tier; adding WDA_MONITOR there would
        # blank the window in screenshots for the least-protected exam.
        e = self._enforcer("low")
        e.activate()
        self.assertNotIn("set_capture_protection", self.backend.calls)

    def test_capture_protection_is_released_on_deactivate(self):
        e = self._enforcer("medium")
        e.activate()
        e.deactivate()
        self.assertIn("release_capture_protection", self.backend.calls)
        # It must be released, not just left set.
        self.assertLess(
            self.backend.calls.index("set_capture_protection"),
            self.backend.calls.index("release_capture_protection"),
        )

    # ---- make sure the split did not move input confinement -----------

    def test_medium_does_not_grab_input(self):
        # The keyboard hook / X11 grabs stay strict-only. If set_strict_mode
        # were called at medium, medium exams would suddenly start blocking
        # Alt+Tab and taking pointer grabs.
        e = self._enforcer("medium")
        e.activate()
        self.assertNotIn("set_strict_mode", self.backend.calls)

    def test_strict_grabs_input(self):
        e = self._enforcer("strict")
        e.activate()
        self.assertIn("set_strict_mode", self.backend.calls)


if __name__ == "__main__":
    unittest.main()
