"""H4 (TINGGI) — teks server masuk ke QLabel yang masih `Qt.AutoText`.

Bug
---
`congratulations.py` sudah benar: `_sanitize_server_text()` +
`setTextFormat(Qt.PlainText)` untuk `exam_name` dan `congrats_message`.
EVERY label lain yang memuat teks dari server terlewat:

  * `exam_viewer._lbl_title`           (exam.name)
  * `identity_dialog` label ujian + label field + label error inline
                                  (exam.name / IdentityField.label)
  * `answer_sheet` label item kiri    (left_items soal menjodohkan)
  * `waiting_approval` subtitle + judul/pesan status
                                  (exam.name / pesan respons server)

Default `QLabel.textFormat()` adalah `Qt.AutoText` (2), jadi teks yang
melewati markup apa adanya. Diukur di runner ini:

    payload                                textFormat   sizeHint
    <span style='font-size:40pt'>PWNED</span>  AutoText    201x77
    <img src="file:///etc/hostname" w=300>  AutoText    300x300
    teks polos biasa                      AutoText    141x24

Dua konsekuensi, bukan satu:

  1. `QTextDocument` milik label MEMBUKA berkas lokal secara sinkron di
     thread GUI (`<img src="file://...">`) — pengungkapan berkas lokal
     plus primitif beku-GUI kalau yang diambil adalah FIFO/device;
  2. satu string server bisa membuat label 300x300 atau 5 baris, yaitu
     menimpa tampilan yang sedang dipakai siswa.

`models.Exam.panel_color` sudah divalidasi regex, jadi pola "semua string
dari server harus dipercaya?" ini memang diketahui — cuma tidak diterapkan
di mana-mana.

Test di bawah: (a) setiap label yang bisa membawa teks server harus
`Qt.PlainText`; (b) payload HTML/`file://` harus tampil sebagai TEKS
literal dan tidak memperbesar label. Event loop-nya sungguhan
(`app.exec_()` dengan stop `QTimer.singleShot`), karena pemuatan `file://`
justru terjadi saat label dirender.
"""

from __future__ import annotations

import importlib
import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import Qt, QTimer, QObject, pyqtSignal
from PyQt5.QtWidgets import QApplication, QLabel

from examvan.models import Exam, IdentityField, SubmitResponse

APP = QApplication.instance() or QApplication([])

# Dua payload yang diukur auditor: satujus kegedean tipografi, satu yang
# memaksa `QTextDocument` membuka berkas lokal.
HTML_PAYLOAD = "<span style='font-size:40pt'>PWNED</span>"
FILE_PAYLOAD = '<img src="file:///etc/hostname" width="300">'

# Tinggi satu baris label polos di runner ini (dihukur, bukan ditebak).
_PLAIN_LINE_HEIGHT = 24


def _pump(ms: int = 120) -> None:
    """Jalankan event loop sungguhan selama `ms` dengan stop keras."""
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


class _NoSecurity(QObject):
    """Enforcer tiruan — label yang diuji tidak butuh platform."""

    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        pass

    def protect_window(self, window) -> bool:
        return True


def _find_label_containing(widget, needle: str) -> QLabel:
    for label in widget.findChildren(QLabel):
        if needle in label.text():
            return label
    raise AssertionError(f"tidak ada QLabel yang memuat {needle!r}")


class PlainTextRenderingTestCase(unittest.TestCase):
    def _assert_does_not_grow(self, label: QLabel) -> None:
        """Label tidak boleh lebih tinggi daripada kembarannya yang AutoText.

        Perbandingannya apples-to-apples: label yang sama persis dirender dua
        kali — sekali dengan format yang sekarang dipakai, sekali dengan
        `Qt.AutoText` — pada lebar yang sama. Dengan AutoText, payload
        `<span style='font-size:40pt'>` jadi 77px dan `<img src="file://">`
        jadi kotak 300x300; sebagai teks polos keduanya satu baris (atau dua
        kalau label-nya `wordWrap`).
        """
        label.show()
        _pump()
        width = max(label.width(), 300)
        twin = QLabel(label.text())
        twin.setTextFormat(Qt.AutoText)
        twin.setWordWrap(label.wordWrap())
        twin.resize(width, max(label.height(), 24))
        twin.show()
        _pump()
        mine = label.heightForWidth(width)
        rendered_as_markup = twin.heightForWidth(width)
        self.assertLess(
            mine, rendered_as_markup,
            f"payload server membuat label {width}x{mine} — sama dengan "
            f"kembarannya yang dirender sebagai rich text ({rendered_as_markup})",
        )
        label.hide()
        twin.hide()


class ExamTitleLabelTestCase(PlainTextRenderingTestCase):
    """`exam_viewer._lbl_title` — `exam.name` dari server."""

    def _viewer(self, name):
        module = importlib.import_module("examvan.ui.exam_viewer")

        with mock.patch.object(module, "SecurityEnforcer", _NoSecurity), \
             mock.patch.object(module, "ExamWebSocket"), \
             mock.patch.object(module.api, "download_pdf",
                               side_effect=OSError("offline")), \
             mock.patch.object(module.api, "send_access_log",
                               return_value=False):
            exam = Exam.from_json({
                "id": 900, "name": name, "status": "active",
                "security_level": "low",
                "questions": [{"number": 1, "type": "single_choice",
                               "choices": ["A", "B"]}],
            })
            viewer = module.ExamViewerWindow(
                exam=exam, server_url="https://exam.example", token="T",
                identity_data={"nama": "A", "nomor_ujian": "N1", "kelas": "9A"},
            )
        self.addCleanup(viewer.hide)
        self.addCleanup(viewer.deleteLater)
        return viewer

    def test_the_exam_title_label_is_plain_text(self):
        viewer = self._viewer(HTML_PAYLOAD)
        label = viewer._lbl_title
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "label judul ujian memuat `exam.name` dari server dan masih "
            "AutoText — server bisa menyuntik rich text ke layar siswa",
        )
        self.assertIn(HTML_PAYLOAD, label.text(),
                      "payload harus tetap TERLIHAT sebagai teks literal, "
                      "bukan hilang begitu saja")
        self._assert_does_not_grow(label)

    def test_a_file_url_in_the_exam_name_is_never_fetched(self):
        viewer = self._viewer(FILE_PAYLOAD)
        label = viewer._lbl_title
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "`<img src=\"file://...\">` di `exam.name` membuat QTextDocument "
            "membaca berkas lokal sinkron di thread GUI",
        )
        self._assert_does_not_grow(label)
        self.assertLess(
            label.sizeHint().height(), 100,
            "label jadi tinggi seperti gambar 300x300 — berkas lokal benar-"
            "benar dimuat oleh QTextDocument",
        )


class IdentityDialogLabelsTestCase(PlainTextRenderingTestCase):
    """`identity_dialog` — `exam.name`, `IdentityField.label`, pesan inline."""

    def _dialog(self, exam_name, field_label):
        from examvan.ui.identity_dialog import IdentityDialog

        exam = Exam.from_json({
            "id": 901, "name": exam_name, "status": "active",
            "security_level": "low",
            "identity_fields": [
                {"key": "nama", "label": field_label, "required": True},
                {"key": "nomor_ujian", "label": "Nomor Ujian",
                 "required": True},
            ],
        })
        dlg = IdentityDialog(exam=exam)
        self.addCleanup(dlg.deleteLater)
        dlg.resize(1024, 600)
        dlg.show()
        _pump()
        return dlg

    def test_exam_name_label_is_plain_text(self):
        dlg = self._dialog(HTML_PAYLOAD, "Nama")
        label = _find_label_containing(dlg, HTML_PAYLOAD)
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "label nama ujian memuat `exam.name` dari server dan masih "
            "AutoText",
        )
        self._assert_does_not_grow(label)

    def test_identity_field_label_is_plain_text(self):
        dlg = self._dialog("Ujian", HTML_PAYLOAD)
        label = _find_label_containing(dlg, HTML_PAYLOAD)
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "label field memuat `IdentityField.label` dari server dan masih "
            "AutoText — guru (atau siapa pun yang menulis konfigurasi) bisa "
            "menyuntik markup ke form identitas",
        )
        self._assert_does_not_grow(label)

    def test_inline_required_error_is_plain_text(self):
        dlg = self._dialog("Ujian", HTML_PAYLOAD)
        with mock.patch("examvan.ui.identity_dialog.QMessageBox"):
            dlg._on_submit()
        err = [
            label for label in dlg.findChildren(QLabel)
            if label.objectName() == "identityFieldError"
            and "wajib diisi" in label.text()
        ]
        self.assertTrue(
            err,
            "pesan error inline tidak muncul — test tidak menguji apa pun",
        )
        err = err[0]
        self.assertEqual(
            err.textFormat(), Qt.PlainText,
            "pesan error inline memuat `IdentityField.label` dari server; "
            "dengan AutoText markup di situ ikut dirender",
        )


class AnswerSheetMatchingLabelTestCase(PlainTextRenderingTestCase):
    """`answer_sheet` — `left_items` soal menjodohkan."""

    def _sheet(self, left):
        from examvan.ui.answer_sheet import AnswerSheetWidget

        sheet = AnswerSheetWidget()
        self.addCleanup(sheet.deleteLater)
        sheet.build_from_questions([{
            "number": 1, "type": "matching",
            "left_items": [left], "right_items": ["A", "B"],
        }])
        sheet.resize(400, 400)
        sheet.show()
        _pump()
        return sheet

    def test_matching_left_item_label_is_plain_text(self):
        sheet = self._sheet(HTML_PAYLOAD)
        label = _find_label_containing(sheet, HTML_PAYLOAD)
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "label item kiri memuat `left_items` dari server dan masih "
            "AutoText",
        )
        self._assert_does_not_grow(label)

    def test_a_file_url_left_item_is_never_fetched(self):
        sheet = self._sheet(FILE_PAYLOAD)
        label = _find_label_containing(sheet, FILE_PAYLOAD)
        self.assertEqual(label.textFormat(), Qt.PlainText)
        self.assertLess(
            label.sizeHint().height(), 100,
            "QTextDocument memuat berkas lokal untuk `<img src=\"file://\">`",
        )


class WaitingApprovalLabelsTestCase(PlainTextRenderingTestCase):
    """`waiting_approval` — `exam.name`, judul dan pesan dari server."""

    def _dialog(self, exam_name):
        from examvan.ui.waiting_approval import WaitingApprovalDialog

        pending = SubmitResponse(
            success=True, status="pending", message="",
            http_status=200,
        )
        with mock.patch(
            "examvan.ui.waiting_approval.api"
        ) as fake_api:
            fake_api.request_approval.return_value = pending
            exam = Exam.from_json({
                "id": 902, "name": exam_name, "status": "active",
                "security_level": "low",
            })
            dlg = WaitingApprovalDialog(
                exam, "https://exam.example",
                {"nama": "A", "nomor_ujian": "N1", "kelas": "9A"},
                token="T",
            )
        self.addCleanup(dlg._stop_polling)
        self.addCleanup(dlg.deleteLater)
        dlg.show()
        _pump()
        return dlg

    def test_subtitle_with_exam_name_is_plain_text(self):
        dlg = self._dialog(HTML_PAYLOAD)
        label = dlg.subtitle_label
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "subtitle memuat `exam.name` dari server dan masih AutoText",
        )
        self._assert_does_not_grow(label)

    def test_subtitle_after_retry_is_plain_text(self):
        # Jalur kedua yang menulis `exam.name` ke label yang sama
        # (`_retry_approval`) — harus punya perlakuan yang sama.
        dlg = self._dialog("Ujian")
        with mock.patch.object(type(dlg), "_start_polling"):
            dlg._retry_approval()
        label = dlg.subtitle_label
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "`_retry_approval` menulis ulang `exam.name` ke label yang sama",
        )

    def test_status_message_from_the_server_is_plain_text(self):
        # `resp.message` dari `request_approval` (server/proxy bisa menulis
        # apa saja, termasuk HTML dari halaman blok).
        dlg = self._dialog("Ujian")
        dlg._on_status_update("error", "Koneksi Terganggu", HTML_PAYLOAD)
        label = dlg.subtitle_label
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "pesan status dari server dirender di label AutoText",
        )
        self._assert_does_not_grow(label)
        self.assertEqual(
            dlg.title_label.textFormat(), Qt.PlainText,
            "judul status juga teks server",
        )


class ServerTextIsSanitisedTestCase(unittest.TestCase):
    """Teks yang dirender harus melewati sanitizer yang sama, bukan hanya PlainText.

    `setTextFormat(Qt.PlainText)` menghentikan markup, tapi karakter
    tersembunyi (Cf) dan bidi override masih bisa menyamarkan isi label
    di mata siswa — persis yang dicegah `stripControlAndBidi` di server.
    """

    def test_bidi_override_is_stripped_before_rendering(self):
        module = importlib.import_module("examvan.ui.exam_viewer")
        sanitize = getattr(module, "_sanitize_server_text", None)
        self.assertTrue(
            callable(sanitize),
            "exam_viewer tidak memakai sanitizer teks server yang sudah ada "
            "(pola `congratulations`); dua implementasi akan pasti berbeda",
        )
        self.assertEqual(sanitize("Andi\u202eBudi"), "AndiBudi")
        self.assertEqual(sanitize("A\u200bB"), "AB")

    def test_the_sanitizer_is_the_one_congratulations_uses(self):
        # SATU salinan, bukan dua. `congratulations` adalah pemiliknya
        # (`_sanitize_server_text`), dan file itu tidak boleh diedit ronde
        # ini — jadi yang dipakai di sini harus IMPORT, bukan menulis ulang.
        from examvan.ui import congratulations

        for module_name in ("exam_viewer", "identity_dialog",
                            "waiting_approval", "answer_sheet"):
            module = importlib.import_module(f"examvan.ui.{module_name}")
            found = getattr(module, "_sanitize_server_text", None)
            self.assertIs(
                found, congratulations._sanitize_server_text,
                f"{module_name} punya salinan sanitizer sendiri — "
                "diverifikasi bahwa hanya ada satu implementasi",
            )


if __name__ == "__main__":
    unittest.main()