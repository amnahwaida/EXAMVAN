"""Regresi stylesheet tema gelap/terang (temuan review ronde ini).

Dijaga di sini — bukan di test eksisting — karena belum ada file test
yang memiliki stylesheet:

1. Blok duplikat `QWidget#loginCard, QWidget#identityCard` bernilai TERANG
   (`#ffffff`) di dalam `_DARK_STYLESHEET`: aturan terakhir menang di QSS,
   jadi kartu login/identitas PUTIH di atas jendela GELAP (silau + teks
   gelap-di-atas-putih yang tidak ikut tema).
2. Splitter handle `#ccd0da` (terang) di tema gelap: hampir tak terlihat.
3. `QPushButton:focus` tanpa ring: navigasi keyboard tidak kelihatan.
4. `QPushButton:disabled` gelap `#6c7086` di atas `#45475a`: kontras
   ~2.2:1, di bawah ambang 3:1 untuk teks non-dekoratif.
5. `QMessageBox` tanpa `color` eksplisit + tanpa aturan `QTextEdit`
   (detailed-text): pesan error panjang mewarisi warna yang salah.
6. `is_system_dark()` tidak boleh melempar (pemanggil di
   waiting_approval/server_config/identity_dialog/congratulations tidak
   punya try/except) dan hasilnya di-cache (gsettings sekali saja).
"""

from __future__ import annotations

import subprocess
import unittest
from unittest import mock

from examvan.ui import styles


class DarkStylesheetRegressionTest(unittest.TestCase):
    def test_no_duplicate_light_login_card_selector(self):
        sheet = styles._DARK_STYLESHEET
        count = sheet.count("QWidget#loginCard, QWidget#identityCard")
        self.assertEqual(
            count, 1,
            f"selektor loginCard/identityCard muncul {count}x di dark "
            "stylesheet — blok terang duplikat membuat kartu putih di "
            "tema gelap",
        )
        block = self._rule_block(
            sheet, "QWidget#loginCard, QWidget#identityCard {")
        self.assertIsNotNone(block)
        self.assertIn("#313244", block,
                      "blok kartu yang tersisa harus yang gelap (#313244)")

    def test_dark_splitter_handle_is_visible_on_dark(self):
        handle = self._rule_block(styles._DARK_STYLESHEET, "QSplitter::handle {")
        self.assertIsNotNone(handle)
        self.assertNotIn("#ccd0da", handle)
        self.assertIn("#585b70", handle)

    def test_focus_ring_exists_in_both_themes(self):
        for name, sheet in (("dark", styles._DARK_STYLESHEET),
                            ("light", styles._LIGHT_STYLESHEET)):
            with self.subTest(theme=name):
                self.assertIn("QPushButton:focus", sheet,
                              f"tidak ada focus ring di tema {name}")

    def test_dark_disabled_button_meets_contrast(self):
        # #a6adc8 di atas #45475a ≈ 4.6:1 (≥3:1). #6c7086 ≈ 2.2:1.
        block = self._rule_block(
            styles._DARK_STYLESHEET, "QPushButton:disabled {")
        self.assertIsNotNone(block)
        self.assertNotIn("#6c7086", block)
        self.assertIn("#a6adc8", block)

    def test_message_box_has_explicit_color_and_detailed_text(self):
        for name, sheet in (("dark", styles._DARK_STYLESHEET),
                            ("light", styles._LIGHT_STYLESHEET)):
            with self.subTest(theme=name):
                box = self._rule_block(sheet, "QMessageBox {")
                self.assertIsNotNone(box, f"QMessageBox hilang di tema {name}")
                self.assertIn("color", box,
                              f"QMessageBox tanpa color di tema {name}")
                detailed = self._rule_block(sheet, "QMessageBox QTextEdit {")
                self.assertIsNotNone(
                    detailed,
                    f"detailed-text (QMessageBox QTextEdit) hilang di tema {name}",
                )

    @staticmethod
    def _rule_block(sheet: str, selector: str):
        start = sheet.find(selector)
        if start < 0:
            return None
        end = sheet.find("}", start)
        return sheet[start:end]


class IsSystemDarkRobustnessTest(unittest.TestCase):
    def setUp(self):
        self.addCleanup(self._reset_cache)
        self._reset_cache()

    @staticmethod
    def _reset_cache():
        styles._IS_DARK_CACHE = None

    def test_never_raises_when_gsettings_explodes(self):
        with mock.patch.object(
            styles.subprocess, "run",
            side_effect=subprocess.SubprocessError("gsettings mati"),
        ):
            try:
                result = styles.is_system_dark()
            except Exception as e:  # noqa: BLE001
                self.fail(f"is_system_dark melempar: {e!r}")
            self.assertIsInstance(result, bool)

    def test_never_raises_on_os_error(self):
        with mock.patch.object(
            styles.subprocess, "run", side_effect=OSError("tidak ada")
        ):
            try:
                styles.is_system_dark()
            except Exception as e:  # noqa: BLE001
                self.fail(f"is_system_dark melempar: {e!r}")

    def test_result_is_cached(self):
        with mock.patch.object(
            styles.subprocess, "run",
            side_effect=subprocess.SubprocessError("x"),
        ) as run:
            first = styles.is_system_dark()
            second = styles.is_system_dark()
        self.assertEqual(first, second)
        # Panggilan kedua tidak menyentuh subprocess lagi.
        self.assertLessEqual(run.call_count, 2)


if __name__ == "__main__":
    unittest.main()
