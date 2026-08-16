"""Unit tests for examvan.config — answer persistence + sticky submitted flag.

Covers the desktop↔Android consistency fixes (Agustus 2026):
- resolve_submit_answers falls back to the disk copy when memory is empty
  (F1 — prevents an empty submit from overwriting real answers in the
  server's grace window);
- mark_submitted/is_submitted persist a STICKY "exam finished" marker that
  clear_answers must NOT remove (F2 — prevents re-entry from re-running the
  exam and overwriting a durable submission).
"""

from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import examvan.config as config


class ConfigTestCase(unittest.TestCase):
    """Redirect config storage to a temp dir for each test."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-config-test-")
        self._dir_patch = mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
        )
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None

    def tearDown(self):
        self._dir_patch.stop()
        self._file_patch.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)


class AnswersPersistenceTest(ConfigTestCase):
    def test_roundtrip(self):
        answers = {"1": "A", "2": ["B", "C"], "3": {"1": "X"}, "4": "teks jawaban"}
        config.save_answers(42, answers)
        self.assertEqual(config.load_answers(42), answers)

    def test_clear_removes(self):
        config.save_answers(42, {"1": "A"})
        config.clear_answers(42)
        self.assertIsNone(config.load_answers(42))

    def test_exam_scoped(self):
        config.save_answers(1, {"1": "A"})
        self.assertIsNone(config.load_answers(2))
        self.assertEqual(config.load_answers(1), {"1": "A"})

    def test_corrupt_file_returns_none(self):
        path = Path(self._tmp) / "answers_42.dat"
        path.write_text("not-valid-base64!!", encoding="ascii")
        self.assertIsNone(config.load_answers(42))

    def test_legacy_json_migrated(self):
        legacy = Path(self._tmp) / "answers_42.json"
        legacy.write_text('{"1": "A"}', encoding="utf-8")
        self.assertEqual(config.load_answers(42), {"1": "A"})
        # Migrated: .dat exists, legacy gone.
        self.assertTrue((Path(self._tmp) / "answers_42.dat").exists())
        self.assertFalse(legacy.exists())


class ResolveSubmitAnswersTest(ConfigTestCase):
    """F1: empty memory must fall back to the disk copy."""

    def test_memory_wins_when_nonempty(self):
        config.save_answers(42, {"1": "OLD"})
        got = config.resolve_submit_answers({"1": "NEW"}, 42)
        self.assertEqual(got, {"1": "NEW"})

    def test_empty_memory_falls_back_to_disk(self):
        config.save_answers(42, {"1": "A", "2": "B"})
        got = config.resolve_submit_answers({}, 42)
        self.assertEqual(got, {"1": "A", "2": "B"})

    def test_empty_memory_no_disk_returns_empty(self):
        self.assertEqual(config.resolve_submit_answers({}, 42), {})

    def test_none_memory_falls_back_to_disk(self):
        config.save_answers(42, {"1": "A"})
        self.assertEqual(config.resolve_submit_answers(None, 42), {"1": "A"})


class SubmittedFlagTest(ConfigTestCase):
    """F2: sticky submitted marker survives clear_answers."""

    def test_mark_then_check(self):
        self.assertFalse(config.is_submitted(42))
        config.mark_submitted(42)
        self.assertTrue(config.is_submitted(42))

    def test_exam_scoped(self):
        config.mark_submitted(1)
        self.assertTrue(config.is_submitted(1))
        self.assertFalse(config.is_submitted(2))

    def test_survives_clear_answers(self):
        config.mark_submitted(42)
        config.save_answers(42, {"1": "A"})
        config.clear_answers(42)
        # Sticky: after a durable submit the answers are gone but the
        # "finished" marker must remain — re-entry shows "already done"
        # instead of re-running the exam (mirror Android submittedOrExited).
        self.assertTrue(config.is_submitted(42))
        self.assertIsNone(config.load_answers(42))

    def test_survives_reload(self):
        config.mark_submitted(42)
        config._cache = None  # simulate process restart
        self.assertTrue(config.is_submitted(42))


if __name__ == "__main__":
    unittest.main()
