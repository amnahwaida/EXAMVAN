"""Normalisasi security level — satu kosakata untuk server dan client.

MASALAH YANG DIPERBAIKI (|Windows low-end, 2026-09-29)
----------------------------------------------------
Server dan client speaks dua bahasa berbeda tentang level keamanan:

  server (webui) : "low" | "medium" | "high"
  client (desktop): "low" | "medium" | "strict"

Bukti sisi server:
  - internal/database/schema.sql            CHECK (security_level IN
                                             ('low','medium','high'))
  - internal/handlers/admin/exams.go:1524   validasi low/medium/high
  - templates/admin/dashboard.html:753-755  dropdown Rendah/Sedang/Tinggi,
    dengan label "Tinggi - TIDAK BISA Keluar (Wajib Submit)"

Client TIDAK PERNAH tahu kata "high". Tiga titik membandingkannya secara
literal, sehingga ujian "Tinggi" bisa jatuh ke cabang terlemah:

  - models.py:77             security_level == "strict"  -> cabang mati
                              (server tidak pernah mengirim "strict")
  - security/enforcer.py:74  level in ("medium",)        -> "high" tak dikenal
  - ui/exam_viewer.py:747    mode in ("medium",)         -> "high" tak dikenal

Untuk Level "high" dengan strict_mode=0, tiga titik itu sekaligus gagal:
fitur medium DAN strict tidak aktif, dan closeEvent jatuh ke dialog
konfirmasi low -> event.accept() -> siswa keluar dari ujian tanpa submit.
 persis gejala "apa pun mode ujiannya, selalu bisa keluar aplikasi".

RINGKASAN: "high" adalah strict, dan tidak ada lagi nilai yang berarti
hilang di antara "high" dan "strict". Satu fungsi ini adalah satu-satunya
tempat yang perlu diubah kalau kosakata server berubah lagi — sebelum
perbaikan ini tiap titik melakukan normalisasi sendiri dan tidak konsisten.
"""

from __future__ import annotations

from typing import Optional

# Canonical client-side tiers. These are the ONLY values the rest of the
# desktop client is allowed to branch on.
LEVEL_LOW = "low"
LEVEL_MEDIUM = "medium"
LEVEL_STRICT = "strict"

# The value the server sends for the strictest tier. Accept it as an alias
# so a payload using either word works.
LEVEL_HIGH = "high"

# Fail-secure default. Matches the DB default (schema.sql) and the Android
# client (ExamModePolicy.DEFAULT_LEVEL). The desktop used to default to
# "low" — the most permissive tier — so a malformed or truncated API
# response silently stripped every lockdown feature.
DEFAULT_LEVEL = LEVEL_MEDIUM

# Anything the client has never heard of maps here rather than to LEVEL_LOW.
_UNKNOWN = DEFAULT_LEVEL

_ALIASES = {
    LEVEL_LOW: LEVEL_LOW,
    LEVEL_MEDIUM: LEVEL_MEDIUM,
    LEVEL_HIGH: LEVEL_STRICT,
    LEVEL_STRICT: LEVEL_STRICT,
}


def normalize_level(
    raw: Optional[str], default: str = DEFAULT_LEVEL
) -> str:
    """Map a server security_level onto a canonical client tier.

    Returns exactly one of LEVEL_LOW / LEVEL_MEDIUM / LEVEL_STRICT, so
    callers can compare against those constants instead of re-parsing the
    raw string. Unrecognised input yields `default` (LEVEL_MEDIUM), never
    LEVEL_LOW: an unknown policy must not silently mean "no protection".
    """
    if not isinstance(raw, str):
        return default
    return _ALIASES.get(raw.strip().lower(), default)


def is_effective_strict(level: Optional[str], strict_mode: bool = False) -> bool:
    """True when the exam must run with the strictest lockdown.

    Both signals count: the tier ("high"/"strict") and the server's
    independent strict_mode flag. They are redundant by design — the server
    sets strict_mode=1 whenever security_level="high"
    (admin/exams.go:1529) — but a database row, a restore, or a hand-written
    payload can carry one without the other, and reading only one of them
    is what let a "Tinggi" exam behave like "Rendah".
    """
    return bool(strict_mode) or normalize_level(level) == LEVEL_STRICT


def enforces_no_free_exit(level: Optional[str], strict_mode: bool = False) -> bool:
    """True when a close attempt must auto-submit instead of letting go.

    This is the "student cannot leave the exam unsubmitted" gate. It is
    true for medium AND strict; only low is a tier where leaving is
    permitted by design (confirmed by a dialog, no auto-submit).

    strict_mode alone also closes the gate. The viewer hands
    Exam.is_strict in as strict_mode, so a strict exam arrives here as
    strict_mode=True — and depending on the tier string the server chose
    that can be paired with a "low". Reading only the tier let that
    combination out.
    """
    return bool(strict_mode) or normalize_level(level) != LEVEL_LOW


def display_level(level: Optional[str], strict_mode: bool = False) -> str:
    """Canonical tier name for the security banner.

    Always one of the three canonical tiers, so the banner can index
    SECURITY_COLORS without a KeyError and without falling back to the
    low-tier colour for an exam that is actually locked down.
    """
    if is_effective_strict(level, strict_mode):
        return LEVEL_STRICT
    return normalize_level(level)


__all__ = [
    "LEVEL_LOW",
    "LEVEL_MEDIUM",
    "LEVEL_HIGH",
    "LEVEL_STRICT",
    "DEFAULT_LEVEL",
    "normalize_level",
    "is_effective_strict",
    "enforces_no_free_exit",
    "display_level",
]
