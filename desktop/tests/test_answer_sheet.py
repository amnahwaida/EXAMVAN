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

from PyQt5.QtWidgets import QApplication, QLineEdit, QRadioButton

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

    def test_matching_range_numbering_labels_the_span(self):
        question = {
            "number": 1,
            "type": "matching",
            "numbering": "range",
            "left_items": ["Satu", "Dua"],
            "right_items": ["A", "B"],
        }
        self.assertEqual(
            "1–2", AnswerSheetWidget._display_number("1", "matching", question)
        )
        question["numbering"] = "single"
        self.assertEqual(
            "1", AnswerSheetWidget._display_number("1", "matching", question)
        )
        question.pop("numbering")
        self.assertEqual(
            "1", AnswerSheetWidget._display_number("1", "matching", question)
        )

    def test_short_answer_multi_saves_and_restores_list(self):
        question = {
            "number": 1,
            "type": "short_answer",
            "numbering": "range",
            "statements": ["Ibu kota", "Hasil"],
            "key": ["Jakarta", "7"],
        }
        self.assertEqual(
            "1–2", AnswerSheetWidget._display_number("1", "short_answer", question)
        )

        w = AnswerSheetWidget()
        w.build_from_questions([question])
        edits = w.findChildren(QLineEdit)
        self.assertEqual(2, len(edits))
        edits[0].setText("Jakarta")
        edits[1].setText("7")
        self.assertEqual(["Jakarta", "7"], w.get_answers()["1"])

        restored = AnswerSheetWidget()
        self.addCleanup(restored.deleteLater)
        restored.build_from_questions([question])
        restored.restore_answers({"1": ["Bandung", "8"]})
        self.assertEqual(["Bandung", "8"], restored.get_answers()["1"])
        restored_edits = restored.findChildren(QLineEdit)
        self.assertEqual("Bandung", restored_edits[0].text())
        self.assertEqual("8", restored_edits[1].text())

        question["numbering"] = "single"
        self.assertEqual(
            "1", AnswerSheetWidget._display_number("1", "short_answer", question)
        )

    def test_true_false_custom_labels_save_true_false_values(self):
        w = AnswerSheetWidget()
        w.build_from_questions([
            {
                "number": 1,
                "type": "true_false",
                "statements": ["Air membeku pada 0 derajat."],
                "true_label": "Sesuai",
                "false_label": "Tidak Sesuai",
            },
        ])
        radios = w.findChildren(QRadioButton)
        labels = [rb.text() for rb in radios]
        self.assertIn("Sesuai", labels)
        self.assertIn("Tidak Sesuai", labels)
        next(rb for rb in radios if rb.text() == "Sesuai").click()
        self.assertEqual(w.get_answers()["1"], ["TRUE"])

        restored = AnswerSheetWidget()
        self.addCleanup(restored.deleteLater)
        restored.build_from_questions([
            {
                "number": 1,
                "type": "true_false",
                "statements": ["Air membeku pada 0 derajat."],
                "true_label": "Sesuai",
                "false_label": "Tidak Sesuai",
            },
        ])
        restored.restore_answers({"1": ["FALSE"]})
        self.assertEqual(restored.get_answers()["1"], ["FALSE"])
        checked = next(rb for rb in restored.findChildren(QRadioButton) if rb.isChecked())
        self.assertEqual(checked.text(), "Tidak Sesuai")

    def test_answer_then_count(self):
        w = AnswerSheetWidget()
        w.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
        ])
        w._on_answer_changed("1", "A")
        self.assertEqual(w.get_answered_count(), (1, 1))


if __name__ == "__main__":
    unittest.main()
