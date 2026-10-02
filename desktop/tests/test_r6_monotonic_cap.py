"""Cap 60 detik episode focus-loss harus pakai monotonic, bukan wall clock.

Bug — `time.time()` untuk cap yang memutuskan auto-submit
------------------------------------------------------
`enforcer.py` menyimpan `_focus_episode_start` dengan `time.time()` dan
memeriksa `(time.time() - start) > 60` di `_poll_focus`. Semua interval lain
di klien memakai `time.monotonic()` (`timer.py`, `api.py`, `ws.py`) karena
wall clock bisa DITERAKHIR: sinkronisasi NTP, langkah DST, atau koreksi
manual jam mesin melompat beberapa menit ke depan atau ke belakang.

Dua arah kerusakannya sama-sama salah:

  * lompatan ke DEPAN → cap 60 detik "terpakai" dalam satu tick polling,
    auto-submit dipaksa pada siswa yang masih mengerjakan soal, dan jawaban
    terkirim tanpa siswa menyentuh apa pun;
  * lompatan ke BELAKANG → cap praktis tidak pernah tercapai, jadi sabuk
    pengaman H3/F-1 (episode yang tidak menyelamatkan jawaban dari submit
    dipaksa setelah 60 detik) mati diam-diam untuk sisa ujian.

Kedua jam dipalsukan, jadi lompatan bisa dibuat tanpa menunggu waktu
sungguhan: lompatan wall clock tidak boleh mengubah keputusan, sedangkan
majunya jam monotonic harus tetap bertindak.
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

APP = QApplication.instance() or QApplication([])

# Dua jam yang sengaja dibuat jauh berbeda supaya test bisa Membuktikannya:
# monotonic dikunci pada 1000, wall clock pada 1_800_000_000 (tahun 2027).
_WALL_START = 1_800_000_000.0
_MONO_START = 1000.0


class _FakeBackend:
    def set_capture_protection(self, window):
        pass

    def confine_pointer(self, window):
        pass

    def activate(self):
        pass

    def deactivate(self):
        pass

    def prevent_sleep(self):
        pass

    def allow_sleep(self):
        pass

    def clear_clipboard(self):
        pass

    def has_multiple_monitors(self):
        return False

    def set_strict_mode(self, window):
        pass

    def release_strict_mode(self, window):
        pass

    def release_capture_protection(self, window):
        pass

    def release_pointer(self):
        pass


class _FakeWindow(QWidget):
    def __init__(self) -> None:
        super().__init__()
        self._active = True

    def isActiveWindow(self):
        return self._active


class _Clocks:
    """Dua jam yang bisa digeser secara terpisah oleh test."""

    def __init__(self) -> None:
        self.wall = _WALL_START
        self.mono = _MONO_START

    def time(self):
        return self.wall

    def monotonic(self):
        return self.mono

    def advance_mono(self, seconds: float) -> None:
        self.mono += seconds

    def jump_wall(self, seconds: float) -> None:
        self.wall += seconds

    def patch(self):
        return mock.patch.multiple(
            "time", time=mock.Mock(side_effect=self.time),
            monotonic=mock.Mock(side_effect=self.monotonic),
        )


class MonotonicFocusEpisodeCapTestCase(unittest.TestCase):
    def _enforcer(self, level="strict"):
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=_FakeBackend())
        patcher.start()
        self.addCleanup(patcher.stop)
        window = _FakeWindow()
        self.addCleanup(window.deleteLater)
        enforcer = SecurityEnforcer(
            security_level=level, strict_mode=True, window=window)
        self.addCleanup(enforcer.deactivate)
        enforcer.activate()
        return enforcer

    def _episode_with_cap_decision(self, clocks):
        """Buka episode focus-loss, lalu satu tick polling. Balikan: (fired)."""
        enforcer = self._enforcer()
        fired = []
        enforcer.auto_submit.connect(lambda: fired.append(True))
        enforcer._on_app_state_changed(Qt.ApplicationInactive)
        enforcer._window._active = True
        enforcer._poll_focus()
        return enforcer, fired

    def test_the_episode_start_is_recorded_from_the_monotonic_clock(self):
        clocks = _Clocks()
        with clocks.patch():
            enforcer, _fired = self._episode_with_cap_decision(clocks)

        self.assertEqual(
            enforcer._focus_episode_start, _MONO_START,
            "awal episode focus-loss dicatat dari jam DINDING: langkah NTP "
            "atau DST menggeser cap 60 detik sesaat-enaknya",
        )

    def test_a_forward_wall_clock_jump_does_not_expire_the_cap(self):
        clocks = _Clocks()
        with clocks.patch():
            enforcer, fired = self._episode_with_cap_decision(clocks)
            # NTP mengoreksi jam mesin satu jam ke depan, monotonic tidak
            # bergerak (tidak ada waktu yang benar-benar lewat untuk cap).
            clocks.jump_wall(3600.0)
            enforcer._poll_focus()

        self.assertEqual(
            fired, [],
            "lompatan jam dinding 1 jam membuat cap 60 detik habis dalam satu "
            "tick → auto-submit memaksa jawaban siswa yang masih bekerja",
        )
        self.assertTrue(enforcer._strict_focus_episode)

    def test_a_backward_wall_clock_jump_does_not_freeze_the_cap(self):
        clocks = _Clocks()
        with clocks.patch():
            enforcer, fired = self._episode_with_cap_decision(clocks)
            clocks.jump_wall(-3600.0)
            # Maju monotonic saja 61 detik: cap HARUS tetap berlaku meski
            # jam dinding mundur satu jam.
            clocks.advance_mono(61.0)
            enforcer._poll_focus()

        self.assertEqual(
            fired, [True],
            "cap 60 detik tidak aktif setelah lompatan jam dinding — "
            "sabuk pengaman episode focus-loss mati diam-diam",
        )

    def test_sixty_seconds_of_real_time_still_forces_the_submit(self):
        clocks = _Clocks()
        with clocks.patch():
            enforcer, fired = self._episode_with_cap_decision(clocks)
            clocks.advance_mono(61.0)
            enforcer._poll_focus()

        self.assertEqual(
            fired, [True],
            "cap 60 detik tidak lagi memaksa auto-submit — polling 500 ms "
            "yang menjaga dari episode focus-loss yang macet",
        )
        self.assertFalse(enforcer._strict_focus_episode)
        self.assertIsNone(enforcer._focus_episode_start)

    def test_fifty_nine_seconds_does_not_force_the_submit(self):
        clocks = _Clocks()
        with clocks.patch():
            enforcer, fired = self._episode_with_cap_decision(clocks)
            clocks.advance_mono(59.0)
            enforcer._poll_focus()

        self.assertEqual(fired, [])

    def test_the_medium_branch_also_uses_the_monotonic_clock(self):
        # Cap ini dihitung di `_poll_focus` untuk semua tier; hanya pencatatan
        # awal episode yang berbeda cabangnya.
        clocks = _Clocks()
        with clocks.patch():
            enforcer = self._enforcer(level="medium")
            fired = []
            enforcer.auto_submit.connect(lambda: fired.append(True))
            enforcer._window._active = False
            enforcer._poll_focus()          # episode baru + countdown
            self.assertEqual(enforcer._focus_episode_start, _MONO_START)
            clocks.jump_wall(3600.0)
            clocks.advance_mono(61.0)
            enforcer._poll_focus()

        self.assertEqual(fired, [True])


if __name__ == "__main__":
    unittest.main()