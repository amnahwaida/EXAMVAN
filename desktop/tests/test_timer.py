"""Unit tests for examvan.ui.timer — deadline computation + suspend resilience.

Covers the desktop↔Android consistency fixes (Agustus 2026):
- server time skew is applied to the countdown (mirror Android ExamDeadline);
- `refresh_deadline()` recomputes the monotonic deadline from the ABSOLUTE
  end_time + skew when the window becomes active again — `time.monotonic()`
  (CLOCK_MONOTONIC) does NOT include suspend time, so a laptop that slept
  for 30 minutes would otherwise show a frozen/misleading countdown
  (mirror Android onResume recompute).
"""

from __future__ import annotations

import time as _time
import unittest
from datetime import datetime, timedelta, timezone

from PyQt5.QtWidgets import QApplication

from examvan import api
from examvan.ui.timer import ElapsedTimerWidget, compute_remaining_seconds

APP = QApplication.instance() or QApplication([])


class ComputeRemainingSecondsTest(unittest.TestCase):
    def test_remaining_with_skew(self):
        now = datetime(2026, 8, 16, 8, 0, 0, tzinfo=timezone.utc)
        end = now + timedelta(minutes=30)
        # skew +5s (server ahead) → remaining 1805s
        remaining = compute_remaining_seconds(
            end.isoformat(), now, skew_ms=5000,
        )
        self.assertAlmostEqual(remaining, 1805.0, places=3)

    def test_negative_when_past_deadline(self):
        now = datetime(2026, 8, 16, 8, 0, 0, tzinfo=timezone.utc)
        end = now - timedelta(minutes=1)
        self.assertLess(compute_remaining_seconds(end.isoformat(), now, 0), 0)

    def test_z_suffix_parsed(self):
        now = datetime(2026, 8, 16, 8, 0, 0, tzinfo=timezone.utc)
        end = now + timedelta(minutes=10)
        remaining = compute_remaining_seconds(end.isoformat().replace("+00:00", "Z"), now, 0)
        self.assertAlmostEqual(remaining, 600.0, places=3)

    def test_none_or_corrupt_end_time(self):
        now = datetime(2026, 8, 16, 8, 0, 0, tzinfo=timezone.utc)
        self.assertIsNone(compute_remaining_seconds(None, now, 0))
        self.assertIsNone(compute_remaining_seconds("bukan-iso", now, 0))


class RefreshDeadlineTest(unittest.TestCase):
    """refresh_deadline() recomputes deadline after suspend (monotonic freeze)."""

    def setUp(self):
        api.set_server_skew_ms(0)
        self._end = (
            datetime.now(timezone.utc) + timedelta(hours=1)
        ).isoformat().replace("+00:00", "Z")

    def test_refresh_recomputes_stale_deadline(self):
        widget = ElapsedTimerWidget(end_time=self._end)
        try:
            # Simulate suspend: monotonic clock froze, so the deadline looks
            # much further away than it really is (stale end_mono).
            stale = widget._end_mono
            widget._end_mono = stale + 1800  # +30 menit palsu
            widget.refresh_deadline()
            refreshed = widget._end_mono
            self.assertLess(refreshed, stale + 1800)
            self.assertAlmostEqual(refreshed, _time.monotonic() + 3600, delta=30)
        finally:
            widget.stop()

    def test_refresh_after_deadline_past_sets_overdue(self):
        widget = ElapsedTimerWidget(end_time=self._end)
        try:
            # Deadline sudah lewat saat refresh (mis. laptop tertidur lama).
            widget._end_time = (
                datetime.now(timezone.utc) - timedelta(minutes=5)
            ).isoformat().replace("+00:00", "Z")
            widget.refresh_deadline()
            self.assertLess(widget._end_mono, _time.monotonic())
        finally:
            widget.stop()

    def test_time_up_fires_once_even_with_refresh(self):
        fired = []

        def _on_time_up():
            fired.append(True)

        widget = ElapsedTimerWidget(end_time=self._end)
        widget.time_up.connect(_on_time_up)
        try:
            # Force overdue, then tick: fires exactly once.
            widget._end_mono = _time.monotonic() - 1
            widget._update()
            widget._update()
            widget.refresh_deadline()  # guard _fired_time_up → no re-fire
            widget._update()
            self.assertEqual(len(fired), 1)
        finally:
            widget.stop()

    def test_no_end_time_elapsed_mode_refresh_noop(self):
        widget = ElapsedTimerWidget(end_time=None)
        try:
            widget.refresh_deadline()  # must not raise
            self.assertIsNone(widget._end_mono)
        finally:
            widget.stop()


if __name__ == "__main__":
    unittest.main()
