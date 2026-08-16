"""Desktop notification helper (mirror Android auto-submit notification).

Digunakan untuk melaporkan hasil auto-submit BACKGROUND setelah window
ujian ditutup segera (mirror Android `showAutoSubmitNotification`):
siswa tidak lagi menunggu hasil jaringan di layar terkunci — window ditutup
di awal, dan hasil (sukses / gagal) dilaporkan lewat notifikasi sistem.

Cross-platform, tanpa dependency baru:
- Linux: `notify-send` (tersedia di GNOME/KDE/XFCE standar);
- Windows: fallback ke tidak ada notifikasi — jawaban tetap tersimpan di
  disk dan layar recovery re-entry (ServerConfigDialog) menawarkan
  "Kirim Lagi", jadi tidak ada jalan buntu.
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from typing import Optional


def send_notification(
    title: str,
    message: str,
    urgency: str = "normal",
) -> bool:
    """Tampilkan notifikasi sistem. Return True bila berhasil.

    Best-effort: kegagalan (notify-send tidak ada / gagal) tidak melempar —
    jawaban tetap aman di disk untuk recovery re-entry.
    """
    try:
        if sys.platform == "win32":
            # Tidak ada helper bawaan yang non-intrusif lintas Windows
            # tanpa dependency baru — lewati (recovery re-entry tetap jalan).
            return False
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
    except Exception:
        return False
