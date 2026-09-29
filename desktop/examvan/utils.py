"""Utility functions: MAC address, device ID, clipboard helpers.

Cross-platform — works on Linux and Windows.
"""

from __future__ import annotations

import hashlib
import platform
import socket
import uuid
from pathlib import Path
from typing import Dict, Optional

from PyQt5.QtWidgets import QApplication

# Cached device label — see get_device_label() for why it must be stable for
# the whole process, not just per call.
_device_label_cache: Optional[str] = None


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

    # 2. Sisa field yang TIDAK terpetakan TIDAK boleh dipaksakan ke slot
    #    standar.
    #
    #    Dulu: `for std_key in (...): if std_key not in result and
    #    remaining: result[std_key] = remaining.pop(0)` — nilai yang tidak
    #    cocok keyword apa pun diisi ke slot identitas menurut urutan dict.
    #    Terbukti: {'nama','kelas','tanggal_lahir'} -> exam_number =
    #    '2010-05-05'. Tanggal lahir tercatat sebagai nomor ujian, tanpa
    #    error dan tanpa log.
    #
    #    Sekarang slot yang tidak terpetakan dibiarkan kosong supaya sisi
    #    server menolak dengan pesan yang bisa dibaca guru
    #    (`exams.go` "Identitas '%s' wajib diisi", HTTP 400). Satu data
    #    salah diam-diam lebih buruk daripada satu error yang jelas, dan
    #    menebak di client justru menghapus satu-satunya sinyal itu.

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
    """Return 'DESKTOP:<device_id>' (universal label for desktop clients).

    Cached for the lifetime of the process. This label is not cosmetic — it
    is the key in four places: `X-Device-Id` for the PDF download, and
    `mac_address` for request-approval, submit, and presence. If it changed
    mid-session the PDF gate would stop matching the approval row (no
    download), and the stale approval would never be revoked — which is
    exactly the failure the comment in `api.download_pdf` (:155-160)
    describes happening when the two call sites used different identities.

    Caching removes the whole class of problem: there is now nothing that
    can make the four call sites disagree. It also insulates the session
    from `uuid.getnode()` changing under us, which on Windows 10/11 it can
    (randomized MAC addresses, and it returns whichever adapter enumerates
    first on a machine with Wi-Fi + Ethernet + Bluetooth + VPN).
    """
    global _device_label_cache
    if _device_label_cache is None:
        _device_label_cache = f"DESKTOP:{get_device_id()}"
    return _device_label_cache


def reset_device_label_cache() -> None:
    """Drop the cached device label. For tests only."""
    global _device_label_cache
    _device_label_cache = None
