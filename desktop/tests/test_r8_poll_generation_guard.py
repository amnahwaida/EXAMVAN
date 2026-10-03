"""MEDIUM — penjaga "generasi" di `waiting_approval` hampa, dan ada konstanta mati.

Bug
---
`_on_status_update` menjadwalkan penutupan dialog 1,5 detik setelah approval
dengan:

    QTimer.singleShot(1500, lambda: self._on_approved_delay(self._poll_generation))

Generasi dibaca saat lambda DIJALANKAN — 1,5 detik kemudian — bukan saat
dijadwalkan. Jadi `generation != self._poll_generation` di
`_on_approved_delay` tidak pernah benar, dan penjaga yang dikomentari
"mencegah poller basi menutup dialog yang sudah di-retry" justru tidak
melakukan apa pun.

Alur yang harus ditahan:

    approval datang  ->  singleShot 1500 ms dijadwalkan (generasi = N)
    siswa/tekan "Minta Izin Lagi"  ->  `_stop_polling()` menaikkan generasi ke N+1
    1,5 detik kemudian  ->  lambda membaca N+1, jadi SAMA dengan yang
                           tersimpan  ->  `accept()` menutup dialog yang
                           sedang dipakai untuk siklus BERIKUTNYA

Efeknya dialog approval yang sudah di-retry tertutup sendiri dan
menghasilkan `Accepted`, jadi `__main__` langsung membuka jendela UJIAN —
tanpa ada persetujuan untuk percobaan itu.

masalah kedua: `_POLL_JOIN_TIMEOUT = 12.0` tidak dipakai siapa pun
(`grep` hanya menemukan baris definisinya dan satu docstring test), jadi
nilinya sangat menyesatkan: terbaca sebagai "ada join 12 detik di sini",
padahal `_stop_polling` justru TIDAK joins dengan sengaja (alasannya
dijelaskan panjang di docstring fungsi itu).

Test memakai event loop sungguhan dengan stop keras, karena yang diuji
adalah apa yang terjadi 1,5 detik kemudian.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QTimer
from PyQt5.QtWidgets import QApplication, QDialog

from examvan.models import Exam, SubmitResponse

APP = QApplication.instance() or QApplication([])


def _run_loop(ms: int) -> None:
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


def _dialog():
    from examvan.ui.waiting_approval import WaitingApprovalDialog

    pending = SubmitResponse(
        success=True, status="pending", message="", http_status=200,
    )
    with mock.patch("examvan.ui.waiting_approval.api") as fake_api:
        fake_api.request_approval.return_value = pending
        exam = Exam.from_json({
            "id": 903, "name": "Ujian", "status": "active",
            "security_level": "low",
        })
        dlg = WaitingApprovalDialog(
            exam, "https://exam.example",
            {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
            token="T",
        )
    return dlg


class StaleApprovalGuardTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def test_a_retried_dialog_is_not_closed_by_the_stale_approval(self):
        dlg = _dialog()
        self.addCleanup(dlg._stop_polling)
        self.addCleanup(dlg.deleteLater)
        dlg.show()
        _run_loop(60)

        # Approval datang → penutupan dijadwalkan.
        dlg._on_status_update("approved", "Disetujui!", "Meng mulai...")
        generation_at_approval = dlg._poll_generation

        # Siswa menekan "Minta Izin Lagi" sebelum 1,5 detik berlalu.
        with mock.patch.object(type(dlg), "_start_polling"):
            dlg._retry_approval()

        self.assertNotEqual(
            dlg._poll_generation, generation_at_approval,
            "retry tidak menaikkan generasi — test tidak menguji apa pun",
        )

        # Biarkan lebih dari 1,5 detik waktu nyata berlalu.
        _run_loop(2200)

        self.assertNotEqual(
            dlg.result(), QDialog.Accepted,
            "poller basi menutup dialog yang sudah di-retry: `__main__` "
            "membaca `Accepted` dan langsung membuka jendela UJIAN tanpa "
            "persetujuan untuk percobaan itu. Penjaga generasi membaca "
            "generasi saat lambda DIJALANKAN, bukan saat dijadwalkan.",
        )

    def test_a_fresh_approval_still_closes_the_dialog(self):
        # Kontrol positif: approval yang MASIH relevan harus tetap menutup
        # dialog — penjaga tidak boleh membekukan alur normal.
        dlg = _dialog()
        self.addCleanup(dlg._stop_polling)
        self.addCleanup(dlg.deleteLater)
        dlg.show()
        _run_loop(60)

        dlg._on_status_update("approved", "Disetujui!", "Meng mulai...")
        _run_loop(2200)

        self.assertEqual(
            dlg.result(), QDialog.Accepted,
            "approval yang masih relevan tidak menutup dialog — dialog "
            "approval menggantung selamanya dan siswa tidak bisa masuk",
        )

    def test_a_cancelled_dialog_is_not_closed_by_a_late_approval(self):
        dlg = _dialog()
        self.addCleanup(dlg._stop_polling)
        self.addCleanup(dlg.deleteLater)
        dlg.show()
        _run_loop(60)
        dlg._on_status_update("approved", "Disetujui!", "Meng mulai...")
        dlg.is_waiting = False
        dlg._stop_polling()
        _run_loop(2200)
        self.assertNotEqual(dlg.result(), QDialog.Accepted)


class DeadConstantTestCase(unittest.TestCase):
    def test_the_unused_poll_join_timeout_constant_is_gone(self):
        import inspect

        from examvan.ui import waiting_approval

        source = inspect.getsource(waiting_approval)
        self.assertNotIn(
            "_POLL_JOIN_TIMEOUT", source,
            "`_POLL_JOIN_TIMEOUT` tidak dipakai siapa pun — nilinya "
            "menyiratkan ada join 12 detik di alur ini, padahal "
            "`_stop_polling` dengan sengaja TIDAK melakukan join. Nilai mati "
            "yang bisa disalahbaca sebagai perilaku nyata lebih baik dihapus.",
        )

    def test_stopping_the_poller_does_not_block_the_gui_thread(self):
        # Yang dijaga: `_stop_polling()` tetap tidak melakukan join yang
        # memblokir (jaringan mati → GUI beku 10 detik).
        dlg = _dialog()
        self.addCleanup(dlg._stop_polling)
        self.addCleanup(dlg.deleteLater)
        dlg.show()
        _run_loop(30)
        start = _monotonic()
        dlg._stop_polling()
        self.assertLess(
            _monotonic() - start, 1.0,
            "`_stop_polling()` memblokir — poller lama bisa terjebak 10 "
            "detik di dalam panggilan HTTP, jadi dialog terlihat beku",
        )


def _monotonic() -> float:
    import time

    return time.monotonic()


if __name__ == "__main__":
    unittest.main()