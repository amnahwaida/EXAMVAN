"""MEDIUM — sapu PDF jalan SEBELUM kunci instansi-tunggal diambil.

Bug
---
Di `__main__.main()` urutannya terbalik:

    :327  _sweep_stale_exam_pdfs()
    :368  CreateMutexW("EXAMVAN_SingleInstance_v1")

Docstring sapu membenarkan keamanannya dengan mutex: "Mutex
instansi-tunggal menjamin tidak ada proses EXAMVAN lain yang sedang
mengunduh saat sapu ini jalan" — padahal mutex itu belum diambil 40 baris
kemudian. Di Linux tidak ada mutex sama sekali, dan di Windows mutex
`CreateMutexW` tanpa awalan `Global`/`Local` milik SESI pemanggil, jadi dua
sesi di satu mesin masing-masing bisa menjalankan app (penaluhan itu
disimpulkan dari semantik Win32, tidak bisa dieksekusi di runner ini).

Konsekuensi nyata: instance yang sedang mengunduh `./api/exams/<id>/pdf`
bisa kehilangan file temp-nya karena instance lain menyapu %TEMP%.

Yang diuji di sini adalah URUTAN yang terlihat, bukan isi internal:
`main()` dipanggil sungguhan (dengan `ctypes.windll` tiruan supaya jalur
Win32 dieksekusi di Linux), dan dua peristiwa direkam: "mutex diambil" dan
"sapu jalan". Sentinel dari sapu menghentikan `main()` tepat setelahnya,
jadi test tidak perlu menjalankan UI.
"""

from __future__ import annotations

import os
import sys
import types
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import __main__ as main_mod
from examvan import config

APP = QApplication.instance() or QApplication([])


class _StopAfterSweep(Exception):
    """Sentinel: hentikan `main()` tepat setelah sapu (belum ada UI)."""


class _ExistingApplication:
    """`QApplication` selama `main()` — instance milik test (PyQt5 = satu saja)."""

    def __new__(cls, *args, **kwargs):
        return APP

    setAttribute = staticmethod(QApplication.setAttribute)
    instance = staticmethod(QApplication.instance)
    processEvents = staticmethod(QApplication.processEvents)
    clipboard = staticmethod(QApplication.clipboard)
    activePopupWidget = staticmethod(QApplication.activePopupWidget)
    topLevelWidgets = staticmethod(QApplication.topLevelWidgets)
    primaryScreen = staticmethod(QApplication.primaryScreen)


def _fake_windll(events: list):
    """`ctypes.windll` tiruan yang mencatat saat mutex benar-benar diambil.

    Dipasang ke modul `ctypes` sungguhan (bukan Mock) karena `main()` melakukan
    `from ctypes import windll` — jadi yang harus ada atribut `windll` di
    `ctypes`, dengan `kernel32.CreateMutexW` + `GetLastError`.
    """

    class _Kernel32:
        def CreateMutexW(self, attrs, owner, name):
            events.append(("mutex", name))
            return 4321

        def GetLastError(self):
            return 0

    return types.SimpleNamespace(kernel32=_Kernel32())


class StartupOrderTestCase(unittest.TestCase):
    def _run_main(self) -> list:
        events: list = []
        real_sweep = main_mod._sweep_stale_exam_pdfs

        def _sweep():
            events.append(("sweep", None))
            raise _StopAfterSweep()

        with mock.patch.object(main_mod, "_setup_logging"), \
             mock.patch.object(main_mod, "_recover_gnome_settings"), \
             mock.patch.object(main_mod, "_recover_windows_settings"), \
             mock.patch.object(main_mod, "atexit"), \
             mock.patch.object(main_mod, "signal"), \
             mock.patch.object(config, "clear_stale_identity_on_startup",
                               return_value=False), \
             mock.patch.object(main_mod, "_sweep_stale_exam_pdfs", _sweep), \
             mock.patch("ctypes.windll", _fake_windll(events), create=True), \
             mock.patch.object(sys, "platform", "win32"), \
             mock.patch.object(sys, "argv", ["examvan"]), \
             mock.patch("PyQt5.QtWidgets.QApplication", _ExistingApplication):
            with self.assertRaises(_StopAfterSweep):
                main_mod.main()
        # Sapu asli harus tetap bisa dipanggil (dipakai test lain).
        self.assertTrue(callable(real_sweep))
        return events

    def test_the_lock_is_taken_before_the_pdf_sweep_runs(self):
        events = self._run_main()
        kinds = [e[0] for e in events]
        self.assertIn(
            "mutex", kinds,
            "mutex instansi-tunggal tidak pernah diambil pada jalur Win32 "
            "— single instance mati total",
        )
        self.assertIn("sweep", kinds, "sapu PDF tidak jalan sama sekali")
        self.assertLess(
            kinds.index("mutex"), kinds.index("sweep"),
            "sapu PDF jalan SEBELUM mutex diambil: instance lain yang sedang "
            "mengunduh naskah ujian bisa file temp-nya dihapus dari %TEMP%",
        )
        self.assertEqual(
            events[[k for k, _ in events].index("mutex")][1],
            "EXAMVAN_SingleInstance_v1",
            "nama mutex berubah — pasangan dengan AppMutex di examvan.iss "
            "putus, pemeriksaan installer tidak pernah menyala",
        )


class NoUiBeforeTheSweepTestCase(unittest.TestCase):
    """Sapu tidak boleh butuh UI: ia jalan sebelum `ServerConfigDialog`."""

    def test_the_sweep_needs_no_qt_widget(self):
        # Menjaga bahwa memindahkan sapu ke setelah mutex tidak menyeretnya
        # ke dalam jalur yang butuh QApplication diconstructed penuh.
        events: list = []

        with mock.patch("tempfile.gettempdir", return_value="/nonexistent-r8"):
            main_mod._sweep_stale_exam_pdfs()
        # Tidak melempa, tidak butuh app sama sekali.
        self.assertEqual(events, [])


if __name__ == "__main__":
    unittest.main()