"""Abstract security backend — platform-specific hooks for enforcement."""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any, Optional


class SecurityBackend(ABC):
    """Platform-specific security operations.

    Each platform implements these methods. The enforcer calls them
    without knowing the underlying OS.
    """

    # ------------------------------------------------------------------
    # Strict mode (fullscreen + input confinement)
    # ------------------------------------------------------------------

    @abstractmethod
    def set_strict_mode(self, window: Any) -> None:
        """Engage strict security: fullscreen frameless, grab input,
        block system shortcuts."""
        ...

    @abstractmethod
    def release_strict_mode(self, window: Any) -> None:
        """Release keyboard/pointer grabs, restore normal window state."""
        ...

    # ------------------------------------------------------------------
    # Screen-capture prevention
    # ------------------------------------------------------------------

    @abstractmethod
    def set_capture_protection(self, window: Any) -> None:
        """Make the exam window resistant to screen capture.

        Separate from set_strict_mode() because capture resistance is wanted
        from medium mode up, while input confinement (keyboard hook / grabs)
        is strict-only. Bundling them meant Windows applied WDA_MONITOR at
        strict level only, so a medium exam had no capture protection at all
        — a protection level students are graded on.
        """
        ...

    @abstractmethod
    def release_capture_protection(self, window: Any) -> None:
        """Undo set_capture_protection()."""
        ...

    # ------------------------------------------------------------------
    # Clipboard
    # ------------------------------------------------------------------

    @abstractmethod
    def clear_clipboard(self) -> None:
        """Clear system clipboard contents."""
        ...

    # ------------------------------------------------------------------
    # Sleep inhibition
    # ------------------------------------------------------------------

    @abstractmethod
    def prevent_sleep(self) -> None:
        """Prevent screen saver / display sleep."""
        ...

    @abstractmethod
    def allow_sleep(self) -> None:
        """Re-allow screen saver / display sleep."""
        ...

    # ------------------------------------------------------------------
    # Device identity
    # ------------------------------------------------------------------

    @abstractmethod
    def get_mac_address(self) -> str:
        """Return stable MAC address string (upper case, colon-separated)."""
        ...

    @abstractmethod
    def get_device_label(self) -> str:
        """Return label like 'DESKTOP:<sha256_prefix>' for server identity."""
        ...

    # ------------------------------------------------------------------
    # Theme detection
    # ------------------------------------------------------------------

    @abstractmethod
    def is_system_dark(self) -> bool:
        """Detect if OS uses dark theme."""
        ...

    # ------------------------------------------------------------------
    # Multi-monitor detection
    # ------------------------------------------------------------------

    @abstractmethod
    def has_multiple_monitors(self) -> bool:
        """Return True if system has more than 1 active monitor."""
        ...

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    @abstractmethod
    def activate(self) -> None:
        """Called when security starts. Acquire resources if needed."""
        ...

    @abstractmethod
    def deactivate(self) -> None:
        """Called when security stops. Release all resources."""
        ...
