"""Titik masuk fokus #2 (`windowHandle().activeChanged`) tidak boleh hilang diam-diam.

Bug — sambungan ditelan `except: pass`
----------------------------------------
`_activate_medium` menyambungkan `_on_window_active_changed` ke
`windowHandle().activeChanged` di dalam `try/except Exception: pass`. Satu-
satunya alasan sambungan itu BERHASIL saat ini adalah urutan kejadian di
`_activate_medium`: `set_capture_protection()` dipanggil lebih dulu dan
memanggil `winId()`, yang membuat HWND — sehingga `windowHandle()` tidak
`None` lagi. Ubah urutannya satu baris dan fokus masuk #2 mati tanpa satu
baris log pun, sementara jalur #1 (`applicationStateChanged`) masih hidup —
jadi tidak ada yang complaining, hanya auto-submit yang lebih lambat.

Yang diuji di sini:

  * `windowHandle()` masih `None` saat aktivasi → ada warning di log, dan
    sambungan BELUM dianggap terpasang;
  * ketika handle akhirnya ada (HWND dibuat saat window ditampilkan),
    sambungan dicoba LAGI dan berhasil — dari event `Show` yang installing
    filter event milik enforcer sudah terima;
  * event `Show` berikutnya tidak menyambungkan dua kali.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, Qt
from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_MEDIUM

APP = QApplication.instance() or QApplication([])


class _FakeSignal:
    def __init__(self) -> None:
        self.connected = []
        self.disconnected = []

    def connect(self, slot, *args, **kwargs):
        self.connected.append(slot)

    def disconnect(self, slot, *args, **kwargs):
        self.disconnected.append(slot)


class _FakeHandle:
    def __init__(self) -> None:
        self.activeChanged = _FakeSignal()


class _LateHandleWindow(QWidget):
    """Window yang `windowHandle()`-nya baru ada setelah HWND dibuat.

    QWidget sungguhan (event filter dan `installEventFilter` butuh itu), tapi
    handle-nya dikendalikan supaya test tidak bergantung pada platform
    offscreen yang kapan membuat HWND.
    """

    def __init__(self) -> None:
        super().__init__()
        self._handle = None

    def create_native_handle(self) -> None:
        self._handle = _FakeHandle()

    def windowHandle(self):  # noqa: N802 (Qt API)
        return self._handle


class _RecordingBackend:
    def set_capture_protection(self, window):
        pass

    def activate(self):
        pass

    def deactivate(self):
        pass

    def has_multiple_monitors(self):
        return False

    def prevent_sleep(self):
        pass

    def allow_sleep(self):
        pass

    def clear_clipboard(self):
        pass

    def confine_pointer(self, window):
        pass

    def set_strict_mode(self, window):
        pass

    def release_strict_mode(self, window):
        pass

    def release_capture_protection(self, window):
        pass

    def release_pointer(self):
        pass


def _deliver_show(window) -> None:
    """Kirim `QEvent.Show` ke window (melalui event filter yang terpasang)."""
    APP.sendEvent(window, QEvent(QEvent.Show))


class ActiveChangedConnectTestCase(unittest.TestCase):
    def _enforcer(self, window):
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=_RecordingBackend())
        patcher.start()
        self.addCleanup(patcher.stop)
        enforcer = SecurityEnforcer(
            security_level=LEVEL_MEDIUM, window=window)
        self.addCleanup(enforcer.deactivate)
        return enforcer

    def test_a_missing_handle_is_reported_in_the_log(self):
        window = _LateHandleWindow()
        self.addCleanup(window.deleteLater)
        enforcer = self._enforcer(window)

        with self.assertLogs("examvan.security.enforcer", level="WARNING"):
            enforcer.activate()

        self.assertFalse(
            getattr(enforcer, "_active_changed_connected", False),
            "sambungan dianggap terpasang padahal `windowHandle()` masih "
            "None — fokus masuk #2 mati dan tidak ada yang tahu",
        )

    def test_the_connect_is_retried_once_the_handle_exists(self):
        window = _LateHandleWindow()
        self.addCleanup(window.deleteLater)
        enforcer = self._enforcer(window)
        with self.assertLogs("examvan.security.enforcer", level="WARNING"):
            enforcer.activate()

        window.create_native_handle()
        _deliver_show(window)

        connected = window.windowHandle().activeChanged.connected
        self.assertEqual(
            len(connected), 1,
            "sambungan `activeChanged` tidak dicoba lagi dari event Show — "
            "HWND yang baru dibuat tidak pernah memberi titik masuk fokus #2",
        )
        slot = connected[0]
        self.assertIs(
            getattr(slot, "__self__", None), enforcer,
            "sambungan mengarah ke objek lain, bukan enforcer yang hidup",
        )
        self.assertEqual(
            slot.__func__, SecurityEnforcer._on_window_active_changed,
        )
        self.assertTrue(
            getattr(enforcer, "_active_changed_connected", False),
            "sambungan berhasil tapi latch-nya tidak berubah — percobaan "
            "berikutnya akan menyambung dua kali",
        )

    def test_a_second_show_does_not_connect_twice(self):
        window = _LateHandleWindow()
        self.addCleanup(window.deleteLater)
        enforcer = self._enforcer(window)
        with self.assertLogs("examvan.security.enforcer", level="WARNING"):
            enforcer.activate()
        window.create_native_handle()

        _deliver_show(window)
        _deliver_show(window)

        self.assertEqual(
            len(window.windowHandle().activeChanged.connected), 1,
            "dua sambungan ke slot yang sama → handler dipanggil dua kali "
            "per perubahan fokus",
        )

    def test_a_window_with_a_handle_at_activation_needs_no_warning(self):
        window = _LateHandleWindow()
        window.create_native_handle()
        self.addCleanup(window.deleteLater)
        enforcer = self._enforcer(window)

        with mock.patch.object(
            enforcer_mod.log, "warning",
            wraps=enforcer_mod.log.warning) as warn:
            enforcer.activate()

        self.assertEqual(
            len(window.windowHandle().activeChanged.connected), 1)
        self.assertEqual(
            warn.call_count, 0,
            "warning palsu saat sambungan berhasil normal — log yang penuh "
            "membuat warning asli tidak terlihat lagi",
        )

    def test_low_level_never_installs_the_second_entry_point(self):
        window = _LateHandleWindow()
        window.create_native_handle()
        self.addCleanup(window.deleteLater)
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=_RecordingBackend())
        patcher.start()
        self.addCleanup(patcher.stop)
        enforcer = SecurityEnforcer(security_level="low", window=window)
        self.addCleanup(enforcer.deactivate)

        enforcer.activate()
        _deliver_show(window)

        self.assertEqual(
            window.windowHandle().activeChanged.connected, [],
            "level low memasang guard fokus yang tidak pernah ada sebelumnya "
            "— low tidak punya focus guard sama sekali",
        )


if __name__ == "__main__":
    unittest.main()