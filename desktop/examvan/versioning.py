"""Version comparison helpers for the desktop client."""

from __future__ import annotations

import re
from typing import Any, List

_LEADING_NUMBER = re.compile(r"^\d+")


def _version_parts(value: Any) -> List[int]:
    text = str(value or "").strip()
    if text[:1] in ("v", "V"):
        text = text[1:]
    parts: List[int] = []
    for segment in text.split("."):
        match = _LEADING_NUMBER.match(segment.strip())
        parts.append(int(match.group()) if match else 0)
    return parts


def compare_versions(left: Any, right: Any) -> int:
    """Return -1, 0, or 1 using the same padding rules as the server."""
    left_parts = _version_parts(left)
    right_parts = _version_parts(right)
    length = max(len(left_parts), len(right_parts))
    for index in range(length):
        left_value = left_parts[index] if index < len(left_parts) else 0
        right_value = right_parts[index] if index < len(right_parts) else 0
        if left_value > right_value:
            return 1
        if left_value < right_value:
            return -1
    return 0


def is_version_outdated(current: Any, required: Any) -> bool:
    """True only when the server publishes a requirement the client misses."""
    required_text = str(required or "").strip()
    if not required_text:
        return False
    return compare_versions(current, required_text) < 0
