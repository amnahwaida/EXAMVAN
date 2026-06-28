#!/usr/bin/env bash
# EXAMVAN Linux Client — Launcher Script
# Usage:
#   ./run.sh                   Normal mode
#   ./run.sh --kiosk           Kiosk mode (launches isolated X session)
#   ./run.sh --kiosk-session   Kiosk session (called internally by .desktop)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/.venv"

# ---- Helpers ---------------------------------------------------------------

info()  { printf "\033[1;34m[INFO]\033[0m  %s\n" "$*"; }
warn()  { printf "\033[1;33m[WARN]\033[0m  %s\n" "$*"; }
error() { printf "\033[1;31m[ERR]\033[0m   %s\n" "$*"; }

# ---- Python check ----------------------------------------------------------

if ! command -v python3 &>/dev/null; then
    error "python3 not found. Install Python 3.8+ first."
    echo "  Ubuntu/Debian: sudo apt install python3 python3-venv python3-pip"
    echo "  Fedora:        sudo dnf install python3 python3-pip"
    echo "  Arch:          sudo pacman -S python python-pip"
    exit 1
fi

PY_VER=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
PY_MAJOR=$(echo "$PY_VER" | cut -d. -f1)
PY_MINOR=$(echo "$PY_VER" | cut -d. -f2)
if [ "$PY_MAJOR" -lt 3 ] || { [ "$PY_MAJOR" -eq 3 ] && [ "$PY_MINOR" -lt 8 ]; }; then
    error "Python 3.8+ required, found Python $PY_VER"
    exit 1
fi

# ---- Virtual environment setup ---------------------------------------------

setup_venv() {
    info "Creating virtual environment at $VENV_DIR ..."
    python3 -m venv "$VENV_DIR"

    info "Installing dependencies into venv ..."
    "$VENV_DIR/bin/pip" install --upgrade pip --quiet
    "$VENV_DIR/bin/pip" install -r "$SCRIPT_DIR/requirements.txt" --quiet
    info "Virtual environment ready."
}

# Auto-create venv if missing
if [ ! -d "$VENV_DIR" ]; then
    setup_venv
fi

# Use venv Python
PYTHON="$VENV_DIR/bin/python"
if [ ! -x "$PYTHON" ]; then
    warn "Venv Python not found at $PYTHON, recreating ..."
    rm -rf "$VENV_DIR"
    setup_venv
fi

# Quick sanity: check deps inside venv
if ! "$PYTHON" -c "import PyQt5" 2>/dev/null; then
    warn "PyQt5 missing in venv, reinstalling ..."
    "$VENV_DIR/bin/pip" install -r "$SCRIPT_DIR/requirements.txt" --quiet
fi
if ! "$PYTHON" -c "import fitz" 2>/dev/null; then
    warn "PyMuPDF missing in venv, reinstalling ..."
    "$VENV_DIR/bin/pip" install PyMuPDF --quiet
fi

# ---- Flags -----------------------------------------------------------------

# --setup: force re-create venv
if [ "${1:-}" = "--setup" ]; then
    rm -rf "$VENV_DIR"
    setup_venv
    info "Setup complete. Run './run.sh' to start."
    exit 0
fi

# --clean: remove venv entirely
if [ "${1:-}" = "--clean" ]; then
    rm -rf "$VENV_DIR"
    info "Virtual environment removed."
    exit 0
fi

# ---- Platform detection -----------------------------------------------------

# NOTE: Do NOT force QT_QPA_PLATFORM=wayland.
# Qt5 on native Wayland cannot detect virtual desktop switches (GNOME gesture
# swipe), which breaks focus-loss security detection in medium/strict mode.
# Qt5 defaults to xcb (XWayland) on GNOME Wayland — this is correct because
# X11 focus tracking properly detects desktop switching.

# Detect Wayland — need kiosk session for proper security
IS_WAYLAND=0
if [ -n "${WAYLAND_DISPLAY:-}" ] || [ "${XDG_SESSION_TYPE:-}" = "wayland" ]; then
    IS_WAYLAND=1
fi

# Ensure Qt picks up the system theme via xcb/xwayland
if [ -z "${QT_QPA_PLATFORMTHEME:-}" ]; then
    if command -v gsettings &>/dev/null; then
        GTK_THEME=$(gsettings get org.gnome.desktop.interface gtk-theme 2>/dev/null || echo "")
        if [ -n "$GTK_THEME" ]; then
            export QT_QPA_PLATFORMTHEME=gnome
        fi
    fi
fi

# ---- Launch ----------------------------------------------------------------

# Wayland mode: launch isolated X11 kiosk session via Xephyr.
# GNOME Wayland blocks keyboard/pointer grabs, so we run a nested
# X server where X11 grabs work and compositor gestures don't apply.
if [ "$IS_WAYLAND" = "1" ] && [ "${EXAMVAN_KIOSK:-}" != "1" ]; then
    info "Running on Wayland — launching isolated X11 kiosk session ..."
    info "(GNOME Wayland blocks keyboard grabs needed for strict mode)"

    if command -v Xephyr &>/dev/null; then
        # Detect screen size
        SCREEN_W=$(xdpyinfo 2>/dev/null | grep "dimensions:" | awk '{print $2}' | cut -dx -f1 || echo "1920")
        SCREEN_H=$(xdpyinfo 2>/dev/null | grep "dimensions:" | awk '{print $2}' | cut -dx -f2 || echo "1080")

        info "Starting Xephyr (${SCREEN_W}x${SCREEN_H}) on display :99 ..."
        Xephyr :99 -screen "${SCREEN_W}x${SCREEN_H}" -ac -br -sw-cursor -noreset &
        XEPHYR_PID=$!

        # Wait for Xephyr to be ready
        for i in $(seq 1 30); do
            if xdpyinfo -display :99 &>/dev/null; then
                break
            fi
            sleep 0.2
        done

        info "Xephyr ready, launching exam app inside ..."
        cd "$SCRIPT_DIR"
        EXAMVAN_KIOSK=1 DISPLAY=:99 "$PYTHON" -m examvan "$@"
        APP_EXIT=$?

        info "Exam app exited (code $APP_EXIT), stopping Xephyr ..."
        kill "$XEPHYR_PID" 2>/dev/null
        wait "$XEPHYR_PID" 2>/dev/null
        exit "$APP_EXIT"
    else
        warn "Xephyr not found. Install: sudo apt install xserver-xephyr"
        warn "Falling back to direct display (security limited)"
    fi
fi

# Kiosk mode: explisit flag
if [ "${1:-}" = "--kiosk" ]; then
    info "Launching EXAMVAN in kiosk mode ..."
    cd "$SCRIPT_DIR"
    "$PYTHON" -c "
from examvan.security.kiosk import launch_kiosk_session
import sys
sys.exit(launch_kiosk_session('$SCRIPT_DIR'))
"
    exit $?
fi

# Normal or kiosk-session mode
cd "$SCRIPT_DIR"
exec "$PYTHON" -m examvan "$@"
