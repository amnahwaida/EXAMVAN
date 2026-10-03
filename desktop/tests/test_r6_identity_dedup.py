"""Ronde 6 (item 1) — dedup field identitas harus case-insensitive, seperti server.

Bug
---
`ui/identity_dialog.py` memakai `if norm_key in seen:` dengan `seen` ber-key
raw (strip saja). Server memakai normalisasi LAIN untuk hal yang sama:

    // webui/internal/handlers/admin/exams.go — validateIdentityFields
    lower := strings.ToLower(key)
    if prev, dup := seen[lower]; dup { ... }

Jadi untuk konfigurasi warisan/import `{key:"Nama"}, {key:"nama"}` (atau
`Nama` + `Nama ` yang lolos ke disk sebelum validasi ini ada):

* client membangun DUA input berlabel sama — siswa mengisinya dua kali;
* hanya satu key yang punya makna, nilai yang lain hilang diam-diam;
* server menganggap itu SATU key (dan kini menolak menyimpannya).

Arahnya sudah diverifikasi: client membangun dua input, server menganggap
satu.

Perbaikan
---------
Dedup memakai `norm_key.casefold()` sebagai kunci peta, TETAPI key ASLI
(yang pertama tampil) tetap dipakai sebagai kunci wire. Yang penting nilai
yang diketik siswa tetap mendarat di key yang dibaca server
(`body.IdentityData[field.Key]`).

Aturan `required` gabung dari HIGH H15 harus tetap hidup untuk tabrakan
case-insensitive: kalau kemunculan kedua yang `required`, field hasil
gabungan wajib diisi — itulah pemberitahuan ke siswa.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog


def _exam(fields):
    return Exam(id=7, name="Ujian", status="active", identity_fields=fields)


def _dialog(fields):
    with mock.patch("examvan.ui.identity_dialog.QDialog.exec_", return_value=1):
        return IdentityDialog(_exam(fields))


def _fill(dialog, values):
    for key, val in values.items():
        dialog._inputs[key].setText(val)


class CaseInsensitiveDedupTest(unittest.TestCase):
    def test_case_only_collision_builds_exactly_one_input(self):
        fields = [
            IdentityField(key="Nama", label="Nama", required=True),
            IdentityField(key="nama", label="Nama", required=True),
        ]
        dlg = _dialog(fields)
        try:
            #Dua key yang menormalisasi sama = SATU field, sama seperti server.
            self.assertEqual(len(dlg._inputs), 1, dlg._inputs)
            self.assertEqual(len(dlg._fields), 1)
        finally:
            dlg.close()

    def test_surviving_original_key_reaches_the_wire(self):
        """Key ASLI yang bertahan — nilai siswa harus mendarat di key itu."""
        fields = [
            IdentityField(key="Nama", label="Nama", required=True),
            IdentityField(key="nama", label="Nama", required=True),
        ]
        dlg = _dialog(fields)
        try:
            _fill(dlg, {"Nama": "Andi"})
            with mock.patch("examvan.ui.identity_dialog.QMessageBox.warning"):
                dlg._on_submit()
            data = dlg.get_identity_data()
            self.assertIn("Nama", data)
            self.assertNotIn("nama", data)
            self.assertEqual(data["Nama"], "Andi")
        finally:
            dlg.close()

    def test_required_promotion_fires_on_the_second_occurrence(self):
        """Kemunculan kedua yang `required` -&gt; hasil gabungan wajib diisi.

        Tanpa ini, konfigurasi `Nama`(opsional) + `nama`(wajib) akan
        kehilangan tanda bintang dan form bisa Accepted kosong.
        """
        fields = [
            IdentityField(key="Nama", label="Nama", required=False),
            IdentityField(key="nama", label="Nama", required=True),
        ]
        dlg = _dialog(fields)
        try:
            merged = next(f for f in dlg._fields if f.key == "Nama")
            self.assertTrue(merged.required)

            # Kosong + wajib -&gt; ditolak, bukan Accepted.
            with mock.patch(
                "examvan.ui.identity_dialog.QMessageBox.warning"
            ) as warn, mock.patch.object(dlg, "accept") as accept:
                dlg._on_submit()
            # Field wajib yang kosong ditolak lewat pesan INLINE di bawah
            # kotaknya, bukan lewat dialog modal.
            accept.assert_not_called()
            warn.assert_not_called()
            self.assertFalse(dlg._error_labels["Nama"].isHidden())
        finally:
            dlg.close()

    def test_required_promotion_fires_when_the_first_occurrence_is_required(self):
        fields = [
            IdentityField(key="Nama", label="Nama", required=True),
            IdentityField(key="nama", label="Nama", required=False),
        ]
        dlg = _dialog(fields)
        try:
            merged = next(f for f in dlg._fields if f.key == "Nama")
            self.assertTrue(merged.required)
            with mock.patch(
                "examvan.ui.identity_dialog.QMessageBox.warning"
            ), mock.patch.object(dlg, "accept") as accept:
                dlg._on_submit()
            accept.assert_not_called()
        finally:
            dlg.close()

    def test_collision_after_strip_is_also_deduplicated(self):
        """Server menormalisasi pada trimmed form; spasi ikut dihitung."""
        fields = [
            IdentityField(key="Nama", label="Nama", required=True),
            IdentityField(key="  Nama  ", label="Nama", required=True),
        ]
        dlg = _dialog(fields)
        try:
            self.assertEqual(len(dlg._inputs), 1, dlg._inputs)
            self.assertEqual(list(dlg._inputs), ["Nama"])
        finally:
            dlg.close()

    def test_distinct_keys_are_untouched(self):
        """Guard: dedup case-insensitive tidak boleh menggabung keys beda."""
        fields = [
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="nomor", label="Nomor", required=True),
            IdentityField(key="nama_lengkap", label="Nama Lengkap",
                          required=False),
            IdentityField(key="kelas", label="Kelas", required=True),
        ]
        dlg = _dialog(fields)
        try:
            self.assertEqual(len(dlg._inputs), 4, dlg._inputs)
            self.assertEqual(
                sorted(dlg._inputs),
                ["kelas", "nama", "nama_lengkap", "nomor"],
            )
        finally:
            dlg.close()

    def test_ambiguous_collision_is_logged(self):
        fields = [
            IdentityField(key="Nama", label="Nama", required=True),
            IdentityField(key="nama", label="Nama", required=True),
        ]
        with self.assertLogs(
            "examvan.ui.identity_dialog", level="WARNING"
        ) as cm:
            dlg = _dialog(fields)
        try:
            joined = " ".join(cm.output)
            self.assertIn("Nama", joined)
            self.assertIn("nama", joined)
        finally:
            dlg.close()


if __name__ == "__main__":  # pragma: no cover
    unittest.main()