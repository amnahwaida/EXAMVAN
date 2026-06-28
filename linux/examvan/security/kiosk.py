"""Kiosk session setup for strict mode.

Generates Openbox kiosk config and manages the isolated X session.
"""

from __future__ import annotations

import logging
import os
import subprocess
import tempfile
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


def launch_kiosk_session(examvan_path: str) -> int:
    """Launch kiosk session — isolated X server via xinit (VT switch).

    On Wayland, GNOME compositor blocks keyboard/pointer grabs and handles
    3-finger swipe gestures at compositor level. By launching a standalone
    X11 session on a separate VT, we get full keyboard/pointer control and
    no Wayland gesture interference.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_dir = os.path.normpath(os.path.join(script_dir, "..", ".."))  # EXAVAN/linux/

    if subprocess.run(["which", "xinit"], capture_output=True).returncode != 0:
        log.error("xinit not found. Install xinit: sudo apt install xinit")
        return 1

    ob_config = generate_openbox_kiosk_config()
    use_openbox = _has_openbox()

    fd, xinitrc = tempfile.mkstemp(suffix=".sh", prefix="examvan_xinit_")
    with os.fdopen(fd, "w") as f:
        f.write(f"""#!/bin/bash
export EXAMVAN_KIOSK=1
xset s off -dpms 2>/dev/null
""")
        if use_openbox:
            f.write(f"""openbox --config-file "{ob_config}" &
sleep 1
""")
        f.write(f"""cd {project_dir}
export PYTHONPATH="{project_dir}:$PYTHONPATH"
exec {project_dir}/.venv/bin/python3 -m examvan --kiosk-session
""")
    os.chmod(xinitrc, 0o755)

    log.info("Launching kiosk session via xinit (VT switch) ...")
    try:
        result = subprocess.run(
            ["xinit", xinitrc, "--", ":1"],
            timeout=86400,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
        )
        return result.returncode
    except FileNotFoundError:
        log.error("xinit not found. Install xinit: sudo apt install xinit")
        return 1
    except subprocess.TimeoutExpired:
        log.warning("Kiosk session timed out")
        return 1
    finally:
        try:
            os.unlink(xinitrc)
        except OSError:
            pass
