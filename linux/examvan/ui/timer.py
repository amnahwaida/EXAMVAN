"""Elapsed timer widget — HH:MM:SS display."""

from __future__ import annotations

from datetime import datetime, timezone

from PyQt5.QtCore import QTimer, Qt
from PyQt5.QtWidgets import QLabel, QWidget, QHBoxLayout


class ElapsedTimerWidget(QWidget):
    """Displays elapsed time since exam start as HH:MM:SS."""

    def __init__(self, parent=None):
        super().__init__(parent)
        self._start_time = datetime.now(timezone.utc)
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
        delta = now - self._start_time
        total_secs = int(delta.total_seconds())
        if total_secs < 0:
            total_secs = 0
        hours = total_secs // 3600
        minutes = (total_secs % 3600) // 60
        seconds = total_secs % 60
        self._label.setText(f"{hours:02d}:{minutes:02d}:{seconds:02d}")

    def get_start_time_iso(self) -> str:
        """Return start time as ISO 8601 UTC string."""
        return self._start_time.strftime("%Y-%m-%dT%H:%M:%SZ")

    def stop(self) -> None:
        self._timer.stop()
