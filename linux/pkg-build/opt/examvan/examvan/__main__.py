"""Entry point: python -m examvan [--kiosk] [--kiosk-session]."""

from __future__ import annotations

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


def _ensure_gnome_restored() -> None:
    """Restore GNOME settings that may have been changed by strict mode.

    Protects against crash/force-kill before deactivate() could restore.
    """
    import subprocess
    # Check if dynamic-workspaces was left as false by a crashed session
    try:
        r = subprocess.run(
            ["gsettings", "get", "org.gnome.mutter", "dynamic-workspaces"],
            capture_output=True, text=True, timeout=3,
        ).stdout.strip()
        if r == "false":
            # Restore defaults
            commands = [
                ["gsettings", "set", "org.gnome.mutter", "dynamic-workspaces", "true"],
                ["gsettings", "set", "org.gnome.desktop.wm.preferences", "num-workspaces", "4"],
                ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-overview", "['<Super>s']"],
                ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-application-view", "['<Super>a']"],
                ["gsettings", "set", "org.gnome.mutter", "overlay-key", "'Super_L'"],
                ["gsettings", "set", "org.gnome.desktop.interface", "enable-hot-corners", "true"],
                ["gsettings", "set", "org.gnome.desktop.peripherals.touchpad", "send-events", "enabled"],
            ]
            for cmd in commands:
                try:
                    subprocess.run(cmd, capture_output=True, timeout=3)
                except Exception:
                    pass
    except Exception:
        pass


def main() -> None:
    # Restore GNOME settings in case previous session crashed before deactivate()
    _ensure_gnome_restored()

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
