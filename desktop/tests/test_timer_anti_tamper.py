"""Siswa tidak boleh MEMBESARKAN sisa waktu dengan mengubah jam komputer.

Temuan N2, review_windows_2026-09-30.md Bagian 2.

Jaminan yang dikodekan
---------------------
Docstring modul `timer.py` berbunyi (kutipan dalam):

    Timer widget - countdown or elapsed depending on exam config.
    Uses monotonic clock to prevent system time manipulation.

dan `_update` mengomentari "Countdown mode via monotonic clock - immune to
system clock jump". Keduanya benar — SAMPAI `refresh_deadline()` dipanggil.
Fungsi itu (:71-83) menghitung ulang deadline dari `datetime.now()` — jam
PERANGKAT, yang boleh diubah siswa.

Bukti eksekusi (compute_remaining_seconds dipanggil langsung):

    exam ends 11:00, skew 0
      device clock 10:00 -> 3600 s   (jujur)
      device clock 09:00 -> 7200 s   (siswa mundurkan jam 1 jam)

`skew` tidak menolong: `api.compute_server_skew_ms` menghitung
`server_time_utc - jam perangkat`, jadi jam yang dimundurkan justru
menghasilkan koreksi yang MENGGANTUNG ke arah yang sama.

Trigger-nya sepele. `ExamViewerWindow.changeEvent` memanggil
`refresh_deadline()` pada setiap `WindowStateChange` yang bukan minimize
(:774) dan setiap `ActivationChange` saat window aktif (:778). Jadi
"membuat window kembali fokus" saja sudah cukup.

Yang dikunci di sini
--------------------
`end_time` datang dari server dan tidak bisa diubah siswa, jadi hasil
hitung ulang WAJIB di-clamp agar tidak pernah melewati deadline absolut
tersebut. Arahnya harus fail-secure: jam yang dimajukan hanya boleh
MEMPERCEPAT, tidak boleh memperpanjang.
"""

from __future__ import annotations

import os
import unittest
from datetime import datetime, timedelta, timezone
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.timer import ElapsedTimerWidget

APP = QApplication.instance() or QApplication([])

END_TIME = "2026-09-30T11:00:00Z"


class RefreshDeadlineTest(unittest.TestCase):
    """`refresh_deadline` tidak boleh memperpanjang deadline."""

    def _widget(self, now, end_mono):
        """Bangun widget dengan deadline monotonic yang sudah ditentukan."""
        # Dibangun tanpa __init__ supaya tidak ada QTimer sungguhan yang
        # dibuat: yang diuji murni aritmetika deadline.
        w = ElapsedTimerWidget.__new__(ElapsedTimerWidget)
        w._start_mono = 0.0
        w._end_time = END_TIME
        w._end_mono = end_mono
        w._fired_time_up = False
        return w

    def _refresh(self, w, device_now):
        # Subclass, bukan Mock: `compute_remaining_seconds` memanggil
        # `datetime.fromisoformat`, yang harus tetap asli supaya
        # perhitungannya sungguhan diuji.
        class _Clock(datetime):
            @classmethod
            def now(cls, tz=None):
                return device_now

        with mock.patch("examvan.ui.timer._time.monotonic", return_value=1000.0), \
             mock.patch("examvan.ui.timer.datetime", _Clock), \
             mock.patch("examvan.ui.timer.api.get_server_skew_ms", return_value=0):
            w.refresh_deadline()
        return w._end_mono

    def test_moving_the_clock_back_does_not_extend_the_deadline(self):
        # Jam perangkat jujur 10:00, deadline 11:00, sisa 1 jam.
        # monotonic() dipatok 1000.0, jadi deadline asli = 1000 + 3600 = 4600.
        # Siswa mundurkan jam ke 09:00 -> sisa dipalsukan jadi 2 jam.
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), 4600.0)
        after = self._refresh(w, datetime(2026, 9, 30, 9, 0, tzinfo=timezone.utc))

        self.assertEqual(
            after, 4600.0,
            "clock rollback must not extend the exam deadline",
        )

    def test_rollback_never_exceeds_the_absolute_deadline(self):
        # Sifat yang dikunci, dinyatakan langsung: berapa pun jam perangkat
        # dimundurkan, deadline monotonic tidak boleh melewati nilai yang
        # menghasilkan deadline absolut = end_time.
        base = datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc)
        w = self._widget(base, 4600.0)
        for hours_back in (1, 2, 5, 24, 72):
            with self.subTest(hours_back=hours_back):
                w._end_mono = 4600.0
                faked = base - timedelta(hours=hours_back)
                after = self._refresh(w, faked)
                # 4600.0 - 1000.0 = 3600 s = sisa asli. Deadline absolut
                # tidak boleh melewati 11:00, jadi sisa <= 3600.
                self.assertLessEqual(after - 1000.0, 3600.0 + 1e-6)

    def test_moving_the_clock_forward_still_shortens(self):
        # Arah sebaliknya boleh berlaku: fail-secure. Jam dimajukan hanya
        # boleh mempercepat, dan itu perilaku yang sudah diharapkan.
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), 4600.0)
        after = self._refresh(w, datetime(2026, 9, 30, 10, 30, tzinfo=timezone.utc))
        self.assertLess(after, 4600.0)

    def test_legitimate_suspend_still_extends_nothing_but_shortens(self):
        # Kasus asli yang mem motivates refresh_deadline: laptop tidur 30 menit
        # -> monotonic tidak ikut jalan -> countdown harus menyusut. Wall
        # clock sudah maju, jadi sisa mengecil dan deadline diperpendek.
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), 4600.0)
        after = self._refresh(w, datetime(2026, 9, 30, 10, 30, tzinfo=timezone.utc))
        self.assertEqual(after, 1000.0 + 1800.0)

    def test_no_end_time_means_no_deadline_to_manipulate(self):
        # Mode elapsed (tanpa end_time) tidak punya deadline; refresh
        # tidak boleh mengarang satu.
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), None)
        w._end_time = None
        w._end_mono = None
        with mock.patch("examvan.ui.timer._time.monotonic", return_value=1000.0), \
             mock.patch("examvan.ui.timer.api.get_server_skew_ms", return_value=0):
            w.refresh_deadline()
        self.assertIsNone(w._end_mono)

    def test_fired_timer_is_never_revived(self):
        # Kalau time_up sudah menyala, refresh tidak boleh menghidupkan
        # deadline lagi — _auto_submit sudah berjalan.
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), 100.0)
        w._fired_time_up = True
        self._refresh(w, datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc))
        self.assertEqual(w._end_mono, 100.0)

    def test_malformed_end_time_does_not_move_an_existing_deadline(self):
        w = self._widget(datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc), 4600.0)
        w._end_time = "not-a-timestamp"
        after = self._refresh(w, datetime(2026, 9, 30, 10, 0, tzinfo=timezone.utc))
        self.assertEqual(after, 4600.0)


class StartTimeTest(unittest.TestCase):
    """`get_start_time_iso` diturunkan dari monotonic, bukan jam perangkat."""

    def test_start_time_tracks_the_monotonic_elapsed_time(self):
        w = ElapsedTimerWidget.__new__(ElapsedTimerWidget)
        w._start_mono = 0.0
        w._end_time = END_TIME
        w._end_mono = None
        w._fired_time_up = False
        wall = datetime(2026, 9, 30, 10, 0, 0, tzinfo=timezone.utc)

        class _Clock(datetime):
            @classmethod
            def now(cls, tz=None):
                return wall

        with mock.patch("examvan.ui.timer._time.monotonic", return_value=1800.0), \
             mock.patch("examvan.ui.timer.datetime", _Clock):
            iso = w.get_start_time_iso()
        # 30 menit sebelum 10:00 = 09:30
        self.assertEqual(iso, "2026-09-30T09:30:00Z")


if __name__ == "__main__":
    unittest.main()
