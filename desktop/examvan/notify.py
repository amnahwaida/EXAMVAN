"""Desktop notification helper (mirror Android auto-submit notification).

Digunakan untuk melaporkan hasil auto-submit BACKGROUND setelah window
ujian ditutup segera (mirror Android `showAutoSubmitNotification`):
siswa tidak lagi menunggu hasil jaringan di layar terkunci — window ditutup
di awal, dan hasil (sukses / gagal) dilaporkan lewat notifikasi sistem.

Cross-platform, tanpa dependency baru:
- Linux: `notify-send` (tersedia di GNOME/KDE/XFCE standar);
-  Windows: balloon tip via PowerShell + System.Windows.Forms.NotifyIcon
  (bawaan .NET Framework / PowerShell 5.1+, tidak butuh package tambahan,
  CREATE_NO_WINDOW agar console tidak berkedip di proses GUI windowed).

Best-effort di semua platform: kegagalan apa pun (helper tidak ada, policy
memblokir PowerShell, dst.) TIDAK melempar — jawaban tetap aman di disk
untuk recovery re-entry (ServerConfigDialog "Kirim Lagi").
"""

from __future__ import annotations

import atexit
import logging
import os
import shutil
import subprocess
import sys
from typing import Optional

log = logging.getLogger(__name__)

# Berapa lama PowerShell menahan balloon tip sebelum proses keluar.
# ShowBalloonTip async — proses harus tetap hidup selama durasi itu.
_WINDOWS_BALLOON_DURATION_MS = 8000

# Berapa lama maksimal menunggu helper notifikasi sebelum menyerah.
_NOTIFY_TIMEOUT = 10

# Proses notifikasi terakhir yang masih hidup. Best-effort: notifikasi
# baru mematikan yang lama dulu supaya balloon tip tidak bertumpuk, dan
# `atexit` memastikan tidak ada PowerShell yatim saat app keluar.
_last_proc: Optional[subprocess.Popen] = None


def _kill_previous() -> None:
    """Matikan proses notifikasi sebelumnya, best-effort (jangan melempar)."""
    global _last_proc
    proc = _last_proc
    _last_proc = None
    if proc is None:
        return
    try:
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except Exception:
                try:
                    proc.kill()
                except Exception:
                    pass
    except Exception:
        pass


def _register_atexit_once() -> None:
    global _atexit_registered
    try:
        _atexit_registered
    except NameError:
        _atexit_registered = False
    if not _atexit_registered:
        atexit.register(_kill_previous)
        _atexit_registered = True


_atexit_registered = False


def send_notification(
    title: str,
    message: str,
    urgency: str = "normal",
) -> bool:
    """Tampilkan notifikasi sistem. Return True bila berhasil.

    Best-effort: kegagalan (helper tidak ada / gagal / policy) tidak melempar —
    jawaban tetap aman di disk untuk recovery re-entry.

    Catatan: panggil dari worker thread — fungsi ini memblokir sampai
    N detik (`_NOTIFY_TIMEOUT`) menunggu helper selesai.
    """
    try:
        if sys.platform == "win32":
            return _send_windows_notification(title, message, urgency)
        return _send_linux_notification(title, message, urgency)
    except Exception:
        log.debug("send_notification failed", exc_info=True)
        return False


def _send_linux_notification(
    title: str, message: str, urgency: str = "normal"
) -> bool:
    """Linux: notify-send. Return True bila dipanggil sukses."""
    if shutil.which("notify-send") is None:
        return False
    args = [
        "notify-send",
        "--urgency=" + urgency,
        "--app-name=EXAMVAN",
        title,
        message,
    ]
    # list-args tanpa shell=True: judul/pesan tidak pernah di-interpolasi
    # ke perintah, jadi teks guru tidak bisa menyuntik opsi/flag.
    _register_atexit_once()
    _kill_previous()
    try:
        proc = subprocess.Popen(
            args,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        global _last_proc
        _last_proc = proc
        try:
            _, stderr = proc.communicate(timeout=_NOTIFY_TIMEOUT)
        except subprocess.TimeoutExpired:
            proc.kill()
            _, stderr = proc.communicate()
            log.warning("notify-send timed out after %ss", _NOTIFY_TIMEOUT)
            return False
        rc = proc.returncode
        if rc != 0:
            log.warning(
                "notify-send failed with rc=%s: %s",
                rc, (stderr or b"").decode("utf-8", errors="replace")[:500],
            )
            return False
        return True
    except Exception:
        log.debug("_send_linux_notification failed", exc_info=True)
        return False


# Skrip PowerShell TETAP: judul/pesan TIDAK di-interpolasi ke sini.
# Keduanya lewat environment (EXAMVAN_NOTIFY_TITLE / EXAMVAN_NOTIFY_BODY);
# yang dipilih di kode hanya ikon (dari urgency, bukan dari teks server).
_WINDOWS_SCRIPT = (
    "Add-Type -AssemblyName System.Windows.Forms;"
    "Add-Type -AssemblyName System.Drawing;"
    "$iconName = $env:EXAMVAN_NOTIFY_ICON;"
    "$n = New-Object System.Windows.Forms.NotifyIcon;"
    "$n.Icon = [System.Drawing.SystemIcons]::$iconName;"
    "$n.BalloonTipTitle = $env:EXAMVAN_NOTIFY_TITLE;"
    "$n.BalloonTipText = $env:EXAMVAN_NOTIFY_BODY;"
    "$n.Visible = $true;"
    f"$n.ShowBalloonTip({_WINDOWS_BALLOON_DURATION_MS});"
    f"Start-Sleep -Milliseconds {_WINDOWS_BALLOON_DURATION_MS};"
    "$n.Dispose()"
)


def _send_windows_notification(
    title: str, message: str, urgency: str = "normal"
) -> bool:
    """Windows: balloon tip via PowerShell NotifyIcon (tanpa dependency baru).

    PowerShell 5.1 + .NET Framework (System.Windows.Forms / System.Drawing)
    tersedia di semua Windows 10/11. Icon disesuaikan urgency:
    critical → SystemIcons.Warning, selainnya → SystemIcons.Information.

    Judul/pesan diteruskan via ENVIRONMENT, bukan interpolasi string:
    teks guru (`congrats_message`) bebas isinya — kutip tunggal di-escape
    pun tetap rapuh (backtick/`$()`/subexpression masih hidup di dalam
    double-quote PowerShell). Lewat `$env:` tidak ada parsing sama sekali.

    Mengapa PowerShell, bukan modul Python: tidak ada dependency bawaan Python
    untuk toast lintas Windows tanpa package tambahan (win10toast & kawan-kawan
    butuh install). NotifyIcon balloon tip cukup: muncul dari tray, tidak
    mengganggu fokus (kiosk), dan window ujian sudah ditutup saat ini dipanggil.

    Catatan: panggil dari worker thread — memblokir sampai N detik.
    """
    icon = "Warning" if urgency == "critical" else "Information"
    args = [
        "powershell",
        "-NoProfile",
        "-NonInteractive",
        "-WindowStyle",
        "Hidden",
        "-Command",
        _WINDOWS_SCRIPT,
    ]
    env = {
        **os.environ,
        "EXAMVAN_NOTIFY_TITLE": title,
        "EXAMVAN_NOTIFY_BODY": message,
        "EXAMVAN_NOTIFY_ICON": icon,
    }
    _register_atexit_once()
    _kill_previous()
    try:
        proc = subprocess.Popen(
            args,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            # CREATE_NO_WINDOW — proses GUI windowed: tanpa ini jendela
            # console PowerShell berkedip di layar saat notifikasi dikirim.
            creationflags=0x08000000,  # CREATE_NO_WINDOW
        )
        global _last_proc
        _last_proc = proc
        try:
            _, stderr = proc.communicate(
                timeout=(_WINDOWS_BALLOON_DURATION_MS // 1000) + 10
            )
        except subprocess.TimeoutExpired:
            proc.kill()
            _, stderr = proc.communicate()
            log.warning("powershell notify timed out")
            return False
        rc = proc.returncode
        if rc != 0:
            log.warning(
                "powershell notify failed with rc=%s: %s",
                rc, (stderr or b"").decode("utf-8", errors="replace")[:500],
            )
            return False
        return True
    except Exception:
        log.debug("_send_windows_notification failed", exc_info=True)
        return False
