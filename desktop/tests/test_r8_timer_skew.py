"""C1 — Koreksi skew server harus NETRAL terhadap manipulasi jam perangkat.

Bukti eksekusi (fungsi asli `compute_remaining_seconds`, dipanggil langsung):

    offset perangkat    skew_ms    kembali    BENAR    delta
    sinkron                   0       3600     3600       0
    5 menit MUNDUR     300000       4200     3600     +600
    1 jam MUNDUR      3600000      10800     3600    +7200
    1 jam MAJU       -3600000      -3600     3600    -7200

`api.compute_server_skew_ms` mengembalikan **server - perangkat** (bukan
sebaliknya — docstring dan `server - device_now` di api.py:186-197). Sisa waktu
yang benar adalah

    end - (perangkat + skew) = end - server_now

yang TIDAK bergantung jam perangkat sama sekali — justru itulah seluruh tujuan
koreksi itu. Kode lama memakai `(end - perangkat) + skew`, yaitu
`benar + 2*skew`.

Dampak: siswa mundurkan jam OS 1 jam -> 2 jam tambahan. Server tetap menerima
submit karena `exams.go` hanya menegakkan `ExamScheduleEnded` = `end_time + 60s`
lalu mengecualikan perangkat yang `exam_approvals`-nya masih `approved`, dan baris
itu baru dihapus SESUDAH submit berhasil. Sebaliknya PC lab yang jamnya 1 jam
cepat langsung auto-submit dan lembar jawaban siswa terpotong tanpa penjelasan.

Referensi Android: `ExamDeadline.kt:28` menghitung `endInstantMs - (nowMs +
skewMs)`, jadi skew dikURANGKAN.

Test di sini menguji sifat, bukan rumus: untuk setiap offset perangkat, hasilnya
harus sama dengan sisa waktu sisi server yang benar (oracle independen yang
dihitung dari `SERVER_NOW`, bukan dari `compute_remaining_seconds`).
"""

from __future__ import annotations

import os
import unittest
from datetime import datetime, timedelta, timezone
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.timer import ElapsedTimerWidget, compute_remaining_seconds

APP = QApplication.instance() or QApplication([])


# Sisi server adalah titik kebenaran: deadline 12:00 UTC, server sekarang
# 11:00 UTC -> 1 jam tersisa. Semua angka lain diturunkan dari sini.
END_UTC = datetime(2026, 10, 3, 12, 0, 0, tzinfo=timezone.utc)
SERVER_NOW = datetime(2026, 10, 3, 11, 0, 0, tzinfo=timezone.utc)
TRUE_REMAINING_S = (END_UTC - SERVER_NOW).total_seconds()  # 3600.0
END_ISO = "2026-10-03T12:00:00Z"

# (label, offset perangkat dalam detik). Offset POSITIF = jam perangkat lebih
# MAJU dari server.
DEVICE_OFFSETS = (
    ("sinkron", 0),
    ("5 menit mundur", -300),
    ("5 menit maju", 300),
    ("1 jam mundur", -3600),
    ("1 jam maju", 3600),
    ("1 hari mundur", -86400),
    ("1 hari maju", 86400),
)


def skew_for(offset_s):
    """skew_ms = server - perangkat, persis seperti compute_server_skew_ms."""
    return int(-offset_s * 1000)


def truth_remaining():
    """Oracle: sisa waktu sisi server, dihitung TIDAK lewat fungsi yang diuji."""
    return (END_UTC - SERVER_NOW).total_seconds()


class TamperNeutralisedTest(unittest.TestCase):
    """Sifat utama: siswa tidak boleh bisa mengubah sisa waktunya sedikit pun."""

    def test_device_clock_manipulation_is_neutralised(self):
        truth = truth_remaining()
        for label, offset in DEVICE_OFFSETS:
            with self.subTest(offset=label):
                device_now = SERVER_NOW + timedelta(seconds=offset)
                got = compute_remaining_seconds(
                    END_ISO, device_now, skew_for(offset))
                self.assertIsNotNone(got)
                self.assertAlmostEqual(
                    got, truth, delta=1.0,
                    msg=(
                        f"offset {label}: sisa {got:.1f}s vs benar "
                        f"{truth:.1f}s — jam perangkat masih mengubah "
                        f"sisa waktu (selisih {got - truth:+.1f}s)"
                    ),
                )

    def test_every_offset_yields_the_identical_remaining(self):
        """Bentuk paling tajam dari sifat yang sama: semua offset -> satu nilai."""
        seen = set()
        for _label, offset in DEVICE_OFFSETS:
            device_now = SERVER_NOW + timedelta(seconds=offset)
            seen.add(round(compute_remaining_seconds(
                END_ISO, device_now, skew_for(offset)), 3))
        self.assertEqual(
            len(seen), 1,
            f"offset jam perangkat menghasilkan sisa berbeda-beda: {seen}",
        )

    def test_zero_skew_is_exact(self):
        self.assertEqual(
            compute_remaining_seconds(END_ISO, SERVER_NOW, 0), TRUE_REMAINING_S)

    def test_reversed_sign_formula_is_the_one_that_fails(self):
        """Guard: kalau skew ditambahkan, sifat utama ini yang runtuh.

        Device mundur 1 jam harus menambah sisa 1 jam (bukan 2).
        """
        offset = -3600
        device_now = SERVER_NOW + timedelta(seconds=offset)
        got = compute_remaining_seconds(END_ISO, device_now, skew_for(offset))
        self.assertNotAlmostEqual(
            got, TRUE_REMAINING_S + 3600.0, delta=1.0,
            msg="menambah skew menghasilkan 2x error (bug yang diperbaiki)",
        )


class DeadlineEdgeTest(unittest.TestCase):
    """Nilai batas tidak boleh berubah karena koreksi skew."""

    def test_already_passed_deadline_is_not_positive(self):
        # Waktu SERVER sudah 10 menit lewat deadline, dan jam perangkat
        # 1 jam mundur dari server (skew +1 jam). Kode lama menghitung
        # -4200 + 3600 = +6600, yaitu 1 jam 50 menit yang SALAH tersisa —
        # deadline yang sudah lewat jadi tidak pernah memicu auto-submit.
        server_now = END_UTC + timedelta(seconds=600)
        offset = -3600
        device_now = server_now + timedelta(seconds=offset)
        got = compute_remaining_seconds(END_ISO, device_now, skew_for(offset))
        self.assertIsNotNone(got)
        self.assertLessEqual(
            got, 0.0,
            "deadline yang sudah lewat tidak boleh menghasilkan sisa positif",
        )
        self.assertAlmostEqual(got, -600.0, delta=1.0)

    def test_passed_deadline_with_zero_skew_is_exact(self):
        server_now = END_UTC + timedelta(seconds=600)
        got = compute_remaining_seconds(END_ISO, server_now, 0)
        self.assertAlmostEqual(got, -600.0, delta=1.0)

    def test_negative_skew_means_server_behind_device(self):
        """Skew negatif = server DI BEHIND perangkat -> sisa MEMBESAR.

        Catatan lama di docstring mengklaim "Negative berarti deadline sudah
        lewat". Salah: yang menentukan tanda sisa waktu hanyalah perbandingan
        `end` dengan waktu server, bukan tanda skew.
        """
        server_now = SERVER_NOW - timedelta(minutes=5)   # server tertinggal
        device_now = server_now - timedelta(minutes=1)   # perangkat 1 mnt mundur
        got = compute_remaining_seconds(END_ISO, device_now, skew_for(-60))
        self.assertGreater(
            got, TRUE_REMAINING_S,
            "server yang tertinggal harus menambah sisa waktu, bukan menguranginya",
        )
        self.assertAlmostEqual(
            got, (END_UTC - server_now).total_seconds(), delta=1.0)


class EndTimeParsingTest(unittest.TestCase):
    """Kontrak parsing tidak berubah oleh perbaikan skew."""

    def test_none_and_empty_end_time(self):
        now = datetime.now(timezone.utc)
        self.assertIsNone(compute_remaining_seconds(None, now, 0))
        self.assertIsNone(compute_remaining_seconds("", now, 0))

    def test_unparseable_end_time(self):
        now = datetime.now(timezone.utc)
        for bad in ("not-a-timestamp", "2026-13-45T99:99:99Z", "???"):
            with self.subTest(bad=bad):
                self.assertIsNone(compute_remaining_seconds(bad, now, 0))

    def test_naive_end_time_is_assumed_utc_and_logged(self):
        with self.assertLogs("examvan.ui.timer", level="ERROR") as cap:
            got = compute_remaining_seconds(
                "2026-10-03T12:00:00", SERVER_NOW, 0)
        self.assertEqual(got, TRUE_REMAINING_S)
        self.assertTrue(
            any("UTC" in line for line in cap.output),
            f"tanpa timezone harus dilog, dapat: {cap.output}",
        )

    def test_offset_end_time_is_honoured(self):
        # 12:00+07:00 = 05:00 UTC -> sudah lewat jauh dari 11:00 UTC.
        got = compute_remaining_seconds(
            "2026-10-03T12:00:00+07:00", SERVER_NOW, 0)
        self.assertLess(got, 0)

    def test_z_suffix_is_accepted(self):
        self.assertEqual(
            compute_remaining_seconds(END_ISO, SERVER_NOW, 0), TRUE_REMAINING_S)


class _FixedClock(datetime):
    """Jam dinding yang bisa digeser test.

    Subclass, bukan Mock, supaya `datetime.fromisoformat` di dalam fungsi yang
    diuji tetap benar-benar dipanggil.
    """

    frozen = SERVER_NOW

    @classmethod
    def now(cls, tz=None):
        return cls.frozen if tz else cls.frozen.replace(tzinfo=None)


class WidgetDeadlineTest(unittest.TestCase):
    """Integrasi `_compute_deadline` / `refresh_deadline` pada widget nyata."""

    END_MONO = 5000.0

    def _build(self, end_time, skew_ms, device_now, monotonic=None):
        monotonic = self.END_MONO if monotonic is None else monotonic
        _FixedClock.frozen = device_now
        with mock.patch("examvan.ui.timer._time") as fake_time, \
             mock.patch("examvan.ui.timer.datetime", _FixedClock), \
             mock.patch("examvan.ui.timer.api.get_server_skew_ms",
                        return_value=skew_ms):
            fake_time.monotonic.return_value = monotonic
            w = ElapsedTimerWidget(end_time=end_time)
        self.addCleanup(w.stop)
        self.addCleanup(w.deleteLater)
        return w

    def _refresh(self, w, skew_ms, device_now, monotonic=None):
        monotonic = self.END_MONO if monotonic is None else monotonic
        _FixedClock.frozen = device_now
        with mock.patch("examvan.ui.timer._time") as fake_time, \
             mock.patch("examvan.ui.timer.datetime", _FixedClock), \
             mock.patch("examvan.ui.timer.api.get_server_skew_ms",
                        return_value=skew_ms):
            fake_time.monotonic.return_value = monotonic
            w.refresh_deadline()

    def test_end_mono_encodes_the_server_side_remaining(self):
        offset = -3600  # jam perangkat 1 jam MUNDUR -> tetap harus 1 jam sisa
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        self.assertIsNotNone(w._end_mono, "end_time valid harus punya deadline")
        self.assertAlmostEqual(
            w._end_mono - self.END_MONO, TRUE_REMAINING_S, delta=1.0,
            msg="deadline monotonic harus mencerminkan sisa waktu sisi server",
        )

    def test_end_mono_is_independent_of_the_device_clock(self):
        seen = set()
        for offset in (0, -300, 300, -3600, 3600, -86400, 86400):
            w = self._build(END_ISO, skew_for(offset),
                            SERVER_NOW + timedelta(seconds=offset))
            seen.add(round(w._end_mono - self.END_MONO, 3))
        self.assertEqual(
            len(seen), 1,
            f"_end_mono berubah menurut jam perangkat: {seen}",
        )

    def test_time_up_fires_immediately_when_server_deadline_passed(self):
        # Waktu SERVER sudah 10 menit lewat deadline; jam perangkat 1 jam
        # MUNDUR dari server. Dengan bug tanda, sisa jadi +6600 s (1 jam 50
        # menit) sehingga time_up TIDAK PERNAH menyala dan siswa boleh
        # lanjut menjawab lewat deadline; dengan koreksi, sisanya -600 s.
        server_now = END_UTC + timedelta(seconds=600)
        offset = -3600
        w = self._build(END_ISO, skew_for(offset),
                        server_now + timedelta(seconds=offset))
        self.assertIsNotNone(w._end_mono)
        self.assertLessEqual(
            w._end_mono, self.END_MONO,
            "deadline server sudah lewat, jadi deadline monotonic harus lampau",
        )
        fired = []
        w.time_up.connect(lambda: fired.append(True))
        with mock.patch("examvan.ui.timer._time") as fake_time:
            fake_time.monotonic.return_value = self.END_MONO
            w._update()
        self.assertEqual(
            len(fired), 1, "time_up harus menyala saat deadline benar-benar lewat")

    def test_time_up_fires_with_a_future_deadline_not_yet(self):
        # Kebalikannya: server masih 1 jam sebelum deadline meski jam
        # perangkat 1 hari mundur — time_up belum boleh menyala.
        offset = -86400
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        fired = []
        w.time_up.connect(lambda: fired.append(True))
        with mock.patch("examvan.ui.timer._time") as fake_time:
            fake_time.monotonic.return_value = self.END_MONO + 1
            w._update()
        self.assertEqual(fired, [], "deadline server belum lewat")
        self.assertEqual(w._label.text(), "00:59:59")

    def test_time_up_does_not_fire_early(self):
        w = self._build(END_ISO, 0, SERVER_NOW)
        fired = []
        w.time_up.connect(lambda: fired.append(True))
        # sisa = 3600 s, jadi 1.5 s sebelum deadline tersisa 1.5 s -> 00:00:01.
        with mock.patch("examvan.ui.timer._time") as fake_time:
            fake_time.monotonic.return_value = self.END_MONO + 3598.5
            w._update()
        self.assertEqual(fired, [], "deadline 1 detik lagi belum boleh submitting")
        self.assertEqual(w._label.text(), "00:00:01")

    def test_refresh_deadline_never_extends_past_server_truth(self):
        """Jam dimundurkan tanpa koreksi skew baru: deadline tidak boleh maju."""
        offset = -3600
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        before = w._end_mono
        self._refresh(w, skew_for(offset),
                      SERVER_NOW + timedelta(seconds=offset - 10800))
        self.assertLessEqual(
            w._end_mono, before + 1e-6,
            "rollback jam tidak boleh memperpanjang deadline",
        )

    def test_refresh_deadline_with_corrected_skew_does_not_extend(self):
        """Digabung dengan koreksi: sisa dari server sama, jadi clamp bertahan."""
        offset = -3600
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        before = w._end_mono
        # Jam perangkat maju 10 menit, koreksi skew TIDAK ikut diperbarui
        # (nilai lama), sehingga waktu server terlihat 10 menit lebih dekat ->
        # deadline BERKEPING, tidak pernah maju.
        self._refresh(w, skew_for(offset),
                      SERVER_NOW + timedelta(seconds=offset + 600))
        self.assertLessEqual(w._end_mono, before + 1e-6)

    def test_refresh_deadline_keeps_honest_remaining_with_fresh_skew(self):
        """Clamp min() tetap berlaku: jam maju 10 menit dan koreksi skew
        ikut diperbarui, sisa benar harus tetap 1 jam."""
        offset = -3600
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        new_offset = offset + 600  # jam maju 10 menit sejak koreksi terakhir
        self._refresh(w, skew_for(new_offset),
                      SERVER_NOW + timedelta(seconds=new_offset))
        self.assertAlmostEqual(
            w._end_mono - self.END_MONO, TRUE_REMAINING_S, delta=1.0,
            msg="sisa yang dihitung ulang harus sama dengan sisa sisi server",
        )

    def test_refresh_deadline_shortens_as_server_time_passes(self):
        """Sisa benar tinggal 1 jam lalu 30 menit -> deadline memendek."""
        w = self._build(END_ISO, 0, SERVER_NOW)
        self.assertAlmostEqual(w._end_mono - self.END_MONO, 3600.0, delta=1.0)
        self._refresh(w, 0, SERVER_NOW + timedelta(minutes=30))
        self.assertAlmostEqual(
            w._end_mono - self.END_MONO, 1800.0, delta=1.0,
            msg="setelah 30 menit, sisa harus 30 menit",
        )

    def test_refresh_deadline_clamps_a_moved_back_clock(self):
        """min() clamp tetap berlaku walau koreksi skew ikut dimundurkan.

        Server benar 11:00, deadline 12:00. Kunci koreksi skew-nya masih yang
        lama (perangkat 1 jam mundur) sementara jam perangkat dimundurkan lagi
        3 jam: tanpa clamp, sisa akan naik 4 jam. Dengan clamp, deadline tetap.
        """
        offset = -3600
        w = self._build(END_ISO, skew_for(offset),
                        SERVER_NOW + timedelta(seconds=offset))
        before = w._end_mono
        self._refresh(w, skew_for(offset - 10800),
                      SERVER_NOW + timedelta(seconds=offset - 10800))
        self.assertAlmostEqual(w._end_mono, before, delta=1e-6)

    def test_elapsed_mode_has_no_deadline_even_with_skew(self):
        w = self._build(None, 500000, SERVER_NOW)
        self.assertIsNone(w._end_mono, "tanpa end_time tidak boleh ada deadline")


if __name__ == "__main__":
    unittest.main()