"""Security enforcement for EXAMVAN desktop client.

Cross-platform: detects OS and returns the correct backend.
"""

from __future__ import annotations

import sys

from .base import SecurityBackend

# Cache module-level: SATU instance backend per proses.
#
# Ini bukan optimizations — ini memperbaiki batas integritas ujian.
#
# `windows_backend` menyimpan state keyboard hook sebagai module globals
# (`_hook_id`, `_hook_thread`, `_hook_ready`, ...), jadi hook itu milik
# PROSES, bukan milik instance. Tapi `get_backend()` dulu mengembalikan
# `WindowsBackend()` BARU setiap dipanggil:
#
#   1. `_stop_keyboard_hook` sengaja tidak menolkan `_hook_id`, dengan
#      komentar yang benar: menolkannya sebelum thread membacanya akan
#      membocorkan hook. Itu benar HANYA kalau satu-satunya penulis
#      `_hook_id` adalah thread itu sendiri.
#   2. Ujian 1 strict selesai -> stop hook (join timeout 1.0 dtk).
#   3. Ujian 2 dimulai -> instance backend BARU -> thread hook BARU
#      menulis `_hook_id` = hook baru.
#   4. Thread LAMA menyelesaikan `finally`-nya dan memanggil
#      `_UnhookWindowsHookEx(_hook_id)` — hook BARU, bukan miliknya.
#   5. Hook baru dilepas, `_hook_id` di-nolkan. Instance baru masih
#      menganggap `_hook_installed = True`, jadi tidak ada retry.
#
# Akibatnya Alt+Tab, Win key, Win+L, dan Ctrl+Shift+Esc berhenti diblokir
# sementara banner tetap menulis "STRICT". Ketidakseimbangan ini tidak pernah
# dikomunikasikan ke user maupun log.
#
# Docstring fungsi ini sudah menjanjikan "Called once at app startup" sejak
# awal — tidak ada yang menegakkannya. Sekarang storage-nya sungguhan.
_shared_backend: SecurityBackend | None = None


def get_backend() -> SecurityBackend:
    """Return the platform-appropriate security backend, shared per process.

    Returns the same instance for every call. See the note on
    `_shared_backend` for why that is an exam-integrity requirement and not
    a preference.
    """
    global _shared_backend
    if _shared_backend is None:
        if sys.platform == "win32":
            from .windows_backend import WindowsBackend

            _shared_backend = WindowsBackend()
        else:
            from .linux_backend import LinuxBackend

            _shared_backend = LinuxBackend()
    return _shared_backend


def reset_backend_cache() -> None:
    """Drop the cached backend. For tests only.

    Also releases whatever the previous instance held (keyboard hook,
    strict mode) before dropping it, so a test that fakes a platform switch
    does not leak a live hook.
    """
    global _shared_backend
    if _shared_backend is not None:
        try:
            _shared_backend.release_strict_mode(None)
        except Exception:
            pass
        try:
            _shared_backend.allow_sleep()
        except Exception:
            pass
    _shared_backend = None


__all__ = ["SecurityBackend", "get_backend", "reset_backend_cache"]
