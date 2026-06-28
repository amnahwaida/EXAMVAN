"""Security Enforcer — dispatches low/medium/strict mode enforcement.

Cross-platform: delegates platform-specific operations to a backend
obtained from get_backend().
"""

from __future__ import annotations

import logging
import sys
from typing import Optional

from PyQt5.QtCore import QObject, Qt, QTimer, pyqtSignal
from PyQt5.QtWidgets import QApplication, QWidget

from . import get_backend

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

        # Platform backend
        self._backend = get_backend()

        # Timers
        self._clipboard_timer = QTimer(self)
        self._clipboard_timer.timeout.connect(self._clear_clipboard)
        self._focus_timer = QTimer(self)
        self._focus_timer.setSingleShot(True)
        self._focus_timer.setInterval(3000)
        self._focus_timer.timeout.connect(self._on_focus_timeout)

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def activate(self) -> None:
        """Activate security enforcement based on mode."""
        if self._active:
            return
        self._active = True
        self._backend.activate()
        log.info("Activating security: level=%s, strict=%s", self._level, self._strict)

        # Check multi-monitor (Windows: display warning; Linux: log)
        if self._backend.has_multiple_monitors():
            log.warning("Multiple monitors detected — security risk")
            if self._strict:
                # In strict mode, the app stays fullscreen on primary monitor
                log.warning("Strict mode active — secondary monitor not covered")

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

        self._clipboard_timer.stop()
        self._focus_timer.stop()
        if hasattr(self, '_poll_timer'):
            self._poll_timer.stop()

        self._backend.release_strict_mode(self._window)
        self._backend.allow_sleep()
        self._backend.deactivate()

        # Linux specific GNOME restore (if Linux)
        if sys.platform != "win32":
            self._recover_gnome_settings()

    # ------------------------------------------------------------------
    # GNOME crash recovery — Linux only
    # ------------------------------------------------------------------

    @staticmethod
    def restore_gnome_settings() -> None:
        """Restore GNOME settings from crash backup. No-op on Windows."""
        if sys.platform == "win32":
            return
        try:
            from .linux_backend import LinuxBackend
            LinuxBackend.restore_gnome_settings()
        except ImportError:
            pass

    def _recover_gnome_settings(self) -> None:
        """Instance-level GNOME restore (deactivate helper)."""
        try:
            from .linux_backend import LinuxBackend
            LinuxBackend.restore_gnome_settings()
        except ImportError:
            pass

    # ------------------------------------------------------------------
    # Low mode
    # ------------------------------------------------------------------

    def _activate_low(self) -> None:
        # Anti-screenshot (Linux X11 only)
        if sys.platform != "win32":
            self._x11_anti_screenshot()

        # Clipboard clear every 3 seconds
        self._clipboard_timer.start(3000)
        self._clear_clipboard()

        # Screen wake lock
        self._backend.prevent_sleep()

    def _x11_anti_screenshot(self) -> None:
        """Apply X11 bypass-compositor hint (Linux only, no-op on Windows)."""
        try:
            from . import x11
            if x11.is_x11() and self._window:
                x11.set_bypass_compositor(self._window)
                log.info("Anti-screenshot: X11 bypass compositor set")
            elif x11.is_wayland():
                log.info("Anti-screenshot: Wayland inherently protected")
        except ImportError:
            pass

    def _clear_clipboard(self) -> None:
        self._backend.clear_clipboard()

    # ------------------------------------------------------------------
    # Medium mode
    # ------------------------------------------------------------------

    def _activate_medium(self) -> None:
        app = QApplication.instance()
        if app:
            app.applicationStateChanged.connect(self._on_app_state_changed)
            log.info("Focus loss monitoring activated")

        if self._window:
            try:
                self._window.windowHandle().activeChanged.connect(
                    self._on_window_active_changed
                )
            except Exception:
                pass

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(500)
        self._poll_timer.timeout.connect(self._poll_focus)
        self._poll_timer.start()
        log.info("Focus poll timer started (500ms interval)")

    def _on_app_state_changed(self, state: Qt.ApplicationState) -> None:
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
        if not self._active or not self._window:
            return
        if not self._window.isActiveWindow():
            self._on_app_state_changed(Qt.ApplicationInactive)
        else:
            self._on_app_state_changed(Qt.ApplicationActive)

    def _poll_focus(self) -> None:
        if not self._active or not self._window:
            return

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
        if not self._active:
            return
        log.warning("Focus lost timeout — triggering auto-submit")
        self.auto_submit.emit()

    # ------------------------------------------------------------------
    # Strict mode
    # ------------------------------------------------------------------

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

        # Platform-specific strict mode (keyboard hook on Windows,
        # X11 grabs + GNOME workspace lock on Linux)
        self._backend.set_strict_mode(self._window)

        # Kiosk session setup (Linux-only)
        if self._kiosk:
            try:
                from .kiosk import setup_kiosk_environment
                setup_kiosk_environment()
            except ImportError:
                pass
