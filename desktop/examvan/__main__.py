"""Entry point: python -m examvan [--kiosk] [--kiosk-session]."""

from __future__ import annotations

import atexit
import logging
import os
import signal
import sys
from logging.handlers import RotatingFileHandler


log = logging.getLogger(__name__)


def _setup_logging() -> None:
    """Pasang file logging — satu-satunya jejak saat app error di lapangan.

    Proses GUI (PyInstaller --windowed) tidak punya stderr yang terlihat:
    tanpa handler, semua log.info/warning dari security backend, download
    PDF, dan submit hilang begitu saja dan bug lapangan tidak bisa
    didiagnosis. Log di ~/.config/examvan/app.log (rotating 1 MB × 3)
    agar folder config tidak membengkak.
    """
    try:
        from pathlib import Path
        log_dir = Path.home() / ".config" / "examvan"
        log_dir.mkdir(parents=True, exist_ok=True)
        handler = RotatingFileHandler(
            log_dir / "app.log",
            maxBytes=1_000_000,
            backupCount=3,
            encoding="utf-8",
        )
        handler.setFormatter(logging.Formatter(
            "%(asctime)s %(levelname)s %(name)s: %(message)s"
        ))
        root = logging.getLogger()
        root.setLevel(logging.INFO)
        root.addHandler(handler)
    except OSError:
        # Disk penuh / permission — jalan tanpa logging daripada gagal total.
        pass


def _maximize_window(widget, fullscreen: bool = False) -> None:
    """Show a dialog/window, maximized — or on the whole screen when asked.

    `fullscreen=True` is for the exam window. It must NOT also call
    showMaximized(): that call overrides the fullscreen state the security
    enforcer had just set, and a strict exam ended up merely maximized
    (frameless and always-on-top, but not fullscreen) — which is not what
    "kiosk" is supposed to mean.

    Two details that are easy to get wrong and were both wrong in the field:

    * The rect is `screen.geometry()` (the WHOLE screen, taskbar included),
      not `availableGeometry()` (the work area). Using the work area for a
      fullscreen window is what left the taskbar visible at the bottom.
    * `showFullScreen()` runs BEFORE the geometry is forced. The exam window
      arrives here already in the fullscreen state (the enforcer sets it from
      the viewer constructor), so re-requesting the state is a no-op and only
      the `setGeometry()` after it actually covers the screen. The other order
      lets the state call pull the window back into the work area.

    The non-fullscreen path is unchanged: dialogs keep `showMaximized()`.
    """
    from PyQt5.QtWidgets import QApplication

    from .ui.fullscreen import fullscreen_geometry

    screen = QApplication.primaryScreen()
    if fullscreen:
        widget.show()
        widget.showFullScreen()
        if screen is not None:
            widget.setGeometry(fullscreen_geometry(screen))
    else:
        if screen is not None:
            widget.setGeometry(screen.availableGeometry())
        widget.show()
        widget.showMaximized()
    QApplication.processEvents()


def _recover_gnome_settings() -> None:
    """Restore GNOME desktop settings after a crash (Linux only).

    Safe to call on Windows (no-op).
    Called at startup, on atexit, and on SIGTERM/SIGINT.
    """
    if sys.platform == "win32":
        return
    try:
        from examvan.security.enforcer import SecurityEnforcer
        SecurityEnforcer.restore_gnome_settings()
    except ImportError:
        pass


def _recover_windows_settings() -> None:
    """Restore Windows system settings after a crash (Windows only).

    Mirror of _recover_gnome_settings(). The security backend disables the
    screen saver for the duration of an exam; without this, a process that
    did not exit cleanly left the student's machine altered. Also a no-op on
    Linux, and a no-op when nothing was ever changed.
    """
    if sys.platform != "win32":
        return
    try:
        from examvan.security.windows_backend import restore_windows_settings
        restore_windows_settings()
    except ImportError:
        pass



def main() -> None:
    _setup_logging()

    # Register crash-recovery handlers
    if sys.platform != "win32":
        atexit.register(_recover_gnome_settings)
        signal.signal(signal.SIGTERM, lambda *_: (_recover_gnome_settings(), os._exit(1)))
        signal.signal(signal.SIGINT, lambda *_: (_recover_gnome_settings(), os._exit(1)))
        _recover_gnome_settings()
    else:
        # Windows had `atexit.register(lambda: None)` here — a literal no-op,
        # so the screen-saver change made by the security backend was never
        # undone unless the exam viewer happened to close cleanly. This is the
        # atexit half of the recovery; the other half is WindowsBackend.activate()
        # at startup, which handles the case where the process never got to
        # run atexit at all.
        atexit.register(_recover_windows_settings)
        _recover_windows_settings()

    kiosk = "--kiosk" in sys.argv or "--kiosk-session" in sys.argv

    from PyQt5.QtCore import Qt
    from PyQt5.QtWidgets import QApplication

    # High-DPI support
    QApplication.setAttribute(Qt.AA_EnableHighDpiScaling, True)
    QApplication.setAttribute(Qt.AA_UseHighDpiPixmaps, True)

    app = QApplication(sys.argv)
    app.setApplicationName("EXAMVAN")
    # Dari APP_VERSION, bukan literal: literal ketiga yang tidak terhubung
    # ke mana pun adalah alasan Properties exe dan installer pernah
    # melaporkan nomor berbeda.
    from . import APP_VERSION
    app.setApplicationVersion(APP_VERSION)
    app.setOrganizationName("EXAMVAN")

    # Apply theme (auto-detect system dark/light mode)
    from . import config
    from .ui.styles import is_system_dark, apply_theme
    apply_theme(dark=is_system_dark())

    # Launch server config dialog
    from .ui.server_config import ServerConfigDialog
    from .ui.exam_viewer import ExamViewerWindow

    windows = []  # prevent GC

    def on_exam_selected(exam, server_url, identity_data):
        dialog.hide()

        from .ui.waiting_approval import WaitingApprovalDialog
        from PyQt5.QtWidgets import QDialog
        
        waiting_dlg = WaitingApprovalDialog(
            exam, server_url, identity_data,
            token=dialog.input_token.text().strip().upper(),
            parent=dialog,
        )
        _maximize_window(waiting_dlg)
        
        if waiting_dlg.exec_() != QDialog.Accepted:
            # Siswa membatalkan / ditolak. Dialog konfigurasi kembali — dan
            # tombol "Hubungkan" HARUS dihidupkan lagi.
            #
            # `_on_connect` men-disable tombol itu, dan jalur sukses hanya
            # meng-emit `_sig_show_identity` — tidak pernah `_sig_enable_btn`.
            # Jadi tanpa baris di bawah, dialog muncul kembali dengan tombol
            # masih mati: tidak ada cancel, tidak ada reset, tidak ada jalan
            # lain kecuali menutup aplikasi. Meminta izin lagi dari dialog
            # persetujuan mustahil karena dialog itu sudah tertutup.
            try:
                dialog.enable_connect()
            except Exception:
                log.warning("could not re-enable connect button", exc_info=True)
            dialog.show()
            _maximize_window(dialog)
            return

        viewer = ExamViewerWindow(
            exam=exam,
            server_url=server_url,
            token=dialog.input_token.text().strip().upper(),
            identity_data=identity_data,
            kiosk_mode=kiosk,
        )

        def _on_viewer_closed():
            viewer.close()
            windows.clear()
            # Token DAN identitas harus dibersihkan.
            #
            # Hanya token yang pernah dibersihkan, dan itu justru menyisakan
            # kebocoran yang lebih buruk: `identity_data` dibaca lagi di
            # ServerConfigDialog._show_identity_dialog lalu dipakai untuk
            # MENGISI form IdentityDialog. Siswa berikutnya akan mendapat
            # form terisi nama siswa sebelumnya dan bisa menekan Enter untuk
            # menjawab atas nama orang itu. Tanpa dialog, tanpa warning,
            # tanpa log. Lihat review_windows_2026-09-30.md Bagian 1.
            dialog.input_token.clear()
            try:
                config.clear_identity()
            except Exception:
                log.warning("could not clear stored identity", exc_info=True)
            _maximize_window(dialog)

        viewer.closed.connect(_on_viewer_closed)
        windows.append(viewer)
        # The exam window owns the WHOLE screen, in every security level.
        # Previously this was `fullscreen=viewer.is_strict`, so a medium or
        # low exam was merely maximized and the taskbar stayed visible — and
        # for strict it was still wrong, because _maximize_window used the
        # work area as the fullscreen rect. See examvan.ui.fullscreen.
        _maximize_window(viewer, fullscreen=True)

    dialog = ServerConfigDialog(kiosk_mode=kiosk)
    dialog.exam_selected.connect(on_exam_selected)
    _maximize_window(dialog)

    sys.exit(app.exec_())


if __name__ == "__main__":
    main()
