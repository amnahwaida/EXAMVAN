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

Kontrak keamanan (revisi): judul/pesan TIDAK di-interpolasi ke skrip
PowerShell — keduanya lewat environment (EXAMVAN_NOTIFY_TITLE /
EXAMVAN_NOTIFY_BODY). Teks guru (`congrats_message`) bebas isinya; kutip
tunggal yang di-escape pun tetap rapuh (backtick/`$()` masih hidup di
dalam string PowerShell). Returncode helper diperiksa: gagal = False.
"""

from __future__ import annotations

import subprocess
import unittest
from unittest import mock

from examvan import notify


def _ok_proc():
    """Popen double yang berperilaku seperti helper yang sukses (rc=0)."""
    proc = mock.Mock()
    proc.communicate.return_value = (b"", b"")
    proc.returncode = 0
    proc.poll.return_value = 0
    return proc


def _fail_proc(rc=1, stderr=b"boom"):
    proc = mock.Mock()
    proc.communicate.return_value = (b"", stderr)
    proc.returncode = rc
    proc.poll.return_value = rc
    return proc


class SendNotificationTest(unittest.TestCase):
    def _popen_ok(self):
        return mock.patch.object(
            notify.subprocess, "Popen", return_value=_ok_proc())

    def test_linux_invokes_notify_send(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                self._popen_ok() as popen:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            ok = notify.send_notification("Judul", "Pesan")
        self.assertTrue(ok)
        args = popen.call_args.args[0]
        self.assertIn("notify-send", args)
        self.assertIn("Judul", args)
        self.assertIn("Pesan", args)

    def test_windows_invokes_powershell_balloon(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                self._popen_ok() as popen:
            fake_sys.platform = "win32"
            ok = notify.send_notification("Judul", "Pesan")
        self.assertTrue(ok)
        args = popen.call_args.args[0]
        self.assertIn("powershell", args)
        self.assertIn("-Command", args)
        script = args[args.index("-Command") + 1]
        self.assertIn("NotifyIcon", script)
        self.assertIn("SystemIcons", script)

    def test_windows_title_and_body_travel_via_environment_not_script(self):
        # Judul/pesan TIDAK boleh muncul di skrip PowerShell — keduanya
        # lewat $env:, jadi teks guru tidak bisa menyuntik statement baru
        # apa pun isinya (kutip, backtick, $()).
        with mock.patch.object(notify, "sys") as fake_sys, \
                self._popen_ok() as popen:
            fake_sys.platform = "win32"
            notify.send_notification("Judul 'aneh'", "Pesan $(jahat)")
        args = popen.call_args.args[0]
        script = args[args.index("-Command") + 1]
        self.assertNotIn("Judul", script)
        self.assertNotIn("Pesan", script)
        env = popen.call_args.kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_TITLE"), "Judul 'aneh'")
        self.assertEqual(env.get("EXAMVAN_NOTIFY_BODY"), "Pesan $(jahat)")

    def test_windows_quotes_need_no_escaping_anymore(self):
        # Dulu kutip tunggal di-escape (O''Brien) karena masuk ke string
        # literal PowerShell. Sekarang lewat env verbatim — tidak ada
        # transformasi yang bisa salah.
        with mock.patch.object(notify, "sys") as fake_sys, \
                self._popen_ok() as popen:
            fake_sys.platform = "win32"
            notify.send_notification("O'Brien", "It's broken")
        env = popen.call_args.kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_TITLE"), "O'Brien")
        self.assertEqual(env.get("EXAMVAN_NOTIFY_BODY"), "It's broken")

    def test_windows_critical_uses_warning_icon(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                self._popen_ok() as popen:
            fake_sys.platform = "win32"
            notify.send_notification("J", "M", urgency="critical")
        env = popen.call_args.kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_ICON"), "Warning")

    def test_windows_normal_uses_information_icon(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                self._popen_ok() as popen:
            fake_sys.platform = "win32"
            notify.send_notification("J", "M", urgency="normal")
        env = popen.call_args.kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_ICON"), "Information")

    def test_missing_notify_send_returns_false(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = None
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_subprocess_failure_does_not_raise(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify.subprocess, "Popen",
                                  side_effect=OSError("notify-send crashed")):
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_windows_powershell_failure_does_not_raise(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify.subprocess, "Popen",
                                  side_effect=OSError("powershell blocked")):
            fake_sys.platform = "win32"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_linux_nonzero_returncode_returns_false(self):
        # Helper ADA tapi gagal (mis. daemon notifikasi mati): rc != 0
        # harus False, bukan True.
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify.subprocess, "Popen",
                                  return_value=_fail_proc(rc=1)):
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_windows_nonzero_returncode_returns_false(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify.subprocess, "Popen",
                                  return_value=_fail_proc(rc=1)):
            fake_sys.platform = "win32"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_linux_timeout_returns_false_not_raises(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                mock.patch.object(notify.subprocess, "Popen") as popen:
            proc = mock.Mock()
            proc.poll.return_value = None
            proc.communicate.side_effect = [
                subprocess.TimeoutExpired("notify-send", 10),
                (b"", b""),
            ]
            proc.returncode = 1
            popen.return_value = proc
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            self.assertFalse(notify.send_notification("Judul", "Pesan"))

    def test_urgency_passed_through(self):
        with mock.patch.object(notify, "sys") as fake_sys, \
                mock.patch.object(notify, "shutil") as fake_shutil, \
                self._popen_ok() as popen:
            fake_sys.platform = "linux"
            fake_shutil.which.return_value = "/usr/bin/notify-send"
            notify.send_notification("J", "M", urgency="critical")
        args = popen.call_args.args[0]
        self.assertIn("--urgency=critical", args)

    def test_previous_notification_is_killed_before_a_new_one(self):
        # Balloon tip lama tidak boleh bertumpuk: notifikasi baru
        # menghentikan proses sebelumnya dulu (best-effort).
        old = mock.Mock()
        old.poll.return_value = None
        notify._last_proc = old
        try:
            with mock.patch.object(notify, "sys") as fake_sys, \
                    mock.patch.object(notify, "shutil") as fake_shutil, \
                    self._popen_ok():
                fake_sys.platform = "linux"
                fake_shutil.which.return_value = "/usr/bin/notify-send"
                notify.send_notification("J", "M")
        finally:
            notify._last_proc = None
        old.terminate.assert_called_once()


if __name__ == "__main__":
    unittest.main()
