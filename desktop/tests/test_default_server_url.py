"""Server URL default — tidak perlu mengetik ulang di tiap komputer lab.

Permintaan lapangan (30 September 2026): "beri default nilai pada server url
examvan.my.id supaya tidak ngetik-ngetik lagi".

Yang dikunci di sini:
  * `DEFAULT_SERVER_URL` adalah satu konstanta, bukan string yang disalin ke
    beberapa tempat ().
  * Dialog mengisi default itu KALAU tidak ada URL tersimpan, dan TIDAK
    menimpa URL yang sudah diketik siswa.
  * Default dikembalikan dalam bentuk kanonik (`https://`, tanpa trailing
    slash) supaya `_on_connect` tidak perlu menebak.
  * `remember_url` yang dicentang MENJADI berarti: pakai default lagi di
    komputer ini, bukan kembali ke kotak kosong. Dikosongkan = jangan simpan,
    jangan pre-fill.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.server_config import DEFAULT_SERVER_URL, ServerConfigDialog

APP = QApplication.instance() or QApplication([])


class _ConfigTestCase(unittest.TestCase):
    def setUp(self):
        import shutil
        import tempfile
        from pathlib import Path

        from examvan import config

        self._tmp = tempfile.mkdtemp(prefix="examvan-url-test-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp)),
            mock.patch.object(config, "_CONFIG_FILE", Path(self._tmp) / "config.json"),
        ]
        for p in self._patches:
            p.start()
        config._cache = None
        self._config = config

    def tearDown(self):
        import shutil

        for p in self._patches:
            p.stop()
        self._config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)


class DefaultUrlConstantTest(unittest.TestCase):
    def test_default_is_the_deployment_host(self):
        self.assertEqual(DEFAULT_SERVER_URL, "https://examvan.my.id")

    def test_default_is_canonical(self):
        # Kalau tidak kanonik, _on_connect harus menebak dan bisa salah.
        self.assertTrue(DEFAULT_SERVER_URL.startswith("https://"))
        self.assertFalse(DEFAULT_SERVER_URL.startswith("http://"))
        self.assertFalse(DEFAULT_SERVER_URL.endswith("/"))


class UrlPrefillTest(_ConfigTestCase):
    def test_blank_config_gets_the_default(self):
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_url.text(), DEFAULT_SERVER_URL)
        dlg.close()

    def test_saved_url_wins_over_the_default(self):
        # URL yang sudah disimpan/diketik siswa tidak boleh ditimpa —
        # overwrite akan membuat lab yang pakai server lain rusak.
        self._config.set("server_url", "https://sekolah.sch.id")
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_url.text(), "https://sekolah.sch.id")
        dlg.close()

    def test_unticking_remember_clears_the_prefill(self):
        # Kontrak yang benar untuk "Simpan URL & Token": dicentang = isi lagi
        # di komputer ini; tidak dicentang = jangan sentuh.
        self._config.set("server_url", "https://sekolah.sch.id")
        self._config.set("remember_url", False)
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_url.text(), "")
        dlg.close()

    def test_unticking_remember_does_not_prefill_the_default(self):
        # Default BUKAN jogan remembering: kalau tidak dicentang, form harus
        # benar-benar kosong supaya siswa melihat apa yang dia ketik.
        self._config.set("remember_url", False)
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_url.text(), "")
        dlg.close()

    def test_unticking_remember_also_stops_prefilling_the_token(self):
        self._config.set("exam_token", "ABCD1234")
        self._config.set("remember_url", False)
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_token.text(), "")
        dlg.close()

    def test_remembering_keeps_the_token_prefilled(self):
        self._config.set("exam_token", "ABCD1234")
        self._config.set("remember_url", True)
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_token.text(), "ABCD1234")
        dlg.close()

    def test_token_key_is_still_part_of_config(self):
        # Token TIDAK BOLEH dihapus dari disk: `_xor_obfuscate` memakainya
        # sebagai kunci decode jawaban yang tersimpan (config.py:32-39).
        # Kalau dihapus, recovery "Kirim Lagi" gagal decode. Yang dilakukan
        # remember=False adalah TIDAK pre-fill, bukan menghapus.
        self.assertIn("exam_token", self._config._defaults)


class BlankStoredUrlTest(_ConfigTestCase):
    def test_empty_stored_url_falls_back_to_the_default(self):
        # `server_url` bisa tersimpan sebagai string kosong (mis. config
        # ditulis versi lama, atau sengaja dikosongkan). Kosong TIDAK boleh
        # berarti form kosong — harus kembali ke default.
        self._config.set("server_url", "")
        dlg = ServerConfigDialog()
        self.assertEqual(dlg.input_url.text(), DEFAULT_SERVER_URL)
        dlg.close()


if __name__ == "__main__":
    unittest.main()
