"""Desktop notification helper (mirror Android auto-submit notification).

Digunakan untuk melaporkan hasil auto-submit BACKGROUND setelah window
ujian ditutup segera (mirror Android `showAutoSubmitNotification`):
siswa tidak lagi menunggu hasil jaringan di layar terkunci — window ditutup
di awal, dan hasil (sukses / gagal) dilaporkan lewat notifikasi sistem.

Cross-platform, tanpa dependency baru:
- Linux: `notify-send` (tersedia di GNOME/KDE/XFCE standar);
- Windows: balloon tip via PowerShell + System.Windows.Forms.NotifyIcon
  (bawaan .NET Framework / PowerShell 5.1+, tidak butuh package tambahan).

Best-effort di semua platform: kegagalan apa pun (helper tidak ada, policy
memblokir PowerShell, dst.) TIDAK melempar — jawaban tetap aman di disk
untuk recovery re-entry (ServerConfigDialog "Kirim Lagi").
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from typing import Optional

# Berapa lama PowerShell menahan balloon tip sebelum proses keluar.
# ShowBalloonTip async — proses harus tetap hidup selama durasi itu.
_WINDOWS_BALLOON_DURATION_MS = 8000


def send_notification(
    title: str,
    message: str,
    urgency: str = "normal",
) -> bool:
    """Tampilkan notifikasi sistem. Return True bila berhasil.

    Best-effort: kegagalan (helper tidak ada / gagal / policy) tidak melempar —
    jawaban tetap aman di disk untuk recovery re-entry.
    """
    try:
        if sys.platform == "win32":
            return _send_windows_notification(title, message, urgency)
        return _send_linux_notification(title, message, urgency)
    except Exception:
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
    subprocess.run(args, timeout=5, check=False)
    return True


def _send_windows_notification(
    title: str, message: str, urgency: str = "normal"
) -> bool:
    """Windows: balloon tip via PowerShell NotifyIcon (tanpa dependency baru).

    PowerShell 5.1 + .NET Framework (System.Windows.Forms / System.Drawing)
    tersedia di semua Windows 10/11. Icon disesuaikan urgency:
    critical → SystemIcons.Warning, selainnya → SystemIcons.Information.

    Mengapa PowerShell, bukan modul Python: tidak ada dependency bawaan Python
    untuk toast lintas Windows tanpa package tambahan (win10toast & kawan-kawan
    butuh install). NotifyIcon balloon tip cukup: muncul dari tray, tidak
    mengganggu fokus (kiosk), dan window ujian sudah ditutup saat ini dipanggil.
    """
    icon = "Warning" if urgency == "critical" else "Information"
    # Escape single-quote untuk string literal PowerShell.
    esc_title = title.replace("'", "''")
    esc_message = message.replace("'", "''")
    script = (
        "Add-Type -AssemblyName System.Windows.Forms;"
        "Add-Type -AssemblyName System.Drawing;"
        f"$n = New-Object System.Windows.Forms.NotifyIcon;"
        f"$n.Icon = [System.Drawing.SystemIcons]::{icon};"
        f"$n.BalloonTipTitle = '{esc_title}';"
        f"$n.BalloonTipText = '{esc_message}';"
        "$n.Visible = $true;"
        f"$n.ShowBalloonTip({_WINDOWS_BALLOON_DURATION_MS});"
        f"Start-Sleep -Milliseconds {_WINDOWS_BALLOON_DURATION_MS};"
        "$n.Dispose()"
    )
    args = [
        "powershell",
        "-NoProfile",
        "-NonInteractive",
        "-WindowStyle",
        "Hidden",
        "-Command",
        script,
    ]
    subprocess.run(
        args,
        timeout=(_WINDOWS_BALLOON_DURATION_MS // 1000) + 10,
        check=False,
    )
    return True
