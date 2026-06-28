"""Security enforcement for EXAMVAN desktop client.

Cross-platform: detects OS and returns the correct backend.
"""

from __future__ import annotations

import sys

from .base import SecurityBackend


def get_backend() -> SecurityBackend:
    """Return the platform-appropriate security backend.

    Called once at app startup. The backend instance should be shared
    across all SecurityEnforcer instances that need platform ops.
    """
    if sys.platform == "win32":
        from .windows_backend import WindowsBackend
        return WindowsBackend()
    else:
        from .linux_backend import LinuxBackend
        return LinuxBackend()


__all__ = ["SecurityBackend", "get_backend"]
