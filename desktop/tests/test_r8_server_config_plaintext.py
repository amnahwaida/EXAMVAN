"""MEDIUM — dua teks server masih dirender sebagai rich text di `server_config`.

Bug
---
`ServerConfigDialog` punya satu `lbl_status` untuk semua status koneksi, dan
DUA di antaranya datang langsung dari server:

  * `health.status` dari `GET /api/health` — `_connect_thread`:
    `f"Gagal terhubung ke server:\n{health.status}"`;
  * `resp.message or resp.error` dari `GET /api/exams/token/<t>` — pesan
    yang ditulis server (atau proxy/WAF, atau halaman blok yang isinya
    HTML).

`QLabel` default `Qt.AutoText`, jadi markup di dalamnya DIEKSEKUSI. Yang
terukur di runner ini (lihat `test_r8_plaintext_labels.py` untuk angka
lengkap): payload `<span style='font-size:40pt'>` jadi 201x77 sebagai
AutoText dan 352x24 sebagai teks polos, dan `<img src="file:///etc/hostname"
width=300">` jadi 300x300 karena `QTextDocument` benar-benar MEMBUKA berkas
lokal secara sinkron di thread GUI.

Modul lain sudah diperbaiki: `congratulations` memiliki
`_sanitize_server_text()` dan `exam_viewer`/`identity_dialog`/
`waiting_approval`/`answer_sheet` meng-IMPORT-nya (test
`ServerTextIsSanitisedTestCase` mengunci bahwa mereka berbagi objek fungsi
yang sama). `server_config` belum masuk daftar itu, jadi dua jalur status di
atas masih AutoText dan belum disanitasi.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import Qt, QTimer
from PyQt5.QtWidgets import QApplication

from examvan.models import HealthResponse, TokenExamResponse
from examvan.ui import server_config as sc_mod

APP = QApplication.instance() or QApplication([])

HTML_PAYLOAD = "<span style='font-size:40pt'>PWNED</span>"
FILE_PAYLOAD = '<img src="file:///etc/hostname" width="300">'


def _pump(ms: int = 120) -> None:
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


class _DialogSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self.dlg = sc_mod.ServerConfigDialog()
        self.addCleanup(self.dlg.deleteLater)
        self.dlg.resize(1024, 600)
        self.dlg.show()
        _pump()

    def _connect_thread(self, health=None, token_response=None):
        """Jalankan `_connect_thread` sungguhan dengan `api` tiruan."""
        fake = mock.MagicMock()
        fake.check_health.return_value = health
        fake.get_exam_by_token.return_value = token_response
        with mock.patch.object(sc_mod, "api", fake), \
             mock.patch.object(sc_mod.config, "set"):
            self.dlg._connect_thread(
                "https://exam.example", "ABCD1234", True,
            )
        # Sinyal dari thread yang sama dipompa supaya slot UI benar-benar
        # jalan: yang diuji adalah apa yang TAMPIL, bukan apa yang di-emit.
        _pump()

    def _assert_does_not_grow(self, label) -> None:
        """Label polos harus lebih rendah daripada kembarannya AutoText.

        Lebar 600 dipakai karena `lbl_status` me-wrap: pada lebar 300 teks
        polos yang panjang ikut jadi dua baris, jadi tinggi keduanya sama
        dan perbandingan tidak membedakan apa pun. Angka yang terukur di
        runner ini pada lebar 600: polos 24px, rich text 77px untuk payload
        40pt dan 300x300 untuk `<img src="file://...">`.
        """
        width = 600
        twin = label.__class__(label.text())
        twin.setTextFormat(Qt.AutoText)
        twin.setWordWrap(label.wordWrap())
        twin.resize(width, max(label.height(), 24))
        label.resize(width, max(label.height(), 24))
        twin.show()
        _pump()
        try:
            self.assertLess(
                label.heightForWidth(width), twin.heightForWidth(width),
                "payload server membuat label setinggi render rich text — "
                "polanya masih AutoText",
            )
            self.assertLess(
                label.sizeHint().height(), 100,
                "label jadi tinggi seperti markup yang dieksekusi",
            )
        finally:
            twin.hide()


class HealthStatusIsPlainTextTestCase(_DialogSandbox):
    def test_the_health_status_label_is_plain_text(self):
        self._connect_thread(
            health=HealthResponse(
                success=False,
                status=f"Gagal terhubung ke server:\n{HTML_PAYLOAD}",
            ),
        )
        label = self.dlg.lbl_status
        self.assertIn(
            HTML_PAYLOAD, label.text(),
            "kontrol: payload harus tetap TERLIHAT sebagai teks literal",
        )
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "`health.status` dari server dirender sebagai rich text — "
            "satu string server bisa menimpa tampilan yang sedang dipakai "
            "siswa atau memaksa QTextDocument membuka berkas lokal",
        )

    def test_the_payload_does_not_grow_the_status_label(self):
        # Diuji lewat `_set_status_slot` dengan payload polos supaya
        # perbandingannya tidak tenggelam di teks status yang lain.
        self.dlg._set_status_slot(HTML_PAYLOAD, True)
        label = self.dlg.lbl_status
        self.assertIn(HTML_PAYLOAD, label.text())
        self._assert_does_not_grow(label)

    def test_a_file_url_in_the_status_is_never_fetched(self):
        self.dlg._set_status_slot(FILE_PAYLOAD, True)
        label = self.dlg.lbl_status
        self.assertEqual(label.textFormat(), Qt.PlainText)
        self._assert_does_not_grow(label)

    def test_control_characters_are_stripped_from_the_health_status(self):
        self._connect_thread(
            health=HealthResponse(
                success=False, status="Gagal\u202eDialihkan\u200bke liar",
            ),
        )
        rendered = self.dlg.lbl_status.text()
        self.assertNotIn("\u202e", rendered, "bidi override masih ada")
        self.assertNotIn("\u200b", rendered, "zero-width space masih ada")


class TokenResponseMessageIsPlainTextTestCase(_DialogSandbox):
    def test_the_token_response_message_label_is_plain_text(self):
        self._connect_thread(
            health=HealthResponse(success=True, status="ok"),
            token_response=TokenExamResponse(
                success=False, message=HTML_PAYLOAD,
            ),
        )
        label = self.dlg.lbl_status
        self.assertIn(HTML_PAYLOAD, label.text())
        self.assertEqual(
            label.textFormat(), Qt.PlainText,
            "pesan `resp.message` dari server dirender sebagai rich text",
        )

    def test_a_file_url_in_the_server_message_is_never_fetched(self):
        self._connect_thread(
            health=HealthResponse(success=True, status="ok"),
            token_response=TokenExamResponse(
                success=False, message=FILE_PAYLOAD,
            ),
        )
        label = self.dlg.lbl_status
        self.assertEqual(label.textFormat(), Qt.PlainText)
        self.assertLess(
            label.sizeHint().height(), 100,
            "QTextDocument memuat berkas lokal untuk `<img src=\"file://\">`",
        )

    def test_bidi_in_the_server_message_is_stripped(self):
        self._connect_thread(
            health=HealthResponse(success=True, status="ok"),
            token_response=TokenExamResponse(
                success=False, message="Andi\u202eBudi\u202e",
            ),
        )
        self.assertNotIn("\u202e", self.dlg.lbl_status.text())


class SanitizerIsSharedTestCase(unittest.TestCase):
    """SATU implementasi, di-`import` — bukan ditulis ulang di file ini."""

    def test_server_config_uses_the_congratulations_sanitizer(self):
        from examvan.ui import congratulations

        found = getattr(sc_mod, "_sanitize_server_text", None)
        self.assertTrue(
            callable(found),
            "server_config tidak punya akses ke sanitizer teks server yang "
            "sudah ada di `congratulations`",
        )
        self.assertIs(
            found, congratulations._sanitize_server_text,
            "server_config punya salinan sanitizer sendiri — dua "
            "implementasi pasti berbeda dalam beberapa bulan",
        )

    def test_the_module_source_does_not_redefine_the_sanitizer(self):
        with open(sc_mod.__file__, encoding="utf-8") as fh:
            text = fh.read()
        self.assertNotIn(
            "def _sanitize_server_text", text,
            "fungsi sanitizer ditulis ulang di server_config, bukan di-import",
        )


if __name__ == "__main__":
    unittest.main()