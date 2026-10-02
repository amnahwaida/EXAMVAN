"""Countdown focus-loss TIDAK boleh dilanjutkan tanpa episode di belakangnya.

Bug — resume tanpa syarat, tanpa mengecek episode
----------------------------------------
`pause_focus_guard()` membekukan countdown saat dialog modal terbuka dan
meneruskannya lagi setelah dialog ditutup — dengan asumsi bahwa episode
focus-loss yang menjadi alasan countdown itu masih ada. Asumsi itu tidak
dijamin: `eventFilter` mengakhiri episode (`_strict_focus_episode = False`,
`_focus_episode_start = None`) setiap ada interaksi NYATA (klik/ketik) di
window ujian, dan interaksi bisa saja terjadi justru selama dialog terbuka
(jendela dialog adalah anak dari window ujian, dan beberapa jalur
mengeksekusi kode yang menutup dialog itu sendiri).

Yang terjadi kalau begitu: dialog ditutup → `_focus_timer.start()` → countdown
3 detik berjalan dengan TIDAK ADA episode di belakangnya. Enforcer pun tidak
punya `_focus_episode_start` untuk di-cap, jadi episode berikutnya bisa
memakai cap 60 detik yang sudah terpakai separuh, dan `_poll_focus` bisa
membatalkan countdown yang sedang berjalan karena tidak melihat episode —
sifat yang tidak bisa dijelaskan siapa pun dari log.

Hari ini route ini tertutup oleh pembatalan `_poll_focus` tiap 500 ms, makanya
ini latent: test di sini memanggil guard secara langsung, persis seperti
`_modal_dialog_guard()` di viewer hacerlo.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, Qt
from PyQt5.QtGui import QMouseEvent
from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_MEDIUM, LEVEL_STRICT

APP = QApplication.instance() or QApplication([])


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
        self._active = False

    def isActiveWindow(self):
        return self._active


def _real_click(enforcer) -> None:
    """Interaksi nyata yang mengakhiri episode focus-loss."""
    enforcer.eventFilter(
        enforcer._window,
        QMouseEvent(
            QEvent.MouseButtonPress,
            enforcer._window.rect().center(),
            Qt.LeftButton,
            Qt.LeftButton,
            Qt.NoModifier,
        ),
    )


class PauseFocusGuardResumeTestCase(unittest.TestCase):
    def _enforcer(self, level=LEVEL_STRICT):
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

    def _open_episode(self, enforcer) -> None:
        enforcer._on_app_state_changed(Qt.ApplicationInactive)
        self.assertTrue(
            enforcer._strict_focus_episode and enforcer._focus_timer.isActive(),
            "episode focus-loss tidak berjalan — test tidak menguji apa pun",
        )

    def test_a_live_episode_is_resumed_after_the_dialog(self):
        # Kontrol positif: jangan sampai penjagaan membekukan countdown yang
        # sah (siswa menjawab dialog konfirmasi tepat saat timer-nya berjalan).
        enforcer = self._enforcer()
        self._open_episode(enforcer)

        with enforcer.pause_focus_guard():
            self.assertFalse(enforcer._focus_timer.isActive())
            fired = []
            enforcer.auto_submit.connect(lambda: fired.append(1))
        APP.processEvents()

        self.assertFalse(fired, "auto-submit menembak dari dalam dialog")
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "countdown yang tertunda tidak dilanjutkan setelah dialog — "
            "siswa kehilangan auto-submit yang seharusnya terjadi",
        )

    def test_a_dead_episode_is_not_resumed(self):
        enforcer = self._enforcer()
        self._open_episode(enforcer)

        with enforcer.pause_focus_guard():
            _real_click(enforcer)      # interaksi nyata mengakhiri episode
            self.assertFalse(enforcer._strict_focus_episode)

        self.assertFalse(
            enforcer._focus_timer.isActive(),
            "countdown dilanjutkan tanpa episode di belakangnya — timer 3 "
            "detik berjalan tanpa `_focus_episode_start`, jadi cap 60 detik "
            "dan pembatalan polling tidak punya konteks",
        )

    def test_the_ended_episode_timestamp_stays_cleared(self):
        enforcer = self._enforcer()
        self._open_episode(enforcer)
        with enforcer.pause_focus_guard():
            _real_click(enforcer)

        self.assertIsNone(
            enforcer._focus_episode_start,
            "countdown dilanjutkan dengan cap 60 detik milik episode yang "
            "sudah selesai",
        )

    def test_medium_behaves_the_same_way(self):
        enforcer = self._enforcer(level=LEVEL_MEDIUM)
        self._open_episode(enforcer)

        with enforcer.pause_focus_guard():
            _real_click(enforcer)

        self.assertFalse(
            enforcer._focus_timer.isActive(),
            "medium masih melanjutkan countdown tanpa episode",
        )

    def test_the_guard_is_still_unpaused_even_when_nothing_is_resumed(self):
        # Penolakan resume tidak boleh meninggalkan guard dalam keadaan
        # tertangguh — kalau iya, `_poll_focus` buta selamanya.
        enforcer = self._enforcer()
        self._open_episode(enforcer)
        with enforcer.pause_focus_guard():
            _real_click(enforcer)

        self.assertFalse(enforcer._focus_guard_paused)
        self.assertEqual(enforcer._focus_guard_depth, 0)

    def test_nested_guards_still_resume_once(self):
        enforcer = self._enforcer()
        self._open_episode(enforcer)

        with enforcer.pause_focus_guard():
            with enforcer.pause_focus_guard():
                pass
            _real_click(enforcer)

        self.assertFalse(enforcer._focus_timer.isActive())


if __name__ == "__main__":
    unittest.main()