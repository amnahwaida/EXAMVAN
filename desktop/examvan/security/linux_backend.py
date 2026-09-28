"""Linux security backend — X11 grabs, systemd-inhibit, GNOME settings, xsel."""

from __future__ import annotations

import hashlib
import json
import logging
import os
import signal
import socket
import subprocess
import uuid
from pathlib import Path
from typing import Any, Optional

from .base import SecurityBackend

log = logging.getLogger(__name__)

# Backup file for GNOME settings — survives crash
_GNOME_BACKUP_DIR = Path.home() / ".config" / "examvan"
_GNOME_BACKUP_FILE = _GNOME_BACKUP_DIR / "gnome_backup.json"


class LinuxBackend(SecurityBackend):
    """Linux (X11/Wayland) security implementation."""

    def __init__(self) -> None:
        self._inhibit_pid: Optional[int] = None
        self._grab_held = False
        # GNOME settings backups stored as {schema_key: value}
        self._gnome_backups: dict = {}

    # ------------------------------------------------------------------
    # Inhibit PID crash recovery
    # ------------------------------------------------------------------

    def cleanup_stale_inhibit(self) -> None:
        """Kill orphaned systemd-inhibit from crashed sessions.
        Call at startup before activating security.
        """
        pid_path = _GNOME_BACKUP_DIR / "inhibit.pid"
        if not pid_path.exists():
            return
        try:
            old_pid = int(pid_path.read_text().strip())
            try:
                os.kill(old_pid, signal.SIGTERM)
                log.info("Cleaned orphaned inhibit PID %d", old_pid)
            except ProcessLookupError:
                pass
            except PermissionError:
                pass
        except (ValueError, OSError):
            pass
        try:
            pid_path.unlink()
        except OSError:
            pass

    def _persist_inhibit_pid(self) -> None:
        if self._inhibit_pid is None:
            return
        _GNOME_BACKUP_DIR.mkdir(parents=True, exist_ok=True)
        pid_path = _GNOME_BACKUP_DIR / "inhibit.pid"
        try:
            pid_path.write_text(str(self._inhibit_pid))
        except OSError:
            pass

    def _clear_inhibit_pid_file(self) -> None:
        pid_path = _GNOME_BACKUP_DIR / "inhibit.pid"
        try:
            if pid_path.exists():
                pid_path.unlink()
        except OSError:
            pass

    # ------------------------------------------------------------------
    # Strict mode
    # ------------------------------------------------------------------

    def set_strict_mode(self, window: Any) -> None:
        from . import x11
        if not window:
            return

        # Fullscreen frameless via Qt — applied by enforcer itself
        # X11-specific extras
        if x11.is_x11():
            if x11.grab_keyboard(window):
                log.info("Strict mode: keyboard grabbed")
                self._grab_held = True
            else:
                log.warning("Strict mode: keyboard grab FAILED")

            if x11.grab_pointer(window):
                log.info("Strict mode: pointer grabbed")
            else:
                log.warning("Strict mode: pointer grab FAILED")

            x11.set_window_type_dock(window)
            log.info("Strict mode: window type set to DOCK")
        else:
            log.warning(
                "Strict mode on Wayland: keyboard/pointer grab not available. "
                "Only fullscreen and focus monitoring active."
            )

        # GNOME workspace lock
        self._gnome_ws_lock()
        self._gnome_overview_block()

    def release_strict_mode(self, window: Any) -> None:
        from . import x11
        if self._grab_held:
            x11.ungrab_keyboard()
            x11.ungrab_pointer()
            self._grab_held = False
            log.info("X11 grabs released")
        self._gnome_ws_restore()

    # ------------------------------------------------------------------
    # Screen-capture prevention
    # ------------------------------------------------------------------

    def set_capture_protection(self, window: Any) -> None:
        """Apply the X11 anti-screenshot hint (medium mode and up).

        Kept separate from set_strict_mode() so the enforcer can turn capture
        resistance on for medium exams too.

        Honest scope note: _NET_WM_BYPASS_COMPOSITOR is a compositing hint,
        NOT a capture control — a compositor still paints the framebuffer that
        a screen-grab or an HDMI capture card reads. The real Linux anti-capture
        work is elsewhere and is strict-only: unbinding GNOME's screenshot
        keybindings and the XGrabKeyboard above. Native Wayland has no
        equivalent of WDA_MONITOR at all, so this is a no-op there.
        """
        self._x11_bypass_compositor(window)

    def release_capture_protection(self, window: Any) -> None:
        """No undo available for the bypass-compositor hint."""
        return

    def _x11_bypass_compositor(self, window: Any) -> None:
        try:
            from . import x11
            if x11.is_x11():
                if window and x11.set_bypass_compositor(window):
                    log.info("Capture protection: X11 bypass compositor set")
                else:
                    log.warning("Capture protection: bypass compositor NOT set")
            elif x11.is_wayland():
                log.warning(
                    "Capture protection: native Wayland has no WDA_MONITOR "
                    "equivalent; only PrintScreen key blocking applies."
                )
        except ImportError:
            pass

    # ------------------------------------------------------------------
    # Clipboard
    # ------------------------------------------------------------------

    def clear_clipboard(self) -> None:
        """Empty the X11/Wayland clipboard.

        Called from a worker thread, so no Qt calls here — the enforcer
        clears QApplication.clipboard() inline on the GUI thread because Qt
        only allows that from the main thread.

        Unlike the Windows backend this still shells out (xsel/xclip/wl-copy),
        because there is no in-process X11 clipboard API. That is affordable
        only because it no longer runs on the GUI thread, and because the
        enforcer's interval was widened.
        """
        for tool, args in [
            ("xsel", ["--clipboard", "--delete"]),
            ("xclip", ["-selection", "clipboard", "-i", "/dev/null"]),
        ]:
            try:
                subprocess.run(
                    [tool] + args,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    timeout=2,
                )
                return
            except (FileNotFoundError, subprocess.TimeoutExpired):
                continue
        # Wayland fallback (wl-copy)
        try:
            subprocess.run(
                ["wl-copy", "--clear"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=2,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

    # ------------------------------------------------------------------
    # Sleep inhibition
    # ------------------------------------------------------------------

    def prevent_sleep(self) -> None:
        """Prevent screen saver / DPMS via systemd-inhibit or xset."""
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
            self._persist_inhibit_pid()
            return
        except FileNotFoundError:
            log.debug("systemd-inhibit not available")

        if self._is_x11():
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

    def allow_sleep(self) -> None:
        if self._inhibit_pid:
            try:
                os.kill(self._inhibit_pid, 15)
                log.info("Screen inhibit stopped (PID %d)", self._inhibit_pid)
            except OSError:
                pass
            self._inhibit_pid = None
        self._clear_inhibit_pid_file()

        # Restore xset settings (screen saver, DPMS)
        try:
            subprocess.run(
                ["xset", "s", "on", "+dpms"],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=3,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

    # ------------------------------------------------------------------
    # Device identity
    # ------------------------------------------------------------------

    def get_mac_address(self) -> str:
        try:
            for addr_path in sorted(Path("/sys/class/net").glob("*/address")):
                mac = addr_path.read_text().strip()
                iface = addr_path.parent.name
                if iface != "lo" and mac != "00:00:00:00:00:00":
                    return mac.upper()
        except OSError:
            pass
        # Fallback: uuid-based
        mac = uuid.getnode()
        return ":".join(f"{(mac >> i) & 0xFF:02X}" for i in range(40, -1, -8))

    def get_device_label(self) -> str:
        mac = self.get_mac_address()
        hostname = socket.gethostname()
        raw = f"{mac}:{hostname}"
        dev_id = hashlib.sha256(raw.encode()).hexdigest()[:32]
        return f"DESKTOP:{dev_id}"

    # ------------------------------------------------------------------
    # Theme detection
    # ------------------------------------------------------------------

    def is_system_dark(self) -> bool:
        """Detect Linux dark theme via env var, gsettings, Qt palette."""
        # 1. GTK_THEME env var
        gtk_theme = os.environ.get("GTK_THEME", "")
        if ":dark" in gtk_theme.lower():
            return True
        if ":light" in gtk_theme.lower():
            return False

        # 2. GNOME color-scheme
        try:
            result = subprocess.run(
                ["gsettings", "get", "org.gnome.desktop.interface", "color-scheme"],
                capture_output=True, text=True, timeout=3,
            )
            scheme = result.stdout.strip().lower()
            if "prefer-dark" in scheme:
                return True
            if "prefer-light" in scheme:
                return False
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

        # 3. GNOME gtk-theme name
        try:
            result = subprocess.run(
                ["gsettings", "get", "org.gnome.desktop.interface", "gtk-theme"],
                capture_output=True, text=True, timeout=3,
            )
            theme_name = result.stdout.strip().strip("'").lower()
            dark_kw = ("dark", "mocha", "frappe", "macchiato")
            light_kw = ("light", "latte")
            if any(kw in theme_name for kw in dark_kw):
                return True
            if any(kw in theme_name for kw in light_kw):
                return False
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

        # 4. Qt palette fallback
        from PyQt5.QtGui import QPalette
        from PyQt5.QtWidgets import QApplication
        app = QApplication.instance()
        if app is not None:
            palette = app.palette()
            bg = palette.color(QPalette.Window)
            return bg.lightness() < 128

        return True

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def activate(self) -> None:
        self.cleanup_stale_inhibit()

    def deactivate(self) -> None:
        self._gnome_ws_restore()
        self.allow_sleep()

    # ------------------------------------------------------------------
    # Multi-monitor detection (Linux via xrandr)
    # ------------------------------------------------------------------

    def has_multiple_monitors(self) -> bool:
        """Return True if more than 1 monitor connected."""
        try:
            r = subprocess.run(
                ["xrandr", "--listmonitors"],
                capture_output=True, text=True, timeout=3,
            )
            if r.returncode == 0 and r.stdout.strip():
                # First line: "Monitors: N"
                line = r.stdout.strip().split("\n")[0]
                cnt = int(line.split()[1])
                return cnt > 1
        except Exception:
            pass
        return False

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _is_x11() -> bool:
        from . import x11
        return x11.is_x11()

    # ------------------------------------------------------------------
    # GNOME workspace lock (block desktop switching gestures)
    # ------------------------------------------------------------------

    def _gnome_ws_lock(self) -> None:
        """Disable GNOME workspace switching + overview gestures."""
        # ALL settings that will be modified MUST be backed up here
        cmds_backup = [
            ("org.gnome.mutter", "dynamic-workspaces"),
            ("org.gnome.mutter", "overlay-key"),
            ("org.gnome.desktop.interface", "enable-hot-corners"),
            ("org.gnome.desktop.peripherals.touchpad", "send-events"),
            ("org.gnome.desktop.wm.keybindings", "panel-run-dialog"),
            ("org.gnome.shell.keybindings", "toggle-overview"),
            ("org.gnome.shell.keybindings", "toggle-application-view"),
            ("org.gnome.shell.keybindings", "screenshot"),
            ("org.gnome.shell.keybindings", "screenshot-window"),
            ("org.gnome.shell.keybindings", "show-screenshot-ui"),
            ("org.gnome.desktop.wm.keybindings", "activate-window-menu"),
        ]
        self._gnome_backups.clear()
        for schema, key in cmds_backup:
            try:
                r = subprocess.run(
                    ["gsettings", "get", schema, key],
                    capture_output=True, text=True, timeout=3,
                )
                if r.returncode == 0:
                    self._gnome_backups[f"{schema}:{key}"] = r.stdout.strip()
            except Exception:
                pass

        # Backup num-workspaces separately (different schema)
        try:
            r = subprocess.run(
                ["gsettings", "get", "org.gnome.desktop.wm.preferences", "num-workspaces"],
                capture_output=True, text=True, timeout=3,
            )
            if r.returncode == 0:
                self._gnome_backups["org.gnome.desktop.wm.preferences:num-workspaces"] = r.stdout.strip()
        except Exception:
            pass

        self._persist_gnome_backup()

        cmds = [
            ["gsettings", "set", "org.gnome.mutter", "dynamic-workspaces", "false"],
            ["gsettings", "set", "org.gnome.desktop.wm.preferences", "num-workspaces", "1"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-overview", "@as []"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "toggle-application-view", "@as []"],
            ["gsettings", "set", "org.gnome.mutter", "overlay-key", "''"],
            ["gsettings", "set", "org.gnome.desktop.interface", "enable-hot-corners", "false"],
            ["gsettings", "set", "org.gnome.desktop.peripherals.touchpad", "send-events", "disabled"],
            ["gsettings", "set", "org.gnome.desktop.wm.keybindings", "panel-run-dialog", "@as []"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "screenshot", "@as []"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "screenshot-window", "@as []"],
            ["gsettings", "set", "org.gnome.shell.keybindings", "show-screenshot-ui", "@as []"],
            ["gsettings", "set", "org.gnome.desktop.wm.keybindings", "activate-window-menu", "@as []"],
        ]
        for c in cmds:
            try:
                subprocess.run(c, capture_output=True, text=True, timeout=3)
            except Exception:
                pass
        log.info("GNOME workspace + overview gestures disabled")

    def _gnome_ws_restore(self) -> None:
        """Restore GNOME workspace + overview settings."""
        pairs = [
            ("org.gnome.mutter", "dynamic-workspaces"),
            ("org.gnome.mutter", "overlay-key"),
            ("org.gnome.desktop.interface", "enable-hot-corners"),
            ("org.gnome.desktop.peripherals.touchpad", "send-events"),
            ("org.gnome.desktop.wm.keybindings", "panel-run-dialog"),
            ("org.gnome.shell.keybindings", "toggle-overview"),
            ("org.gnome.shell.keybindings", "toggle-application-view"),
            ("org.gnome.shell.keybindings", "screenshot"),
            ("org.gnome.shell.keybindings", "screenshot-window"),
            ("org.gnome.shell.keybindings", "show-screenshot-ui"),
            ("org.gnome.desktop.wm.keybindings", "activate-window-menu"),
            ("org.gnome.desktop.wm.preferences", "num-workspaces"),
        ]
        for schema, key in pairs:
            val = self._gnome_backups.get(f"{schema}:{key}")
            if val is not None:
                try:
                    subprocess.run(
                        ["gsettings", "set", schema, key, val],
                        capture_output=True, text=True, timeout=3,
                    )
                except Exception:
                    pass

        self._clear_gnome_backup()
        log.info("GNOME settings restored")

    def _gnome_overview_block(self) -> None:
        """Force-close GNOME overview via D-Bus."""
        try:
            subprocess.run(
                ["gdbus", "call", "--session",
                 "--dest", "org.gnome.Shell",
                 "--object-path", "/org/gnome/Shell",
                 "--method", "org.gnome.Shell.FocusSearch", "''"],
                capture_output=True, text=True, timeout=2,
            )
        except Exception:
            pass

    # ------------------------------------------------------------------
    # Persistent GNOME backup — survives crash
    # ------------------------------------------------------------------

    def _persist_gnome_backup(self) -> None:
        """Write GNOME settings backup to file so crash recovery can restore."""
        key_map = {
            "org.gnome.mutter:dynamic-workspaces": "dynamic_workspaces",
            "org.gnome.mutter:overlay-key": "overlay_key",
            "org.gnome.desktop.interface:enable-hot-corners": "hot_corners",
            "org.gnome.desktop.peripherals.touchpad:send-events": "touchpad",
            "org.gnome.desktop.wm.keybindings:panel-run-dialog": "run_dialog",
            "org.gnome.shell.keybindings:toggle-overview": "toggle_overview",
            "org.gnome.shell.keybindings:toggle-application-view": "toggle_app_view",
            "org.gnome.shell.keybindings:screenshot": "screenshot",
            "org.gnome.shell.keybindings:screenshot-window": "screenshot_window",
            "org.gnome.shell.keybindings:show-screenshot-ui": "show_screenshot_ui",
            "org.gnome.desktop.wm.keybindings:activate-window-menu": "window_menu",
            "org.gnome.desktop.wm.preferences:num-workspaces": "num_workspaces",
        }
        data = {}
        for dict_key, data_key in key_map.items():
            val = self._gnome_backups.get(dict_key)
            if val is not None:
                data[data_key] = val
        _GNOME_BACKUP_DIR.mkdir(parents=True, exist_ok=True)
        try:
            tmp = _GNOME_BACKUP_FILE.with_suffix(".tmp")
            with open(tmp, "w") as f:
                json.dump(data, f)
            tmp.replace(_GNOME_BACKUP_FILE)
        except OSError:
            pass

    @staticmethod
    def _clear_gnome_backup() -> None:
        try:
            if _GNOME_BACKUP_FILE.exists():
                _GNOME_BACKUP_FILE.unlink()
        except OSError:
            pass

    @staticmethod
    def restore_gnome_settings() -> None:
        """Restore GNOME settings from backup file.

        Public — called at startup from __main__.py to recover from
        a crashed session where deactivate() never ran.
        """
        if not _GNOME_BACKUP_FILE.exists():
            return
        try:
            with open(_GNOME_BACKUP_FILE) as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError):
            return

        restores = []

        # Schema key pairs: (data_key, schema, key, default_fallback?)
        settings_map = [
            ("dynamic_workspaces", "org.gnome.mutter", "dynamic-workspaces", "false"),
            ("overlay_key", "org.gnome.mutter", "overlay-key", "'Super_L'"),
            ("hot_corners", "org.gnome.desktop.interface", "enable-hot-corners", "true"),
            ("touchpad", "org.gnome.desktop.peripherals.touchpad", "send-events", "enabled"),
            ("run_dialog", "org.gnome.desktop.wm.keybindings", "panel-run-dialog", "['<Alt>F2']"),
            ("toggle_overview", "org.gnome.shell.keybindings", "toggle-overview", "['<Super>s']"),
            ("toggle_app_view", "org.gnome.shell.keybindings", "toggle-application-view", "['<Super>a']"),
            ("screenshot", "org.gnome.shell.keybindings", "screenshot", "['Print']"),
            ("screenshot_window", "org.gnome.shell.keybindings", "screenshot-window", "['<Alt>Print']"),
            ("show_screenshot_ui", "org.gnome.shell.keybindings", "show-screenshot-ui", "['<Shift>Print']"),
            ("window_menu", "org.gnome.desktop.wm.keybindings", "activate-window-menu", "['<Alt>space']"),
            ("num_workspaces", "org.gnome.desktop.wm.preferences", "num-workspaces", "4"),
        ]

        for dk, schema, key, fallback in settings_map:
            val = data.get(dk)
            if val is not None:
                restores.append(["gsettings", "set", schema, key, val])

        for c in restores:
            try:
                subprocess.run(c, capture_output=True, timeout=3)
            except Exception:
                pass

        # Re-enable touchpad explicitly — most visible crash symptom
        touchpad_val = data.get("touchpad")
        if touchpad_val is not None:
            try:
                subprocess.run(
                    ["gsettings", "set", "org.gnome.desktop.peripherals.touchpad", "send-events", "enabled"],
                    capture_output=True, timeout=3,
                )
            except Exception:
                pass

        LinuxBackend._clear_gnome_backup()
        log.info("GNOME settings restored from crash backup")
