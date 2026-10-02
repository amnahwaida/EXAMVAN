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


class EmptyKeyGuardTest(unittest.TestCase):
    """Field dengan key kosong/whitespace mendapat kunci sintetis (C2).

    Dulu field tanpa key DIBUANG → form kosong tetap Accepted → server 400
    selamanya. Sekarang: key dinormalisasi strip, tiap field tanpa key
    memakai `field_<index>`, widget tetap dibangun, nilai tetap terkumpul.
    """

    def test_empty_key_gets_a_fallback_widget_and_value(self):
        fields = [
            IdentityField(key="", label="Tanpa Key", required=True),
            IdentityField(key="student_name", label="Nama", required=True),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        self.assertIn("field_0", dialog._inputs)
        self.assertIn("student_name", dialog._inputs)
        dialog._inputs["field_0"].setText("X1")
        dialog._inputs["student_name"].setText("Budi")
        data = dialog.get_identity_data()
        self.assertEqual(data["field_0"], "X1")
        self.assertEqual(data["student_name"], "Budi")

    def test_whitespace_only_key_counts_as_empty(self):
        fields = [
            IdentityField(key="   ", label="Spasi", required=False),
            IdentityField(key="student_name", label="Nama", required=True),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        self.assertIn("field_0", dialog._inputs)
        self.assertNotIn("", dialog._inputs)
        self.assertNotIn("   ", dialog._inputs)

    def test_single_empty_key_also_reports_legacy_empty_key(self):
        # Tepat SATU field tanpa key → nilai JUGA ada di bawah "": baris
        # server lama menyimpan Key:"" dan mencari IdentityData[""].
        fields = [
            IdentityField(key="", label="Tanpa Key", required=False),
            IdentityField(key="student_name", label="Nama", required=True),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        dialog._inputs["field_0"].setText("LEGASI")
        dialog._inputs["student_name"].setText("Budi")
        data = dialog.get_identity_data()
        self.assertEqual(data["field_0"], "LEGASI")
        self.assertEqual(data[""], "LEGASI")

    def test_multiple_empty_keys_do_not_report_legacy_empty_key(self):
        fields = [
            IdentityField(key="", label="A", required=False),
            IdentityField(key="  ", label="B", required=False),
            IdentityField(key="student_name", label="Nama", required=True),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        dialog._inputs["field_0"].setText("v0")
        dialog._inputs["field_1"].setText("v1")
        dialog._inputs["student_name"].setText("Budi")
        data = dialog.get_identity_data()
        self.assertEqual(data["field_0"], "v0")
        self.assertEqual(data["field_1"], "v1")
        self.assertNotIn("", data)

    def test_multiple_empty_all_optional_still_accepts(self):
        # Beberapa-tapi-semua-opsional: lanjut (server melewati yang
        # non-required) — penolakan hanya untuk yang required.
        fields = [
            IdentityField(key="", label="A", required=False),
            IdentityField(key="", label="B", required=False),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        with mock.patch.object(dialog, "accept") as accept:
            dialog._on_submit()
        accept.assert_called_once()

    def test_multiple_empty_with_required_refuses_join(self):
        fields = [
            IdentityField(key="", label="Tanpa Key", required=True),
            IdentityField(key="  ", label="Lain", required=False),
            IdentityField(key="student_name", label="Nama", required=True),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        dialog._inputs["field_0"].setText("terisi")
        dialog._inputs["field_1"].setText("terisi")
        dialog._inputs["student_name"].setText("Budi")
        with mock.patch(
            "examvan.ui.identity_dialog.QMessageBox.warning"
        ) as warn, mock.patch.object(dialog, "accept") as accept:
            dialog._on_submit()
        warn.assert_called_once()
        args = warn.call_args.args
        self.assertIn("Tanpa Key", args[2])
        self.assertIn("hubungi pengawas", args[2])
        accept.assert_not_called()

    def test_stripped_keys_are_deduplicated(self):
        # L1: "nama " dan "nama" adalah kunci yang sama setelah strip
        # (strip saja — case dipertahankan apa adanya).
        fields = [
            IdentityField(key="nama ", label="Nama", required=True),
            IdentityField(key="nama", label="Nama Lagi", required=False),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        self.assertEqual(set(dialog._inputs), {"nama"})


class DuplicateRequiredMergeTest(unittest.TestCase):
    """C4: tabrakan key — required dari SALAH SATU menang (bintang tampil)."""

    def test_required_from_either_collision_wins(self):
        fields = [
            IdentityField(key="student_name", label="Nama", required=False),
            IdentityField(key="student_name", label="Nama Lagi", required=True),
        ]
        exam = _make_exam(fields)
        with self.assertLogs(
            "examvan.ui.identity_dialog", level="WARNING"
        ) as cm:
            dialog = IdentityDialog(exam)
        merged = next(
            f for f in dialog._fields if f.key == "student_name")
        self.assertTrue(
            merged.required,
            "salah satu tabrakan required → gabungan harus required "
            "(bintang tampil sebagai pemberitahuan ke siswa)",
        )
        # Widget yang dipakai tetap yang PERTAMA.
        self.assertEqual(len(dialog._inputs), 1)
        self.assertTrue(
            any("duplikat" in m for m in cm.output),
            "harus ada log peringatan untuk key duplikat",
        )

    def test_first_required_stays_required(self):
        fields = [
            IdentityField(key="student_name", label="Nama", required=True),
            IdentityField(key="student_name", label="Nama Lagi", required=False),
        ]
        exam = _make_exam(fields)
        dialog = IdentityDialog(exam)
        merged = next(
            f for f in dialog._fields if f.key == "student_name")
        self.assertTrue(merged.required)


class DefaultFieldsMatchServerTest(unittest.TestCase):
    """L3: fallback client == default server saat identity_fields kosong.

    Server memakai default yang sama bila config kosong
    (webui/internal/handlers/api/exams.go `defaultIdentityFields` +
    public/hasil.go + admin/submissions.go): student_name/Nama,
    exam_number/Nomor Ujian, student_class/Kelas — semua required.
    Kalau salah satu sisi berubah tanpa sisi lain, izin/pencocokan
    identitas meleset diam-diam.
    """

    # Cermin literal `defaultIdentityFields` Go (jangan diubah tanpa
    # mengubah server juga).
    SERVER_DEFAULTS = [
        ("student_name", "Nama", True),
        ("exam_number", "Nomor Ujian", True),
        ("student_class", "Kelas", True),
    ]

    def test_client_fallback_keys_match_server_defaults(self):
        client = [
            (f.key, f.label, f.required)
            for f in IdentityDialog._DEFAULT_FIELDS
        ]
        self.assertEqual(client, self.SERVER_DEFAULTS)

    def test_empty_identity_fields_use_the_fallback(self):
        exam = Exam(id=7, name="Ujian", status="active",
                    identity_fields=[])
        dialog = IdentityDialog(exam)
        self.assertEqual(
            {f.key for f in dialog._fields},
            {"student_name", "exam_number", "student_class"},
        )


class ValidationUXTest(unittest.TestCase):
    """L4: error diringkas (~5 + sisa) dan fokus ke pelanggar pertama."""

    def _dialog(self, n_required=8):
        fields = [
            IdentityField(key=f"k{i}", label=f"Kolom {i}", required=True)
            for i in range(n_required)
        ]
        return IdentityDialog(_make_exam(fields))

    def test_many_errors_are_summarized(self):
        dialog = self._dialog(n_required=8)
        with mock.patch(
            "examvan.ui.identity_dialog.QMessageBox.warning"
        ) as warn, mock.patch.object(dialog, "accept"):
            dialog._on_submit()
        warn.assert_called_once()
        text = warn.call_args.args[2]
        self.assertIn("…dan 3 field lain", text)

    def test_few_errors_are_shown_in_full(self):
        dialog = self._dialog(n_required=3)
        with mock.patch(
            "examvan.ui.identity_dialog.QMessageBox.warning"
        ) as warn, mock.patch.object(dialog, "accept"):
            dialog._on_submit()
        text = warn.call_args.args[2]
        self.assertNotIn("field lain", text)
        self.assertIn("Kolom 0", text)

    def test_first_offender_gets_focus(self):
        dialog = self._dialog(n_required=3)
        dialog._inputs["k1"].setText("terisi")
        dialog._inputs["k2"].setText("terisi")
        seen = []
        dialog._inputs["k0"].setFocus = lambda: seen.append("k0")  # noqa: E731
        with mock.patch(
            "examvan.ui.identity_dialog.QMessageBox.warning"
        ), mock.patch.object(dialog, "accept"):
            dialog._on_submit()
        self.assertEqual(seen, ["k0"])
