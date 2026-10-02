"""Proteksi tangkapan layar pada MEDIUM harus pulih sendiri.

Bug (MEDIUM) — capture protection dipasang sekali, tidak pernah self-heal
---------------------------------------------------------------------------
`reassert_capture_protection()` dipanggil dari `_poll_focus` HANYA di dalam
blok `if self._strict:`. Akibatnya:

  * strict  → `WDA_MONITOR` dipasang ulang tiap 500 ms, jadi HWND yang
              tergantikan (mis. oleh `setWindowFlags()`) pulih sendiri;
  * medium  → satu kali saat `_activate_medium`, lalu event-driven dari
    `QEvent.WindowStateChange`. Rekonstruksi HWND di luar event itu
    (setWindowFlags, perubahan display, beberapa versi DWM) meninggalkan
    ujian MEDIUM tanpa proteksi sampai akhir — SENJA, sementara banner tetap
    berbunyi "MEDIUM";
  * low     → memang tidak boleh punya WDA_MONITOR sama sekali.

Test memakai backend pencatat: N tick `_poll_focus` pada medium harus
meminta N kali proteksi lagi, dan low harus tetap nol.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_LOW, LEVEL_MEDIUM, LEVEL_STRICT

APP = QApplication.instance() or QApplication([])


class _RecordingBackend:
    """Backend yang mencatat setiap permintaan proteksi capture."""

    def __init__(self) -> None:
        self.calls: list[str] = []
        self.captured = 0

    def set_capture_protection(self, window) -> None:
        self.calls.append("set_capture_protection")
        self.captured += 1

    def release_capture_protection(self, window) -> None:
        self.calls.append("release_capture_protection")

    # Sisanya hanya perlu ada supaya `activate()` tidak meledak.
    def activate(self):
        self.calls.append("activate")

    def deactivate(self):
        self.calls.append("deactivate")

    def has_multiple_monitors(self):
        return False

    def prevent_sleep(self):
        self.calls.append("prevent_sleep")

    def allow_sleep(self):
        self.calls.append("allow_sleep")

    def clear_clipboard(self):
        self.calls.append("clear_clipboard")

    def set_strict_mode(self, window):
        self.calls.append("set_strict_mode")

    def release_strict_mode(self, window):
        self.calls.append("release_strict_mode")

    def confine_pointer(self, window):
        self.calls.append("confine_pointer")

    def release_pointer(self):
        self.calls.append("release_pointer")


class _FakeWindow(QWidget):
    """QWidget nyata dengan status fokus yang dikontrol manual."""

    def __init__(self) -> None:
        super().__init__()
        self._active = True

    def isActiveWindow(self):
        return self._active


class _CaptureReassertCadenceTestCase(unittest.TestCase):
    def _enforcer(self, level: str, strict: bool = False):
        backend = _RecordingBackend()
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=backend)
        patcher.start()
        self.addCleanup(patcher.stop)
        window = _FakeWindow()
        self.addCleanup(window.deleteLater)
        enforcer = SecurityEnforcer(
            security_level=level, strict_mode=strict, window=window)
        self.addCleanup(enforcer.deactivate)
        enforcer.activate()
        enforcer.wait_for_clipboard_clear()
        return enforcer, backend

    def _ticks(self, enforcer, count: int = 5) -> None:
        for _ in range(count):
            enforcer._poll_focus()

    # -- medium: kaden 500 ms, sama seperti strict ----------------------

    def test_medium_reasserts_capture_protection_on_every_poll(self):
        enforcer, backend = self._enforcer(LEVEL_MEDIUM)
        baseline = backend.captured

        self._ticks(enforcer, 5)

        self.assertEqual(
            backend.captured - baseline, 5,
            "medium hanya memasang proteksi capture %d kali untuk 5 tick "
            "polling: HWND yang tergantikan di tengah ujian membiarkan "
            "WDA_MONITOR hilang sampai akhir, tanpa satu baris log pun"
            % (backend.captured - baseline),
        )

    def test_medium_reasserts_even_while_the_focus_episode_is_open(self):
        # `_poll_focus` bisa keluar lebih awal pada beberapa cabang
        # (popup aplikasi terbuka). Kodus proteksi tidak boleh bergantung
        # pada itu.
        enforcer, backend = self._enforcer(LEVEL_MEDIUM)
        enforcer._window._active = False
        enforcer._on_app_state_changed(0)  # ApplicationInactive
        baseline = backend.captured

        self._ticks(enforcer, 3)

        self.assertEqual(
            backend.captured - baseline, 3,
            "proteksi capture berhenti diulang selama episode focus-loss "
            "justru saat jendela sedang tidak aktif",
        )

    # -- strict: tidak boleh berubah ------------------------------------

    def test_strict_still_reasserts_every_poll(self):
        enforcer, backend = self._enforcer(LEVEL_STRICT, strict=True)
        baseline = backend.captured

        self._ticks(enforcer, 4)

        self.assertEqual(backend.captured - baseline, 4)

    # -- low: harus tetap nol -------------------------------------------

    def test_low_never_gets_capture_protection_from_polling(self):
        enforcer, backend = self._enforcer(LEVEL_LOW)
        baseline = backend.captured

        self._ticks(enforcer, 5)

        self.assertEqual(
            backend.captured, 0,
            "level low tidak boleh mendapat WDA_MONITOR — Ujian paling "
            "terbuka tidak boleh ikut terkunci hanya lewat polling",
        )
        self.assertEqual(backend.captured, baseline)


if __name__ == "__main__":
    unittest.main()