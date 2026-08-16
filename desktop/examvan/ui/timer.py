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


class ElapsedTimerWidget(QWidget):
    """Displays countdown (if end_time set) or elapsed time."""

    time_up = pyqtSignal()  # emitted once when countdown reaches 0

    def __init__(self, end_time: Optional[str] = None, parent=None):
        super().__init__(parent)
        # Monotonic clock — immune to system clock changes
        self._start_mono = _time.monotonic()
        self._end_mono: Optional[float] = None  # monotonic deadline
        self._fired_time_up = False

        if end_time:
            try:
                end_wall = datetime.fromisoformat(
                    end_time.replace("Z", "+00:00")
                )
                # Convert wall-clock deadline to monotonic time. Koreksi
                # server time skew (mirror Android ExamDeadline): `now` adalah
                # waktu perangkat, `api.get_server_skew_ms()` = jam server -
                # jam perangkat. Deadlinenya sendiri dihitung ulang dari
                # end_time absolut + skew, bukan dari jam lokal mentah.
                skew_s = api.get_server_skew_ms() / 1000.0
                now = datetime.now(timezone.utc)
                duration = (end_wall - now).total_seconds() + skew_s
                if duration > 0:
                    self._end_mono = _time.monotonic() + duration
                else:
                    # Already past deadline — fire immediately
                    self._end_mono = _time.monotonic() - 1
            except Exception:
                pass

        self._setup_ui()
        self._timer = QTimer(self)
        self._timer.timeout.connect(self._update)
        self._timer.start(1000)
        self._update()

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
