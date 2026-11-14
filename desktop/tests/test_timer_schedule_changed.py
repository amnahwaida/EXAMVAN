"""Test schedule_changed — deadline klien mengikuti jadwal baru (review
klien 14 Nov 2026): end_time lama yang di-cache dari payload join sudah basi
saat pengawas mengubah jadwal mid-exam; klien wajib menerapkan deadline baru
(otoritatif dari server, tanpa clamp min fail-secure yang anti-perpanjang).
"""
from datetime import datetime, timedelta, timezone

import pytest

from examvan.ui.timer import ElapsedTimerWidget as ExamTimer


@pytest.fixture(scope="module")
def qapp():
    from PyQt5.QtWidgets import QApplication
    app = QApplication.instance() or QApplication([])
    yield app


def _iso(delta_minutes):
    return (datetime.now(timezone.utc) + timedelta(minutes=delta_minutes)).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )


def test_set_end_time_applies_new_authoritative_deadline(qapp):
    widget = ExamTimer(end_time=_iso(10))
    widget.refresh_deadline()

    new_end = _iso(90)
    widget.set_end_time(new_end)

    assert widget._end_time == new_end
    # _end_mono dibangun ulang penuh dari end_time baru (bukan clamp min):
    # sisa sekitar 90 menit (toleransi 10 detik).
    remaining = (widget._end_mono or 0) - __import__("time").monotonic()
    assert 89 * 60 <= remaining <= 91 * 60, f"remaining={remaining}s"


def test_set_end_time_shortens_deadline(qapp):
    widget = ExamTimer(end_time=_iso(120))
    widget.refresh_deadline()

    widget.set_end_time(_iso(5))

    remaining = (widget._end_mono or 0) - __import__("time").monotonic()
    assert 4 * 60 <= remaining <= 6 * 60, f"remaining={remaining}s"


def test_set_end_time_invalid_clears_deadline(qapp):
    widget = ExamTimer(end_time=_iso(30))
    widget.refresh_deadline()

    widget.set_end_time("bukan-iso")

    assert widget._end_mono is None, "end_time rusak -> tanpa deadline (countdown tidak jalan)"
