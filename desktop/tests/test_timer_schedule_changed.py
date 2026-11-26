"""Test schedule_changed — deadline klien mengikuti jadwal baru (review
klien 14 Nov 2026): end_time lama yang di-cache dari payload join sudah basi
saat pengawas mengubah jadwal mid-exam; klien wajib menerapkan deadline baru
(otoritatif dari server, tanpa clamp min fail-secure yang anti-perpanjang).
"""
import unittest
from datetime import datetime, timedelta, timezone

from PyQt5.QtWidgets import QApplication

from examvan.ui.timer import ElapsedTimerWidget as ExamTimer


APP = QApplication.instance() or QApplication([])


def _iso(delta_minutes):
    return (datetime.now(timezone.utc) + timedelta(minutes=delta_minutes)).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )


class ScheduleChangedTest(unittest.TestCase):
    def test_set_end_time_applies_new_authoritative_deadline(self):
        widget = ExamTimer(end_time=_iso(10))
        widget.refresh_deadline()

        new_end = _iso(90)
        widget.set_end_time(new_end)

        self.assertEqual(widget._end_time, new_end)
        # _end_mono dibangun ulang penuh dari end_time baru (bukan clamp min):
        # sisa sekitar 90 menit (toleransi 10 detik).
        remaining = (widget._end_mono or 0) - __import__("time").monotonic()
        self.assertGreaterEqual(remaining, 89 * 60)
        self.assertLessEqual(remaining, 91 * 60)

    def test_set_end_time_shortens_deadline(self):
        widget = ExamTimer(end_time=_iso(120))
        widget.refresh_deadline()

        widget.set_end_time(_iso(5))

        remaining = (widget._end_mono or 0) - __import__("time").monotonic()
        self.assertGreaterEqual(remaining, 4 * 60)
        self.assertLessEqual(remaining, 6 * 60)

    def test_set_end_time_invalid_clears_deadline(self):
        widget = ExamTimer(end_time=_iso(30))
        widget.refresh_deadline()

        widget.set_end_time("bukan-iso")

        self.assertIsNone(
            widget._end_mono,
            "end_time rusak -> tanpa deadline (countdown tidak jalan)",
        )


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
