"""Timer widget — countdown or elapsed depending on exam config.

Uses monotonic clock to prevent system time manipulation.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from typing import Optional

from PyQt5.QtCore import QTimer, Qt, pyqtSignal
from PyQt5.QtWidgets import QLabel, QWidget, QHBoxLayout

import time as _time

from .. import api


def compute_remaining_seconds(
    end_time: Optional[str], now_utc: datetime, skew_ms: int
) -> Optional[float]:
    """Sisa waktu (detik) sampai deadline, atau None bila end_time rusak.

    Murni & bisa diuji. `now_utc` adalah waktu perangkat; `skew_ms` = jam
    server - jam perangkat (mirror Android ExamDeadline). Negative berarti
    deadline sudah lewat.
    """
    if not end_time:
        return None
    try:
        end_wall = datetime.fromisoformat(end_time.replace("Z", "+00:00"))
        return (end_wall - now_utc).total_seconds() + (skew_ms / 1000.0)
    except Exception:
        return None


class ElapsedTimerWidget(QWidget):
    """Displays countdown (if end_time set) or elapsed time."""

    time_up = pyqtSignal()  # emitted once when countdown reaches 0

    def __init__(self, end_time: Optional[str] = None, parent=None):
        super().__init__(parent)
        # Monotonic clock — immune to system clock changes
        self._start_mono = _time.monotonic()
        self._end_time = end_time
        self._end_mono: Optional[float] = None  # monotonic deadline
        self._fired_time_up = False

        self._compute_deadline()

        self._setup_ui()
        self._timer = QTimer(self)
        self._timer.timeout.connect(self._update)
        self._timer.start(1000)
        self._update()

    def _compute_deadline(self) -> None:
        """Convert wall-clock deadline to monotonic time (dengan koreksi skew)."""
        remaining = compute_remaining_seconds(
            self._end_time, datetime.now(timezone.utc), api.get_server_skew_ms()
        )
        if remaining is None:
            return
        if remaining > 0:
            self._end_mono = _time.monotonic() + remaining
        else:
            # Already past deadline — fire immediately
            self._end_mono = _time.monotonic() - 1

    def refresh_deadline(self) -> None:
        """Hitung ulang deadline dari wall clock, TANPA pernah memperpanjangnya.

        Mirror Android onResume (ExamDeadline.remainingMs dihitung ulang):
        `time.monotonic()` tidak termasuk waktu suspend, jadi setelah laptop
        tertidur countdown akan membeku. Wall clock yang benar dipakai untuk
       issorsafe: laptop tidur 30 menit -> jam sudah maju -> sisa mengecil ->
        deadline diperpendek. Itu memang tujuannya, dan tetap berlaku.

        Yang TIDAK boleh berlaku: siswa mundurkan jam perangkat untuk
        memperpanjang ujian. `end_time` datang dari server dan tidak bisa
        diubah siswa, jadi hasil hitung ulang di-CLAMP agar tidak pernah
        melewati deadline absolut itu. Arahnya fail-secure:

          * jam dimajukan  -> sisa mengecil, deadline BERKEPING (aman)
          * jam dimundurkan -> sisa membesar, deadline TETAP (dibaikan)

        Batasnya `min()`, karena monotonic sudah berdiri sebagai jam yang
        tidak bisa dimanipulasi: memperpanjang dari sana berarti menghitung
        mundur dari titik yang sama, yang persis hal yang tidak diizinkan.
        Bug yang diperbaiki: review_windows_2026-09-30.md Bagian 2 —
        countdown pernah melompat dari 01:00:00 ke 02:00:00 begitu siswa
        mengklik window, sementara server sudah menolak dengan 403.
        """
        if self._fired_time_up:
            return
        if self._end_mono is None:
            # Mode elapsed (tanpa end_time) tidak punya deadline untuk
            # diperbarui; jangan mengarang satu.
            return

        remaining = compute_remaining_seconds(
            self._end_time, datetime.now(timezone.utc), api.get_server_skew_ms()
        )
        if remaining is None:
            # end_time rusak: deadline yang ada lebih baik daripada
            # membiarkan deadline yang sekarang.
            return

        if remaining > 0:
            self._end_mono = min(self._end_mono, _time.monotonic() + remaining)
        else:
            # Sudah lewat deadline memotong, whatever pun jam perangkat —
            # ini juga menutup jalan mundur lewat jam.
            self._end_mono = min(self._end_mono, _time.monotonic() - 1)

    def _setup_ui(self) -> None:
        layout = QHBoxLayout(self)
        layout.setContentsMargins(0, 0, 0, 0)

        self._icon = QLabel("⏱")
        self._icon.setStyleSheet("font-size: 16px;")
        layout.addWidget(self._icon)

        self._label = QLabel("00:00:00")
        self._label.setStyleSheet(
            "font-size: 16px; font-weight: bold; font-family: monospace;"
        )
        layout.addWidget(self._label)

    def _update(self) -> None:
        now_mono = _time.monotonic()

        if self._end_mono is not None:
            # Countdown mode via monotonic clock — immune to system clock jump
            remaining = self._end_mono - now_mono
            if remaining > 0:
                total = int(remaining)
                h = total // 3600
                m = (total % 3600) // 60
                s = total % 60
                self._label.setText(f"{h:02d}:{m:02d}:{s:02d}")
            else:
                # Overdue — fire time_up exactly once
                if not self._fired_time_up:
                    self._fired_time_up = True
                    self.time_up.emit()
                overdue = int(-remaining)
                h = overdue // 3600
                m = (overdue % 3600) // 60
                s = overdue % 60
                self._label.setText(f"-{h:02d}:{m:02d}:{s:02d}")
        else:
            # Elapsed mode (no end_time)
            elapsed = now_mono - self._start_mono
            total = int(elapsed)
            if total < 0:
                total = 0
            h = total // 3600
            m = (total % 3600) // 60
            s = total % 60
            self._label.setText(f"{h:02d}:{m:02d}:{s:02d}")

    def get_start_time_iso(self) -> str:
        """Return the wall-clock start time in ISO format.

        Derived from monotonic clock so system time changes don't affect it.
        """
        elapsed = _time.monotonic() - self._start_mono
        now_wall = datetime.now(timezone.utc)
        start = now_wall - timedelta(seconds=elapsed)
        return start.strftime("%Y-%m-%dT%H:%M:%SZ")

    def stop(self) -> None:
        self._timer.stop()
