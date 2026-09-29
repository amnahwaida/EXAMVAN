"""Layar penuh yang benar-benar menutupi taskbar Windows.

BUG LAPANGAN (Windows, 30 September 2026)
----------------------------------------
"Belum tertampil benar-benar fullscreen — taskbar di bawah masih keliatan."

Dua hal yang tidak boleh disamakan, dan keduanya sudah tercampur di kode
sebelum modul ini ada:

  * **State fullscreen** (`QWidget.isFullScreen()`) — flag yang menyatakan
    jendela berada pada state fullscreen.
  * **Geometri nyata** — di mana HWND itu benar-benar berada.

Keduanya bisa berbeda, dan pada bug ini memang berbeda: state-nya
`WindowFullScreen`, sementara HWND-nya duduk di `availableGeometry()`.
`availableGeometry()` adalah *area kerja* — layar dikurangi taskbar
(1920x1080 -> 1920x1040). Kalau taskbar autochide, keduanya sama, dan gejala
hilang. Itulah sebabnya laporan ini tidak selalu bisa direproduksi.

Rantainya:

  1. `ExamViewerWindow.__init__` -> `_init_security()` -> enforcer
     `_activate_strict()` memanggil `showFullScreen()` pada window yang belum
     tampil. Qt menampilkannya dan state fullscreen langsung terpasang.

  2. `__main__.main()` memanggil `_maximize_window(viewer, fullscreen=True)`,
     yang lama-lama melakukan `setGeometry(screen.availableGeometry())` —
     memakai AREA KERJA untuk jendela fullscreen.

  3. Pada window yang sudah fullscreen, `setGeometry()` MEMINDAHKAN HWND ke
     area kerja. `showFullScreen()` sesudahnya tidak memperbaiki apa pun,
     karena tidak ada *perubahan* state (sudah `WindowFullScreen`) sehingga
     tidak ada re-layout.

  4. `_enforce_fullscreen()` tidak pernah menolong karena ia hanya menanyakan
     `isFullScreen()`, yang tetap `True` pada kondisi rusak ini.

Jadi predicate "sudah fullscreen?" di sini sengaja menjawab dari GEOMETRI,
bukan dari flag — dan `apply_fullscreen()` memasang state dulu, baru memaksa
rect, karena urutan itulah yang menutup celah di langkah 3.
"""

from __future__ import annotations

from PyQt5.QtCore import QRect
from PyQt5.QtWidgets import QApplication


def fullscreen_geometry(screen) -> QRect:
    """Rect yang harus ditempati jendela fullscreen: SELURUH layar.

    Sengaja `geometry()`, bukan `availableGeometry()`: yang kedua mengecualikan
    taskbar, dan itulah tepat penyebab taskbar tetap terlihat.
    """
    return screen.geometry()


def covers_fullscreen(widget, screen) -> bool:
    """True when `widget` actually fills the whole screen.

    `isFullScreen()` is deliberately NOT consulted. It stays True while the
    HWND sits in the work area, which is the exact state this module exists to
    detect and repair.

    Compared with `>=` on both axes: a window may legitimately report a frame
    a pixel or two larger than the screen (DPI rounding, some multi-monitor
    layouts), and that is still "covering it".
    """
    if screen is None:
        return False
    target = fullscreen_geometry(screen)
    current = widget.frameGeometry()
    return (
        current.width() >= target.width()
        and current.height() >= target.height()
    )


def apply_fullscreen(widget) -> bool:
    """Put `widget` on the whole screen, taskbar included.

    Returns True when the window ends up covering the screen.

    The order is the whole point:

      1. `show()` — Qt ignores state requests on a hidden window.
      2. `showFullScreen()` — sets the state.
      3. `setGeometry()` — forces the actual rect.

    Step 3 is not redundant. If the window was ALREADY in the fullscreen
    state when we got here (which is the case: the enforcer calls
    `showFullScreen()` from the viewer's constructor, before this runs),
    step 2 changes nothing and Qt never re-applies the fullscreen geometry —
    so step 3 is the only thing that actually covers the taskbar. Doing 3
    before 2 instead lets step 2 pull the window straight back into the work
    area.
    """
    screen = widget.screen() or QApplication.primaryScreen()
    if screen is None:
        # Tanpa screen tidak ada yang bisa dipastikan, jadi jangan klaim
        # berhasil — pemanggil memakai nilai balik ini untuk logging.
        widget.show()
        widget.showFullScreen()
        return False

    widget.show()
    widget.showFullScreen()
    widget.setGeometry(fullscreen_geometry(screen))
    return covers_fullscreen(widget, screen)


__all__ = ["fullscreen_geometry", "covers_fullscreen", "apply_fullscreen"]
