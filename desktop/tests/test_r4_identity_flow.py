"""R4 identity flow end-to-end: dialog → canonical mapping → submit key.

Menjahit C2 + C4 + H1/H4 dalam satu alur: field tanpa key dan key duplikat
dari API, nilai yang dikumpulkan dialog, pemetaan kanonik yang dipakai
submit/approval, dan kunci siswa yang dipakai gerbang izin — semuanya harus
sepakat untuk identitas yang sama.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog
from examvan.utils import build_student_key, map_identity_to_standard


def _exam(fields):
    return Exam(id=7, name="Ujian", status="active", identity_fields=fields)


class R4IdentityFlowTest(unittest.TestCase):
    def test_empty_and_duplicate_keys_flow_to_canonical_slots(self):
        # API mengirim: satu field tanpa key (required), satu duplikat
        # (optional dulu, required kemudian), dan field biasa.
        fields = [
            IdentityField(key="", label="Absen", required=False),
            IdentityField(key="nama_peserta", label="Nama", required=False),
            IdentityField(key="nama_peserta", label="Nama (2)", required=True),
            IdentityField(key="nomor_ujian", label="Nomor", required=True),
            IdentityField(key="kelas", label="Kelas", required=True),
        ]
        dialog = IdentityDialog(_exam(fields))
        try:
            # Fallback untuk yang tanpa key + dedup gabungan-required.
            self.assertIn("field_0", dialog._inputs)
            merged = next(
                f for f in dialog._fields if f.key == "nama_peserta")
            self.assertTrue(merged.required)

            dialog._inputs["field_0"].setText("X")
            dialog._inputs["nama_peserta"].setText("Budi")
            dialog._inputs["nomor_ujian"].setText("N01")
            dialog._inputs["kelas"].setText("9A")
            with mock.patch.object(dialog, "accept") as accept:
                dialog._on_submit()
            accept.assert_called_once()

            data = dialog.get_identity_data()
            # Tepat satu empty-key → warisan "" ikut ada.
            self.assertEqual(data[""], "X")
            std = map_identity_to_standard(data)
            self.assertEqual(
                (std.get("student_name"), std.get("exam_number"),
                 std.get("student_class")),
                ("Budi", "N01", "9A"),
            )
            # Kunci siswa dari data mentah == dari data kanonik (parity).
            self.assertEqual(
                build_student_key(data),
                build_student_key({
                    "student_name": "Budi", "exam_number": "N01",
                    "student_class": "9A"}),
            )
            self.assertEqual(build_student_key(data), "n01")
        finally:
            dialog.close()

    def test_refused_join_never_reaches_mapping(self):
        fields = [
            IdentityField(key="", label="A", required=True),
            IdentityField(key=" ", label="B", required=False),
            IdentityField(key="nama", label="Nama", required=True),
        ]
        dialog = IdentityDialog(_exam(fields))
        try:
            dialog._inputs["field_0"].setText("x")
            dialog._inputs["field_1"].setText("y")
            dialog._inputs["nama"].setText("Budi")
            with mock.patch(
                "examvan.ui.identity_dialog.QMessageBox.warning"
            ) as warn, mock.patch.object(dialog, "accept") as accept:
                dialog._on_submit()
            warn.assert_called_once()
            accept.assert_not_called()
        finally:
            dialog.close()


if __name__ == "__main__":
    unittest.main()
