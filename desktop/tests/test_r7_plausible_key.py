"""Ronde 7 (M2) — key identitas hanya ditolak kalau server tidak bisa membacanya.

Bug
---
`ui/identity_dialog._plausible_key` menolak key yang "tidak memuat
sekidaknya satu huruf". Server tidak punya aturan seperti itu:

    // webui/internal/handlers/admin/exams.go:validateIdentityFields
    key, ok := raw.(string)
    if !ok || strings.TrimSpace(key) == "" { tolak }

    // webui/internal/handlers/api/exams.go:identityFieldValue
    if v, ok := data[k].(string); ok { ... }

Yang dibutuhkan server hanya "key ini string yang tidak kosong" — dan
key itu dipakai PERSIS seperti yang tersimpan, tanpa pola apa pun. Admin UI
sendiri lowering label lalu mengganti semua non-alphanumeric dengan `_`
(`static/js/admin.js:1012`), jadi label "2024/2025" disimpan dengan key
`2024_2025`. Key itu tidak punya satu pun huruf.

Dampaknya tidak bisa diperbaiki dari sisi siswa:

* sebagai field WAJIB -> `_on_submit` menolak join dengan "konfigurasi
  ujian salah" untuk SELURUH kelas yang memakai ujian itu, padahal
  konfigurasinya sah;
* sebagai field OPSIONAL -> field-nya dibuang diam-diam, jadi siswa
  tidak pernah ditanyakan tahun ajaran.

Perbaikan
---------
Predikatnya diganti dengan milik server: sebuah key tidak bisa
dibaca HANYA kalau nilai JSON-nya bukan string (`{"key": 123}` gagal
decode ke `Key string`, UnmarshalTypeError-nya dibuang, dan field itu
tersimpan dengan `Key:""`). Fakta itu sekarang DIBAWA oleh
`models.IdentityField.key_is_text` alih-alih ditebak ulang dari bentuk
teks key-nya, dan tebakan bentuk-teks itu dihapus.

Jalur penolakan yang sudah ada (wajib -> tolak join dengan pesan
konfigurasi; opsional + ada alternatif -> buang; opsional tanpa
alternatif -> tetap ditampilkan) tidak berubah.
"""

from __future__ import annotations

import logging
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog, _plausible_key

LOGGER = "examvan.models"
LOGGER_UI = "examvan.ui.identity_dialog"

# Key yang DIHASILKAN admin UI dari label (`label.toLowerCase()
# .replace(/[^a-z0-9]/g,'_')`). `2024_2025` tidak punya satu huruf pun —
# inilah yang bikin heuristic lama menolak seluruh kelas.
ADMIN_DERIVED_KEYS = ["2024_2025", "9", "no_absen", "nama", "nisn"]


def _exam(fields):
    return Exam(id=7, name="Ujian Matematika", status="active",
                identity_fields=fields)


def _dialog(fields):
    with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                    return_value=1):
        return IdentityDialog(_exam(fields))


class PlausibleKeyIsTheServerPredicateTest(unittest.TestCase):
    def test_admin_derived_keys_without_letters_are_readable(self):
        for key in ADMIN_DERIVED_KEYS:
            with self.subTest(key=key):
                self.assertTrue(
                    _plausible_key(IdentityField(key=key, label=key)),
                    f"key {key!r} adalah string yang bisa dibaca server "
                    "(`identityFieldValue` membacanya persis)",
                )

    def test_a_real_non_string_key_is_not_readable(self):
        # Angka JSON tidak bisa decode ke `Key string` di Go: field itu
        # tersimpan dengan `Key:""` dan tidak akan pernah cocok.
        self.assertFalse(
            _plausible_key(IdentityField(key="123.0", label="Absen",
                                         key_is_text=False)),
        )

    def test_from_json_records_the_string_fact_per_field(self):
        payload = {
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [
                {"key": "2024_2025", "label": "2024/2025", "required": True},
                {"key": "9", "label": "9", "required": True},
                {"key": "no_absen", "label": "No. Absen", "required": True},
                {"key": "nama", "label": "Nama", "required": True},
                {"key": 123, "label": "Absen", "required": True},
                {"key": None, "label": "Tanpa Key", "required": False},
            ],
        }
        exam = Exam.from_json(payload)
        got = {f.key: f.key_is_text for f in exam.identity_fields}
        self.assertEqual(got, {
            "2024_2025": True, "9": True, "no_absen": True, "nama": True,
            "123": False, "": True,
        })

    def test_default_is_readable_so_hand_built_fields_stay_usable(self):
        self.assertTrue(_plausible_key(IdentityField(key="nama", label="Nama")))


class RequiredNumericKeyCaseTest(unittest.TestCase):
    """Key rusak yang benar-benar tidak terbaca: wajib tetap menolak."""

    def test_required_unreadable_key_refuses_the_join(self):
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="123.0", label="Absen", required=True,
                          key_is_text=False),
        ])
        self.addCleanup(dlg.close)
        self.assertEqual(len(dlg._fields), 2)
        dlg._inputs["nama"].setText("Budi")
        dlg._inputs["123.0"].setText("12")
        with mock.patch(
            "examvan.ui.identity_dialog.QMessageBox.warning"
        ) as warn, mock.patch.object(dlg, "accept") as accept:
            dlg._on_submit()
        accept.assert_not_called()
        self.assertTrue(warn.called)
        self.assertEqual(dlg._broken_key_labels, ["Absen"])

    def test_optional_unreadable_key_is_dropped_when_alternative_exists(self):
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="123.0", label="Absen", required=False,
                          key_is_text=False),
        ])
        self.addCleanup(dlg.close)
        keys = [f.key for f in dlg._fields]
        self.assertIn("nama", keys)
        self.assertNotIn("123.0", keys)

    def test_optional_unreadable_key_is_kept_when_it_is_the_only_one(self):
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="123.0", label="Absen", required=False,
                          key_is_text=False),
            IdentityField(key="kode_pos", label="Kode Pos", required=False),
        ])
        self.addCleanup(dlg.close)
        keys = [f.key for f in dlg._fields]
        self.assertIn("kode_pos", keys)


class DigitOnlyKeyIsNeverRejectedTest(unittest.TestCase):
    """Key angka hasil admin UI harus LEWAT untuk wajib dan opsional."""

    def test_required_digit_only_key_does_not_refuse_the_join(self):
        # Label "2024/2025" -> key `2024_2025` (semua non-alphanumeric jadi
        # `_` di admin UI). Before the fix the whole class was locked out.
        exam = Exam.from_json({
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [
                {"key": "nama", "label": "Nama", "required": True},
                {"key": "nomor_ujian", "label": "Nomor Ujian",
                 "required": True},
                {"key": "2024_2025", "label": "2024/2025", "required": True},
            ],
        })
        dlg = _dialog(exam.identity_fields)
        self.addCleanup(dlg.close)
        self.assertEqual(dlg._broken_key_labels, [])
        dlg._inputs["nama"].setText("Budi")
        dlg._inputs["nomor_ujian"].setText("N01")
        dlg._inputs["2024_2025"].setText("2024/2025")
        with mock.patch.object(dlg, "accept") as accept:
            dlg._on_submit()
        accept.assert_called_once()
        self.assertEqual(dlg.get_identity_data()["2024_2025"], "2024/2025")

    def test_optional_digit_only_key_is_asked_not_dropped(self):
        exam = Exam.from_json({
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [
                {"key": "nama", "label": "Nama", "required": True},
                {"key": "2024_2025", "label": "2024/2025", "required": False},
            ],
        })
        dlg = _dialog(exam.identity_fields)
        self.addCleanup(dlg.close)
        keys = [f.key for f in dlg._fields]
        self.assertIn("2024_2025", keys)
        self.assertIn("2024_2025", dlg._inputs)

    def test_required_single_digit_key_does_not_refuse_the_join(self):
        exam = Exam.from_json({
            "id": 7,
            "name": "Ujian",
            "status": "active",
            "identity_fields": [
                {"key": "9", "label": "9", "required": True},
            ],
        })
        dlg = _dialog(exam.identity_fields)
        self.addCleanup(dlg.close)
        self.assertEqual(dlg._broken_key_labels, [])
        dlg._inputs["9"].setText("9")
        with mock.patch.object(dlg, "accept") as accept:
            dlg._on_submit()
        accept.assert_called_once()

    def test_no_absen_and_nama_are_unaffected(self):
        exam = Exam.from_json({
            "id": 7,
            "name": "Ujian",
            "status": "active",
            "identity_fields": [
                {"key": "no_absen", "label": "No. Absen", "required": True},
                {"key": "nama", "label": "Nama", "required": True},
            ],
        })
        dlg = _dialog(exam.identity_fields)
        self.addCleanup(dlg.close)
        self.assertEqual(dlg._broken_key_labels, [])
        dlg._inputs["no_absen"].setText("12")
        dlg._inputs["nama"].setText("Budi")
        with mock.patch.object(dlg, "accept") as accept:
            dlg._on_submit()
        accept.assert_called_once()

    def test_nothing_is_logged_as_a_broken_key(self):
        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append(record.getMessage())

        logger = logging.getLogger(LOGGER_UI)
        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.DEBUG)
        try:
            dlg = _dialog([
                IdentityField(key="2024_2025", label="2024/2025",
                              required=True),
                IdentityField(key="nama", label="Nama", required=True),
            ])
            dlg.close()
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        joined = " ".join(records).lower()
        self.assertNotIn("tidak valid", joined)
        self.assertNotIn("dibuang", joined)


if __name__ == "__main__":  # pragma: no cover
    unittest.main()