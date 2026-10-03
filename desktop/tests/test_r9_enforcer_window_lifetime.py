"""Regresi ronde 9 — enforcer harus mati bersama window-nya.

Crash yang ditemukan CI
-----------------------
`python -m unittest discover -s tests` di CI berakhir `Aborted (core dumped)`,
exit 134, dengan jejak:

    File "examvan/security/enforcer.py", line 997, in _poll_focus
        if not self._window.isActiveWindow():
    RuntimeError: wrapped C/C++ object of type ExamViewerWindow has been deleted

Di runner lokal suite justru hijau, jadi ini GAP yang tidak terlihat dari
lokal:`_poll_timer` berdetak tiap 500 ms, jadi hinge-nya hanya menyala kalau
jendela benar-benar dihapus (bukan disembunyikan) tepat saat timer aktif.

Akar masalahnya struktural, bukan timing
----------------------------------------
`ExamViewerWindow._init_security` memanggil

    SecurityEnforcer(security_level=..., window=self, ...)

TANPA `parent=`. `SecurityEnforcer` adalah `QObject` dengan parameter
`parent=None`, jadi enforcer TIDAK anak dari jendela — dan `QTimer(self)`
pun ter-induk ke enforcer, bukan ke jendela. Akibatnya ketika objek C++
jendela dihapus (`WA_DeleteOnClose`, `deleteLater`, atau viewportQPixmap
sekali pakai), timer tetap hidup dan `_poll_focus` menyentuh objek yang
sudah mati. `RuntimeError` di dalam slot Qt = `qFatal` = SIGABRT.

Ini bug PRODUKSI, bukan artefak tes: di aplikasi asli jendela ujian punya
`WA_DeleteOnClose`, dan ada beberapa jalur yang menutup jendela tanpa
`deactivate()` lebih dulu.

Perbaikan dua lapis, keduanya diuji di sini
-------------------------------------------
1. STRUKTUR: enforcer di-parent-kan ke jendela, jadi seluruh timer-nya ikut
   mati bersama jendela. Ini yang menghilangkan akar masalahnya.
2. PEMBELAAN: semua jalan yang menyentuh `self._window` recycle kalau
   objek C++-nya sudah dihapus, sehingga urutan teardown yang mana pun tidak
   lagi bisa mengubah aplikasi menjadi abort. Lapis kedua bukan pengganti lapis
   pertama: `_init_security` tetap dipanggil di banyak tes dengan
   `__new__` sehingga parenting tidak selalu ada.
"""

from __future__ import annotations

import os
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QCoreApplication, QTimer
from PyQt5.QtWidgets import QApplication

from examvan.security import enforcer as enforcer_mod

APP = QApplication.instance() or QApplication([])


def _delete_cpp(obj) -> None:
    """Hapus objek C++ secara pasti, bukan lewat `deleteLater()` saja.

    `deleteLater()` hanya menjadwalkan; peristiwa-nya butuh event loop
    yang sehat, dan justru itulah yang sedang kita tes. `sip.delete()`
    memaksa penghapusan supaya kondisi "C++ object has been deleted"
    benar-benar terjadi.
    """
    try:
        import sip  # PyQt5
    except ImportError:  # pragma: no cover
        from PyQt5 import sip  # type: ignore
    sip.delete(obj)


class EnforcerIsParentedToItsWindowTest(unittest.TestCase):
    def test_the_viewer_parents_the_enforcer_to_the_window(self):
        from examvan.ui import exam_viewer as ev

        src = open(ev.__file__, encoding="utf-8").read()
        self.assertIn(
            "parent=self",
            src,
            "SecurityEnforcer dipanggil tanpa `parent=`, jadi enforcer (dan "
            "QTimer-nya) tidak ikut mati ketika jendela dihapus — itulah "
            "akar RuntimeError di `_poll_focus` (CI exit 134)",
        )


class PollTimerSurvivesWindowDeletionTest(unittest.TestCase):
    def _make_enforcer(self, window, level="medium", strict=False):
        en = enforcer_mod.SecurityEnforcer(
            security_level=level,
            strict_mode=strict,
            window=window,
        )
        # `_poll_focus` return awal kalau belum aktif, jadi tanpa ini test
        # hanya menguji jalur yang tidak melakukan apa-apa — persis jebakan
        # yang membuat crash ini lolos dari suite lokal.
        en._active = True
        en._focus_guard_paused = False
        en._app_popup_open = lambda: False
        return en

    def test_poll_focus_is_a_noop_once_the_window_cpp_object_is_gone(self):
        from PyQt5.QtWidgets import QWidget

        win = QWidget()
        en = self._make_enforcer(win)
        # Paksa timer tetap hidup seperti pada ExamViewer nyata.
        en._poll_timer = QTimer(en)
        en._poll_timer.setInterval(10)
        en._poll_timer.timeout.connect(en._poll_focus)
        en._poll_timer.start()

        _delete_cpp(win)
        # Detak manual: kalau `_poll_focus` tidak reciclaje, ini melempar
        # RuntimeError persis seperti yang membuat CI abort.
        en._poll_focus()  # tidak boleh melempar

        en._poll_timer.stop()

    def test_a_dead_window_does_not_start_the_countdown(self):
        from PyQt5.QtWidgets import QWidget

        win = QWidget()
        en = self._make_enforcer(win)
        en._focus_timer = QTimer(en)
        _delete_cpp(win)
        en._poll_focus()
        self.assertFalse(
            en._focus_timer.isActive(),
            "countdown auto-submit dimulai untuk jendela yang sudah tidak ada",
        )
        en._focus_timer.stop()

    def test_strict_paths_tolerate_a_dead_window(self):
        # `raise_`/`activateWindow`/`ClipCursor` menyentuh jendela langsung.
        from PyQt5.QtWidgets import QWidget

        win = QWidget()
        en = self._make_enforcer(win, level="strict", strict=True)
        en._focus_timer = QTimer(en)
        _delete_cpp(win)
        # Semua jalur ini harus kembali diam-diam, bukan melempar.
        en._reassert_strict_window() if hasattr(en, "_reassert_strict_window") else None
        en._poll_focus()
        en._focus_timer.stop()

    def test_the_poll_timer_does_not_outlive_its_parent(self):
        # Parenting adalah perbaikannya yang struktural; kalau ini gagal,
        # Parenting adalah perbaikannya yang struktural; tanpa itu
        # `_poll_focus` hanya bisaDepends on membrane order.
        from PyQt5.QtWidgets import QWidget

        win = QWidget()
        en = enforcer_mod.SecurityEnforcer(
            security_level="medium",
            strict_mode=False,
            window=win,
            parent=win,
        )
        en._active = True
        self.assertIs(
            en.parent(), win,
            "enforcer harus anak dari jendela supaya timernya ikut mati",
        )


if __name__ == "__main__":
    unittest.main()