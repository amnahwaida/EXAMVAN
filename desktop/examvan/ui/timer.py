"""Timer widget — countdown or elapsed depending on exam config.

Perlindungan terhadap jam sistem ada DUA LAPIS, dan keduanya harus jujur
dinyatakan:

1. `end_time` datang dari server dan tidak bisa diubah siswa, dan deadline
   dipinjam jadi titik monotonic SEKALI saat widget dibangun (`_end_mono`).
   Selama proses berjalan, label countdown dihitung dari
   `time.monotonic()` — jam dinding tidak bisa menggesernya.
2. `refresh_deadline()` (dipanggil tiap kali window regain focus) menghitung
   ULANG deadline dari jam dinding, karena `monotonic()` tidak ikut jalan
   saat laptop suspend dan countdown akan membeku. Hasilnya di-clamp dengan
   `min()`, jadi hitung ulang hanya boleh MEMPERCEPAT, tidak pernah
   memperpanjang.

Jam dinding TIDAK pernah "dipulihkan" atau dibetonkan: ia tetap sumber
kebenaran untuk hitung ulang, dan koreksi `skew` itulah yang membuatnya setara
dengan waktu server. Yang dinetralkan adalah manipulasi jam lewat koreksi
`skew` (lihat `compute_remaining_seconds`), bukan lewat jam monotonic sebagai
sumber deadline.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from typing import Optional

from PyQt5.QtCore import QTimer, Qt, pyqtSignal
from PyQt5.QtWidgets import QLabel, QWidget, QHBoxLayout

import logging as _logging
import time as _time

from .. import api

_log = _logging.getLogger(__name__)


def compute_remaining_seconds(
    end_time: Optional[str], now_utc: datetime, skew_ms: int
) -> Optional[float]:
    """Sisa waktu (detik) sampai deadline, atau None bila end_time rusak.

    Murni & bisa diuji. `now_utc` adalah waktu PERANGKAT; `skew_ms` =
    server - perangkat (`api.compute_server_skew_ms`, yang menghitung
    `server - device_now`).

    Aritmetikanya: sisa benar = `end - (perangkat + skew)` = `end - server_now`.
    Jadi skew DIKURANGKAN, persis seperti referensi Android
    (`ExamDeadline.kt:28`: `endInstantMs - (nowMs + skewMs)`). Hasilnya tidak
    bergantung jam perangkat sama sekali — itulah seluruh tujuan koreksinya,
    dan itulah yang membuat memundurkan jam OS tidak menambah waktu ujian.

    Bug yang diperbaiki (C1): tanda sebelumnya `+ skew`, sehingga
    `(end - perangkat) + skew` = `sisa_benar + 2*skew`. Siswa yang mundurkan
    jam 1 jam mendapat 2 jam tambahan, dan PC yang jamnya 1 jam cepat langsung
    auto-submit. Bukti eksekusi (fungsi lama):

        offset perangkat    skew_ms    kembali    BENAR    delta
        sinkron                   0       3600     3600       0
        5 menit mundur     300000       4200     3600     +600
        1 jam mundur      3600000      10800     3600    +7200
        1 jam maju       -3600000      -3600     3600    -7200

    Tanda sisa waktu TIDAK ditentukan oleh tanda skew. Skew negatif berarti
    server DI BEHIND perangkat, dan itu menambah sisa waktu; yang menentukan
    tanda sisa hanyalah perbandingan `end` dengan waktu server.
    """
    if not end_time:
        return None
    try:
        end_wall = datetime.fromisoformat(end_time.replace("Z", "+00:00"))
        if end_wall.tzinfo is None:
            # end_time naive (tanpa offset): dulu raise di aritmetika
            # naive-aware → None → ujian diam-diam jadi mode elapsed tanpa
            # deadline. Asumsikan UTC supaya deadline tetap berlaku.
            _log.error("end_time tanpa timezone, diasumsikan UTC: %r", end_time)
            end_wall = end_wall.replace(tzinfo=timezone.utc)
        return (end_wall - now_utc).total_seconds() - (skew_ms / 1000.0)
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
        # Nilai start diabadikan SEKALI di sini: get_start_time_iso() tidak
        # boleh dihitung ulang dari jam dinding tiap dipanggil (drift).
        self._start_iso = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        self._compute_deadline()

        self._setup_ui()
        self._timer = QTimer(self)
        self._timer.timeout.connect(self._update)
        self._timer.start(1000)
        # JANGAN panggil self._update() langsung di sini: untuk ujian yang
        # sudah lewat deadline itu meng-emit time_up SEBELUM ada listener
        # yang terhubung, latch _fired_time_up terbakar selamanya dan
        # re-entry tidak pernah auto-submit. Jadwalkan ke event loop agar
        # listener sempat terhubung dulu.
        QTimer.singleShot(0, self._update)

    def _compute_deadline(self) -> None:
        """Wall-clock deadline server -> titik monotonic lokal.

        `remaining` di sini sudah DIBERSIHKAN dari jam perangkat: dengan
        koreksi skew yang benar, sisanya adalah `end - server_now`, jadi
        builder widget ini tidak lagi bisa dipakai memperpanjang atau
        memendekkan ujian dengan mengubah jam OS. Satu-satunya jam yang
        dipakai sesudah titik ini di-pinjam adalah `monotonic`.
        """
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
        tertidur countdown akan membeku. Jam dinding dipakai lagi untuk itu.

        Dua hal yang harus dibedakan, karena keduanya sering dicampur:

          * memanipulasi jam TIDAK menambah waktu. `remaining` sudah
            dinetralkan terhadap jam perangkat oleh koreksi skew
            (`compute_remaining_seconds`), jadi menggeser jam hanya
            menggeser sisa sebesar yang tidak dikoreksi — yaitu nol.
          * kekosongan monotonic saat suspend BUKAN manipulasi dan tidak
            boleh diperbaiki dengan memperpanjang deadline. Itu sebabnya
            clampnya tetap `min()`.

        Arahnya fail-secure:

          * waktu server berjalan / jam dimajukan -> sisa mengecil,
            deadline BERKEPING (aman)
          * jam dimundurkan tanpa koreksi baru -> sisa membesar, deadline
            TETAP (dibaikan)

        Sisa yang benar sudah sama untuk semua offset jam perangkat, jadi
        clamp `min()` kini hampir tidak pernah bekerja pada jalur happy —
        ia tetap ada sebagai jaring pengaman untuk kasus saat koreksi skew
        belum tersedia (`skew_ms == 0`) atau basi karena health check
        gagal diperbarui.
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

        Nilai diabadikan saat __init__ supaya tidak drift dari jam dinding.
        """
        return self._start_iso

    def stop(self) -> None:
        self._timer.stop()
