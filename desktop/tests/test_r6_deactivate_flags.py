"""`deactivate()` tidak boleh menyentuh window yang tidak pernah ia ubah.

Bug — flag window ditulis ulang + `show()` di SEMUA tier
---------------------------------------------------------
`deactivate()` selalu melakukan tiga hal yang HANYA relevan untuk strict:

    self._window.setWindowFlag(Qt.FramelessWindowHint, False)
    self._window.setWindowFlag(Qt.WindowStaysOnTopHint, False)
    self._window.show()

Di low dan medium kedua flag itu tidak pernah dipasang (`_activate_strict`
hanya jalan di strict), jadi hasilnya: `setWindowFlag` → Qt menyembunyikan
window untuk membuang HWND lama dan membuat yang baru → `show()` menampilkannya
lagi. Urutan yang sama di ketiga tier, termasuk `show()` tanpa syarat: pada
low-level exit dan setelah submit, jendela yang baru saja disembunyikan
ditampilkan SEKARANG lagi (terukur `visible=False` → `visible=True`).

Arahnya fail-open (tidak menambah proteksi salah), tapi ini kedipan yang
benar-benar terlihat oleh siswa, dan mengacaukan z-order halaman "selamat".
Jadi yang diuji di sini: di low/medium `setWindowFlag` TIDAK boleh
dipanggil dan window tersembunyi harus tetap tersembunyi; di strict flag
tetap dilepas seperti sebelumnya.

`show()` juga harus bersyarat: jangan pernah menampilkan window yang
sebelumnya tidak terlihat (hanya show kalau tadinya terlihat).
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_LOW, LEVEL_MEDIUM, LEVEL_STRICT

APP = QApplication.instance() or QApplication([])


class _RecordingBackend:
    def __init__(self) -> None:
        self.calls: list[str] = []

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

    def activate(self):
        self.calls.append("activate")

    def deactivate(self):
        self.calls.append("deactivate")

    def prevent_sleep(self):
        self.calls.append("prevent_sleep")

    def allow_sleep(self):
        self.calls.append("allow_sleep")

    def clear_clipboard(self):
        self.calls.append("clear_clipboard")

    def has_multiple_monitors(self):
        return False


class _SpyWindow(QWidget):
    """QWidget nyata (visibilitas asli) + pencatat perubahan flag."""

    def __init__(self) -> None:
        super().__init__()
        self.flag_calls: list[tuple] = []
        self.show_calls = 0

    def setWindowFlag(self, flag, on=True):  # noqa: N802 (Qt API)
        self.flag_calls.append((flag, bool(on)))
        super().setWindowFlag(flag, on)

    def show(self):
        self.show_calls += 1
        super().show()


class DeactivateWindowFlagsTestCase(unittest.TestCase):
    def _enforcer(self, level, window=None, strict=False):
        backend = _RecordingBackend()
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=backend)
        patcher.start()
        self.addCleanup(patcher.stop)
        window = window if window is not None else _SpyWindow()
        self.addCleanup(window.hide)
        self.addCleanup(window.deleteLater)
        enforcer = SecurityEnforcer(
            security_level=level, strict_mode=strict, window=window)
        enforcer.activate()
        enforcer.wait_for_clipboard_clear()
        # Pencatat dimulai setelah aktivasi: yang diuji HANYA efek deactivate.
        window.flag_calls.clear()
        return enforcer, window

    # -- low / medium: jangan sentuh sama sekali -------------------------

    def test_low_does_not_rewrite_window_flags(self):
        _enforcer, window = self._enforcer(LEVEL_LOW)
        window.show_calls = 0

        _enforcer.deactivate()

        self.assertEqual(
            window.flag_calls, [],
            "deactivate() menulis ulang flag window yang tidak pernah "
            "dipasang di low → hide + HWND-recreate + show untuk sia-sia",
        )

    def test_medium_does_not_rewrite_window_flags(self):
        enforcer, window = self._enforcer(LEVEL_MEDIUM)

        enforcer.deactivate()

        self.assertEqual(window.flag_calls, [])

    def test_low_does_not_show_a_hidden_window(self):
        enforcer, window = self._enforcer(LEVEL_LOW)
        self.assertFalse(window.isVisible())

        enforcer.deactivate()

        self.assertFalse(
            window.isVisible(),
            "deactivate() menampilkan lagi jendela yang baru saja ditutup "
            "siswa — kedipan nyata setelah dialog keluar low",
        )

    def test_medium_does_not_show_a_hidden_window(self):
        enforcer, window = self._enforcer(LEVEL_MEDIUM)

        enforcer.deactivate()

        self.assertFalse(
            window.isVisible(),
            "deactivate() menampilkan lagi jendela yang sedang disembunyikan "
            "(jalur auto-submit) — z-order halaman 'selamat' kacau",
        )
        self.assertEqual(window.show_calls, 0)

    def test_low_keeps_the_capture_protection_release(self):
        # Yang TIDAK boleh hilang: backend tetap harus dilepas di semua tier.
        enforcer, _window = self._enforcer(LEVEL_LOW)

        enforcer.deactivate()

        self.assertIn("release_capture_protection", enforcer._backend.calls)
        self.assertIn("deactivate", enforcer._backend.calls)

    # -- strict: flag tetap dilepas, tapi tidak tanpa syarat -----------

    def test_strict_clears_the_flags_it_set(self):
        enforcer, window = self._enforcer(LEVEL_STRICT)
        self.assertTrue(
            window.windowFlags() & Qt.FramelessWindowHint,
            "ujian strict harus mulai frameless — kalau tidak, test ini "
            "tidak menguji apa pun",
        )

        enforcer.deactivate()

        self.assertIn(
            (Qt.FramelessWindowHint, False), window.flag_calls,
            "flag strict tidak dilepas → halaman 'selemat' tetap frameless "
            "dan always-on-top (siswa terjebak)",
        )
        self.assertIn((Qt.WindowStaysOnTopHint, False), window.flag_calls)

    def test_strict_re_shows_the_window_only_if_it_was_visible(self):
        enforcer, window = self._enforcer(LEVEL_STRICT)
        self.assertTrue(window.isVisible())
        window.show_calls = 0

        enforcer.deactivate()

        self.assertEqual(
            window.show_calls, 1,
            "HWND strict harus dikembalikan ke siswa setelah flag dilepas",
        )

    def test_strict_does_not_show_a_window_that_was_hidden(self):
        enforcer, window = self._enforcer(LEVEL_STRICT)
        window.hide()
        self.assertFalse(window.isVisible())
        window.show_calls = 0

        enforcer.deactivate()

        self.assertFalse(
            window.isVisible(),
            "deactivate() menampilkan window yang sedang disembunyikan "
            "padahal HWND-nya sengaja dirapikan untuk halaman 'selemat'",
        )
        self.assertEqual(window.show_calls, 0)


if __name__ == "__main__":
    unittest.main()