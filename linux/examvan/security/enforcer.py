"""Security Enforcer — dispatches low/medium/strict mode enforcement.

Low:    anti-screenshot, clipboard clear, screen wake lock
Medium: all low + focus loss detection → auto-submit
Strict: all medium + keyboard grab, pointer grab, fullscreen, kiosk
"""

from __future__ import annotations

import logging
import os
import subprocess
from typing import Optional

from PyQt5.QtCore import QObject, Qt, QTimer, pyqtSignal
from PyQt5.QtGui import QCloseEvent
from PyQt5.QtWidgets import QApplication, QWidget

from . import x11
from ..utils import clear_clipboard, clear_clipboard_wl

log = logging.getLogger(__name__)


class SecurityEnforcer(QObject):
    """Enforces exam security based on mode."""

    auto_submit = pyqtSignal()

    def __init__(
        self,
        security_level: str = "low",
        strict_mode: bool = False,
        window: Optional[QWidget] = None,
        kiosk_mode: bool = False,
        parent=None,
    ):
        super().__init__(parent)
        self._level = security_level
        self._strict = strict_mode or security_level == "strict"
        self._window = window
        self._kiosk = kiosk_mode
        self._active = False
        self._inhibit_pid: Optional[int] = None
        self._clipboard_timer = QTimer(self)
        self._clipboard_timer.timeout.connect(self._clear_clipboard)
        self._focus_timer = QTimer(self)
        self._focus_timer.setSingleShot(True)
        self._focus_timer.setInterval(3000)  # 3 seconds
        self._focus_timer.timeout.connect(self._on_focus_timeout)
        self._grab_held = False
        self._gnome_ws_backup: Optional[str] = None  # backup dynamic-workspaces
        self._overlay_backup: Optional[str] = None  # backup overlay-key
        self._hot_corners_backup: Optional[str] = None  # backup hot-corners

    def activate(self) -> None:
        """Activate security enforcement based on mode."""
        if self._active:
            return
        self._active = True
        log.info("Activating security: level=%s, strict=%s", self._level, self._strict)

        # Low mode features (always active)
        self._activate_low()

        if self._level in ("medium",) or self._strict:
            self._activate_medium()

        if self._strict:
            self._activate_strict()

    def deactivate(self) -> None:
        """Deactivate all security enforcement."""
        if not self._active:
            return
        self._active = False
        log.info("Deactivating security")

        # Stop timers
        self._clipboard_timer.stop()
        self._focus_timer.stop()
        if hasattr(self, '_poll_timer'):
            self._poll_timer.stop()

        # Release X11 grabs
        if self._grab_held:
            x11.ungrab_keyboard()
            x11.ungrab_pointer()
            self._grab_held = False

        # Restore GNOME workspace switching + remove gesture-blocker extension
        self._gnome_ws_restore()
        self._gnome_ext_remove()

        # Kill wake lock
        self._stop_inhibit()

    # -----------------------------------------------------------------------
    # GNOME workspace lock (block desktop switching gestures)
    # -----------------------------------------------------------------------

    def _gnome_ws_lock(self) -> None:
        """Disable GNOME workspace switching + overview gestures.

        Blocks:
        - 3-finger swipe left/right (workspace switch) — num-workspaces=1
        - 3-finger swipe up (overview) — clear toggle-overview binding
        - 3-finger swipe down (app view) — clear toggle-application-view binding
        - Super key gestures — clear overlay-key
        """
        # Backup dynamic-workspaces
        try:
            r = subprocess.run(
                ["gsettings", "get", "org.gnome.mutter", "dynamic-workspaces"],
                capture_output=True, text=True, timeout=3,
            )
            self._gnome_ws_backup = r.stdout.strip()
        except Exception:
            self._gnome_ws_backup = None

        # Backup overlay-key
        try:
            r = subprocess.run(
                ["gsettings", "get", "org.gnome.mutter", "overlay-key"],
                capture_output=True, text=True, timeout=3,
            )
            self._overlay_backup = r.stdout.strip()
        except Exception:
            self._overlay_backup = None

        # Backup hot-corners
        try:
            r = subprocess.run(
                ["gsettings", "get", "org.gnome.desktop.interface", "enable-hot-corners"],
                capture_output=True, text=True, timeout=3,
            )
            self._hot_corners_backup = r.stdout.strip()
        except Exception:
            self._hot_corners_backup = None

        cmds = [
            # Only 1 workspace — no way to slide
            ["gsettings", "set", "org.gnome.mutter", "dynamic-workspaces", "false"],
            ["gsettings", "set", "org.gnome.desktop.wm.preferences", "num-workspaces", "1"],
            # Block overview/app-view keyboard shortcuts
            ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-overview", "@as []"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-application-view", "@as []"],
            # Block Super key based gestures
            ["gsettings", "set", "org.gnome.mutter", "overlay-key", "''"],
            # Disable hot corners
            ["gsettings", "set", "org.gnome.desktop.interface", "enable-hot-corners", "false"],
        ]
        for c in cmds:
            try:
                subprocess.run(c, capture_output=True, text=True, timeout=3)
            except Exception:
                pass
        log.info("GNOME workspace + overview gestures disabled for strict mode")

    def _gnome_ws_restore(self) -> None:
        """Restore GNOME workspace + overview settings."""
        if self._gnome_ws_backup is not None:
            try:
                subprocess.run(
                    ["gsettings", "set", "org.gnome.mutter", "dynamic-workspaces", self._gnome_ws_backup],
                    capture_output=True, text=True, timeout=3,
                )
            except Exception:
                pass
        if self._overlay_backup is not None:
            try:
                subprocess.run(
                    ["gsettings", "set", "org.gnome.mutter", "overlay-key", self._overlay_backup],
                    capture_output=True, text=True, timeout=3,
                )
            except Exception:
                pass
        if self._hot_corners_backup is not None:
            try:
                subprocess.run(
                    ["gsettings", "set", "org.gnome.desktop.interface", "enable-hot-corners", self._hot_corners_backup],
                    capture_output=True, text=True, timeout=3,
                )
            except Exception:
                pass
        log.info("GNOME workspace + overview gestures restored")

    # -----------------------------------------------------------------------
    # GNOME Shell extension — block touchpad 3-finger gestures
    # -----------------------------------------------------------------------

    _EXT_UUID = "gesture-blocker@examvan.app"
    _EXT_DIR = None

    def _gnome_ext_path(self) -> str:
        if self._EXT_DIR is None:
            base = os.path.expanduser("~/.local/share/gnome-shell/extensions")
            self._EXT_DIR = os.path.join(base, self._EXT_UUID)
        return self._EXT_DIR

    def _gnome_ext_install(self) -> None:
        """Install and enable gesture-blocker GNOME Shell extension."""
        ext_dir = self._gnome_ext_path()
        try:
            os.makedirs(ext_dir, exist_ok=True)
            # Copy bundled extension files
            import shutil
            pkg_dir = os.path.dirname(os.path.abspath(__file__))
            src_dir = os.path.join(pkg_dir, "gesture_block")
            for f in ("extension.js", "metadata.json"):
                src = os.path.join(src_dir, f)
                if os.path.exists(src):
                    shutil.copy2(src, os.path.join(ext_dir, f))
            log.info("Extension installed at %s", ext_dir)
            # Enable via gnome-extensions
            subprocess.run(
                ["gnome-extensions", "enable", self._EXT_UUID],
                capture_output=True, text=True, timeout=5,
            )
            log.info("Extension enabled")
        except Exception as e:
            log.warning("Extension install failed: %s", e)

    def _gnome_ext_remove(self) -> None:
        """Disable and remove gesture-blocker extension."""
        try:
            subprocess.run(
                ["gnome-extensions", "disable", self._EXT_UUID],
                capture_output=True, text=True, timeout=5,
            )
        except Exception:
            pass
        try:
            import shutil
            ext_dir = self._gnome_ext_path()
            if os.path.exists(ext_dir):
                shutil.rmtree(ext_dir)
                log.info("Extension removed from %s", ext_dir)
        except Exception as e:
            log.warning("Extension removal failed: %s", e)

    # -----------------------------------------------------------------------
    # Low mode: anti-screenshot, clipboard, wake lock
    # -----------------------------------------------------------------------

    def _activate_low(self) -> None:
        # Anti-screenshot (X11 only)
        if x11.is_x11() and self._window:
            x11.set_bypass_compositor(self._window)
            log.info("Anti-screenshot: X11 bypass compositor set")
        elif x11.is_wayland():
            log.info("Anti-screenshot: Wayland inherently protected")

        # Clipboard clear every 3 seconds
        self._clipboard_timer.start(3000)
        self._clear_clipboard()

        # Screen wake lock
        self._start_inhibit()

    def _clear_clipboard(self) -> None:
        """Clear clipboard using appropriate method for display server."""
        if x11.is_wayland():
            clear_clipboard_wl()
        clear_clipboard()  # Qt fallback always runs

    def _start_inhibit(self) -> None:
        """Prevent screen saver / DPMS via systemd-inhibit or xset."""
        # Method 1: systemd-inhibit
        try:
            proc = subprocess.Popen(
                [
                    "systemd-inhibit",
                    "--what=idle",
                    "--who=examvan",
                    "--why=Ujian sedang berlangsung",
                    "sleep", "infinity",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            self._inhibit_pid = proc.pid
            log.info("Screen inhibit started (PID %d)", proc.pid)
            return
        except FileNotFoundError:
            log.debug("systemd-inhibit not available")

        # Method 2: xset (X11 only)
        if x11.is_x11():
            try:
                subprocess.run(
                    ["xset", "s", "off", "-dpms"],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    timeout=3,
                )
                log.info("Screen saver disabled via xset")
            except (FileNotFoundError, subprocess.TimeoutExpired):
                log.warning("Cannot disable screen saver")

    def _stop_inhibit(self) -> None:
        """Kill wake lock subprocess."""
        if self._inhibit_pid:
            try:
                os.kill(self._inhibit_pid, 15)  # SIGTERM
                log.info("Screen inhibit stopped (PID %d)", self._inhibit_pid)
            except OSError:
                pass
            self._inhibit_pid = None

    # -----------------------------------------------------------------------
    # Medium mode: focus loss detection → auto-submit
    # -----------------------------------------------------------------------

    def _activate_medium(self) -> None:
        # Monitor application state changes (reliable on X11)
        app = QApplication.instance()
        if app:
            app.applicationStateChanged.connect(self._on_app_state_changed)
            log.info("Focus loss monitoring activated")

        # Also monitor window focus if available
        if self._window:
            try:
                self._window.windowHandle().activeChanged.connect(
                    self._on_window_active_changed
                )
            except Exception:
                pass

        # Poll timer: on Wayland, desktop switch doesn't always trigger
        # applicationStateChanged — poll isActiveWindow() as fallback.
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(500)
        self._poll_timer.timeout.connect(self._poll_focus)
        self._poll_timer.start()
        log.info("Focus poll timer started (500ms interval)")

    def _on_app_state_changed(self, state: Qt.ApplicationState) -> None:
        """Handle application focus changes."""
        if not self._active:
            return
        if state == Qt.ApplicationInactive:
            log.warning("Focus lost — starting 3s auto-submit countdown")
            self._focus_timer.start()
        elif state == Qt.ApplicationActive:
            if self._focus_timer.isActive():
                log.info("Focus regained — cancelling auto-submit countdown")
                self._focus_timer.stop()

    def _on_window_active_changed(self) -> None:
        """Handle window-level focus changes."""
        if not self._active or not self._window:
            return
        if not self._window.isActiveWindow():
            self._on_app_state_changed(Qt.ApplicationInactive)
        else:
            self._on_app_state_changed(Qt.ApplicationActive)

    def _poll_focus(self) -> None:
        """Poll window focus. In strict mode, force-raise window to top.

        On Wayland GNOME, 3-finger swipe up/down opens overview even with
        keybindings removed. Force window to front continuously so overview
        has no visible window to show.
        """
        if not self._active or not self._window:
            return

        # Strict mode: aggressively keep window on top
        if self._strict:
            self._window.raise_()
            self._window.activateWindow()

        if not self._window.isActiveWindow():
            if not self._focus_timer.isActive():
                log.warning("Poll: window not active — starting 3s countdown")
                self._focus_timer.start()
        else:
            if self._focus_timer.isActive():
                log.info("Poll: window active again — cancelling countdown")
                self._focus_timer.stop()

    def _on_focus_timeout(self) -> None:
        """Focus lost for 3+ seconds — trigger auto-submit."""
        if not self._active:
            return
        log.warning("Focus lost timeout — triggering auto-submit")
        self.auto_submit.emit()

    # -----------------------------------------------------------------------
    # Strict mode: keyboard/pointer grab, fullscreen, kiosk
    # -----------------------------------------------------------------------

    def _activate_strict(self) -> None:
        if not self._window:
            return

        # Fullscreen frameless
        self._window.setWindowFlags(
            self._window.windowFlags()
            | Qt.FramelessWindowHint
            | Qt.WindowStaysOnTopHint
        )
        self._window.showFullScreen()

        # X11 keyboard + pointer grab
        if x11.is_x11():
            if x11.grab_keyboard(self._window):
                log.info("Strict mode: keyboard grabbed")
            else:
                log.warning("Strict mode: keyboard grab FAILED")

            if x11.grab_pointer(self._window):
                log.info("Strict mode: pointer grabbed")
            else:
                log.warning("Strict mode: pointer grab FAILED")

            self._grab_held = True

            # Set window type dock to appear above panels
            x11.set_window_type_dock(self._window)

        elif x11.is_wayland():
            log.warning(
                "Strict mode on Wayland: keyboard/pointer grab not available. "
                "Only fullscreen and focus monitoring active."
            )

        # Lock GNOME workspace switching + install gesture-blocker extension
        self._gnome_ws_lock()
        self._gnome_ext_install()

        # In kiosk session mode, additional setup is handled by kiosk.py
        if self._kiosk:
            from .kiosk import setup_kiosk_environment
            setup_kiosk_environment()
