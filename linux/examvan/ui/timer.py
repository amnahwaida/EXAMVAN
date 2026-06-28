"""Timer widget — countdown or elapsed depending on exam config."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Optional

from PyQt5.QtCore import QTimer, Qt, pyqtSignal
from PyQt5.QtWidgets import QLabel, QWidget, QHBoxLayout


class ElapsedTimerWidget(QWidget):
    """Displays countdown (if end_time set) or elapsed time."""

    time_up = pyqtSignal()  # emitted once when countdown reaches 0

    def __init__(self, end_time: Optional[str] = None, parent=None):
        super().__init__(parent)
        self._start_time = datetime.now(timezone.utc)
        self._end_time: Optional[datetime] = None
        self._fired_time_up = False  # guard: emit only once

        if end_time:
            try:
                self._end_time = datetime.fromisoformat(
                    end_time.replace("Z", "+00:00")
                )
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

    def set_start_time(self, start: datetime) -> None:
        self._start_time = start
        self._update()

    def _update(self) -> None:
        now = datetime.now(timezone.utc)
        if self._end_time:
            if now < self._end_time:
                # countdown — time remaining
                delta = self._end_time - now
                total = int(delta.total_seconds())
                if total < 0:
                    total = 0
                h = total // 3600
                m = (total % 3600) // 60
                s = total % 60
                self._label.setText(f"{h:02d}:{m:02d}:{s:02d}")
                self._fired_time_up = False  # reset so overdue fires once
            else:
                # overdue — fire time_up once, show negative
                if not self._fired_time_up:
                    self._fired_time_up = True
                    self.time_up.emit()
                delta = now - self._end_time
                total = int(delta.total_seconds())
                h = total // 3600
                m = (total % 3600) // 60
                s = total % 60
                self._label.setText(f"-{h:02d}:{m:02d}:{s:02d}")
        else:
            # elapsed since start (no end time)
            delta = now - self._start_time
            total = int(delta.total_seconds())
            if total < 0:
                total = 0
            h = total // 3600
            m = (total % 3600) // 60
            s = total % 60
            self._label.setText(f"{h:02d}:{m:02d}:{s:02d}")

    def get_start_time_iso(self) -> str:
        return self._start_time.strftime("%Y-%m-%dT%H:%M:%SZ")

    def stop(self) -> None:
        self._timer.stop()
