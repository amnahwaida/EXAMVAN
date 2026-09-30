"""Lockdown GNOME tidak boleh menodai mesin tanpa jalan pulang.

Insiden yang melatarbelakangi file ini — mesin pengembang dibiarkan
ternoda berhari-hari:

  * Test suite (QT_QPA_PLATFORM=offscreen) membangun window level strict
    yang memanggil `_gnome_ws_lock()`: touchpad dimatikan, workspace
    dikunci ke 1, overlay key dikosongkan.
  * Proses test mati tanpa `deactivate()`.
  * Saat proses berikutnya membaca setelan yang ternoda, backup yang
    tersimpan justru nilai TERNAODA — nilai asli hilang selamanya.
  * Lebih parah lagi: `_gnome_ws_restore()` dari instance kosong
    menghapus file backup TANPA me-restore apa pun, jadi crash-backup
    dari sesi nyata ikut hangus.

Perbaikan yang dijaga test di sini:

  1. `_desktop_lockdown_allowed()` menolak lockdown di sesi
     non-interaktif (offscreen/minimal) dan menghormati kill-switch
     `EXAMVAN_NO_DESKTOP_LOCKDOWN` / `EXAMVAN_NO_X11_GRAB`.
  2. `_gnome_ws_lock()` MENGGABUNGkan crash-backup yang belum
     dipulihkan, bukan menimpanya dengan setelan yang sedang ternoda.
  3. `_gnome_ws_restore()` tidak pernah menghapus file backup kecuali
     semua nilai benar-benar berhasil ditulis kembali.
  4. `restore_gnome_settings()` (recovery startup) berlaku sama:
     gagal sebagian = backup tersisa untuk percobaan berikutnya.
"""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan.security import linux_backend as lb


class _BackupSandbox(unittest.TestCase):
    """Isolasi file backup ke direktori sementara."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = Path(self._tmp.name)
        for attr, value in (
            ("_GNOME_BACKUP_DIR", self.dir),
            ("_GNOME_BACKUP_FILE", self.dir / "gnome_backup.json"),
        ):
            patcher = mock.patch.object(lb, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    # -- helpers -------------------------------------------------------

    @staticmethod
    def _make_run(gets: dict[str, str], set_failures: set[str] | None = None):
        """subprocess.run palsu: `gsettings get` menjawab dari `gets`,
        `gsettings set` berhasil kecuali key-nya ada di `set_failures`."""
        set_failures = set_failures or set()

        def run(cmd, **kwargs):
            class R:
                returncode = 0
                stdout = ""
                stderr = ""

            r = R()
            if "get" in cmd:
                r.returncode = 0 if cmd[-1] in gets else 1
                r.stdout = gets.get(cmd[-1], "") + "\n"
            elif "set" in cmd:
                if cmd[-2] in set_failures:
                    r.returncode = 1
                    r.stderr = "simulated failure"
            return r

        return run

    def _write_backup(self, data: dict) -> None:
        lb._GNOME_BACKUP_FILE.write_text(json.dumps(data), encoding="utf-8")

    def _read_backup(self) -> dict:
        return json.loads(lb._GNOME_BACKUP_FILE.read_text(encoding="utf-8"))


# ---------------------------------------------------------------------------
# 1. Guard sesi non-interaktif
# ---------------------------------------------------------------------------


class DesktopLockdownGuardTestCase(unittest.TestCase):
    def test_offscreen_platform_is_refused(self):
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "offscreen"}):
            self.assertFalse(
                lb._desktop_lockdown_allowed(),
                "offscreen (test/CI) tidak boleh mengubah setelan desktop",
            )

    def test_minimal_platform_is_refused(self):
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "minimal"}):
            self.assertFalse(lb._desktop_lockdown_allowed())

    def test_xcb_platform_is_allowed(self):
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "xcb"}):
            self.assertTrue(lb._desktop_lockdown_allowed())

    def test_unset_platform_is_allowed(self):
        # Sesi ujian nyata tidak men-set QT_QPA_PLATFORM secara eksplisit.
        env = {k: v for k, v in os.environ.items() if k != "QT_QPA_PLATFORM"}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertTrue(lb._desktop_lockdown_allowed())

    def test_explicit_kill_switch_is_respected(self):
        with mock.patch.dict(
            os.environ,
            {"QT_QPA_PLATFORM": "xcb", "EXAMVAN_NO_DESKTOP_LOCKDOWN": "1"},
        ):
            self.assertFalse(lb._desktop_lockdown_allowed())

    def test_legacy_grab_kill_switch_also_blocks_lockdown(self):
        # Satu kill-switch lama cukup untuk mematikan lockdown penuh di
        # mesin developer — tidak ada kejutan sebagian.
        with mock.patch.dict(
            os.environ,
            {"QT_QPA_PLATFORM": "xcb", "EXAMVAN_NO_X11_GRAB": "1"},
        ):
            self.assertFalse(lb._desktop_lockdown_allowed())

    def test_set_strict_mode_touches_nothing_when_guarded(self):
        # Jalur penuh: set_strict_mode di platform offscreen tidak boleh
        # memanggil grab maupun gsettings.
        backend = lb.LinuxBackend()
        window = mock.Mock()
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "offscreen"}):
            with mock.patch.object(lb.subprocess, "run") as run:
                backend.set_strict_mode(window)
        run.assert_not_called()
        self.assertFalse(backend._grab_held)


# ---------------------------------------------------------------------------
# 2. Lock harus menggabungkan crash-backup, bukan menimpanya
# ---------------------------------------------------------------------------


class LockMergesStaleBackupTestCase(_BackupSandbox):
    _TOUCHPAD_KEY = "org.gnome.desktop.peripherals.touchpad:send-events"
    _WORKSPACE_KEY = "org.gnome.desktop.wm.preferences:num-workspaces"

    def test_original_values_survive_a_lock_over_polluted_settings(self):
        # Desktop sedang ternoda: touchpad 'disabled', workspace '1'.
        # Crash-backup berisi nilai ASLI sebelum ternoda.
        self._write_backup(
            {"touchpad": "'enabled'", "num_workspaces": "4"}
        )
        backend = lb.LinuxBackend()
        polluted = {
            "send-events": "'disabled'",
            "num-workspaces": "1",
            "dynamic-workspaces": "false",
            "overlay-key": "''",
            "enable-hot-corners": "false",
            "panel-run-dialog": "@as []",
            "toggle-overview": "@as []",
            "toggle-application-view": "@as []",
            "screenshot": "@as []",
            "screenshot-window": "@as []",
            "show-screenshot-ui": "@as []",
            "activate-window-menu": "@as []",
        }
        with mock.patch.object(lb, "_desktop_lockdown_allowed", return_value=True):
            with mock.patch.object(lb.subprocess, "run", self._make_run(polluted)):
                backend._gnome_ws_lock()

        self.assertEqual(
            backend._gnome_backups.get(self._TOUCHPAD_KEY),
            "'enabled'",
            "nilai asli touchpad dari crash-backup tertimpa setelan "
            "ternaoda — pemulihan mustahil",
        )
        self.assertEqual(
            backend._gnome_backups.get(self._WORKSPACE_KEY), "4"
        )
        # File persisten juga harus memuat nilai asli.
        persisted = self._read_backup()
        self.assertEqual(persisted["touchpad"], "'enabled'")
        self.assertEqual(persisted["num_workspaces"], "4")

    def test_lock_without_stale_backup_records_current_values(self):
        backend = lb.LinuxBackend()
        current = {
            "send-events": "'enabled'",
            "num-workspaces": "4",
            "dynamic-workspaces": "true",
            "overlay-key": "'Super_L'",
            "enable-hot-corners": "true",
            "panel-run-dialog": "['<Alt>F2']",
            "toggle-overview": "['<Super>s']",
            "toggle-application-view": "['<Super>a']",
            "screenshot": "['Print']",
            "screenshot-window": "['<Alt>Print']",
            "show-screenshot-ui": "['<Shift>Print']",
            "activate-window-menu": "['<Alt>space']",
        }
        with mock.patch.object(lb, "_desktop_lockdown_allowed", return_value=True):
            with mock.patch.object(lb.subprocess, "run", self._make_run(current)):
                backend._gnome_ws_lock()

        self.assertEqual(
            backend._gnome_backups.get(self._TOUCHPAD_KEY), "'enabled'"
        )
        self.assertTrue(lb._GNOME_BACKUP_FILE.exists())

    def test_lock_is_a_noop_when_guarded(self):
        # Guard menolak → tidak ada baca/tulis apa pun, backup tak tersentuh.
        self._write_backup({"touchpad": "'enabled'"})
        backend = lb.LinuxBackend()
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "offscreen"}):
            with mock.patch.object(lb.subprocess, "run") as run:
                backend._gnome_ws_lock()
        run.assert_not_called()
        self.assertEqual(self._read_backup(), {"touchpad": "'enabled'"})


# ---------------------------------------------------------------------------
# 3. Restore tidak boleh menghanguskan backup
# ---------------------------------------------------------------------------


class RestoreKeepsBackupUntilItSucceedsTestCase(_BackupSandbox):
    _TOUCHPAD_KEY = "org.gnome.desktop.peripherals.touchpad:send-events"

    def test_empty_instance_restores_from_file_instead_of_deleting_it(self):
        # Bug yang menghanguskan backup Anda: instance baru (dict kosong)
        # memanggil restore → file crash-backup terhapus tanpa restore.
        self._write_backup({"touchpad": "'enabled'", "num_workspaces": "4"})
        backend = lb.LinuxBackend()  # _gnome_backups kosong
        calls = []
        with mock.patch.object(
            lb.subprocess, "run",
            lambda cmd, **kw: calls.append(cmd) or mock.Mock(returncode=0),
        ):
            backend._gnome_ws_restore()

        set_keys = {(c[2], c[3]) for c in calls if "set" in c}
        self.assertIn(
            ("org.gnome.desktop.peripherals.touchpad", "send-events"),
            set_keys,
            "nilai crash-backup tidak dipulihkan — file hanya terhapus",
        )
        self.assertFalse(lb._GNOME_BACKUP_FILE.exists())

    def test_failed_restore_keeps_the_backup_file(self):
        # gsettings set gagal (mis. dconf lock) → nilai asli belum kembali;
        # menghapus backup di kondisi ini = kehilangan selamanya.
        backend = lb.LinuxBackend()
        backend._gnome_backups = {
            self._TOUCHPAD_KEY: "'enabled'",
            "org.gnome.desktop.wm.preferences:num-workspaces": "4",
        }
        self._write_backup(
            {"touchpad": "'enabled'", "num_workspaces": "4"}
        )
        with mock.patch.object(
            lb.subprocess, "run", self._make_run({}, set_failures={"send-events"})
        ):
            backend._gnome_ws_restore()

        self.assertTrue(
            lb._GNOME_BACKUP_FILE.exists(),
            "backup dihapus padahal sebagian restore gagal",
        )
        remaining = self._read_backup()
        self.assertEqual(remaining.get("touchpad"), "'enabled'")
        # Kunci yang sukses tidak boleh ikut tersimpan lagi.
        self.assertNotIn("num_workspaces", remaining)

    def test_successful_restore_clears_the_backup(self):
        backend = lb.LinuxBackend()
        backend._gnome_backups = {self._TOUCHPAD_KEY: "'enabled'"}
        self._write_backup({"touchpad": "'enabled'"})
        with mock.patch.object(lb.subprocess, "run", self._make_run({})):
            backend._gnome_ws_restore()
        self.assertFalse(lb._GNOME_BACKUP_FILE.exists())
        self.assertEqual(backend._gnome_backups, {})

    def test_restore_with_nothing_to_do_leaves_the_file_alone(self):
        # Tidak ada backup in-memory dan tidak ada file → early return;
        # jangan sampai jalur ini menghapus backup yang ditulis proses lain.
        backend = lb.LinuxBackend()
        with mock.patch.object(lb.subprocess, "run") as run:
            backend._gnome_ws_restore()
        run.assert_not_called()


# ---------------------------------------------------------------------------
# 4. Recovery startup berlaku sama
# ---------------------------------------------------------------------------


class CrashRecoveryTestCase(_BackupSandbox):
    def test_recovery_applies_values_then_deletes_the_file(self):
        self._write_backup({"touchpad": "'enabled'", "num_workspaces": "4"})
        calls = []
        with mock.patch.object(
            lb.subprocess, "run",
            lambda cmd, **kw: calls.append(cmd) or mock.Mock(returncode=0),
        ):
            lb.LinuxBackend.restore_gnome_settings()

        set_keys = {(c[2], c[3]) for c in calls if "set" in c}
        self.assertIn(("org.gnome.desktop.peripherals.touchpad", "send-events"), set_keys)
        self.assertIn(("org.gnome.desktop.wm.preferences", "num-workspaces"), set_keys)
        self.assertFalse(lb._GNOME_BACKUP_FILE.exists())

    def test_partial_failure_keeps_remaining_backup(self):
        self._write_backup({"touchpad": "'enabled'", "num_workspaces": "4"})
        with mock.patch.object(
            lb.subprocess, "run",
            self._make_run({}, set_failures={"send-events"}),
        ):
            lb.LinuxBackend.restore_gnome_settings()

        self.assertTrue(
            lb._GNOME_BACKUP_FILE.exists(),
            "recovery menghapus backup padahal restore touchpad gagal",
        )
        self.assertEqual(self._read_backup(), {"touchpad": "'enabled'"})

    def test_no_backup_file_means_no_gsettings_calls(self):
        with mock.patch.object(lb.subprocess, "run") as run:
            lb.LinuxBackend.restore_gnome_settings()
        run.assert_not_called()

    def test_missing_touchpad_value_falls_back_to_enabled(self):
        # Crash paling umum: touchpad mati tapi backup tidak mencatatnya
        # (mis. file lama) — recovery tetap harus menyalakannya kembali.
        self._write_backup({"num_workspaces": "4"})
        calls = []
        with mock.patch.object(
            lb.subprocess, "run",
            lambda cmd, **kw: calls.append(cmd) or mock.Mock(returncode=0),
        ):
            lb.LinuxBackend.restore_gnome_settings()

        touchpad_sets = [c for c in calls if "set" in c and c[3] == "send-events"]
        self.assertTrue(touchpad_sets, "touchpad tidak di-enable ulang")
        self.assertEqual(touchpad_sets[0][4], "enabled")

    def test_corrupt_backup_file_is_ignored_safely(self):
        lb._GNOME_BACKUP_FILE.write_text("{bukan json", encoding="utf-8")
        with mock.patch.object(lb.subprocess, "run") as run:
            lb.LinuxBackend.restore_gnome_settings()
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
