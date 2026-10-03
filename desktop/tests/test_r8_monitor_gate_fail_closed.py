"""H8 (TINGGI) — gate monitor-ganda strict tidak pernah bisa gagal-terutup.

Bug
---
`windows_backend._has_multiple_monitors()`:

    try:
        return _GetSystemMetrics(SM_CMONITORS) > 1
    except Exception:
        return False

Dua pemanggilnya (`server_config._strict_monitor_ok` dan
`__main__._start_exam`) sama-sama mendokumentasikan policy FAIL-CLOSED —
"detector exception = tolak" — dan punya cabang `except Exception` yang
menampilkan "Tidak Dapat Memeriksa Layar" lalu menolak. Cabang itu TIDAK PERNAH
terEKsekusi: `except` di atas hanya menangkap exception Python-level dari
`get_backend()`, sedangkan `_GetSystemMetrics` yang tidak ada akan.Resolve lewat
module `__getattr__` → `_bind()` → melempar AttributeError yang converted
menjadi `False`.

Jujur, `WindowsBackend.__init__` menelan kegagalan `_bind()` dengan
`log.warning` saja, lalu `_GetSystemMetrics` tidak pernah terikat.

Akibatnya: di mesin yang binding Win32-nya gagal, ujian STRICT lançamento
dengan monitor sebanyak apa pun dan tanpa satu pesan pun.

Test di bawah menyuntikkan `_GetSystemMetrics` yang melempar, lalu memeriksa
dua lapis: (1) backend tidak lagi melaporkan "satu monitor" untuk kondisi
"TIDAK BISA DIPASTIKAN"; (2) gate strict yang memakainya benar-benar menolak
dengan pesan, bukan melepas ujian.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.security import windows_backend as wb

APP = QApplication.instance() or QApplication([])


def _raising_metrics():
    """`_GetSystemMetrics` yang selalu melempar (binding Win32 gagal)."""

    def _get(index):  # noqa: ARG001 — bentuknya harus sama dengan Win32
        raise OSError("GetSystemMetrics tidak bisa dipanggil")

    return _get


class MonitorDetectionTestCase(unittest.TestCase):
    def test_a_raising_probe_is_not_reported_as_a_single_monitor(self):
        backend = wb.WindowsBackend()
        with mock.patch.object(wb, "_GetSystemMetrics", _raising_metrics(),
                                                    create=True):
            with self.assertRaises(
                (RuntimeError, OSError, AttributeError, ImportError),
                msg="detektor yang gagal TIDAK boleh dijawab 'satu monitor': "
                    "keduanya pemanggil (_strict_monitor_ok, __main__)"
                    "dokumentasikan policy fail-CLOSED, jadi kegagalan "
                    "harus sampai ke mereka sebagai exception",
            ):
                backend.has_multiple_monitors()

    def test_a_zero_monitor_count_is_also_undeterminable(self):
        # `GetSystemMetrics` mengembalikan 0 saat gagal; 0 monitor bukan
        # "satu monitor", itu "tidak diketahui".
        backend = wb.WindowsBackend()
        with mock.patch.object(wb, "_GetSystemMetrics", lambda index: 0,
                                       create=True):
            with self.assertRaises(RuntimeError):
                backend.has_multiple_monitors()

    def test_a_failed_bind_is_remembered_and_fails_closed(self):
        # Kegagalan `_bind()` di `__init__` dulu hanya `log.warning`, jadi
        # tidak ada jejak apa pun selain log dan hasil yang SALAH.
        backend = wb.WindowsBackend.__new__(wb.WindowsBackend)
        backend._bind_failed = True
        with self.assertRaises(RuntimeError):
            backend.has_multiple_monitors()

    def test_a_working_probe_still_answers_normally(self):
        # Kontrol positif: satu monitor = False, dua = True. Jalur ini TIDAK
        # boleh berubah.
        for reported, expected in ((1, False), (2, True), (4, True)):
            with self.subTest(monitors=reported):
                backend = wb.WindowsBackend()
                backend._bind_failed = False
                with mock.patch.object(wb, "_GetSystemMetrics",
                                       lambda index, n=reported: n,
                                       create=True):
                    self.assertIs(
                        backend.has_multiple_monitors(), expected,
                    )


class StrictGateUsesTheDetectorTestCase(unittest.TestCase):
    """Gate strict yang memakai hasilnya harus benar-benar menolak."""

    def test_strict_exam_is_refused_when_the_detector_fails(self):
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.hide)
        self.addCleanup(dlg.deleteLater)

        backend = wb.WindowsBackend()
        with mock.patch.object(wb, "_GetSystemMetrics", _raising_metrics(),
                                                    create=True), \
             mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch("examvan.ui.server_config.QMessageBox") as box:
            allowed = dlg._strict_monitor_ok()

        self.assertFalse(
            allowed,
            "ujian strict DILEPASkan saat jumlah monitor tidak bisa "
            "dipastikan — policy fail-CLOSED di docstring fungsi ini "
            "menjadi tidak berlaku",
        )
        self.assertTrue(
            box.warning.called,
            "penolakan harus accompanied pesan yang bisa ditindak; "
            "melepas ujian tanpa pesan apa pun lebih buruk",
        )
        self.assertEqual(
            dlg.lbl_status.text(), "Pemeriksaan layar gagal — coba lagi.",
            "siswa tidak diberi tahu kenapa tombolnya tidak bereaksi",
        )

    def test_strict_exam_still_starts_on_a_genuine_single_monitor(self):
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.hide)
        self.addCleanup(dlg.deleteLater)

        backend = wb.WindowsBackend()
        with mock.patch.object(wb, "_GetSystemMetrics", lambda index: 1,
                                                    create=True), \
             mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch("examvan.ui.server_config.QMessageBox") as box:
            allowed = dlg._strict_monitor_ok()

        self.assertTrue(
            allowed,
            "satu monitor yang benar-benar terdeteksi harus tetap "
            "MEMASUKKAN ujian strict — fail-closed tidak boleh berarti "
            "tidak ada ujian yang bisa dimulai",
        )
        self.assertFalse(box.warning.called)


if __name__ == "__main__":
    unittest.main()