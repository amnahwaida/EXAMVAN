"""Kunci perbaikan enforcement R4 (H3/F-1, M-1..M-4, L-1, L-4, L-5).

Setiap kelas mengunci SATU item supaya regresi tertangkap di sisi yang
mengubahnya. Gaya mengikuti suite yang ada: komentar Indonesia, backend
palsu supaya jalan di Linux tanpa windll/X server.
"""

from __future__ import annotations

import os
import time
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, Qt
from PyQt5.QtGui import QMouseEvent
from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import DEFAULT_LEVEL, LEVEL_MEDIUM
import examvan.security.windows_backend as wb
import examvan.security.linux_backend as lb

APP = QApplication.instance() or QApplication([])


class _FakeBackend:
    """Backend minimal untuk mesin guard fokus enforcer."""

    def __init__(self):
        self.calls = []
        self.fail_capture = False

    def set_capture_protection(self, window):
        self.calls.append("set_capture_protection")
        if self.fail_capture:
            raise RuntimeError("compositor menolak")

    def confine_pointer(self, window):
        self.calls.append("confine_pointer")

    # Tidak dipakai jalur ini, tapi activate() menyentuhnya.
    def activate(self):
        pass

    def prevent_sleep(self):
        pass

    def clear_clipboard(self):
        pass

    def has_multiple_monitors(self):
        return False

    def release_strict_mode(self, window):
        pass

    def release_capture_protection(self, window):
        pass

    def release_pointer(self):
        pass

    def allow_sleep(self):
        pass

    def deactivate(self):
        pass


class _FakeWindow(QWidget):
    """QWidget nyata (QTimer/eventFilter butuh itu) dengan fokus manual."""

    def __init__(self):
        super().__init__()
        self._active = True

    def isActiveWindow(self):
        return self._active


def _enforcer(level="medium", strict=False, backend=None):
    backend = backend if backend is not None else _FakeBackend()
    patcher = mock.patch.object(
        enforcer_mod, "get_backend", return_value=backend
    )
    patcher.start()
    win = _FakeWindow()
    e = SecurityEnforcer(security_level=level, strict_mode=strict, window=win)
    e._active = True
    e._patcher = patcher
    e._fake_backend = backend
    return e


def _stop(e):
    e._patcher.stop()
    e._window.deleteLater()
    e.deleteLater()


# ---------------------------------------------------------------------------
# M-1 — default fail-secure
# ---------------------------------------------------------------------------


class DefaultLevelTestCase(unittest.TestCase):
    def test_default_bukan_low(self):
        b = _FakeBackend()
        with mock.patch.object(enforcer_mod, "get_backend", return_value=b):
            e = SecurityEnforcer(window=_FakeWindow())
        try:
            self.assertEqual(e.level, LEVEL_MEDIUM)
            self.assertEqual(e.level, DEFAULT_LEVEL)
        finally:
            e._window.deleteLater()
            e.deleteLater()


# ---------------------------------------------------------------------------
# H3/F-1 — episode focus-loss jalur sinyal + cap 60 detik
# ---------------------------------------------------------------------------


class StrictSignalEpisodeTestCase(unittest.TestCase):
    def test_inactive_menandai_episode_dan_mulai_countdown(self):
        e = _enforcer("strict", strict=True)
        try:
            e._on_app_state_changed(Qt.ApplicationInactive)
            self.assertTrue(e._strict_focus_episode)
            self.assertIsNotNone(e._focus_episode_start)
            self.assertTrue(e._focus_timer.isActive())
        finally:
            _stop(e)

    def test_reaktivasi_sendiri_tidak_membatalkan_episode_strict(self):
        e = _enforcer("strict", strict=True)
        try:
            e._on_app_state_changed(Qt.ApplicationInactive)
            e._on_app_state_changed(Qt.ApplicationActive)
            self.assertTrue(
                e._focus_timer.isActive(),
                "activateWindow() 500 ms membatalkan countdown-nya sendiri",
            )
        finally:
            _stop(e)

    def test_interaksi_nyata_mengakhiri_episode(self):
        e = _enforcer("strict", strict=True)
        try:
            e._on_app_state_changed(Qt.ApplicationInactive)
            ev = QMouseEvent(
                QEvent.MouseButtonPress,
                e._window.rect().center(),
                Qt.LeftButton,
                Qt.LeftButton,
                Qt.NoModifier,
            )
            e.eventFilter(e._window, ev)
            self.assertFalse(e._strict_focus_episode)
            self.assertIsNone(e._focus_episode_start)
            # Setelah episode berakhir, kembalinya fokus boleh membatalkan.
            e._on_app_state_changed(Qt.ApplicationActive)
            self.assertFalse(e._focus_timer.isActive())
        finally:
            _stop(e)

    def test_cap_60_detik_memaksa_submit(self):
        e = _enforcer("strict", strict=True)
        try:
            fired = []
            e.auto_submit.connect(lambda: fired.append(True))
            e._on_app_state_changed(Qt.ApplicationInactive)
            # Jam monotonic: episode focus-loss dicatat/dihitung dengan
            # `time.monotonic()` (bukan `time.time()`) supaya langkah jam
            # mesin tidak bisa menggeser cap ini — lihat
            # tests/test_r6_monotonic_cap.py.
            e._focus_episode_start = time.monotonic() - 61.0
            e._window._active = True
            e._poll_focus()
            self.assertEqual(fired, [True])
            self.assertFalse(e._strict_focus_episode)
        finally:
            _stop(e)

    def test_belum_60_detik_tidak_dipaksa(self):
        e = _enforcer("strict", strict=True)
        try:
            fired = []
            e.auto_submit.connect(lambda: fired.append(True))
            e._on_app_state_changed(Qt.ApplicationInactive)
            e._focus_episode_start = time.monotonic()
            e._window._active = False
            e._poll_focus()
            self.assertEqual(fired, [])
        finally:
            _stop(e)


# ---------------------------------------------------------------------------
# L-1 — budget deferral hanya reset saat episode baru
# ---------------------------------------------------------------------------


class DeferBudgetTestCase(unittest.TestCase):
    def test_inactive_berulang_tidak_mereset_budget(self):
        e = _enforcer("medium")
        try:
            e._on_app_state_changed(Qt.ApplicationInactive)
            e._focus_defer_count = 2  # simulasi dua tunda popup
            e._on_app_state_changed(Qt.ApplicationInactive)
            self.assertEqual(e._focus_defer_count, 2)
            self.assertTrue(e._focus_timer.isActive())
        finally:
            _stop(e)

    def test_episode_baru_mereset_budget(self):
        e = _enforcer("medium")
        try:
            e._on_app_state_changed(Qt.ApplicationInactive)
            e._focus_defer_count = 2
            e._on_focus_timeout()  # tanpa popup -> submit + reset
            self.assertEqual(e._focus_defer_count, 0)
            # Episode berikutnya mulai dari nol lagi.
            e._on_app_state_changed(Qt.ApplicationInactive)
            self.assertEqual(e._focus_defer_count, 0)
        finally:
            _stop(e)


# ---------------------------------------------------------------------------
# M-2 — reassert capture protection
# ---------------------------------------------------------------------------


class ReassertCaptureTestCase(unittest.TestCase):
    def test_reassert_memanggil_backend_saat_aktif(self):
        e = _enforcer("medium")
        try:
            e.reassert_capture_protection()
            self.assertIn(
                "set_capture_protection", e._fake_backend.calls
            )
        finally:
            _stop(e)

    def test_reassert_diam_saat_tidak_aktif_atau_tanpa_window(self):
        e = _enforcer("medium")
        win = e._window
        try:
            e._active = False
            e.reassert_capture_protection()
            e._active = True
            e._window = None
            e.reassert_capture_protection()
            self.assertNotIn(
                "set_capture_protection", e._fake_backend.calls
            )
        finally:
            e._window = win
            _stop(e)

    def test_reassert_menelan_gagal_backend(self):
        e = _enforcer("medium")
        try:
            e._fake_backend.fail_capture = True
            e.reassert_capture_protection()  # tidak boleh raise
        finally:
            _stop(e)

    def test_poll_strict_memasang_ulang_capture(self):
        e = _enforcer("strict", strict=True)
        try:
            e._window._active = True
            e._poll_focus()
            self.assertIn("confine_pointer", e._fake_backend.calls)
            self.assertIn(
                "set_capture_protection", e._fake_backend.calls
            )
        finally:
            _stop(e)


# ---------------------------------------------------------------------------
# L-4 — deactivate mereset guard fokus
# ---------------------------------------------------------------------------


class DeactivateResetsGuardTestCase(unittest.TestCase):
    def test_deactivate_mereset_pause_dan_depth(self):
        e = _enforcer("medium")
        try:
            e._focus_guard_paused = True
            e._focus_guard_depth = 3
            e.deactivate()
            self.assertFalse(e._focus_guard_paused)
            self.assertEqual(e._focus_guard_depth, 0)
        finally:
            _stop(e)


# ---------------------------------------------------------------------------
# M-3 — _stop_keyboard_hook mengembalikan bool
# ---------------------------------------------------------------------------


class StopHookBoolTestCase(unittest.TestCase):
    def test_tidak_ada_thread_dianggap_sudah_berhenti(self):
        backend = wb.WindowsBackend()
        with mock.patch.object(wb, "_hook_thread_id", None), mock.patch.object(
            wb, "_hook_thread", None
        ):
            self.assertTrue(backend._stop_keyboard_hook())

    def test_thread_hidup_mengembalikan_false(self):
        backend = wb.WindowsBackend()
        thread = mock.Mock()
        thread.is_alive.return_value = True
        with mock.patch.object(wb, "_hook_thread_id", 1234), mock.patch.object(
            wb, "_hook_thread", thread
        ), mock.patch.object(wb, "_PostThreadMessageW", create=True):
            self.assertFalse(backend._stop_keyboard_hook())

    def test_release_melepas_flag_hanya_bila_stop_berhasil(self):
        backend = wb.WindowsBackend()
        backend._hook_installed = True
        with mock.patch.object(
            backend, "_stop_keyboard_hook", return_value=False
        ):
            backend.release_strict_mode(None)
            self.assertTrue(backend._hook_installed)
        with mock.patch.object(
            backend, "_stop_keyboard_hook", return_value=True
        ):
            backend.release_strict_mode(None)
            self.assertFalse(backend._hook_installed)


# ---------------------------------------------------------------------------
# M-4 — grab pointer Linux independen
# ---------------------------------------------------------------------------


class LinuxPointerGrabTestCase(unittest.TestCase):
    def test_pointer_dilepas_walau_keyboard_gagal(self):
        backend = lb.LinuxBackend()
        backend._grab_held = False
        backend._pointer_grabbed = True
        with mock.patch(
            "examvan.security.x11.ungrab_keyboard"
        ) as uk, mock.patch(
            "examvan.security.x11.ungrab_pointer"
        ) as up, mock.patch.object(
            backend, "_gnome_ws_restore"
        ):
            backend.release_strict_mode(mock.Mock())
        uk.assert_not_called()
        up.assert_called_once()
        self.assertFalse(backend._pointer_grabbed)

    def test_keduanya_dilepas_dan_flag_nol(self):
        backend = lb.LinuxBackend()
        backend._grab_held = True
        backend._pointer_grabbed = True
        with mock.patch(
            "examvan.security.x11.ungrab_keyboard"
        ), mock.patch("examvan.security.x11.ungrab_pointer"), mock.patch.object(
            backend, "_gnome_ws_restore"
        ):
            backend.release_strict_mode(mock.Mock())
        self.assertFalse(backend._grab_held)
        self.assertFalse(backend._pointer_grabbed)

    def test_confine_pointer_override_memanggil_grab(self):
        backend = lb.LinuxBackend()
        win = mock.Mock()
        with mock.patch(
            "examvan.security.x11.grab_pointer", return_value=True
        ) as grab:
            backend.confine_pointer(win)
        grab.assert_called_once_with(win)
        self.assertTrue(backend._pointer_grabbed)

    def test_confine_pointer_none_tidak_grab(self):
        backend = lb.LinuxBackend()
        with mock.patch(
            "examvan.security.x11.grab_pointer"
        ) as grab:
            backend.confine_pointer(None)
        grab.assert_not_called()

    def test_release_pointer_override_memanggil_ungrab(self):
        backend = lb.LinuxBackend()
        backend._pointer_grabbed = True
        with mock.patch("examvan.security.x11.ungrab_pointer") as up:
            backend.release_pointer()
        up.assert_called_once()
        self.assertFalse(backend._pointer_grabbed)


# ---------------------------------------------------------------------------
# L-5 — blokir Ctrl+S / Ctrl+P / F12, biarkan Ctrl+C/V/Delete
# ---------------------------------------------------------------------------


class StrictKeyCoverageTestCase(unittest.TestCase):
    def test_ctrl_s_dan_ctrl_p_diblokir(self):
        self.assertTrue(wb.should_block_key(0x53, ctrl_down=True))
        self.assertTrue(wb.should_block_key(0x50, ctrl_down=True))

    def test_f_key_bare_tetap_lolos(self):
        # Native Qt, bukan browser: tidak ada DevTools di F12, jadi
        # memblokir F12 hanya memutus jaminan bahwa F-key biasa sampai
        # ke PDF viewer.
        for vk in (0x70, 0x75, 0x7B):
            self.assertFalse(wb.should_block_key(vk), hex(vk))

    def test_ctrl_c_v_dan_delete_lolos(self):
        # Kunci penyuntingan teks yang sah di kolom jawaban; mitigasinya
        # penyapu clipboard + wipe PrintScreen, bukan blokir kunci.
        self.assertFalse(wb.should_block_key(0x43, ctrl_down=True))
        self.assertFalse(wb.should_block_key(0x56, ctrl_down=True))
        self.assertFalse(wb.should_block_key(0x2E))
        self.assertFalse(wb.should_block_key(0x2E, ctrl_down=True))


if __name__ == "__main__":
    unittest.main()
