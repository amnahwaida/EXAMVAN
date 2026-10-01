"""Tests for IdentityDialog — duplicate field key guard (HIGH H15).

Verifies that duplicate IdentityField keys in the API response are detected
and deduplicated, so the second input does not silently overwrite the first
in the _inputs dict.
"""

from __future__ import annotations

import logging
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog


def _make_exam(fields=None):
    if fields is None:
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="exam_number", label="Nomor Ujian", required=True),
            IdentityField(key="student_class", label="Kelas", required=True),
        ]
    return Exam(id=7, name="Ujian", status="active", identity_fields=fields)


class DuplicateKeyGuardTest(unittest.TestCase):
    def test_duplicate_keys_are_deduplicated(self):
        """Duplicate field keys should not crash and should keep only the first."""
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="student_name", label="Nama Lagi", required=False),
            IdentityField(key="exam_number", label="Nomor Ujian", required=True),
        ]
        exam = _make_exam(fields)
        with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                        return_value=1):
            dialog = IdentityDialog(exam)

        # Only 2 unique keys in the inputs dict.
        self.assertEqual(len(dialog._inputs), 2)
        self.assertIn("student_name", dialog._inputs)
        self.assertIn("exam_number", dialog._inputs)

        # The first field with that key wins (label "Nama").
        label_widget = dialog._inputs["student_name"]
        self.assertIsNotNone(label_widget)

    def test_warning_is_logged_for_duplicate_key(self):
        """Duplicate keys should produce a warning log entry."""
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="student_name", label="Nama Lagi", required=True),
        ]
        exam = _make_exam(fields)
        with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                        return_value=1):
            with self.assertLogs(
                "examvan.ui.identity_dialog", level="WARNING"
            ) as cm:
                IdentityDialog(exam)

        dup_log = [m for m in cm.output if "duplikat" in m[0] or "duplikat" in m]
        self.assertTrue(dup_log, "harus ada log peringatan untuk key duplikat")

    def test_no_duplicate_keys_unchanged(self):
        """Normal (no duplicates) dialog has same number of inputs as fields."""
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="exam_number", label="Nomor Ujian", required=True),
        ]
        exam = _make_exam(fields)
        with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                        return_value=1):
            dialog = IdentityDialog(exam)

        self.assertEqual(len(dialog._inputs), 2)

    def test_submission_uses_deduplicated_fields(self):
        """get_identity_data returns unique keys only."""
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="student_name", label="Nama Lagi", required=False),
            IdentityField(key="exam_number", label="Nomor Ujian", required=True),
        ]
        exam = _make_exam(fields)
        with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                        return_value=1):
            dialog = IdentityDialog(exam)

        # Set text in both "student_name" inputs (only one exists after dedup).
        dialog._inputs["student_name"].setText("Budi")
        dialog._inputs["exam_number"].setText("N01")

        data = dialog.get_identity_data()
        self.assertEqual(set(data.keys()), {"student_name", "exam_number"})
        self.assertEqual(data["student_name"], "Budi")
        self.assertEqual(data["exam_number"], "N01")


if __name__ == "__main__":
    unittest.main()
