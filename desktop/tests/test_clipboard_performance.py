"""Clipboard clear tidak boleh jadi mahal — replay freeze kursor (Windows).

Gejala lapangan (Windows low-end, 2026-09-29): kursor membeku setiap
beberapa detik. Bukan heartbeat (itu 60 detik, exam_viewer.py) dan bukan
screenshot (aplikasi tidak pernah mengambil layar). Penyebabnya:

    windows_backend.clear_clipboard()  ->  subprocess.run(
        ["cmd.exe", "/c", "echo.|clip"], ...)

dipanggil tiap 3000 ms dari timer Qt yang berjalan di GUI thread
(enforcer._activate_low). Jadi tiap 3 detik aplikasi me-fork cmd.exe yang
lalu me-spawn clip.exe, dan `subprocess.run` yang blocking froze seluruh
event loop selama itu.

Blok itu juga 100% redundan: Win32 OpenClipboard/EmptyClipboard (baris di
atasnya) sudah mengosongkan clipboard, dan QApplication.clipboard().clear()
adalah fallback. Test di bawah mengunci tiga hal:
  1. tidak ada proses yang di-spawn untuk mengosongkan clipboard,
  2. platform clear tetap jalan (tidak dihapus bersama subprocess-nya),
  3. clear tidak memblokir GUI thread.
"""

from __future__ import annotations

import threading
import time
import unittest
from unittest import mock

import pathlib

from examvan.security import windows_backend as wb
from tests.test_windows_backend_binding import REQUIRED_PROTOTYPES
REQUIRED_WIN32_NAMES = REQUIRED_PROTOTYPES


class NoSubprocessSpawnTestCase(unittest.TestCase):
    """Cycle 6 — the actual cause of the freeze."""

    def setUp(self):
        # Record any attempt to spawn a process. The mock returns normally
        # (rather than raising) because the code under test wraps the call in
        # `except Exception` — a raising side effect would be swallowed and
        # the test would pass for the wrong reason.
        #
        # Patched on the real `subprocess` module, not on `wb.subprocess`:
        # the module no longer imports subprocess at all, and looking the
        # attribute up on it falls into the PEP-562 __getattr__ hook, which
        # tries to bind Win32 and explodes on Linux.
        import subprocess as _sp

        self.run = mock.patch.object(_sp, "run").start()
        self.popen = mock.patch.object(_sp, "Popen").start()
        self.addCleanup(mock.patch.stopall)

    def test_clear_clipboard_spawns_no_process(self):
        wb.WindowsBackend().clear_clipboard()
        self.run.assert_not_called()
        self.popen.assert_not_called()

    def test_clear_clipboard_uses_win32_api(self):
        """Jalur Win32 adalah jalur otoritatif dan harus tetap dipakai.

        Test ini sebelumnya memock `_OpenClipboard` dengan `create=True`,
        yang berarti ia MENGARANG atribut yang memang hilang. Jadi testnya
        hijau bukan karena Win32-nya bekerja, tapi karena ia memasang
        pengganti untuk fungsi yang tidak pernah ada. Sekarang
        `_bind()` benar-benar memasang prototype-nya, jadi `create=True`
        dihapus dan windll dipalsukan supaya `_bind()` jalan di Linux.

        Test ini gagal kalau prototype-nya hilang lagi.
        """
        from tests.test_windows_backend_binding import _fake_windll

        import ctypes

        backend = wb.WindowsBackend()
        opened = mock.Mock(return_value=True)
        with mock.patch.object(ctypes, "windll", _fake_windll(), create=True):
            wb._bind()
            with mock.patch.object(wb, "_OpenClipboard", opened), \
                 mock.patch.object(wb, "_EmptyClipboard") as emptied, \
                 mock.patch.object(wb, "_CloseClipboard"):
                backend.clear_clipboard()
        opened.assert_called()
        emptied.assert_called()

    def test_the_win32_mocks_do_not_fabricate_missing_attributes(self):
        # Penjaga: kalau ada test lain yang memakai create=True untuk
        # prototype Win32, ia akan mengarang ulang bug yang sama.
        src = pathlib.Path(__file__).read_text(encoding="utf-8")
        for name in REQUIRED_WIN32_NAMES:
            self.assertNotIn(
                f'"{name}", opened, create=True', src,
                f"{name} di-mock dengan create=True: nama yang hilang akan "
                f"dibuatkan dan testnya lulus tanpa menguji apa pun",
            )

    def test_clear_clipboard_does_not_touch_qt_clipboard(self):
        # The enforcer clears the Qt clipboard inline on the GUI thread,
        # because Qt only allows that from the main thread. The backend now
        # runs on a worker, so if it reached for QApplication there it would
        # be an out-of-thread clipboard access.
        from PyQt5.QtWidgets import QApplication

        with mock.patch.object(
            QApplication, "instance", side_effect=AssertionError("touched Qt")
        ) as instance:
            wb.WindowsBackend().clear_clipboard()
        instance.assert_not_called()

    def test_module_has_no_subprocess_left(self):
        # subprocess is no longer needed at all once the fork is gone.
        # Checked against the module dict: attribute lookup on the module
        # itself goes through the PEP-562 __getattr__ Win32 binder.
        self.assertNotIn("subprocess", vars(wb))


class ClipboardOffGuiThreadTestCase(unittest.TestCase):
    """Cycle 7 — the platform clear must not run on the GUI thread."""

    def setUp(self):
        from examvan.security import enforcer as enforcer_mod
        from examvan.security.enforcer import SecurityEnforcer

        self.enforcer_mod = enforcer_mod
        self.SecurityEnforcer = SecurityEnforcer
        self.backend = _SlowBackend()
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=self.backend
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        self.e = SecurityEnforcer(security_level="low", window=None)
        self.e._active = True
        self.addCleanup(self._shutdown)

    def _shutdown(self):
        self.e._active = False
        self.e.wait_for_clipboard_clear()

    def test_platform_clear_runs_off_the_calling_thread(self):
        caller = threading.current_thread().ident
        self.e._clear_clipboard()
        self.e.wait_for_clipboard_clear()
        self.assertTrue(self.backend.cleared, "clipboard was never cleared")
        self.assertNotEqual(
            self.backend.thread_id,
            caller,
            "platform clipboard clear ran on the GUI thread",
        )

    def test_slow_clear_does_not_block_the_caller(self):
        # Simulates OLE data being serialised on EmptyClipboard: 1.5 s of
        # work that, on the GUI thread, is exactly the frozen cursor.
        self.backend.delay = 1.5
        start = time.monotonic()
        self.e._clear_clipboard()
        elapsed = time.monotonic() - start
        self.assertLess(
            elapsed,
            0.5,
            "clear_clipboard blocked the caller for %.2fs" % elapsed,
        )
        self.e.wait_for_clipboard_clear(timeout=5.0)
        self.assertTrue(self.backend.cleared)

    def test_overlapping_clears_are_skipped_not_queued(self):
        self.backend.delay = 0.4
        self.e._clear_clipboard()
        self.e._clear_clipboard()
        self.e._clear_clipboard()
        self.e.wait_for_clipboard_clear(timeout=5.0)
        # One worker max: a queue of stale wipes would only pile up.
        self.assertEqual(self.backend.calls, 1)

    def test_deactivate_waits_for_in_flight_clear(self):
        self.backend.delay = 0.3
        self.e._clear_clipboard()
        self.e.deactivate()
        self.assertTrue(self.backend.cleared)
        self.assertFalse(self.e._active)


class ClipboardIntervalTestCase(unittest.TestCase):
    """Cycle 7 — the timer itself is no longer needed at 3 s."""

    def test_interval_is_not_three_seconds(self):
        from examvan.security import enforcer as enforcer_mod

        self.assertGreaterEqual(enforcer_mod.CLIPBOARD_INTERVAL_MS, 5000)
        self.assertLessEqual(enforcer_mod.CLIPBOARD_INTERVAL_MS, 15000)


class _SlowBackend:
    """Backend whose clear_clipboard takes a configurable amount of time."""

    def __init__(self):
        self.delay = 0.0
        self.calls = 0
        self.cleared = False
        self.thread_id = None

    def clear_clipboard(self):
        self.calls += 1
        self.thread_id = threading.current_thread().ident
        time.sleep(self.delay)
        self.cleared = True

    def activate(self):
        pass

    def deactivate(self):
        pass

    def allow_sleep(self):
        pass

    def prevent_sleep(self):
        pass

    def set_capture_protection(self, w):
        pass

    def release_capture_protection(self, w):
        pass

    def set_strict_mode(self, w):
        pass

    def release_strict_mode(self, w):
        pass

    def confine_pointer(self, w):
        pass

    def release_pointer(self):
        pass

    def has_multiple_monitors(self):
        return False

    def is_system_dark(self):
        return True


if __name__ == "__main__":
    unittest.main()
