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


def launch_kiosk_session(examvan_path: str) -> int:
    """Launch a kiosk X session with Openbox + exam app.

    This creates a standalone X session (via startx/xinit) with Openbox
    as the window manager in kiosk mode and the exam app as the sole client.

    Returns the exit code of the session.
    """
    ob_config = generate_openbox_kiosk_config()

    # Build xinitrc script
    fd, xinitrc = tempfile.mkstemp(suffix=".sh", prefix="examvan_xinit_")
    with os.fdopen(fd, "w") as f:
        f.write(f"""#!/bin/sh
export EXAMVAN_KIOSK=1

# Disable screen saver and DPMS
xset s off -dpms 2>/dev/null

# Start Openbox with kiosk config
OPENBOX_CONFIG="{ob_config}" openbox --config-file "{ob_config}" &
OB_PID=$!

# Wait for Openbox to be ready
sleep 1

# Launch exam app
python3 -m examvan --kiosk-session
APP_EXIT=$?

# Kill Openbox
kill $OB_PID 2>/dev/null

# Clean up temp config
rm -f "{ob_config}"

exit $APP_EXIT
""")
    os.chmod(xinitrc, 0o755)

    log.info("Launching kiosk session via xinit...")
    try:
        result = subprocess.run(
            ["xinit", xinitrc, "--", ":1", "vt7"],
            timeout=86400,  # Max 24h session
        )
        return result.returncode
    except FileNotFoundError:
        log.error("xinit not found. Install xinit package.")
        return 1
    except subprocess.TimeoutExpired:
        log.warning("Kiosk session timed out")
        return 1
    finally:
        try:
            os.unlink(xinitrc)
        except OSError:
            pass
