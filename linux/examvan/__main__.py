"""Entry point: python -m examvan [--kiosk] [--kiosk-session]."""

from __future__ import annotations

import atexit
import os
import signal
import sys


def _maximize_window(widget) -> None:
    """Maximize a window reliably on XWayland and Wayland.

    showMaximized() may not work under xcb because the WM hasn't processed
    the request before the window is painted. Explicitly set geometry as
    fallback, and process events so the WM can respond.
    """
    from PyQt5.QtWidgets import QApplication
    screen = QApplication.primaryScreen()
    if screen:
        geo = screen.availableGeometry()
        widget.setGeometry(geo)
    widget.show()
    widget.showMaximized()
    QApplication.processEvents()


def _recover_gnome_settings() -> None:
    """Restore GNOME settings from crash backup file.

    Called at startup, on atexit, and on SIGTERM/SIGINT.
    Safe to call when no backup file exists (no-op).
    """
    # Lazy import to keep startup fast when not needed
    from examvan.security.enforcer import SecurityEnforcer
    SecurityEnforcer.restore_gnome_settings()


def main() -> None:
    # Register crash-recovery handlers BEFORE anything touches GNOME settings
    atexit.register(_recover_gnome_settings)
    signal.signal(signal.SIGTERM, lambda *_: (_recover_gnome_settings(), os._exit(1)))
    signal.signal(signal.SIGINT, lambda *_: (_recover_gnome_settings(), os._exit(1)))

    # Restore GNOME settings in case previous session crashed
    _recover_gnome_settings()

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
