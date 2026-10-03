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

import pathlib
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


class ThemeIsForcedDarkTest(unittest.TestCase):
    """Tema aplikasi dikunci gelap, bukan mengikuti Pengaturan Windows.

    Sebelumnya `apply_theme(dark=is_system_dark())`: di PC yang
    Appearance-nya light, seluruh aplikasi tampil terang. Ruang kelas
    sering punya PC dengan tema sistem berbeda-beda dalam satu
    ruangan, jadi temanya dipaksa gelap di `styles._APP_THEME_DARK`.

    Yang dijaga di sini bukan "nilainya True" saja -- itu trivially
    lulus begitu variabelnya dibalik. Yang dijaga adalah KONSISTENSI:
    tidak boleh ada tempat yang masih menanyakan tema sistem, karena
    itulah yang membuat kartu terang muncul di jendela gelap.
    """

    def test_app_theme_is_dark(self):
        self.assertTrue(styles.app_theme_dark())

    def test_app_theme_does_not_consult_the_system(self):
        # Kalau `app_theme_dark()` ikut membaca registry/gsettings,
        # keputusan "abaikan tema sistem" hanya berlaku di sebagian
        # tempat. Dipaksa lewat mock yang MELEDAK kalau dipanggil.
        with mock.patch.object(
            styles, "_is_system_dark_uncached",
            side_effect=AssertionError("tema sistem tidak boleh dikonsultasikan"),
        ):
            self.assertTrue(styles.app_theme_dark())

    # Empat modul ini dibaca dari FILE, bukan di-import: meng-import
    # `server_config`/`congratulations` menarik `examvan.ws` yang butuh
    # PyQt5.QtWebSockets, dan test stylesheet tidak boleh ikut gagal
    # hanya karena modul opsional tidak terpasang.
    _UI_DIR = pathlib.Path(__file__).resolve().parents[1] / "examvan" / "ui"

    def _ui_source(self, module: str) -> str:
        return (self._UI_DIR / f"{module}.py").read_text(encoding="utf-8")

    def test_no_ui_module_asks_the_system_theme_anymore(self):
        # Empat modul dulu memilih warna kartunya sendiri lewat
        # `is_system_dark()`. Modul yang terlewat = kartu terang di
        # jendela gelap, persis regresi butir 1.
        offenders = [
            module
            for module in ("identity_dialog", "server_config",
                           "waiting_approval", "congratulations")
            if "is_system_dark" in self._ui_source(module)
        ]
        self.assertEqual(
            offenders, [],
            f"modul ini masih menanyakan tema sistem: {offenders}",
        )

    def test_inline_cards_use_the_dark_palette(self):
        # Kartu-kartu itu punya warna sendiri (inline stylesheet), jadi
        # tidak ikut QSS global. Kalau warna gelapnya hilang, kartu
        # transparan di atas jendela gelap = teks hilang.
        for module, object_name in (
            ("identity_dialog", "identityCard"),
            ("server_config", "loginCard"),
            ("waiting_approval", "waitingCard"),
            ("congratulations", "congratsCard"),
        ):
            with self.subTest(module=module):
                self.assertIn(
                    f"QWidget#{object_name} {{ background-color: #313244;",
                    self._ui_source(module),
                    f"kartu {object_name} tidak lagi memakai warna gelap",
                )

    def test_light_palette_is_still_available_but_unused(self):
        # `_LIGHT_STYLESHEET` sengaja TIDAK dihapus: ia rujukan palet dan
        # masih diuji aturan-aturatnya. Yang dikunci di sini hanya bahwa
        # tidak ada jalur di aplikasi yang memakainya.
        self.assertIn("QDialog", styles._LIGHT_STYLESHEET)
        self.assertFalse(styles._LIGHT_STYLESHEET in [
            styles._DARK_STYLESHEET])


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
