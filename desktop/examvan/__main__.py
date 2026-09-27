"""Entry point: python -m examvan [--kiosk] [--kiosk-session]."""

from __future__ import annotations

import atexit
import logging
import os
import signal
import sys
from logging.handlers import RotatingFileHandler


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


def _maximize_window(widget) -> None:
    """Maximize a window reliably on XWayland and Wayland."""
    from PyQt5.QtWidgets import QApplication
    screen = QApplication.primaryScreen()
    if screen:
        geo = screen.availableGeometry()
        widget.setGeometry(geo)
    widget.show()
    widget.showMaximized()
    QApplication.processEvents()


def _recover_gnome_settings() -> None:
    """Restore GNOME settings from crash backup (Linux only).

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


def main() -> None:
    _setup_logging()

    # Register crash-recovery handlers (Linux GNOME settings)
    if sys.platform != "win32":
        atexit.register(_recover_gnome_settings)
        signal.signal(signal.SIGTERM, lambda *_: (_recover_gnome_settings(), os._exit(1)))
        signal.signal(signal.SIGINT, lambda *_: (_recover_gnome_settings(), os._exit(1)))
        _recover_gnome_settings()
    else:
        # Windows: register minimal exit handler
        atexit.register(lambda: None)

    kiosk = "--kiosk" in sys.argv or "--kiosk-session" in sys.argv

    from PyQt5.QtCore import Qt
    from PyQt5.QtWidgets import QApplication

    # High-DPI support
    QApplication.setAttribute(Qt.AA_EnableHighDpiScaling, True)
    QApplication.setAttribute(Qt.AA_UseHighDpiPixmaps, True)

    app = QApplication(sys.argv)
    app.setApplicationName("EXAMVAN")
    app.setApplicationVersion("1.0.0")
    app.setOrganizationName("EXAMVAN")

    # Apply theme (auto-detect system dark/light mode)
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
            # Clear saved token so user must re-enter for next exam
            dialog.input_token.clear()
            _maximize_window(dialog)

        viewer.closed.connect(_on_viewer_closed)
        windows.append(viewer)
        _maximize_window(viewer)

    dialog = ServerConfigDialog(kiosk_mode=kiosk)
    dialog.exam_selected.connect(on_exam_selected)
    _maximize_window(dialog)

    sys.exit(app.exec_())


if __name__ == "__main__":
    main()
