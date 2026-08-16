"""Unit tests for examvan.notify — desktop notification helper.

Covers the desktop↔Android consistency fix (Agustus 2026):
- auto-submit now runs in the BACKGROUND after the window closes immediately
  (mirror Android autoSubmitAndExit), so the result is reported via a
  system notification instead of a modal dialog inside the locked window.
- The helper must be best-effort: missing notify-send, Windows, or any
  failure must never raise.
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

    def test_windows_is_noop_but_does_not_raise(self):
        with mock.patch.object(notify, "sys") as fake_sys:
            fake_sys.platform = "win32"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

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
