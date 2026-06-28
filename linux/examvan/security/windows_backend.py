"""Windows security backend — Win32 API via ctypes.

Provides keyboard hook (Alt+Tab, PrintScreen, Win key blocking),
clipboard clearing, screen capture prevention, sleep inhibition,
and dark mode detection — all via ctypes (no pywin32 needed).
"""

from __future__ import annotations

import hashlib
import logging
import socket
import sys
import threading
import time
import uuid
from ctypes import (
    CFUNCTYPE,
    POINTER,
    Structure,
    byref,
    c_char_p,
    c_int,
    c_size_t,
    c_uint,
    c_uint32,
    c_ulong,
    c_void_p,
    cast,
    windll,
)
from ctypes.wintypes import (
    BOOL,
    BYTE,
    DWORD,
    HANDLE,
    HHOOK,
    HWND,
    LPARAM,
    LPVOID,
    LPWSTR,
    MSG,
    UINT,
    WCHAR,
    WPARAM,
)
from typing import Any, Callable, Optional

from .base import SecurityBackend

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Win32 constants
# ---------------------------------------------------------------------------

# Virtual key codes
VK_TAB = 0x09
VK_MENU = 0x12  # ALT
VK_ESCAPE = 0x1B
VK_SNAPSHOT = 0x2C  # PrintScreen
VK_F4 = 0x73
VK_LWIN = 0x5B
VK_RWIN = 0x5C
VK_DELETE = 0x2E

# Hook type
WH_KEYBOARD_LL = 13

# Hook actions
HC_ACTION = 0

# SetWindowsHookEx return code to block key
BLOCK_KEY = 1
PASS_KEY = 0

# SetThreadExecutionState flags
ES_CONTINUOUS = 0x80000000
ES_DISPLAY_REQUIRED = 0x00000002
ES_SYSTEM_REQUIRED = 0x00000001

# SetWindowDisplayAffinity
WDA_NONE = 0x00000000
WDA_MONITOR = 0x00000001

# GWL style
GWL_EXSTYLE = -20
WS_EX_LAYERED = 0x80000
WS_EX_TRANSPARENT = 0x20
WS_EX_TOOLWINDOW = 0x80

# SPI for screen saver
SPI_SETSCREENSAVEACTIVE = 0x0011
SPIF_UPDATEINIFILE = 0x01

# Registry
HKEY_CURRENT_USER = 0x80000001
KEY_READ = 0x20019
REG_DWORD = 4
ERROR_SUCCESS = 0


# ---------------------------------------------------------------------------
# Win32 structures
# ---------------------------------------------------------------------------

class KBDLLHOOKSTRUCT(Structure):
    _fields_ = [
        ("vkCode", DWORD),
        ("scanCode", DWORD),
        ("flags", DWORD),
        ("time", DWORD),
        ("dwExtraInfo", c_void_p),
    ]


# ---------------------------------------------------------------------------
# Win32 function prototypes
# ---------------------------------------------------------------------------

_user32 = windll.user32
_kernel32 = windll.kernel32
_advapi32 = windll.advapi32

# Keyboard hook
_SetWindowsHookExW = _user32.SetWindowsHookExW
_SetWindowsHookExW.restype = HHOOK
_SetWindowsHookExW.argtypes = [c_int, c_void_p, c_void_p, DWORD]

_CallNextHookEx = _user32.CallNextHookEx
_CallNextHookEx.restype = c_void_p
_CallNextHookEx.argtypes = [HHOOK, c_int, WPARAM, LPARAM]

_UnhookWindowsHookEx = _user32.UnhookWindowsHookEx
_UnhookWindowsHookEx.restype = BOOL
_UnhookWindowsHookEx.argtypes = [HHOOK]

_GetMessageW = _user32.GetMessageW
_GetMessageW.restype = BOOL
_GetMessageW.argtypes = [POINTER(MSG), HWND, UINT, UINT]

_PostThreadMessageW = _user32.PostThreadMessageW
_PostThreadMessageW.restype = BOOL
_PostThreadMessageW.argtypes = [DWORD, UINT, WPARAM, LPARAM]

WM_QUIT = 0x0012

# Clipboard
_OpenClipboard = _user32.OpenClipboard
_OpenClipboard.restype = BOOL
_OpenClipboard.argtypes = [HWND]

_EmptyClipboard = _user32.EmptyClipboard
_EmptyClipboard.restype = BOOL

_CloseClipboard = _user32.CloseClipboard
_CloseClipboard.restype = BOOL

# Sleep
_SetThreadExecutionState = _kernel32.SetThreadExecutionState
_SetThreadExecutionState.restype = DWORD
_SetThreadExecutionState.argtypes = [DWORD]

# Display affinity (prevent screenshot)
_SetWindowDisplayAffinity = _user32.SetWindowDisplayAffinity
_SetWindowDisplayAffinity.restype = BOOL
_SetWindowDisplayAffinity.argtypes = [HWND, DWORD]

# Window style
_GetWindowLongW = _user32.GetWindowLongW
_GetWindowLongW.restype = c_int
_GetWindowLongW.argtypes = [HWND, c_int]

_SetWindowLongW = _user32.SetWindowLongW
_SetWindowLongW.restype = c_int
_SetWindowLongW.argtypes = [HWND, c_int, c_int]

# Screen saver
_SystemParametersInfoW = _user32.SystemParametersInfoW
_SystemParametersInfoW.restype = BOOL
_SystemParametersInfoW.argtypes = [UINT, UINT, LPVOID, UINT]

# Registry (for dark mode detection)
_RegOpenKeyExW = _advapi32.RegOpenKeyExW
_RegOpenKeyExW.restype = c_int  # LONG
_RegOpenKeyExW.argtypes = [c_void_p, LPWSTR, DWORD, DWORD, POINTER(c_void_p)]

_RegQueryValueExW = _advapi32.RegQueryValueExW
_RegQueryValueExW.restype = c_int
_RegQueryValueExW.argtypes = [c_void_p, LPWSTR, c_void_p, POINTER(DWORD), BYTE * 4, POINTER(DWORD)]

_RegCloseKey = _advapi32.RegCloseKey
_RegCloseKey.restype = c_int
_RegCloseKey.argtypes = [c_void_p]

# GetSystemMetrics for multi-monitor
_GetSystemMetrics = _user32.GetSystemMetrics
_GetSystemMetrics.restype = c_int
_GetSystemMetrics.argtypes = [c_int]

SM_CMONITORS = 80


# ---------------------------------------------------------------------------
# Keyboard hook callback
# ---------------------------------------------------------------------------

# GLOBALS — prevent GC of callback and hook handles
_hook_id: Optional[HHOOK] = None
_hook_thread_id: Optional[int] = None
_hook_callback: Any = None  # Keep CFUNCTYPE wrapper reference
_hook_proc_wrapper: Any = None  # Extra guard against GC

# Guard: hook installed flag (thread-safe via hook message queue)
_hook_ready = threading.Event()


def _keyboard_hook_proc(nCode: int, wParam: WPARAM, lParam: LPARAM) -> int:
    """Low-level keyboard hook — blocks dangerous key combos.

    Handles both WM_KEYDOWN (0x0100) and WM_SYSKEYDOWN (0x0104).
    WM_SYSKEYDOWN is used by Alt+Tab and other system keystrokes.
    """
    if nCode == HC_ACTION and wParam in (0x0100, 0x0101, 0x0104, 0x0105):
        # 0x0100=KEYDOWN, 0x0101=KEYUP, 0x0104=SYSKEYDOWN, 0x0105=SYSKEYUP
        kbd = cast(lParam, POINTER(KBDLLHOOKSTRUCT)).contents
        vk = kbd.vkCode
        flags = kbd.flags

        # Check modifier state
        alt_down = (_user32.GetAsyncKeyState(VK_MENU) & 0x8000) != 0
        ctrl_down = (_user32.GetAsyncKeyState(0x11) & 0x8000) != 0  # VK_CONTROL
        shift_down = (_user32.GetAsyncKeyState(0x10) & 0x8000) != 0  # VK_SHIFT

        # LLKHF_ALTDOWN = bit 5 of flags — more reliable than GetAsyncKeyState
        # for detecting Alt during WM_SYSKEYDOWN
        alt_flag = bool(flags & 0x20)  # LLKHF_ALTDOWN
        alt_pressed = alt_down or alt_flag

        # Track Win key state — GetAsyncKeyState catches held Win keys
        # even if our hook blocked the original keydown
        win_down = (_user32.GetAsyncKeyState(VK_LWIN) & 0x8000) != 0 or \
                   (_user32.GetAsyncKeyState(VK_RWIN) & 0x8000) != 0

        # --- Blocked keys ---
        # Alt+Tab
        if alt_pressed and vk == VK_TAB:
            return BLOCK_KEY
        # Alt+F4
        if alt_pressed and vk == VK_F4:
            return BLOCK_KEY
        # Alt+Escape
        if alt_pressed and vk == VK_ESCAPE:
            return BLOCK_KEY
        # Alt+Enter
        if alt_pressed and vk == 0x0D:  # VK_RETURN
            return BLOCK_KEY
        # Win key (Start menu)
        if vk in (VK_LWIN, VK_RWIN):
            return BLOCK_KEY
        # Win+Shift+S (Snipping Tool) — block S when Win is held
        if win_down and vk == 0x53:  # VK_S
            return BLOCK_KEY
        # Win+R (Run dialog)
        if win_down and vk == 0x52:  # VK_R
            return BLOCK_KEY
        # Win+D / Win+M (show desktop / minimize all)
        if win_down and vk in (0x44, 0x4D):  # VK_D, VK_M
            return BLOCK_KEY
        # Win+E (File Explorer)
        if win_down and vk == 0x45:  # VK_E
            return BLOCK_KEY
        # Win+I (Settings)
        if win_down and vk == 0x49:  # VK_I
            return BLOCK_KEY
        # Win+Pause (System Properties)
        if win_down and vk == 0x13:  # VK_PAUSE
            return BLOCK_KEY
        # PrintScreen
        if vk == VK_SNAPSHOT:
            return BLOCK_KEY
        # Alt+PrintScreen (active window screenshot)
        if alt_pressed and vk == VK_SNAPSHOT:
            return BLOCK_KEY
        # Shift+PrintScreen (screenshot variation)
        if shift_down and vk == VK_SNAPSHOT:
            return BLOCK_KEY
        # Ctrl+Shift+Esc (Task Manager)
        if ctrl_down and shift_down and vk == VK_ESCAPE:
            return BLOCK_KEY
        # Ctrl+Esc (Start menu)
        if ctrl_down and vk == VK_ESCAPE and not alt_pressed and not shift_down:
            return BLOCK_KEY
        # Alone Escape (block in strict mode)
        if vk == VK_ESCAPE and not alt_pressed and not ctrl_down and not shift_down and not win_down:
            return BLOCK_KEY
        # Left/Right Alt alone
        if vk == VK_MENU and alt_flag:
            return BLOCK_KEY

    # Pass through everything else
    return _CallNextHookEx(HHOOK(0), nCode, wParam, lParam)


def _hook_thread_func() -> None:
    """Message-pump thread for the keyboard hook."""
    global _hook_id, _hook_callback, _hook_proc_wrapper, _hook_ready
    try:
        # Set the hook — store global ref to prevent GC
        HOOKPROC = CFUNCTYPE(c_int, c_int, WPARAM, LPARAM)
        _hook_proc_wrapper = HOOKPROC(_keyboard_hook_proc)
        _hook_callback = _hook_proc_wrapper  # Extra guard

        _hook_id = _SetWindowsHookExW(
            WH_KEYBOARD_LL,
            cast(_hook_proc_wrapper, c_void_p),
            _kernel32.GetModuleHandleW(None),
            0,  # 0 = global hook (no DLL needed for WH_KEYBOARD_LL)
        )
        if not _hook_id:
            log.warning("Keyboard hook installation failed (error %d)", _kernel32.GetLastError())
            _hook_ready.set()  # Signal failure so caller doesn't hang
            return

        log.info("Keyboard hook installed successfully")
        _hook_ready.set()  # Signal success

        # Message pump — needed for the hook to work
        msg = MSG()
        while True:
            ret = _GetMessageW(byref(msg), HWND(0), 0, 0)
            if ret <= 0:  # 0 = WM_QUIT, -1 = error
                break

    except Exception as e:
        log.warning("Keyboard hook thread error: %s", e)
        _hook_ready.set()
    finally:
        if _hook_id:
            _UnhookWindowsHookEx(_hook_id)
            _hook_id = None
            log.info("Keyboard hook removed")


def _get_hwnd(window: Any) -> Optional[int]:
    """Get Windows HWND from a Qt widget."""
    try:
        return int(window.winId())
    except Exception:
        return None


def _has_multiple_monitors() -> bool:
    """Check if system has more than one monitor."""
    try:
        return _GetSystemMetrics(SM_CMONITORS) > 1
    except Exception:
        return False


class WindowsBackend(SecurityBackend):
    """Windows security implementation using Win32 API."""

    def __init__(self) -> None:
        self._hook_started = False
        self._hook_installed = False
        self._exec_state_handle: Optional[int] = None

    # ------------------------------------------------------------------
    # Strict mode
    # ------------------------------------------------------------------

    def set_strict_mode(self, window: Any) -> None:
        hwnd = _get_hwnd(window)
        if hwnd:
            # Prevent screen capture via PrintScreen & most capture tools
            try:
                _SetWindowDisplayAffinity(HWND(hwnd), WDA_MONITOR)
                log.info("Screen capture prevention enabled (WDA_MONITOR)")
            except Exception as e:
                log.warning("SetWindowDisplayAffinity failed: %s", e)

            # Remove window border via extended style
            try:
                ex_style = _GetWindowLongW(HWND(hwnd), GWL_EXSTYLE)
                ex_style &= ~WS_EX_LAYERED
                _SetWindowLongW(HWND(hwnd), GWL_EXSTYLE, ex_style)
            except Exception:
                pass

        # Install keyboard hook
        if not self._hook_installed:
            ok = self._start_keyboard_hook()
            self._hook_installed = ok
            if not ok:
                log.warning("Keyboard hook failed — running without low-level key blocking")

    def release_strict_mode(self, window: Any) -> None:
        hwnd = _get_hwnd(window)
        if hwnd:
            try:
                _SetWindowDisplayAffinity(HWND(hwnd), WDA_NONE)
            except Exception:
                pass

        if self._hook_installed:
            self._stop_keyboard_hook()
            self._hook_installed = False

    # ------------------------------------------------------------------
    # Clipboard
    # ------------------------------------------------------------------

    def clear_clipboard(self) -> None:
        # Clear Win32 clipboard
        try:
            if _OpenClipboard(HWND(0)):
                _EmptyClipboard()
                _CloseClipboard()
        except Exception:
            pass
        # Clear clipboard history via cmd (overwrite with empty)
        try:
            import subprocess
            subprocess.run(
                ["cmd.exe", "/c", "echo.|clip"],
                capture_output=True, timeout=2,
            )
        except Exception:
            pass
        # Also clear via Qt (cross-platform fallback)
        try:
            from PyQt5.QtWidgets import QApplication
            app = QApplication.instance()
            if app:
                app.clipboard().clear()
        except Exception:
            pass

    # ------------------------------------------------------------------
    # Sleep inhibition
    # ------------------------------------------------------------------

    def prevent_sleep(self) -> None:
        try:
            self._exec_state_handle = _SetThreadExecutionState(
                ES_CONTINUOUS | ES_DISPLAY_REQUIRED | ES_SYSTEM_REQUIRED
            )
            log.info("Sleep prevention enabled")
        except Exception as e:
            log.warning("SetThreadExecutionState failed: %s", e)

        # Disable screen saver
        try:
            _SystemParametersInfoW(SPI_SETSCREENSAVEACTIVE, 0, LPVOID(0), SPIF_UPDATEINIFILE)
        except Exception:
            pass

    def allow_sleep(self) -> None:
        if self._exec_state_handle is not None:
            try:
                _SetThreadExecutionState(ES_CONTINUOUS)
                log.info("Sleep prevention disabled")
            except Exception:
                pass
            self._exec_state_handle = None

        # Re-enable screen saver
        try:
            _SystemParametersInfoW(SPI_SETSCREENSAVEACTIVE, 1, LPVOID(0), SPIF_UPDATEINIFILE)
        except Exception:
            pass

    # ------------------------------------------------------------------
    # Device identity
    # ------------------------------------------------------------------

    def get_mac_address(self) -> str:
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
        """Detect Windows dark mode via registry."""
        try:
            hkey = c_void_p()
            ret = _RegOpenKeyExW(
                HKEY_CURRENT_USER,
                "Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize",
                0,
                KEY_READ,
                byref(hkey),
            )
            if ret == ERROR_SUCCESS and hkey:
                value_type = DWORD()
                data = (BYTE * 4)()
                data_size = DWORD(4)
                ret = _RegQueryValueExW(
                    hkey,
                    "AppsUseLightTheme",
                    None,
                    byref(value_type),
                    data,
                    byref(data_size),
                )
                _RegCloseKey(hkey)
                if ret == ERROR_SUCCESS and value_type.value == REG_DWORD:
                    return data[0] == 0  # 0 = dark, 1 = light
        except Exception:
            pass

        # Fallback: Qt palette
        try:
            from PyQt5.QtGui import QPalette
            from PyQt5.QtWidgets import QApplication
            app = QApplication.instance()
            if app is not None:
                palette = app.palette()
                bg = palette.color(QPalette.Window)
                return bg.lightness() < 128
        except Exception:
            pass

        return True

    # ------------------------------------------------------------------
    # Multi-monitor detection
    # ------------------------------------------------------------------

    def has_multiple_monitors(self) -> bool:
        """Return True if system has more than 1 active monitor.

        Used by enforcer to show warning / block exam.
        """
        return _has_multiple_monitors()

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        self.allow_sleep()

    # ------------------------------------------------------------------
    # Internal: keyboard hook management
    # ------------------------------------------------------------------

    def _start_keyboard_hook(self) -> bool:
        """Start keyboard hook thread. Returns True if installed successfully."""
        global _hook_thread_id, _hook_ready
        _hook_ready.clear()
        try:
            hook_thread = threading.Thread(target=_hook_thread_func, daemon=True)
            hook_thread.start()
            _hook_thread_id = hook_thread.native_id

            # Wait for hook to install (poll with timeout)
            ready = _hook_ready.wait(timeout=2.0)
            if not ready:
                log.warning("Keyboard hook did not become ready within 2s")
                return False
            if not _hook_id:
                log.warning("Keyboard hook installation reported as not ready")
                return False
            log.info("Keyboard hook thread started (tid=%s)", _hook_thread_id)
            return True
        except Exception as e:
            log.warning("Failed to start keyboard hook thread: %s", e)
            return False

    def _stop_keyboard_hook(self) -> None:
        global _hook_id, _hook_thread_id
        if _hook_thread_id is not None:
            try:
                _PostThreadMessageW(DWORD(_hook_thread_id), WM_QUIT, WPARAM(0), LPARAM(0))
            except Exception:
                pass
            _hook_thread_id = None
        _hook_id = None
        log.info("Keyboard hook stopped")
