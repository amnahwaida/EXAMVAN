"""M1 (MEDIUM) — kedalaman focus-guard tidak boleh jatuh ke −1.

Bug
---
`pause_focus_guard()` menghitung kedalaman dengan integer:

    self._focus_guard_depth += 1
    if self._focus_guard_depth == 1: ... stop timer, paused = True
    ...
    self._focus_guard_depth -= 1
    if self._focus_guard_depth == 0: paused = False; maybe resume

`serta` `deactivate()` melakukan `self._focus_guard_depth = 0` tanpa syarat.
Satu `deactivate()` DI DALAM guard yang masih terbuka (jalur nyata:
`_on_auto_submit_done` melepas lockdown, dan itu bisa terjadi sementara
dialog modal masih tersimpan di nested event loop — mis. event
`exam_terminated` tiba tepat ketika siswa sedang membaca "Yakin ingin
mengumpulkan?") membuat `__exit__` yang tertunda mengurangi dari 0:

    guard masuk   -> depth = 1, paused = True
    deactivate()  -> depth = 0   (dibuat ulang, bukan dikurangi)
    guard keluar  -> depth = -1, dan `== 0` tidak pernah terpenuhi lagi

Setelah itu `depth == 1` dan `depth == 0` tidak akan pernah terjadi lagi,
jadi SETIAP dialog modal berikutnya di sesi itu berjalan TANPA
penangguhan: countdown 3 detik focus-loss berjalan di belakangnya dan bisa
menembak auto-submit sementara siswa masih membaca pertanyaannya.

Diperparah oleh `_focus_guard_resume` yang tidak ikut di-reset: dialog
berikutnya akan melanjutkan countdown yang sudah tidak berlaku.
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


class FocusGuardDepthTestCase(unittest.TestCase):
    def _enforcer(self, level=LEVEL_MEDIUM):
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=_FakeBackend()
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        window = _FakeWindow()
        self.addCleanup(window.deleteLater)
        enforcer = SecurityEnforcer(
            security_level=level, strict_mode=False, window=window
        )
        self.addCleanup(enforcer.deactivate)
        enforcer.activate()
        return enforcer

    def _guard_around_a_live_countdown(self, enforcer):
        """Dialog berikutnya dengan countdown yang sedang hidup."""
        enforcer._focus_timer.start()
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "countdown focus-loss tidak berjalan — test tidak menguji apa pun",
        )
        with enforcer.pause_focus_guard():
            paused = enforcer._focus_guard_paused
            timer_stopped = not enforcer._focus_timer.isActive()
        return paused, timer_stopped

    # -- inti bug ----------------------------------------------------------

    def test_deactivate_inside_a_guard_keeps_the_next_dialog_safe(self):
        enforcer = self._enforcer()
        guard = enforcer.pause_focus_guard()
        guard.__enter__()
        # `deactivate()` di tengah dialog: lockdown dilepas (mis. halaman
        # selesai dibangun) sementara `__exit__`-nya masih tertunda.
        enforcer.deactivate()
        guard.__exit__(None, None, None)

        self.assertGreaterEqual(
            enforcer._focus_guard_depth, 0,
            "kedalaman focus-guard turun ke %r — guard berikutnya tidak akan "
            "pernah melihat depth 1 lagi, jadi tidak ada dialog modal yang "
            "ditanggunkan" % (enforcer._focus_guard_depth,),
        )
        paused, timer_stopped = self._guard_around_a_live_countdown(enforcer)
        self.assertTrue(
            paused,
            "dialog kedua berjalan tanpa penangguhan: countdown 3 detik "
            "menembak auto-submit di belakang dialog yang sedang dibaca "
            "siswa (guard depth sudah rusak oleh deactivate di dalam guard)",
        )
        self.assertTrue(
            timer_stopped,
            "countdown tidak dibekukan selama dialog — auto-submit bisa "
            "menembak sementara siswa masih membaca",
        )

    def test_nested_guards_survive_a_deactivate_in_the_middle(self):
        enforcer = self._enforcer()
        outer = enforcer.pause_focus_guard()
        outer.__enter__()
        inner = enforcer.pause_focus_guard()
        inner.__enter__()
        enforcer.deactivate()
        inner.__exit__(None, None, None)
        outer.__exit__(None, None, None)
        self.assertGreaterEqual(enforcer._focus_guard_depth, 0)
        paused, timer_stopped = self._guard_around_a_live_countdown(enforcer)
        self.assertTrue(
            paused and timer_stopped,
            "guard bersarang yang rusak oleh deactivate di tengahnya — "
            "dialog berikutnya tidak ditanggunkan",
        )

    # -- `_focus_guard_resume` --------------------------------------------

    def test_deactivate_inside_a_guard_clears_the_resume_flag(self):
        enforcer = self._enforcer()
        # Episode focus-loss yang sedang hidup → `_focus_guard_resume` True.
        enforcer._on_app_state_changed(Qt.ApplicationInactive)
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "countdown tidak berjalan — test tidak menguji apa pun",
        )

        guard = enforcer.pause_focus_guard()
        guard.__enter__()
        self.assertTrue(
            enforcer._focus_guard_resume,
            "countdown yang hidup tidak di-flag untuk dilanjutkan — test "
            "tidak menguji apa pun",
        )
        enforcer.deactivate()
        self.assertTrue(
            enforcer._focus_guard_abandoned,
            "guard yang masih terbuka tidak ditandai abandoned — "
            "`__exit__` yang telat masih boleh menghidupkan countdown di "
            "enforcer yang sudah tidak aktif",
        )
        self.assertFalse(
            enforcer._focus_guard_resume,
            "`_focus_guard_resume` masih True setelah deactivate — dialog "
            "berikutnya akan melanjutkan countdown yang sudah tidak berlaku",
        )
        guard.__exit__(None, None, None)
        self.assertFalse(
            enforcer._focus_timer.isActive(),
            "countdown yang sudah dibebaskan di-resume ulang setelah "
            "deactivate — tidak ada episode pun yang membenarkannya",
        )

    def test_strict_behaves_the_same_way(self):
        enforcer = self._enforcer(level=LEVEL_STRICT)
        guard = enforcer.pause_focus_guard()
        guard.__enter__()
        enforcer.deactivate()
        guard.__exit__(None, None, None)
        self.assertGreaterEqual(enforcer._focus_guard_depth, 0)
        paused, timer_stopped = self._guard_around_a_live_countdown(enforcer)
        self.assertTrue(paused and timer_stopped)


if __name__ == "__main__":
    unittest.main()