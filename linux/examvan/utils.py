"""Utility functions: MAC address, device ID, clipboard helpers.

Cross-platform — works on Linux and Windows.
"""

from __future__ import annotations

import hashlib
import platform
import socket
import subprocess
import uuid
from pathlib import Path
from typing import Dict, Optional

from PyQt5.QtWidgets import QApplication


def map_identity_to_standard(identity_data: Dict[str, str]) -> Dict[str, str]:
    """Map dynamic identity field keys to standard Go backend keys.

    The Go backend always expects these keys in identity_data:
      student_name, exam_number, student_class

    But exams can use custom keys like 'nama', 'nomor_ujian', 'kelas', etc.
    This function semantically matches them by keyword scanning.
    """
    result: Dict[str, str] = {}
    items = list(identity_data.items())
    if not items:
        return result

    # Keyword groups for matching
    name_kw = ("nama", "name", "siswa", "student", "peserta")
    number_kw = ("nomor", "number", "no_", "ujian", "exam", "nis", "nip", "peserta")
    class_kw = ("kelas", "class", "rombel", "kelompok")

    def matches(key: str, keywords: tuple) -> bool:
        k = key.lower()
        return any(kw in k for kw in keywords)

    assigned = set()  # track assigned values to avoid duplicates

    # 1. Match by keyword (highest confidence)
    for key, val in items:
        if "student_name" not in result and matches(key, name_kw):
            result["student_name"] = val
            assigned.add(val)
    for key, val in items:
        if "exam_number" not in result and matches(key, number_kw) and val not in assigned:
            result["exam_number"] = val
            assigned.add(val)
    for key, val in items:
        if "student_class" not in result and matches(key, class_kw) and val not in assigned:
            result["student_class"] = val
            assigned.add(val)

    # 2. Fill remaining standard keys from unmatched fields in order
    remaining = [val for _, val in items if val not in assigned]
    for std_key in ("student_name", "exam_number", "student_class"):
        if std_key not in result and remaining:
            result[std_key] = remaining.pop(0)

    # Remove empty values — Go backend rejects empty student_name/number/class
    return {k: v for k, v in result.items() if v}


def get_mac_address() -> str:
    """Get first non-loopback MAC address.

    Linux: reads /sys/class/net/*/address.
    Windows/fallback: uuid.getnode().
    """
    # Linux: sysfs is fastest and most reliable
    if not platform.system() == "Windows":
        try:
            for addr_path in sorted(Path("/sys/class/net").glob("*/address")):
                mac = addr_path.read_text().strip()
                iface = addr_path.parent.name
                if iface != "lo" and mac != "00:00:00:00:00:00":
                    return mac.upper()
        except OSError:
            pass

    # Cross-platform fallback via uuid
    mac = uuid.getnode()
    return ":".join(f"{(mac >> i) & 0xFF:02X}" for i in range(40, -1, -8))


def get_device_id() -> str:
    """Generate stable device identifier: SHA256(MAC + hostname)."""
    mac = get_mac_address()
    hostname = socket.gethostname()
    raw = f"{mac}:{hostname}"
    return hashlib.sha256(raw.encode()).hexdigest()[:32]


def get_device_label() -> str:
    """Return 'DESKTOP:<device_id>' (universal label for desktop clients)."""
    return f"DESKTOP:{get_device_id()}"


def clear_clipboard() -> None:
    """Clear system clipboard via Qt and platform fallback tools."""
    try:
        app = QApplication.instance()
        if app:
            app.clipboard().clear()
    except Exception:
        pass

    # Platform-specific tools
    if platform.system() == "Windows":
        _clear_clipboard_windows()
    else:
        _clear_clipboard_x11()


def _clear_clipboard_x11() -> None:
    """Clear clipboard via X11 tools (xsel/xclip)."""
    for tool, args in [
        ("xsel", ["--clipboard", "--delete"]),
        ("xclip", ["-selection", "clipboard", "-i", "/dev/null"]),
    ]:
        try:
            subprocess.run(
                [tool] + args,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=2,
            )
            return
        except (FileNotFoundError, subprocess.TimeoutExpired):
            continue


def _clear_clipboard_windows() -> None:
    """Clear clipboard via Win32 API."""
    try:
        import ctypes
        from ctypes.wintypes import HWND
        user32 = ctypes.windll.user32
        if user32.OpenClipboard(HWND(0)):
            user32.EmptyClipboard()
            user32.CloseClipboard()
    except Exception:
        pass


def clear_clipboard_wl() -> None:
    """Clear clipboard on Wayland via wl-copy."""
    try:
        subprocess.run(
            ["wl-copy", "--clear"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=2,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
