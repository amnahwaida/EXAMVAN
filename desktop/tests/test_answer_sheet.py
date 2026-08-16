"""Unit tests for examvan.ui.answer_sheet — no fabricated default questions.

Covers the desktop↔Android consistency fix (Agustus 2026): when the exam has
no question config (PDF-only), the desktop answer sheet must stay EMPTY
(mirror Android: totalQuestions = 0 + overlay hidden). Previously the widget
fabricated 40 dummy single-choice questions (A-E), letting students submit
answers to questions that don't exist.
"""

from __future__ import annotations

import os
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.answer_sheet import AnswerSheetWidget


def _app() -> QApplication:
    app = QApplication.instance()
    if app is None:
        app = QApplication([])
    return app


class AnswerSheetBuildTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.app = _app()

    def test_empty_questions_builds_nothing(self):
        w = AnswerSheetWidget()
        w.build_from_questions([])
        self.assertEqual(w._questions, [])
        self.assertEqual(w.get_answered_count(), (0, 0))
        self.assertEqual(w.get_answers(), {})

    def test_questions_built_normally(self):
        w = AnswerSheetWidget()
        w.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 2, "type": "short_answer"},
        ])
        self.assertEqual(len(w._questions), 2)
        self.assertEqual(w.get_answered_count(), (0, 2))

    def test_no_dummy_questions_can_be_answered(self):
        # Empty questions → no widgets, no answers possible.
        w = AnswerSheetWidget()
        w.build_from_questions([])
        self.assertEqual(w._answer_widgets, {})

    def test_answer_then_count(self):
        w = AnswerSheetWidget()
        w.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
        ])
        w._on_answer_changed("1", "A")
        self.assertEqual(w.get_answered_count(), (1, 1))


if __name__ == "__main__":
    unittest.main()
