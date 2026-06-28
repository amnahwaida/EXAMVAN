"""X11 ctypes bindings for security features.

Provides keyboard grab, pointer grab, anti-screenshot hints,
and window type manipulation via libX11 and libXtst.
"""

from __future__ import annotations

import ctypes
import ctypes.util
import logging
import os
from typing import Optional

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Load X11 libraries
# ---------------------------------------------------------------------------

_libx11_path = ctypes.util.find_library("X11")
_libxt_path = ctypes.util.find_library("Xtst")

_xlib: Optional[ctypes.CDLL] = None
_xlib_display = None  # Cached Display*

if _libx11_path:
    try:
        _xlib = ctypes.CDLL(_libx11_path)
    except OSError:
        log.warning("Cannot load libX11 — X11 security features disabled")

# ---------------------------------------------------------------------------
# X11 constants
# ---------------------------------------------------------------------------

GrabModeAsync = 1
CurrentTime = 0
KeyPressMask = 1 << 0
KeyReleaseMask = 1 << 1
ButtonPressMask = 1 << 2
ButtonReleaseMask = 1 << 3
PointerMotionMask = 1 << 6
SubstructureNotifyMask = 1 << 17

# Atoms
XA_CARDINAL = 6
XA_ATOM = 4

# ---------------------------------------------------------------------------
# Setup & teardown
# ---------------------------------------------------------------------------


def _get_display():
    """Get the default X11 Display connection (cached)."""
    global _xlib_display
    if _xlib is None:
        return None
    if _xlib_display is None:
        _xlib.XOpenDisplay.restype = ctypes.c_void_p
        display_str = os.environ.get("DISPLAY", "")
        _xlib_display = _xlib.XOpenDisplay(display_str.encode() if display_str else None)
    return _xlib_display


def _get_x11_window_id(qwindow) -> int:
    """Convert a QWindow or QWidget to X11 Window ID."""
    try:
        # QWidget → winId() returns the X11 window handle
        return int(qwindow.winId())
    except Exception:
        return 0


def _intern_atom(name: str) -> int:
    """Intern an X11 atom by name."""
    display = _get_display()
    if not display or not _xlib:
        return 0
    _xlib.XInternAtom.restype = ctypes.c_ulong
    _xlib.XInternAtom.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_int]
    return _xlib.XInternAtom(display, name.encode(), 0)


def _xchange_property(window_id: int, atom_name: str, value: int, atom_type: str = "CARDINAL") -> None:
    """Set an X11 property on a window."""
    display = _get_display()
    if not display or not _xlib:
        return

    atom = _intern_atom(atom_name)
    type_atom = _intern_atom(f"XA_{atom_type}") if atom_type != "CARDINAL" else XA_CARDINAL

    # For _NET_WM_BYPASS_COMPOSITOR, the atom type is CARDINAL and value is 1
    data = (ctypes.c_ulong * 1)(value)
    _xlib.XChangeProperty(
        display,
        ctypes.c_ulong(window_id),
        ctypes.c_ulong(atom),
        ctypes.c_ulong(type_atom if isinstance(type_atom, int) else XA_CARDINAL),
        32,  # format
        0,   # PropModeReplace
        ctypes.cast(data, ctypes.c_void_p),
        1,   # nelements
    )
    _xlib.XFlush(display)


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


def is_x11() -> bool:
    """Check if running on X11 display server."""
    session_type = os.environ.get("XDG_SESSION_TYPE", "")
    if session_type == "x11":
        return True
    if "DISPLAY" in os.environ and "WAYLAND_DISPLAY" not in os.environ:
        return True
    return False


def is_wayland() -> bool:
    """Check if running on Wayland display server."""
    session_type = os.environ.get("XDG_SESSION_TYPE", "")
    if session_type == "wayland":
        return True
    if "WAYLAND_DISPLAY" in os.environ:
        return True
    return False


def set_bypass_compositor(widget) -> bool:
    """Set _NET_WM_BYPASS_COMPOSITOR hint to prevent compositing/screenshots.

    Returns True on success.
    """
    wid = _get_x11_window_id(widget)
    if not wid:
        return False
    try:
        _xchange_property(wid, "_NET_WM_BYPASS_COMPOSITOR", 1)
        log.info("Set _NET_WM_BYPASS_COMPOSITOR on window %d", wid)
        return True
    except Exception as e:
        log.warning("Failed to set bypass compositor: %s", e)
        return False


def set_window_type_dock(widget) -> bool:
    """Set _NET_WM_WINDOW_TYPE to _NET_WM_WINDOW_TYPE_DOCK.

    This makes the window appear above panels/taskbars.
    """
    wid = _get_x11_window_id(widget)
    if not wid:
        return False
    display = _get_display()
    if not display or not _xlib:
        return False

    try:
        atom_wm_type = _intern_atom("_NET_WM_WINDOW_TYPE")
        atom_dock = _intern_atom("_NET_WM_WINDOW_TYPE_DOCK")

        data = (ctypes.c_ulong * 1)(atom_dock)
        _xlib.XChangeProperty(
            display,
            ctypes.c_ulong(wid),
            ctypes.c_ulong(atom_wm_type),
            ctypes.c_ulong(XA_ATOM),
            32,
            0,
            ctypes.cast(data, ctypes.c_void_p),
            1,
        )
        _xlib.XFlush(display)
        log.info("Set window type DOCK on window %d", wid)
        return True
    except Exception as e:
        log.warning("Failed to set window type dock: %s", e)
        return False


def grab_keyboard(widget) -> bool:
    """Grab keyboard input, preventing Alt-Tab, Alt-F4, Super, etc.

    Returns True on success.
    """
    wid = _get_x11_window_id(widget)
    if not wid:
        return False
    display = _get_display()
    if not display or not _xlib:
        return False

    try:
        _xlib.XGrabKeyboard.restype = ctypes.c_int
        _xlib.XGrabKeyboard.argtypes = [
            ctypes.c_void_p,  # display
            ctypes.c_ulong,   # window
            ctypes.c_int,     # owner_events
            ctypes.c_int,     # pointer_mode
            ctypes.c_int,     # keyboard_mode
            ctypes.c_ulong,   # time
        ]
        result = _xlib.XGrabKeyboard(
            display,
            ctypes.c_ulong(wid),
            1,  # owner_events = True (events still propagate for our window)
            GrabModeAsync,
            GrabModeAsync,
            CurrentTime,
        )
        _xlib.XFlush(display)
        success = result == 0  # GrabSuccess = 0
        if success:
            log.info("Keyboard grabbed on window %d", wid)
        else:
            log.warning("Keyboard grab failed on window %d (status=%d)", wid, result)
        return success
    except Exception as e:
        log.warning("Failed to grab keyboard: %s", e)
        return False


def ungrab_keyboard() -> None:
    """Release keyboard grab."""
    display = _get_display()
    if not display or not _xlib:
        return
    try:
        _xlib.XUngrabKeyboard(display, CurrentTime)
        _xlib.XFlush(display)
        log.info("Keyboard ungrabbed")
    except Exception as e:
        log.warning("Failed to ungrab keyboard: %s", e)


def grab_pointer(widget) -> bool:
    """Grab pointer, confining cursor to widget bounds.

    Returns True on success.
    """
    wid = _get_x11_window_id(widget)
    if not wid:
        return False
    display = _get_display()
    if not display or not _xlib:
        return False

    try:
        _xlib.XGrabPointer.restype = ctypes.c_int
        _xlib.XGrabPointer.argtypes = [
            ctypes.c_void_p,  # display
            ctypes.c_ulong,   # window
            ctypes.c_int,     # owner_events
            ctypes.c_uint,    # event_mask
            ctypes.c_int,     # pointer_mode
            ctypes.c_int,     # keyboard_mode
            ctypes.c_ulong,   # confine_to
            ctypes.c_ulong,   # cursor
            ctypes.c_ulong,   # time
        ]
        result = _xlib.XGrabPointer(
            display,
            ctypes.c_ulong(wid),
            0,  # owner_events = False
            ButtonPressMask | ButtonReleaseMask | PointerMotionMask,
            GrabModeAsync,
            GrabModeAsync,
            ctypes.c_ulong(wid),  # confine_to = exam window
            0,   # cursor = None (keep current)
            CurrentTime,
        )
        _xlib.XFlush(display)
        success = result == 0  # GrabSuccess = 0
        if success:
            log.info("Pointer grabbed and confined to window %d", wid)
        else:
            log.warning("Pointer grab failed on window %d (status=%d)", wid, result)
        return success
    except Exception as e:
        log.warning("Failed to grab pointer: %s", e)
        return False


def ungrab_pointer() -> None:
    """Release pointer grab."""
    display = _get_display()
    if not display or not _xlib:
        return
    try:
        _xlib.XUngrabPointer(display, CurrentTime)
        _xlib.XFlush(display)
        log.info("Pointer ungrabbed")
    except Exception as e:
        log.warning("Failed to ungrab pointer: %s", e)
