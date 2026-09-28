"""Satuan kosakata security level client vs server (replay bug|Windows 2026).

Server (webui) HANYA mengirim 'low' | 'medium' | 'high':
  - internal/database/schema.sql   CHECK (security_level IN ('low','medium','high'))
  - internal/handlers/admin/exams.go:1524  validasi low/medium/high
  - templates/admin/dashboard.html:753-755  dropdown Rendah/Sedang/Tinggi

Client (desktop) sebelumnya HANYA mengenal 'low' | 'medium' | 'strict'.
'strict' tidak pernah dikirim server, jadi tiga titik ini bocor:

  - models.py:77        is_strict  -> security_level == "strict"  (cabang mati)
  - security/enforcer.py:74         level in ("medium",)           ('high' tak dikenal)
  - ui/exam_viewer.py:747           mode in ("medium",)            ('high' tak dikenal)

Gejala lapangan: ujian mode "Tinggi" yang dijanjikan "TIDAK BISA Keluar"
jatuh ke cabang low -> dialog konfirmasi -> event.accept() -> siswa keluar
bebas. Modul ini menutup celah itu di SATU tempat.
"""

from __future__ import annotations

import unittest

from examvan.security_levels import (
    DEFAULT_LEVEL,
    LEVEL_HIGH,
    LEVEL_LOW,
    LEVEL_MEDIUM,
    LEVEL_STRICT,
    display_level,
    enforces_no_free_exit,
    is_effective_strict,
    normalize_level,
)


class NormalizeLevelTestCase(unittest.TestCase):
    def test_server_vocabulary_maps_to_client_vocabulary(self):
        # The whole point: "high" from the server must become a level the
        # client actually understands.
        self.assertEqual(normalize_level(LEVEL_HIGH), LEVEL_STRICT)
        self.assertEqual(normalize_level("medium"), LEVEL_MEDIUM)
        self.assertEqual(normalize_level("low"), LEVEL_LOW)

    def test_client_strict_still_accepted(self):
        # Older/hand-written payloads may still say "strict".
        self.assertEqual(normalize_level(LEVEL_STRICT), LEVEL_STRICT)

    def test_is_idempotent(self):
        for raw in ("low", "medium", "high", "strict"):
            once = normalize_level(raw)
            self.assertEqual(normalize_level(once), once)

    def test_case_and_whitespace_insensitive(self):
        # Server writes the value verbatim from JSON; a stray "High" or a
        # trailing space must not silently fall back to the default tier.
        self.assertEqual(normalize_level("HIGH"), LEVEL_STRICT)
        self.assertEqual(normalize_level("  high  "), LEVEL_STRICT)
        self.assertEqual(normalize_level("Medium"), LEVEL_MEDIUM)

    def test_missing_value_uses_default(self):
        self.assertEqual(normalize_level(None), DEFAULT_LEVEL)
        self.assertEqual(normalize_level(""), DEFAULT_LEVEL)
        self.assertEqual(normalize_level("   "), DEFAULT_LEVEL)

    def test_unknown_value_uses_default(self):
        # An unrecognised level must not be treated as "no protection".
        self.assertEqual(normalize_level("gigantic"), DEFAULT_LEVEL)

    def test_default_is_medium_not_low(self):
        # Fail-secure. The DB default is 'medium' (schema.sql) and Android
        # defaults to medium (ExamModePolicy.DEFAULT_LEVEL); the desktop used
        # to default to "low", the most permissive tier, so a malformed
        # response silently stripped lockdown.
        self.assertEqual(DEFAULT_LEVEL, LEVEL_MEDIUM)

    def test_explicit_default_is_honoured(self):
        self.assertEqual(normalize_level("nonsense", default=LEVEL_LOW), LEVEL_LOW)
        self.assertEqual(normalize_level(None, default=LEVEL_STRICT), LEVEL_STRICT)


class IsEffectiveStrictTestCase(unittest.TestCase):
    def test_high_alone_is_strict(self):
        # The regression: security_level='high' with strict_mode=false used
        # to resolve to NOT strict, disabling every lockdown feature.
        self.assertTrue(is_effective_strict(LEVEL_HIGH, strict_mode=False))

    def test_strict_mode_flag_alone_is_strict(self):
        # Combination the viewer builds for a strict exam.
        self.assertTrue(is_effective_strict(LEVEL_LOW, strict_mode=True))

    def test_medium_alone_is_not_strict(self):
        self.assertFalse(is_effective_strict(LEVEL_MEDIUM, strict_mode=False))

    def test_low_alone_is_not_strict(self):
        self.assertFalse(is_effective_strict(LEVEL_LOW, strict_mode=False))


class EnforcesNoFreeExitTestCase(unittest.TestCase):
    """True = a close attempt must NOT let the student out unsubmitted."""

    def test_high_cannot_exit_freely(self):
        self.assertTrue(enforces_no_free_exit(LEVEL_HIGH, strict_mode=False))

    def test_strict_cannot_exit_freely(self):
        self.assertTrue(enforces_no_free_exit(LEVEL_STRICT, strict_mode=False))

    def test_medium_cannot_exit_freely(self):
        self.assertTrue(enforces_no_free_exit(LEVEL_MEDIUM, strict_mode=False))

    def test_low_may_exit_freely(self):
        # Low is the "bisa keluar" tier by design — confirm dialog, no
        # auto-submit. It must keep working.
        self.assertFalse(enforces_no_free_exit(LEVEL_LOW, strict_mode=False))

    def test_low_plus_strict_flag_cannot_exit_freely(self):
        self.assertTrue(enforces_no_free_exit(LEVEL_LOW, strict_mode=True))


class DisplayLevelTestCase(unittest.TestCase):
    def test_banner_never_shows_unknown_level(self):
        # The banner looked the level up in SECURITY_COLORS; an unknown
        # level either KeyError'd or silently rendered in the low colour.
        for raw in ("low", "medium", "high", "strict", "HIGH", "wat", None, ""):
            self.assertIn(display_level(raw), (LEVEL_LOW, LEVEL_MEDIUM, LEVEL_STRICT))

    def test_banner_shows_strict_for_strict_effective_exams(self):
        self.assertEqual(display_level(LEVEL_HIGH, strict_mode=False), LEVEL_STRICT)
        self.assertEqual(display_level(LEVEL_LOW, strict_mode=True), LEVEL_STRICT)

    def test_banner_shows_raw_tier_otherwise(self):
        self.assertEqual(display_level(LEVEL_LOW, strict_mode=False), LEVEL_LOW)
        self.assertEqual(display_level(LEVEL_MEDIUM, strict_mode=False), LEVEL_MEDIUM)


if __name__ == "__main__":
    unittest.main()
