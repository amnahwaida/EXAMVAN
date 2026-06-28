"""Kiosk session setup for strict mode.

Generates Openbox kiosk config and manages the isolated X session.
"""

from __future__ import annotations

import logging
import os
import subprocess
import tempfile
import time
from pathlib import Path
from typing import Optional

log = logging.getLogger(__name__)

_OPENBOX_KIOSK_RC = """<?xml version="1.0" encoding="UTF-8"?>
<openbox_config xmlns="http://openbox.org/3.4/rc">
  <resistance>
    <strength>0</strength>
    <screen_edge_strength>0</screen_edge_strength>
  </resistance>
  <focus>
    <focusNew>yes</focusNew>
    <followMouse>no</followMouse>
    <focusLast>yes</focusLast>
    <underMouse>no</underMouse>
    <focusDelay>200</focusDelay>
    <raiseOnFocus>yes</raiseOnFocus>
  </focus>
  <placement>
    <policy>Smart</policy>
    <center>yes</center>
    <monitor>Primary</monitor>
    <primaryMonitor>1</primaryMonitor>
  </placement>
  <theme>
    <name>Clearlooks</name>
    <titleLayout></titleLayout>
    <keepBorder>no</keepBorder>
    <animateIconify>no</animateIconify>
  </theme>
  <desktops>
    <number>1</number>
  </desktops>
  <keyboard>
    <!-- Disable ALL keybindings -->
  </keyboard>
  <mouse>
    <context name="Frame">
      <mousebind button="Left" action="Press">
        <action name="Focus"/>
      </mousebind>
    </context>
    <context name="Root">
      <!-- Disable right-click desktop menu -->
    </context>
  </mouse>
  <applications>
    <!-- Force all windows fullscreen -->
    <application class="*">
      <decor>no</decor>
      <fullscreen>yes</fullscreen>
      <maximized>yes</maximized>
      <skip_pager>yes</skip_pager>
      <skip_taskbar>yes</skip_taskbar>
    </application>
  </applications>
</openbox_config>
"""


def is_kiosk_session() -> bool:
    """Check if running from a kiosk .desktop session."""
    return os.environ.get("EXAMVAN_KIOSK", "") == "1"


def generate_openbox_kiosk_config() -> str:
    """Write temporary Openbox kiosk rc.xml. Returns path to the config file."""
    fd, path = tempfile.mkstemp(suffix=".xml", prefix="examvan_ob_")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(_OPENBOX_KIOSK_RC)
    log.info("Openbox kiosk config written to %s", path)
    return path


def setup_kiosk_environment() -> None:
    """Configure the environment for kiosk mode.

    Should be called from the exam app after it launches in a kiosk session.
    """
    # Disable screen saver
    if os.environ.get("DISPLAY"):
        try:
            subprocess.run(
                ["xset", "s", "off", "-dpms"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=3,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

    # Hide cursor (optional, via unclutter)
    try:
        subprocess.Popen(
            ["unclutter", "-idle", "3", "-root"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except FileNotFoundError:
        pass

    log.info("Kiosk environment configured")


def _has_openbox() -> bool:
    """Check if Openbox window manager is installed."""
    return subprocess.run(["which", "openbox"], capture_output=True).returncode == 0


def _find_free_display() -> Optional[str]:
    """Find a free X display number by probing Unix sockets.

    Xorg uses Unix sockets at /tmp/.X11-unix/X{n} by default (not TCP).
    Scans display numbers from :0 to :199 and returns the first whose
    socket file does not exist. Falls back to TCP probe if Unix socket
    directory doesn't exist.
    """
    # Method 1: Unix socket probe (primary for modern Xorg)
    socket_dir = Path("/tmp/.X11-unix")
    if socket_dir.exists():
        for num in range(0, 200):
            sock_path = socket_dir / f"X{num}"
            if not sock_path.exists():
                return f":{num}"
        return None

    # Method 2: TCP port probe (fallback)
    import socket as _socket
    for num in range(99, 200):
        port = 6000 + num
        sock = _socket.socket(_socket.AF_INET, _socket.SOCK_STREAM)
        sock.settimeout(0.1)
        try:
            result = sock.connect_ex(("127.0.0.1", port))
            if result != 0:
                return f":{num}"
        finally:
            sock.close()
    return None


def launch_kiosk_session(examvan_path: str) -> int:
    """Launch kiosk session — Xephyr nested X server.

    Runs Xephyr (nested X server window) inside the Wayland desktop, then
    launches the exam app inside it. The app gets a proper X11 environment
    where keyboard/pointer grabs work, separate from Wayland compositor.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_dir = os.path.normpath(os.path.join(script_dir, "..", ".."))
    venv_python = os.path.join(project_dir, ".venv", "bin", "python3")

    if not os.path.exists(venv_python):
        log.error("Virtual environment not found at %s", venv_python)
        return 1

    # Find a free display number (avoid conflict with existing X servers)
    display = _find_free_display()
    if display is None:
        display = ":99"  # fallback
    display_env = os.environ.copy()
    display_env["DISPLAY"] = display
    display_env["EXAMVAN_KIOSK"] = "1"

    # 1. Start Xephyr
    log.info("Starting Xephyr on %s ...", display)
    xephyr = subprocess.Popen(
        ["Xephyr", display, "-screen", "1920x1080",
         "-ac", "-br", "-sw-cursor", "-noreset"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )

    # 2. Wait for Xephyr to be ready
    for i in range(50):
        time.sleep(0.2)
        r = subprocess.run(["xdpyinfo", "-display", display],
                           capture_output=True, timeout=2)
        if r.returncode == 0:
            log.info("Xephyr ready after %d attempts", i + 1)
            break
    else:
        log.error("Xephyr failed to start")
        xephyr.kill()
        return 1

    # 3. Launch exam app inside Xephyr
    log.info("Launching exam app inside Xephyr ...")
    app = subprocess.Popen(
        [venv_python, "-m", "examvan", "--kiosk-session"],
        cwd=project_dir,
        env=display_env,
    )

    # 4. Cleanup on app exit
    app.wait()
    log.info("Exam app exited (code %d)", app.returncode)
    xephyr.terminate()
    try:
        xephyr.wait(timeout=5)
    except subprocess.TimeoutExpired:
        xephyr.kill()
    return app.returncode
