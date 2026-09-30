"""Windows security backend — Win32 API via ctypes.

Provides keyboard hook (Alt+Tab, PrintScreen, Win key blocking),
clipboard clearing, screen capture prevention, sleep inhibition,
and dark mode detection — all via ctypes (no pywin32 needed).
"""

from __future__ import annotations

import json
import logging
import sys
import threading
import time
from pathlib import Path
from ctypes import (
    CFUNCTYPE,
    POINTER,
    Structure,
    byref,
    c_char_p,
    c_int,
    c_longlong,
    c_size_t,
    c_uint,
    c_uint32,
    c_ulong,
    c_void_p,
    cast,
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

# Messages
#
# HARUS di level modul, bukan lokal di `_win32_prototypes()`.
#
# Fungsi itu me-return hanya nama berawalan `_`, jadi konstanta yang
# dinamai tanpa garis bawah tidak pernah masuk `globals()`. `WM_QUIT`
# yang hilang berarti `_stop_keyboard_hook()` gagal diam-diam: NameError
# tertelan `except`, WM_QUIT tidak terkirim, thread hook tidak keluar,
# dan keyboard hook memblokir Alt+Tab/Win sampai process.Exit.
# Lihat tests/test_windows_constants.py.
WM_QUIT = 0x0012

# GetSystemMetrics indices
#
# Sama seperti WM_QUIT: hilang dari globals() -> `_has_multiple_monitors()`
# selalu mengembalikan False di setiap PC Windows.
SM_CMONITORS = 80

# GWL style
GWL_EXSTYLE = -20
WS_EX_LAYERED = 0x80000
WS_EX_TRANSPARENT = 0x20
WS_EX_TOOLWINDOW = 0x80

# SPI for screen saver
SPI_GETSCREENSAVEACTIVE = 0x0057
SPI_SETSCREENSAVEACTIVE = 0x0011
SPIF_SENDCHANGE = 0x0002
# SPIF_UPDATEINIFILE (0x01) SENGAJA TIDAK dipakai — lihat prevent_sleep().

# CREATE_NO_WINDOW / _SUBPROCESS_FLAGS dihapus bersama subprocess.clear:
# satu-satunya pemakai flag ini adalah `cmd.exe /c echo.|clip` yang di-fork
# tiap 3 detik untuk mengosongkan clipboard (menyebabkan kursor membeku di
# PC low-end). Modul ini tidak lagi menjalankan proses apa pun.

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
# Binding dilakukan LAZILY, bukan di import time.
#
# ctypes hanya menyediakan `windll` di Windows, jadi binds di module level
# membuat modul ini mustahil di-import di Linux/macOS. Akibatnya SELURUH
# security backend Windows - termasuk keputusan blokir-key yang jadi inti
# integritas ujian - tidak punya test coverage apa pun, karena tidak bisa
# di-import di runner CI mana pun.
#
# Yang ditemukan saat memperbaiki ini: ctypes.wintypes SEBENARNYA bisa
# di-import di Linux (13 tipe lengkap), jadi `windll` adalah satu-satunya
# penghalang. Setelah binding ditunda, modul ini bisa di-import di mana saja
# dan setiap keputusan logika di dalamnya bisa diuji dengan mock.
#
# PEP 562 __getattr__ hanya dipanggil ketika nama TIDAK ditemukan sebagai
# global, jadi ini transparan bagi ~50 call site yang tetap menulis
# _user32.GetAsyncKeyState(...) seperti sebelumnya.
_bound = False


def _bind() -> None:
    """Resolve windll + set every Win32 prototype. Runs at most once."""
    global _bound, _user32, _kernel32, _advapi32
    if _bound:
        return

    from ctypes import windll

    _user32 = windll.user32
    _kernel32 = windll.kernel32
    _advapi32 = windll.advapi32

    # Prototype WAJIB di-set di sini. Tanpa baris ini, _bound tetap False
    # dan setiap pemanggilan Win32 berakhir NameError yang tertelan
    # log.warning -- baner tetap menulis "STRICT" padahal tidak ada satu pun
    # proteksi yang aktif. Lihat _win32_prototypes.
    globals().update(_win32_prototypes(_user32, _kernel32, _advapi32))
    _bound = True

# PEP 562. Dipanggil hanya ketika nama tidak ada sebagai global, yaitu
# sebelum _bind() sempat berjalan. Setelah _bind(), semua nama ada sebagai
# global asli sehingga fungsi ini tidak pernah dipanggil lagi.
def __getattr__(name: str):
    if not _bound:
        try:
            _bind()
        except ImportError as e:
            # Di luar Windows, windll tidak ada sama sekali. Naikkan sebagai
            # AttributeError (bukan ImportError) supaya hasattr() dan
            # getattr(name, default) tetap berperilaku seperti biasa --
            # keduanya adalah inti Python dan beberapa perpustakaan memakainya
            # untuk mendeteksi fitur opsional.
            if sys.platform != "win32":
                raise AttributeError(
                    f"{name!r} needs the Win32 API and is unavailable on "
                    f"{sys.platform}; windows_backend can only bind on Windows"
                ) from e
            raise
        if name in globals():
            return globals()[name]
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


# ---------------------------------------------------------------------------
# Keyboard hook callback
# ---------------------------------------------------------------------------

# GLOBALS — prevent GC of callback and hook handles
_hook_id: Optional[HHOOK] = None
_hook_thread: Optional[threading.Thread] = None
_hook_thread_id: Optional[int] = None
_hook_callback: Any = None  # Keep CFUNCTYPE wrapper reference
_hook_proc_wrapper: Any = None  # Extra guard against GC

# Guard: hook installed flag (thread-safe via hook message queue)
_hook_ready = threading.Event()


def _win32_prototypes(_user32, _kernel32, _advapi32) -> dict:
    """Set every Win32 prototype we use and return them keyed by global name.

    INI HARUS DIPANGGIL DARI _bind(). Blok ini sebelumnya berada di modul
    __getattr__ SETELAH `raise AttributeError`, jadi tidak pernah dieksekusi:
    yang terjadi hanya _user32/_kernel32/_advapi32 yang ter-assign, semua
    prototype tetap hilang, dan `_bound` tidak pernah jadi True.

    Akibatnya setiap fitur Windows gagal dengan NameError yang tertelan
    log.warning -- keyboard hook (Alt+Tab, Win, Ctrl+Shift+Esc), capture
    protection (anti-screenshot), prevent_sleep, clipboard Win32, dan deteksi
    multi-monitor -- sementara baner tetap menulis "STRICT".

    Dipisah jadi fungsi supaya tidak butuh statement `global` berisi 17 nama:
    assignment-nya jadi variabel lokal, lalu caller memindahkannya ke
    globals(). Daftar prototype jadi bisa diuji tanpa menyentuh windll.
    """
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

    # NB: WM_QUIT dan SM_CMONITORS TIDAK didefinisikan di sini. Keduanya
    # konstanta, bukan prototype, dan filter `startswith("_")` pada return
    # dict akan membuangnya. Lihat blok konstanta di level modul.

    return {k: v for k, v in locals().items()
            if k.startswith("_") and not k.startswith("__")}


def should_block_key(
    vk: int,
    *,
    alt_pressed: bool = False,
    ctrl_down: bool = False,
    shift_down: bool = False,
    win_down: bool = False,
    alt_flag: bool = False,
) -> bool:
    """Decide whether a keystroke must be swallowed by the keyboard hook.

    Pure function: no Win32, no ctypes, no state. Extracted from the
    LLKHF callback so the blocking RULES can be unit-tested on any
    platform. They used to live inside a ctypes callback, which is why
    this much exam-integrity logic had never been executed by a test on
    any operating system.

    Returns True to block, False to pass through to the next hook.
    """
    # --- Blocked keys ---
    # Alt+Tab
    if alt_pressed and vk == VK_TAB:
        return True
    # Alt+F4
    if alt_pressed and vk == VK_F4:
        return True
    # Alt+Escape
    if alt_pressed and vk == VK_ESCAPE:
        return True
    # Alt+Enter
    if alt_pressed and vk == 0x0D:  # VK_RETURN
        return True
    # Win key (Start menu)
    if vk in (VK_LWIN, VK_RWIN):
        return True

    # All Win+<key> combos
    if win_down:
        # Navigation & system
        if vk == VK_TAB:        # Win+Tab (Task View)
            return True
        if vk == 0x4C:          # Win+L (Lock screen) -- CRITICAL
            return True
        if vk == 0x50:          # Win+P (Project / second screen)
            return True
        if vk == 0x54:          # Win+T (Cycle taskbar)
            return True
        if vk == 0x58:          # Win+X (Quick Link menu)
            return True
        if vk == 0x57:          # Win+W (Widgets)
            return True
        if vk == 0x5A:          # Win+Z (Snap layouts)
            return True
        if vk == 0x41:          # Win+A (Action Center)
            return True
        if vk == 0x4E:          # Win+N (Notification Center)
            return True
        if vk == 0x42:          # Win+B (focus notification area)
            return True
        if vk == 0x46:          # Win+F (Feedback Hub)
            return True
        if vk == 0x51:          # Win+Q (Cortana / Search)
            return True
        # Win+0 through Win+9 (taskbar items 0-9) -- launch pinned apps!
        if 0x30 <= vk <= 0x39:
            return True
        # Win+F1 (Help)
        if vk == 0x70:          # VK_F1
            return True
        # Accessories & tools
        if vk == 0x47:          # Win+G (Game Bar / screen recording)
            return True
        if vk == 0x48:          # Win+H (Dictation)
            return True
        if vk == 0x4B:          # Win+K (Wireless display / Cast)
            return True
        if vk == 0x56:          # Win+V (Clipboard history)
            return True
        if vk == 0x59:          # Win+Y (Mixed Reality / desktop switch)
            return True
        # Files & search
        if vk == 0x53:          # Win+S (Search / Snip)
            return True
        if vk == 0x52:          # Win+R (Run dialog)
            return True
        if vk in (0x44, 0x4D):  # Win+D / Win+M (desktop)
            return True
        if vk == 0x45:          # Win+E (File Explorer)
            return True
        if vk == 0x49:          # Win+I (Settings)
            return True
        if vk == 0x13:          # Win+Pause (System Properties)
            return True
        # Input / misc
        if vk == 0x20:          # Win+Space (Input language)
            return True
        if vk == 0xBC:          # Win+, (Peek at desktop)
            return True
        if vk == 0xBE:          # Win+. (Emoji picker)
            return True
        if vk == 0xBA:          # Win+; (Emoji picker alt)
            return True
        if vk == 0xDB:          # Win+[ (window snap left)
            return True
        if vk == 0xDD:          # Win+] (window snap right)
            return True
        if vk == 0x23:          # Win+End (window snap right half)
            return True
        if vk == 0x24:          # Win+Home (minimize all non-active)
            return True
        # Arrow keys (window snap / move)
        if vk in (0x25, 0x26, 0x27, 0x28):  # Win+Left/Up/Right/Down
            return True
        # Accessibility
        if vk == 0xBB:          # Win+= (Magnifier zoom in)
            return True
        if vk == 0xBD:          # Win+- (Magnifier zoom out)
            return True
        if vk == 0x55:          # Win+U (Ease of Access)
            return True
        if vk == 0x4F:          # Win+Ctrl+O (OSK) — blocked via win_down
            return True
        if vk == VK_ESCAPE:     # Win+Esc exits the Magnifier
            return True
        # NB: Win+Esc was listed in a comment here claiming it was "handled
        # by the regular Escape block below". That check requires
        # `not win_down`, so Win+Esc was never actually blocked. Caught by
        # tests/test_windows_backend.py.
    # PrintScreen
    if vk == VK_SNAPSHOT:
        return True
    # Alt+PrintScreen (active window screenshot)
    if alt_pressed and vk == VK_SNAPSHOT:
        return True
    # Shift+PrintScreen (screenshot variation)
    if shift_down and vk == VK_SNAPSHOT:
        return True
    # Ctrl+Shift+Esc (Task Manager)
    if ctrl_down and shift_down and vk == VK_ESCAPE:
        return True
    # Ctrl+Esc (Start menu)
    if ctrl_down and vk == VK_ESCAPE and not alt_pressed and not shift_down:
        return True
    # Alone Escape (block in strict mode)
    if vk == VK_ESCAPE and not alt_pressed and not ctrl_down and not shift_down and not win_down:
        return True
    # Left/Right Alt alone
    if vk == VK_MENU and alt_flag:
        return True

    # Pass through everything else. Wajib eksplisit: dalam callback aslinya
    # jatuh ke akhir fungsi berarti "oper ke hook berikutnya", tapi fungsi
    # pure yang dikembalikan bool harus mengembalikan False, bukan None.
    return False


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

        if should_block_key(
            vk,
            alt_pressed=alt_pressed,
            ctrl_down=ctrl_down,
            shift_down=shift_down,
            win_down=win_down,
            alt_flag=alt_flag,
        ):
            return BLOCK_KEY

    # Pass through everything else
    return _CallNextHookEx(HHOOK(0), nCode, wParam, lParam)


def _hook_thread_func() -> None:
    """Message-pump thread for the keyboard hook."""
    global _hook_id, _hook_callback, _hook_proc_wrapper, _hook_ready
    try:
        # Set the hook — store global ref to prevent GC
        # CFUNCTYPE return c_longlong (64-bit) for LRESULT on x64 Windows.
        # Using 32-bit c_int would truncate the return value on x64.
        HOOKPROC = CFUNCTYPE(c_longlong, c_int, WPARAM, LPARAM)
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

        # Message pump — needed for WH_KEYBOARD_LL to deliver events.
        # Post a self-message (WM_APP) and pump it once BEFORE signaling
        # _hook_ready, so the pump is confirmed running when the caller
        # returns. Without this, _stop_keyboard_hook could POST WM_QUIT
        # before GetMessageW() starts, losing the quit message forever.
        _user32.PostThreadMessageW(
            _kernel32.GetCurrentThreadId(),
            0x4A,  # WM_APP — arbitrary, ignored
            WPARAM(0), LPARAM(0),
        )
        msg = MSG()
        # First GetMessageW returns the WM_APP wakeup. Pump is alive.
        _GetMessageW(byref(msg), HWND(0), 0, 0)
        # NOW signal success — pump can receive WM_QUIT
        _hook_ready.set()

        # Main pump loop
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


# ---------------------------------------------------------------------------
# Screen-saver state — backup / restore / crash recovery
# ---------------------------------------------------------------------------
# Cermin pola Linux (gnome_backup.json): simpan nilai SEBELUM diubah, pulihkan
# saat exit bersih, dan sapu sisa run yang crash saat start berikutnya.
#
# File ini ada karena SPI_SETSCREENSAVEACTIVE tanpa SPIF_UPDATEINIFILE hanya
# berlaku untuk sesi login berjalan. Kalau EXAMVAN mati saat ujian (mati
# listrik, Task Manager, crash), screensaver siswa tetap nonaktif sampai mereka
# logoff — dan di ruang kelas yang rarely logout. Backup + restore otomatis
# di start berikutnya menutup jendela itu tanpa menunggu logout.

_STATE_DIR = Path.home() / ".config" / "examvan"
_STATE_FILE = _STATE_DIR / "windows_state.json"


def _get_screen_saver_active() -> Optional[bool]:
    """Read the current screen-saver setting. None if the call fails."""
    flag = BOOL()
    try:
        if not _SystemParametersInfoW(
            SPI_GETSCREENSAVEACTIVE, 0, byref(flag), 0
        ):
            return None
        return bool(flag.value)
    except Exception as e:
        log.warning("SPI_GETSCREENSAVEACTIVE failed: %s", e)
        return None


def _set_screen_saver_active(active: bool) -> bool:
    """Set the screen-saver setting for THIS SESSION ONLY.

    Never passes SPIF_UPDATEINIFILE. That flag writes the change to the user's
    registry, which is what used to leave a student's screen saver permanently
    disabled after a crash: nothing re-enabled it except a clean exit, and a
    crashed process never reaches one.
    """
    try:
        return bool(
            _SystemParametersInfoW(
                SPI_SETSCREENSAVEACTIVE,
                1 if active else 0,
                LPVOID(0),
                SPIF_SENDCHANGE,
            )
        )
    except Exception as e:
        log.warning("SPI_SETSCREENSAVEACTIVE failed: %s", e)
        return False


def _write_screen_saver_backup(active: bool) -> None:
    try:
        _STATE_DIR.mkdir(parents=True, exist_ok=True)
        tmp = _STATE_FILE.with_suffix(".tmp")
        tmp.write_text(
            json.dumps({"screen_saver_active": bool(active)}), encoding="utf-8"
        )
        tmp.replace(_STATE_FILE)
    except OSError as e:
        # Not fatal: the worst case is a stale session setting, which
        # reverts at logoff anyway.
        log.warning("could not persist screen-saver backup: %s", e)


def _clear_screen_saver_backup() -> None:
    try:
        if _STATE_FILE.exists():
            _STATE_FILE.unlink()
    except OSError as e:
        log.warning("could not clear screen-saver backup: %s", e)


def restore_windows_settings() -> None:
    """Undo any Windows setting this app changed. Safe to call repeatedly.

    Called from allow_sleep() on a clean exit, from activate() at startup (to
    recover a previous run that died mid-exam), and from atexit. No-op when
    there is no backup file, i.e. nothing was ever changed.
    """
    if not _STATE_FILE.exists():
        return
    previous: Optional[bool] = None
    try:
        data = json.loads(_STATE_FILE.read_text(encoding="utf-8"))
        value = data.get("screen_saver_active")
        if isinstance(value, bool):
            previous = value
    except (OSError, ValueError, json.JSONDecodeError) as e:
        log.warning("could not read screen-saver backup: %s", e)

    if previous is not None:
        # Restore what the machine had BEFORE us — not "enabled". A student who
        # deliberately keeps their screen saver off should not have EXAMVAN
        # silently turn it back on.
        _set_screen_saver_active(previous)
        log.info("Restored screen saver to %s (after EXAMVAN)", previous)
    _clear_screen_saver_backup()


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
        # Capture resistance is applied separately by
        # set_capture_protection() so medium mode gets it too; only input
        # confinement is strict-only.
        hwnd = _get_hwnd(window)
        if hwnd:
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
        if self._hook_installed:
            self._stop_keyboard_hook()
            self._hook_installed = False

    # ------------------------------------------------------------------
    # Screen-capture prevention
    # ------------------------------------------------------------------

    def set_capture_protection(self, window: Any) -> None:
        """Black the exam window out of captured output (medium mode and up).

        WDA_MONITOR blanks the window in screen captures taken through
        BitBlt / PrintWindow / Desktop Duplication, and the DWM honours it for
        PrintScreen too. It is not a security boundary — Microsoft documents
        it as best-effort, and a privileged driver or an HDMI capture card
        defeats it — but it is far stronger than the Linux equivalent, and it
        used to be applied at strict level ONLY, leaving medium exams with no
        capture resistance at all.

        Note WDA_EXCLUDEFROMCAPTURE (0x11, Windows 10 2004+) is the stronger
        modern flag but behaves differently on older builds and in
        multi-monitor setups, so WDA_MONITOR is kept as the portable choice.
        """
        hwnd = _get_hwnd(window)
        if not hwnd:
            return
        try:
            _SetWindowDisplayAffinity(HWND(hwnd), WDA_MONITOR)
            log.info("Screen capture prevention enabled (WDA_MONITOR)")
        except Exception as e:
            log.warning("SetWindowDisplayAffinity failed: %s", e)

    def release_capture_protection(self, window: Any) -> None:
        hwnd = _get_hwnd(window)
        if not hwnd:
            return
        try:
            _SetWindowDisplayAffinity(HWND(hwnd), WDA_NONE)
        except Exception:
            pass

    # ------------------------------------------------------------------
    # Clipboard
    # ------------------------------------------------------------------

    def clear_clipboard(self) -> None:
        """Empty the Win32 clipboard.

        Runs on a worker thread (SecurityEnforcer._clear_clipboard), so it
        must stay free of Qt calls — QApplication.clipboard() is main-thread
        only — and must not spawn processes.

        This used to also run `subprocess.run(["cmd.exe", "/c", "echo.|clip"])`
        on a 3-second timer. That forked a cmd.exe, which spawned clip.exe,
        while blocking the GUI thread: on low-end lab machines the cursor
        visibly froze every few seconds. It was also redundant — the Win32
        call above empties the same clipboard, and the enforcer clears the
        Qt side inline on the GUI thread.
        """
        try:
            if _OpenClipboard(HWND(0)):
                _EmptyClipboard()
                _CloseClipboard()
        except Exception:
            log.debug("EmptyClipboard failed", exc_info=True)

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

        # Disable the screen saver for this session, remembering what it was.
        #
        # The previous version called SPI_SETSCREENSAVEACTIVE with
        # SPIF_UPDATEINIFILE and no backup, and re-enabled with a hardcoded 1.
        # Two bugs in eight lines: the registry write made a student's screen
        # saver stay off PERMANENTLY if the process died (nothing restores it
        # except a clean exit, which a crash never reaches), and forcing 1
        # silently re-enabled a screen saver the user had chosen to keep off.
        previous = _get_screen_saver_active()
        if previous is None:
            log.warning("could not read screen-saver state; leaving it alone")
            return
        if not previous:
            # Already off — nothing to change, and nothing to restore later.
            return
        if _set_screen_saver_active(False):
            _write_screen_saver_backup(previous)
            log.info("Screen saver disabled for this session (was %s)", previous)

    def allow_sleep(self) -> None:
        if self._exec_state_handle is not None:
            try:
                _SetThreadExecutionState(ES_CONTINUOUS)
                log.info("Sleep prevention disabled")
            except Exception:
                pass
            self._exec_state_handle = None

        # Put the screen saver back the way this machine had it, and drop the
        # backup so a later run does not "restore" it a second time.
        restore_windows_settings()

    # ------------------------------------------------------------------
    # Device identity
    # ------------------------------------------------------------------

    def get_mac_address(self) -> str:
        # Delegate, don't reimplement. This used to be a second, independent
        # copy of the same SHA256(MAC:hostname) logic, which meant two
        # implementations of the exam device identity in one repo and two
        # answers that could differ. See utils.get_device_label().
        from ..utils import get_mac_address

        return get_mac_address()

    def get_device_label(self) -> str:
        # Single source of truth: examvan.utils.get_device_label(), which
        # caches for the process lifetime. This label keys the PDF approval
        # gate, request-approval, submit, and presence — they must not be
        # able to disagree.
        from ..utils import get_device_label

        return get_device_label()

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
        # Crash recovery: a leftover backup file means a previous run changed
        # the screen saver and then died without restoring it (power loss,
        # Task Manager, crash). Put the machine back before this exam starts,
        # so the damage window is "until EXAMVAN is launched again" instead of
        # "forever". No-op when no backup exists.
        #
        # Linux has the equivalent hook via cleanup_stale_inhibit() plus the
        # GNOME backup file; without this, Windows had no recovery path at all.
        restore_windows_settings()

    def deactivate(self) -> None:
        self.allow_sleep()

    # ------------------------------------------------------------------
    # Internal: keyboard hook management
    # ------------------------------------------------------------------

    def _start_keyboard_hook(self) -> bool:
        """Start keyboard hook thread. Returns True if installed successfully."""
        global _hook_thread, _hook_thread_id, _hook_ready
        _hook_ready.clear()
        try:
            hook_thread = threading.Thread(target=_hook_thread_func, daemon=True)
            hook_thread.start()
            _hook_thread = hook_thread
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
        """Stop keyboard hook thread.

        Posts WM_QUIT to the hook thread and waits briefly for it to
        exit. The hook thread's finally handles UnhookWindowsHookEx
        so we DON'T set _hook_id = None here (race: we'd null it before
        the thread reads it, leaking the hook).
        """
        global _hook_id, _hook_thread, _hook_thread_id
        if _hook_thread_id is not None:
            try:
                _PostThreadMessageW(DWORD(_hook_thread_id), WM_QUIT, WPARAM(0), LPARAM(0))
            except Exception:
                pass
            if _hook_thread is not None:
                _hook_thread.join(timeout=1.0)
            _hook_thread_id = None
            _hook_thread = None
        # Do NOT set _hook_id = None here — hook thread's finally does it
        log.info("Keyboard hook stop requested")
