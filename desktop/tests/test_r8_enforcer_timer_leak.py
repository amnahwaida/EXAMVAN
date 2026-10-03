"""MEDIUM — `_activate_medium` menumpuk timer polling fokus.

Bug
---
`SecurityEnforcer._activate_medium()` membuat timer BARU setiap kali
dipanggil:

    self._poll_timer = QTimer(self)
    self._poll_timer.setInterval(FOCUS_POLL_INTERVAL_MS)
    self._poll_timer.timeout.connect(self._poll_focus)
    self._poll_timer.start()

sedangkan `deactivate()` hanya `stop()`-kan timer yang terakhir
disimpan. Timer lama di-parent ke enforcer, jadi tetap hidup dengan
sambungan `timeout → _poll_focus` yang aktif. Efeknya kalau
`_activate_medium` terpanggil dua kali: `_poll_focus` berjalan dua kali per
500 ms, dan di strict itu berarti dua kali `raise_()`,
`activateWindow()`, dan `ClipCursor` per tick.

Hari ini belum terjangkau — `_init_security` mengaktifkan sekali dan
`activate()` dilatch oleh `_active` — tapi itu hanya satu perubahan urutan
baris. Yang dijaga di sini adalah invariant: hanya boleh ada SATU timer
polling hidup per enforcer.

Catatan jujur soal apa yang TIDAK diuji di sini: `_poll_focus` di strict
memang memanggil Win32 (`raise_`/`activateWindow`/`ClipCursor`), dan
double-panggilannya tidak bisa dideteksi di Linux. Yang diuji adalah
kebocoran timer itu sendiri, dengan event loop sungguhan.
"""

from __future__ import annotations

import contextlib
import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, QObject, QTimer, QPoint, pyqtSignal
from PyQt5.QtWidgets import QApplication, QVBoxLayout, QWidget

from examvan.security import enforcer as enf

APP = QApplication.instance() or QApplication([])


def _pump(ms: int = 120) -> None:
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


class _Backend:
    def __init__(self) -> None:
        self.confine_calls = 0

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        pass

    def set_capture_protection(self, window) -> bool:
        return True

    def release_capture_protection(self, window) -> None:
        pass

    def confine_pointer(self, window) -> None:
        self.confine_calls += 1

    def release_pointer(self) -> None:
        pass

    def set_strict_mode(self, window) -> None:
        pass

    def release_strict_mode(self, window) -> None:
        pass

    def has_multiple_monitors(self) -> bool:
        return False

    def is_system_dark(self) -> bool:
        return True

    def allow_sleep(self) -> None:
        pass


class _Window(QWidget):
    auto_submit = pyqtSignal()

    def __init__(self) -> None:
        super().__init__()
        QVBoxLayout(self)
        self._shown = False

    def showEvent(self, event):  # noqa: N802 (Qt API)
        super().showEvent(event)
        self._shown = True

    def eventFilter(self, watched, event):
        return False


class PollTimerLeakTestCase(unittest.TestCase):
    def _enforcer(self, window):
        self.backend = _Backend()
        enforcer = enf.SecurityEnforcer(
            security_level="medium", strict_mode=False, window=window,
        )
        self.addCleanup(enforcer.deleteLater)
        return enforcer

    def _live_poll_timers(self, enforcer) -> list:
        """Timer polling fokus yang HIDUP.

        Disaring lewat interval: enforcer punya tiga timer lain
        (clipboard 10 s, countdown fokus 3 s, dan satu timer sekali-pakai),
        dan yang bocor hanya yang kaden `FOCUS_POLL_INTERVAL_MS`.
        """
        return [
            timer for timer in enforcer.findChildren(QTimer)
            if timer.isActive()
            and timer.interval() == enf.FOCUS_POLL_INTERVAL_MS
        ]

    def test_a_second_activation_does_not_add_a_second_poll_timer(self):
        window = _Window()
        self.addCleanup(window.hide)
        window.show()
        _pump()
        enforcer = self._enforcer(window)

        with mock.patch.object(enf, "get_backend", return_value=self.backend):
            enforcer.activate()
            first = self._live_poll_timers(enforcer)
            self.assertEqual(
                len(first), 1,
                f"aktivasi pertama harusnya menghasilkan tepat satu timer "
                f"aktif, ditemukan {len(first)}",
            )
            # Paksa jalur yang tidak dilatch oleh `_active` — lihat catatan
            # di docstring modul.
            enforcer._activate_medium()
            _pump()
            second = self._live_poll_timers(enforcer)

        self.assertEqual(
            len(second), 1,
            f"dua timer polling fokus hidup bersamaan: {len(second)} — "
            f"`_poll_focus` akan berjalan dua kali per 500 ms, dan di "
            f"strict itu berarti dua kali raise_/activateWindow/ClipCursor",
        )

    def test_deactivate_stops_every_poll_timer(self):
        window = _Window()
        self.addCleanup(window.hide)
        window.show()
        _pump()
        enforcer = self._enforcer(window)

        with mock.patch.object(enf, "get_backend", return_value=self.backend):
            enforcer.activate()
            enforcer._activate_medium()
            enforcer.deactivate()
            _pump()
            left = self._live_poll_timers(enforcer)

        self.assertEqual(
            left, [],
            f"timer polling tetap aktif setelah deactivate(): {left} — "
            f"poller yang sudah tidak diaktifkan masih memanggil "
            f"raise_()/activateWindow() pada HWND yang tidak dilihat lagi",
        )

    def test_the_surviving_timer_is_still_connected_to_poll_focus(self):
        # Kontrol positif: setelah perbaikan, timer yang tersisa harus tetap
        # alatnya — kalau `deleteLater`/`disconnect` dilakukan keliru,
        # `_poll_focus` berhenti jalan sama sekali (guard fokus mati tanpa
        # jejak).
        #
        # Hitungan diambil dari patch di level KELAS, SEBELUM
        # `_activate_medium` menyambungkan signal: signal sudah memegang
        # reference ke bound method saat itu, jadi patch sesudahnya tidak
        # akan terCHNGE.
        window = _Window()
        self.addCleanup(window.hide)
        window.show()
        _pump()
        enforcer = self._enforcer(window)
        calls = []

        with mock.patch.object(enf, "get_backend", return_value=self.backend), \
             mock.patch.object(
                 enf.SecurityEnforcer, "_poll_focus",
                 autospec=True,
                 side_effect=lambda _self: calls.append(1),
             ):
            enforcer.activate()
            enforcer._activate_medium()
            enforcer._poll_timer.setInterval(10)
            _pump(90)

        self.assertTrue(
            calls,
            "tidak ada satu pun `_poll_focus` yang berjalan dalam 90 ms — "
            "timer polling fokus mati, guard fokus berhenti bekerja",
        )
        self.assertLessEqual(
            len(calls), 20,
            f"_poll_focus dipanggil {len(calls)} kali dalam 90 ms dengan "
            f"interval 10 ms — lebih dari satu timer hidup",
        )


if __name__ == "__main__":
    unittest.main()