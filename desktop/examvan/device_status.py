"""Status perangkat untuk toolbar ujian desktop.

Mirror `DeviceStatusPolicy.kt` di Android: kategori baterai murni supaya
ambang batasnya bisa diuji tanpa I/O, plus pembaca platform yang best-effort.
Desktop tidak punya baterai (PC lab) bukan error — pemanggil menyembunyikan
indikator ketika `read_battery_status()` mengembalikan None.
"""

from __future__ import annotations

import ctypes
import logging
import os
import sys
from dataclasses import dataclass
from typing import Optional

log = logging.getLogger(__name__)

BATTERY_CRITICAL_THRESHOLD = 15
BATTERY_LOW_THRESHOLD = 30

BATTERY_CRITICAL = "critical"
BATTERY_LOW = "low"
BATTERY_NORMAL = "normal"

# Windows SYSTEM_POWER_STATUS.BatteryFlag bits.
_BATTERY_FLAG_CHARGING = 0x08
_BATTERY_FLAG_NO_BATTERY = 0x80
_BATTERY_PERCENT_UNKNOWN = 255
_AC_LINE_ONLINE = 1


@dataclass(frozen=True)
class BatteryStatus:
    """Level baterai 0..100 dan apakah perangkat sedang mengisi daya."""

    percent: int
    charging: bool

    @property
    def category(self) -> str:
        return battery_category(self.percent)


class _SystemPowerStatus(ctypes.Structure):
    _fields_ = [
        ("ACLineStatus", ctypes.c_ubyte),
        ("BatteryFlag", ctypes.c_ubyte),
        ("BatteryLifePercent", ctypes.c_ubyte),
        ("SystemStatusFlag", ctypes.c_ubyte),
        ("BatteryLifeTime", ctypes.c_ulong),
        ("BatteryFullLifeTime", ctypes.c_ulong),
    ]


def battery_category(level_percent: int) -> str:
    """Kategori ambang yang sama dengan Android (15% dan 30%)."""
    level = max(0, min(100, int(level_percent)))
    if level < BATTERY_CRITICAL_THRESHOLD:
        return BATTERY_CRITICAL
    if level < BATTERY_LOW_THRESHOLD:
        return BATTERY_LOW
    return BATTERY_NORMAL


def battery_text(status: BatteryStatus) -> str:
    """Label ringkas; warna chip membawa kategori, tooltip membawa detail."""
    if status.charging:
        return f"Baterai {status.percent}% (mengisi)"
    return f"Baterai {status.percent}%"


def battery_from_windows_status(
    ac_line_status: int,
    battery_flag: int,
    life_percent: int,
) -> Optional[BatteryStatus]:
    """Konversi nilai mentah GetSystemPowerStatus ke BatteryStatus.

    Dipisah dari ctypes supaya keputusan 'tidak ada baterai' dan 'mengisi'
    bisa dites di Linux.
    """
    if life_percent == _BATTERY_PERCENT_UNKNOWN:
        return None
    if battery_flag & _BATTERY_FLAG_NO_BATTERY:
        return None
    if life_percent < 0 or life_percent > 100:
        return None
    charging = bool(battery_flag & _BATTERY_FLAG_CHARGING) or (
        ac_line_status == _AC_LINE_ONLINE
    )
    return BatteryStatus(percent=int(life_percent), charging=charging)


def read_battery_status() -> Optional[BatteryStatus]:
    """Baca status baterai platform; None berarti tidak ada baterai/unknown."""
    try:
        if sys.platform == "win32":
            return _read_windows_battery()
        return _read_linux_battery()
    except Exception:
        log.debug("gagal membaca status baterai", exc_info=True)
        return None


def _read_windows_battery() -> Optional[BatteryStatus]:
    status = _SystemPowerStatus()
    if not ctypes.windll.kernel32.GetSystemPowerStatus(ctypes.byref(status)):
        return None
    return battery_from_windows_status(
        ac_line_status=int(status.ACLineStatus),
        battery_flag=int(status.BatteryFlag),
        life_percent=int(status.BatteryLifePercent),
    )


def _read_linux_battery(root: str = "/sys/class/power_supply") -> Optional[BatteryStatus]:
    try:
        entries = sorted(os.listdir(root))
    except OSError:
        return None
    for name in entries:
        if not name.startswith("BAT"):
            continue
        base = os.path.join(root, name)
        try:
            with open(os.path.join(base, "capacity"), encoding="ascii") as f:
                percent = int(f.read().strip())
            with open(os.path.join(base, "status"), encoding="ascii") as f:
                state = f.read().strip().lower()
        except (OSError, ValueError):
            continue
        if percent < 0 or percent > 100:
            continue
        return BatteryStatus(
            percent=percent,
            charging=state in ("charging", "full"),
        )
    return None


__all__ = [
    "BATTERY_CRITICAL",
    "BATTERY_CRITICAL_THRESHOLD",
    "BATTERY_LOW",
    "BATTERY_LOW_THRESHOLD",
    "BATTERY_NORMAL",
    "BatteryStatus",
    "battery_category",
    "battery_from_windows_status",
    "battery_text",
    "read_battery_status",
]
