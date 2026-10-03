"""Ronde 6 (item 2) — key identitas non-string harus terdeteksi, bukan diam.

Bug
---
`models.Exam.from_json` melakukan `key=str(f.get("key") or "")`. Key angka
`123` dari JSON menjadi `"123.0"` di client (float repr), sedangkan sisi
server berbeda:

    // webui/internal/handlers/api/exams.go — expectedFields
    _ = json.Unmarshal(raw, &fields)     // error DIBUANG

`123` tidak bisa decode ke `Key string` field struct, jadi `Key` tetap
`""` dan field itu tidak akan pernah cocok. Akibatnya client mengirim
`{"123.0": "..."}` sementara server mencari `body.IdentityData[""]`:
400 selamanya, dan tidak ada yang melaporkannya.

Sisi pengawas sudah menolak konfigurasi seperti ini
(`admin/exams.go:validateIdentityFields`: "key harus berupa teks yang tidak
kosong"). Yang belum ada adalah JEJAK di client, dan pengaman supaya form
yang lolos tetap bisa didiagnosis.

Perbaikan
---------
1. Coercion tetap (tidak boleh crash — server rusak tidak boleh menjatuhkan
   dialog join), TETAPI sekarang di-LOG dengan nama ujian + key bermasalah.
2. Field yang key-nya bukan identifier yang masuk akal (`"123.0"` — angka,
   selalu bawa titik desimal) dibuang HANYA bila ada alternatif yang bisa
   dipakai; tidak pernah membuang field `required`.
3. Kalau memang tidak ada alternatif yang bisa dipakai, dialog TOLAK join
   dengan pesan jelas — bukan Accepted dengan form kosong. Ini menjaga
   fix sebelumnya (field tanpa key tidak boleh lolos diam-diam).
"""

from __future__ import annotations

import logging
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog


LOGGER = "examvan.models"


def _exam(fields):
    return Exam(id=7, name="Ujian Matematika", status="active",
                identity_fields=fields)


class NumericKeyDoesNotCrashTest(unittest.TestCase):
    def test_numeric_key_does_not_crash_from_json(self):
        payload = {
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [{"key": 123, "label": "Absen", "required": True}],
        }
        exam = Exam.from_json(payload)  # tidak boleh melempar
        self.assertEqual(exam.name, "Ujian Matematika")
        self.assertEqual(len(exam.identity_fields), 1)

    def test_numeric_key_is_logged_with_exam_name_and_key(self):
        payload = {
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [{"key": 123, "label": "Absen", "required": True}],
        }
        with self.assertLogs(LOGGER, level="WARNING") as cm:
            Exam.from_json(payload)
        joined = " ".join(cm.output)
        self.assertIn("Ujian Matematika", joined)
        self.assertIn("123", joined)

    def test_bool_and_none_key_also_logged(self):
        payload = {
            "id": 9,
            "name": "Ujian Biologi",
            "status": "active",
            "identity_fields": [
                {"key": True, "label": "A", "required": True},
                {"key": None, "label": "B", "required": False},
            ],
        }
        with self.assertLogs(LOGGER, level="WARNING") as cm:
            Exam.from_json(payload)
        joined = " ".join(cm.output)
        self.assertIn("Ujian Biologi", joined)

    def test_no_crash_and_no_warning_for_string_keys(self):
        payload = {
            "id": 7,
            "name": "Ujian",
            "status": "active",
            "identity_fields": [
                {"key": "nama", "label": "Nama", "required": True},
            ],
        }
        logger = logging.getLogger(LOGGER)
        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append(record.getMessage())

        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.WARNING)
        try:
            exam = Exam.from_json(payload)
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        self.assertEqual([f.key for f in exam.identity_fields], ["nama"])
        self.assertEqual(records, [])


class NumericKeyDialogTest(unittest.TestCase):
    """Konsekuensi di dialog: tidak boleh Accepted dengan form tak berguna."""

    def _make_dialog(self, fields):
        with mock.patch(
            "examvan.ui.identity_dialog.QDialog.exec_", return_value=1
        ):
            return IdentityDialog(_exam(fields))

    def test_required_numeric_key_is_never_dropped_and_refuses_join(self):
        """Field `required` dengan key rusak TIDAK BOLEH dibuang diam-diam.

        Kalau dibuang, form jadi kosong tapi lolos → server 400 selamanya.
        Satu-satunya jalan adalah pengawas memperbaiki konfigurasi.
        """
        exam = Exam.from_json({
            "id": 7,
            "name": "Ujian Matematika",
            "status": "active",
            "identity_fields": [
                {"key": 123, "label": "Absen", "required": True},
            ],
        })
        dlg = self._make_dialog(exam.identity_fields)
        try:
            self.assertEqual(len(dlg._fields), 1)
            with mock.patch(
                "examvan.ui.identity_dialog.QMessageBox.warning"
            ) as warn, mock.patch.object(dlg, "accept") as accept:
                dlg._on_submit()
            accept.assert_not_called()
            self.assertTrue(warn.called)
            # Pesan harus menyebut MASALAH KONFIGURASI (bukan sekadar
            # "wajib diisi" — itu juga yang terjadi sebelum fix, dan justru
            # yang membuat diagnosis mustahil: 400-nya tak seorang pun
            # bisa perbaiki).
            title = " ".join(str(a) for a in (warn.call_args[0] or ()))
            self.assertIn("key", title.lower())
        finally:
            dlg.close()

    def test_optional_numeric_key_drops_when_an_alternative_exists(self):
        """Key rusak yang OPSIONAL boleh dibuang bila ada alternatif.

        Ronde 7 (M2): yang menentukan "tidak terbaca" BUKAN bentuk teks
        key, melainkan nilai JSON-nya bukan string — itu satu-satunya
        syarat yang-dihasilkan server (`validateIdentityFields` +
        `identityFieldValue`). `key_is_text=False` meniru `{"key": 123}`
        yang tiba dari `Exam.from_json`; string `"123.0"` yang diketik
        tangan adalah key SAH yang dibaca server persis seperti tersimpan,
        jadi tidak boleh di-drop lagi.
        """
        fields = [
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="123.0", label="Absen", required=False,
                          key_is_text=False),
        ]
        dlg = self._make_dialog(fields)
        try:
            keys = [f.key for f in dlg._fields]
            self.assertIn("nama", keys)
            self.assertNotIn("123.0", keys)
        finally:
            dlg.close()

    def test_optional_numeric_key_kept_when_it_is_the_only_one(self):
        """Tanpa alternatif, field tetap dipakai (dengan catatan di log).

        Membuangnya berarti form kosong Accepted; lebih baik satu input
        yang bisa diisi dan eindekskan daripada 400 selamanya.
        """
        fields = [
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="123.0", label="Absen", required=False,
                          key_is_text=False),
            IdentityField(key="kode_pos", label="Kode Pos", required=False),
        ]
        dlg = self._make_dialog(fields)
        try:
            keys = [f.key for f in dlg._fields]
            self.assertIn("kode_pos", keys)
        finally:
            dlg.close()

    def test_a_digit_only_string_key_is_never_dropped(self):
        """`2024_2025` adalah string SAH — hasil normalisasi label
        "2024/2025" di admin UI. Menolaknya pernah mengunci seluruh kelas
        yang memakai ujian dengan field wajib tersebut."""
        fields = [
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="2024_2025", label="2024/2025",
                          required=False),
        ]
        dlg = self._make_dialog(fields)
        try:
            self.assertIn("2024_2025", [f.key for f in dlg._fields])
        finally:
            dlg.close()


if __name__ == "__main__":  # pragma: no cover
    unittest.main()