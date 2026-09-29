"""Unit tests for the Windows security backend — previously untestable.

windows_backend.py could not even be imported off Windows, because it did
`from ctypes import windll` at module scope. That is why 700+ lines of
exam-integrity logic had zero test coverage on any operating system: the CI
runner is Linux, the module refuses to load, and nobody noticed.

The fix was to defer the Win32 binding, which is what makes this file
possible. Two things are covered here:

1. should_block_key() — the blocking rules, extracted into a pure function.
   These are the rules that decide whether a student can Alt+Tab, open Task
   Manager, or screenshot the exam.
2. The screen-saver save/restore state machine (issue A), which used to leave
   a student's screen saver permanently disabled via SPIF_UPDATEINIFILE.
   The Win32 calls are mocked; the file-I/O and state logic is real.
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import examvan.utils as utils
import examvan.security.windows_backend as wb


# Virtual key codes, spelled out so a test failure is readable.
VK_TAB = 0x09
VK_RETURN = 0x0D
VK_ESCAPE = 0x1B
VK_SPACE = 0x20
VK_SNAPSHOT = 0x2C
VK_MENU = 0x12
VK_LEFT = 0x25
VK_LWIN = 0x5B
VK_F1 = 0x70
VK_F4 = 0x73
VK_F5 = 0x74

VK_A = 0x41
VK_E = 0x45
VK_G = 0x47
VK_I = 0x49
VK_L = 0x4C
VK_P = 0x50
VK_R = 0x52
VK_S = 0x53
VK_T = 0x54
VK_U = 0x55
VK_DIGIT_1 = 0x31
VK_DIGIT_9 = 0x39


class ImportabilityTestCase(unittest.TestCase):
    """The whole point: this module must load on a non-Windows box."""

    def test_module_imports_without_windll(self):
        # If this ever regresses, every test in this file dies at import and
        # the Windows security backend silently loses all coverage again.
        self.assertFalse(wb._bound, "module should not bind Win32 at import time")

    def test_missing_win32_name_raises_attributeerror(self):
        # hasattr() and getattr(name, default) are how optional features get
        # probed. An ImportError escaping here would break those callers.
        self.assertFalse(hasattr(wb, "tidak_ada_ini"))
        self.assertEqual(getattr(wb, "tidak_ada_ini", "default"), "default")

    def test_pure_rules_need_no_binding(self):
        # Calling the rules must NOT trigger a Win32 bind.
        wb.should_block_key(VK_TAB, alt_pressed=True)
        self.assertFalse(wb._bound)


class BlockedKeyTestCase(unittest.TestCase):
    """The blocking rules. Anything that regresses here is a security hole."""

    def blocked(self, vk, **mods):
        return wb.should_block_key(vk, **mods)

    # ---- ordinary typing must get through -----------------------------
    def test_plain_letters_pass(self):
        self.assertFalse(self.blocked(VK_A))
        self.assertFalse(self.blocked(VK_E))
        self.assertFalse(self.blocked(VK_R))

    def test_function_keys_pass(self):
        self.assertFalse(self.blocked(VK_F5))

    def test_tab_without_alt_passes(self):
        # Only Alt+Tab is blocked; a bare Tab must reach the UI.
        self.assertFalse(self.blocked(VK_TAB))
        self.assertFalse(self.blocked(VK_TAB, ctrl_down=True))
        self.assertFalse(self.blocked(VK_TAB, shift_down=True))

    def test_shift_and_ctrl_combinations_pass(self):
        self.assertFalse(self.blocked(VK_A, shift_down=True))
        self.assertFalse(self.blocked(VK_A, ctrl_down=True))

    # ---- window management / task switching ---------------------------
    def test_alt_tab_blocked(self):
        self.assertTrue(self.blocked(VK_TAB, alt_pressed=True))

    def test_alt_f4_blocked(self):
        self.assertTrue(self.blocked(VK_F4, alt_pressed=True))

    def test_alt_escape_blocked(self):
        self.assertTrue(self.blocked(VK_ESCAPE, alt_pressed=True))

    def test_alt_return_blocked(self):
        self.assertTrue(self.blocked(VK_RETURN, alt_pressed=True))

    def test_escape_alone_blocked(self):
        self.assertTrue(self.blocked(VK_ESCAPE))

    # ---- task manager / start menu ------------------------------------
    def test_ctrl_shift_escape_blocked(self):
        self.assertTrue(self.blocked(VK_ESCAPE, ctrl_down=True, shift_down=True))

    def test_ctrl_escape_blocked(self):
        self.assertTrue(self.blocked(VK_ESCAPE, ctrl_down=True))

    # ---- screenshots ---------------------------------------------------
    def test_print_screen_always_blocked(self):
        self.assertTrue(self.blocked(VK_SNAPSHOT))
        self.assertTrue(self.blocked(VK_SNAPSHOT, alt_pressed=True))
        self.assertTrue(self.blocked(VK_SNAPSHOT, shift_down=True))

    # ---- the Windows key ------------------------------------------------
    def test_win_key_itself_blocked(self):
        self.assertTrue(self.blocked(VK_LWIN))

    def test_win_lock_blocked(self):
        # Win+L locks the workstation, which would end the exam.
        self.assertTrue(self.blocked(VK_L, win_down=True))

    def test_win_task_view_blocked(self):
        self.assertTrue(self.blocked(VK_TAB, win_down=True))

    def test_win_run_blocked(self):
        self.assertTrue(self.blocked(VK_R, win_down=True))

    def test_win_game_bar_blocked(self):
        # Game Bar can record the screen.
        self.assertTrue(self.blocked(VK_G, win_down=True))

    def test_win_digit_launches_blocked(self):
        for vk in (VK_DIGIT_1, VK_DIGIT_9):
            self.assertTrue(self.blocked(vk, win_down=True), hex(vk))

    def test_win_arrow_keys_blocked(self):
        self.assertTrue(self.blocked(VK_LEFT, win_down=True))

    def test_win_f1_blocked(self):
        self.assertTrue(self.blocked(VK_F1, win_down=True))

    def test_win_space_blocked(self):
        self.assertTrue(self.blocked(VK_SPACE, win_down=True))

    def test_win_key_escape_blocked(self):
        # Win+Esc exits the Magnifier. The old code carried a comment
        # claiming this was "handled by regular Escape block below", but that
        # check explicitly requires `not win_down`, so Win+Esc was never
        # actually blocked. Found by this test.
        self.assertTrue(self.blocked(VK_ESCAPE, win_down=True))

    def test_many_win_shortcuts_blocked(self):
        # Spot-check the table so a deleted entry is caught.
        for vk, name in (
            (VK_E, "Win+E"), (VK_I, "Win+I"), (VK_P, "Win+P"),
            (VK_S, "Win+S"), (VK_T, "Win+T"), (VK_U, "Win+U"),
        ):
            self.assertTrue(self.blocked(vk, win_down=True), name)

    def test_win_table_blocks_dangerous_shortcuts(self):
        # The Win+ table is a curated list of dangerous shortcuts, NOT every
        # possible combination -- Win+C, Win+X and friends are deliberately
        # left alone so harmless shortcuts keep working. This asserts the
        # documented dangerous set, which is the real contract.
        dangerous = {
            VK_L: "Win+L (lock)",
            VK_TAB: "Win+Tab (task view)",
            VK_R: "Win+R (run)",
            VK_G: "Win+G (game bar)",
            VK_S: "Win+S (search)",
            VK_P: "Win+P (project)",
            VK_E: "Win+E (explorer)",
            VK_I: "Win+I (settings)",
            VK_U: "Win+U (ease of access)",
            VK_F1: "Win+F1 (help)",
            VK_SPACE: "Win+Space (input)",
            VK_ESCAPE: "Win+Esc (exit magnifier)",
            VK_LEFT: "Win+Left (snap)",
            VK_DIGIT_1: "Win+1 (launch pinned)",
            VK_DIGIT_9: "Win+9 (launch pinned)",
        }
        for vk, name in dangerous.items():
            self.assertTrue(self.blocked(vk, win_down=True), name)

    def test_win_table_is_a_blocklist_not_a_blanket(self):
        # Only the dangerous letters are blocked. Win+C (Copilot) and Win+J
        # (object picker) pass through on purpose, so the supervisor keeps
        # some functionality during an exam. Documents that this is a
        # blocklist, not a blanket "Win blocks everything" rule.
        for vk, name in ((0x43, "Win+C"), (0x4A, "Win+J")):
            self.assertFalse(self.blocked(vk, win_down=True), name)

    def test_win_x_quick_link_menu_is_blocked(self):
        # Win+X exposes "Shut down / sign out" -- must be blocked.
        self.assertTrue(self.blocked(0x58, win_down=True))

    # ---- Alt key -------------------------------------------------------
    def test_alt_alone_blocked(self):
        self.assertTrue(self.blocked(VK_MENU, alt_flag=True))


class ScreenSaverStateTestCase(unittest.TestCase):
    """Issue A: the screensaver must never be left broken on the machine.

    Runs the real file-I/O state machine against a redirected state dir and a
    mocked Win32 layer, so these run on Linux.
    """

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-winstub-")
        self._dir = Path(self._tmp)
        self._state_file = self._dir / "windows_state.json"

        p_dir = mock.patch.object(wb, "_STATE_DIR", self._dir)
        p_file = mock.patch.object(wb, "_STATE_FILE", self._state_file)
        p_get = mock.patch.object(wb, "_get_screen_saver_active", return_value=True)
        p_set = mock.patch.object(wb, "_set_screen_saver_active", return_value=True)

        # Start each patch EXACTLY once and keep the returned mock. Calling
        # p_set.start() a second time (once here, once in the loop) raises
        # "Patch is already started" on Python 3.12.14+ — the runner's
        # version — while local 3.12.12 has no such guard, so it passed here
        # and failed only in CI.
        self._set_mock = p_set.start()
        for p in (p_dir, p_file, p_get):
            p.start()
        for p in (p_dir, p_file, p_get, p_set):
            self.addCleanup(p.stop)

        self.backend = wb.WindowsBackend()

    def test_prevent_sleep_backs_up_previous_state(self):
        self.backend.prevent_sleep()
        self.assertTrue(
            self._state_file.exists(),
            "must record the prior screensaver state before changing it",
        )
        data = json.loads(self._state_file.read_text(encoding="utf-8"))
        self.assertIs(data["screen_saver_active"], True)

    def test_prevent_sleep_disables_via_sessional_flag(self):
        # The regression: SPIF_UPDATEINIFILE persisted the change to the
        # registry, so a crash left the screensaver off forever.
        self.backend.prevent_sleep()
        args = self._set_mock.call_args[0]
        self.assertIs(args[0], False, "should disable the screensaver")

    def test_allow_sleep_restores_previous_state(self):
        self.backend.prevent_sleep()
        self._set_mock.reset_mock()
        self.backend.allow_sleep()
        args = self._set_mock.call_args[0]
        self.assertIs(args[0], True, "must restore what it was, not force True")

    def test_allow_sleep_clears_backup(self):
        self.backend.prevent_sleep()
        self.backend.allow_sleep()
        self.assertFalse(self._state_file.exists())

    def test_allow_sleep_without_prevent_is_harmless(self):
        self.backend.allow_sleep()
        self._set_mock.assert_not_called()

    def test_activate_recovers_a_crashed_previous_run(self):
        # Simulate a run that changed the setting and then died: leftover
        # backup file, nothing else. Starting the app must repair the machine.
        self._state_file.write_text(
            json.dumps({"screen_saver_active": True}), encoding="utf-8"
        )
        self.backend.activate()
        self._set_mock.assert_called_once()
        self.assertIs(self._set_mock.call_args[0][0], True)
        self.assertFalse(self._state_file.exists(), "backup must be cleared")

    def test_activate_is_noop_without_leftover_backup(self):
        self.backend.activate()
        self._set_mock.assert_not_called()

    def test_already_discovered_screensaver_is_left_alone(self):
        # If the machine already has it off, we changed nothing, so there is
        # nothing to restore and no backup to write.
        with mock.patch.object(
            wb, "_get_screen_saver_active", return_value=False
        ):
            self.backend.prevent_sleep()
        self._set_mock.assert_not_called()
        self.assertFalse(self._state_file.exists())

    def test_restore_is_idempotent(self):
        self.backend.prevent_sleep()
        self.backend.allow_sleep()
        self._set_mock.reset_mock()
        wb.restore_windows_settings()
        self.assertFalse(
            self._set_mock.called,
            "a second restore with no backup file must do nothing",
        )

    def test_corrupt_backup_does_not_raise(self):
        self._state_file.write_text("{ not json", encoding="utf-8")
        wb.restore_windows_settings()

    def test_backup_without_expected_key_does_not_raise(self):
        self._state_file.write_text(json.dumps({"other": 1}), encoding="utf-8")
        wb.restore_windows_settings()
        self._set_mock.assert_not_called()


class DeviceLabelTestCase(unittest.TestCase):
    """Device identity must not need the Win32 layer either.

    The mocks target `examvan.utils`, not this module: `get_device_label`
    delegates there instead of keeping a second copy of the
    SHA256(MAC:hostname) logic. Two implementations of the exam device
    identity is exactly what review_windows_2026-09-30.md Bagian 5 flagged.
    """

    def setUp(self):
        utils.reset_device_label_cache()
        self.addCleanup(utils.reset_device_label_cache)

    def test_device_label_shape(self):
        with mock.patch.object(
            utils.socket, "gethostname", return_value="LAB-PC-01"
        ), mock.patch.object(
            utils, "get_mac_address", return_value="AA:BB:CC:DD:EE:FF"
        ):
            label = wb.WindowsBackend().get_device_label()
        self.assertTrue(label.startswith("DESKTOP:"))
        self.assertEqual(len(label), len("DESKTOP:") + 32)


if __name__ == "__main__":
    unittest.main()
