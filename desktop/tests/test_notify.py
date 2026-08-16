"""Unit tests for examvan.notify — desktop notification helper.

Covers the desktop↔Android consistency fix (Agustus 2026):
- auto-submit now runs in the BACKGROUND after the window closes immediately
  (mirror Android autoSubmitAndExit), so the result is reported via a
  system notification instead of a modal dialog inside the locked window.
- Linux: notify-send. Windows: PowerShell NotifyIcon balloon tip (bawaan
  .NET Framework, tanpa dependency baru) — hasil auto-submit tidak lagi
  "hilang" di Windows.
- The helper must be best-effort: missing helper, PowerShell failure, or any
  failure must never raise — jawaban tetap aman di disk untuk recovery.
"""

from __future__ import annotations

import unittest
from unittest import mock

from examvan import notify


class SendNotificationTest(unittest.TestCase):
    def test_linux_invokes_notify_send(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            ok = notify.send_notification("Judul", "Pesan")
        self.assertTrue(ok)
        args = fake_sub.run.call_args.args[0]
        self.assertIn("notify-send", args)
        self.assertIn("Judul", args)
        self.assertIn("Pesan", args)

    def test_windows_invokes_powershell_balloon(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "win32"
            ok = notify.send_notification("Judul", "Pesan")
        self.assertTrue(ok)
        args = fake_sub.run.call_args.args[0]
        self.assertIn("powershell", args)
        self.assertIn("-Command", args)
        script = args[args.index("-Command") + 1]
        self.assertIn("NotifyIcon", script)
        self.assertIn("Judul", script)
        self.assertIn("Pesan", script)
        self.assertIn("SystemIcons]::Information", script)

    def test_windows_critical_uses_warning_icon(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "win32"
            notify.send_notification("J", "M", urgency="critical")
        args = fake_sub.run.call_args.args[0]
        script = args[args.index("-Command") + 1]
        self.assertIn("SystemIcons]::Warning", script)

    def test_windows_escapes_single_quotes(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "win32"
            notify.send_notification("O'Brien", "It's broken")
        args = fake_sub.run.call_args.args[0]
        script = args[args.index("-Command") + 1]
        self.assertIn("O''Brien", script)
        self.assertIn("It''s broken", script)

    def test_missing_notify_send_returns_false(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = None
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_subprocess_failure_does_not_raise(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            fake_sub.run.side_effect = OSError("notify-send crashed")
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_windows_powershell_failure_does_not_raise(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "win32"
            fake_sub.run.side_effect = OSError("powershell blocked")
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_urgency_passed_through(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify, "subprocess") as fake_sub:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            notify.send_notification("J", "M", urgency="critical")
        args = fake_sub.run.call_args.args[0]
        self.assertIn("--urgency=critical", args)


if __name__ == "__main__":
    unittest.main()
