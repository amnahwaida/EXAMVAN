"""Nomor versi harus terlihat, bukan menempel di tepi jendela.

Satu-satunya cara memastikan build mana yang sedang terpasang adalah
membaca nomor versi di dialog konfigurasi. Kalau labelnya tertutup taskbar
atau terpotong di tepi layar, alat diagnosa itu jadi tidak berguna --
dan justru itu yang terjadi: labelnya ditambahkan ke layout LUAR
(`outer`) setelah `addStretch(2)`, jadi tertorong ke tepi bawah jendela.
Di jendela yang maximized, tepi bawah itu berimpit dengan taskbar.

Sekarang labelnya anak dari kartu, yang ikut isi kartu: selalu di dalam
layar, tidak pernah tertutup.

Yang diuji: posisi dan keterbacaan yang dilihat pengguna -- label benar
-benar ada, punya nomor yang benar, font tidak minuscule, dan tepi bawahnya
jauh dari tepi jendela. Assertion atas "ada di dalam kartu" saja akan
lulus untuk penempatan yang masih tertutup, jadi keduanya dipakai.
"""

from __future__ import annotations

import unittest

from PyQt5.QtCore import QPoint
from PyQt5.QtWidgets import QApplication

from examvan import APP_VERSION
from examvan.ui.server_config import ServerConfigDialog

APP = QApplication.instance() or QApplication([])

W, H = 1920, 1080
# Sisa ruang di bawah label harus lebih besar dari tinggi taskbar (~48px),
# supaya label tidak bisa ikut tertutup walau jendela dimaximize.
SAFE_MARGIN = 120


def _shown(width: int = W, height: int = H):
    dlg = ServerConfigDialog()
    dlg.resize(width, height)
    dlg.show()
    APP.processEvents()
    return dlg


class VersionLabelIsVisibleTest(unittest.TestCase):
    def _label(self, dlg):
        label = getattr(dlg, "_ver_label", None)
        self.assertIsNotNone(
            label,
            "ServerConfigDialog tidak punya _ver_label -- nomor versi "
            "tidak bisa dilihat, jadi tidak ada cara memastikan build yang "
            "terpasang",
        )
        return label

    def test_the_label_exists_and_shows_the_current_version(self):
        dlg = _shown()
        try:
            self.assertEqual(self._label(dlg).text(), f"v{APP_VERSION}")
        finally:
            dlg.close()

    def test_the_label_is_inside_the_card_not_the_window_edge(self):
        # Steiner: kalau label adalah anak dari QDialog langsung, ia ikut
        #)|\n# volont\tdipilin outer dan terdorong ke tepi. Yang benar: anak
        # dari kartu, jadi ikut contentsMargins kartu.
        dlg = _shown()
        try:
            label = self._label(dlg)
            self.assertIsNot(
                label.parentWidget(), dlg,
                "label versi harus anak dari kartu, bukan dari dialog",
            )
        finally:
            dlg.close()

    def test_the_label_is_actually_visible(self):
        # Label yang disembunyikan atau dikecilkan tetap memenuhi semua
        # pemeriksaan geometri: induknya benar, posisinya di dalam, fontnya
        # cukup besar. Yang membedakan hanya ini -- dan justru "tidak
        # kelihatan" itu bug yang dilaporkan.
        dlg = _shown()
        try:
            label = self._label(dlg)
            self.assertTrue(
                label.isVisible(),
                "label versi tidak terlihat; tidak ada cara memastikan "
                "build yang terpasang",
            )
            self.assertGreater(label.width(), 0)
            self.assertGreater(label.height(), 0)
        finally:
            dlg.close()

    def test_the_label_is_not_pinned_to_the_bottom_edge(self):
        # Inilah yang membuat label tertutup sebelumnya.
        dlg = _shown()
        try:
            label = self._label(dlg)
            bottom = label.mapTo(dlg, QPoint(0, 0)).y() + label.height()
            self.assertLess(
                bottom, dlg.height() - SAFE_MARGIN,
                f"label versi berakhir di y={bottom} dari jendela setinggi "
                f"{dlg.height()}; sisa {dlg.height() - bottom}px masih "
                f"kurang untuk bebas dari taskbar",
            )
        finally:
            dlg.close()

    def test_the_label_is_not_pinned_to_the_top_edge_either(self):
        # Versi di pojok atas juga tidak ideal: sulit dibaca sebagai
        # keterangan, dan bersebelahan dengan border kartu.
        dlg = _shown()
        try:
            label = self._label(dlg)
            top = label.mapTo(dlg, QPoint(0, 0)).y()
            self.assertGreater(top, 20)
        finally:
            dlg.close()

    def test_the_font_is_legible(self):
        # 11px memang terlalu kecil untuk dibaca dari kursi siswa; minimal
        # 12px supaya masih terbaca di layar lab yang jauh.
        dlg = _shown()
        try:
            label = self._label(dlg)
            self.assertGreaterEqual(
                label.font().pointSize() if label.font().pointSize() > 0
                else label.font().pixelSize(),
                12,
            )
        finally:
            dlg.close()

    def test_it_stays_visible_in_a_short_window(self):
        # Jendela pendek pernah jadi tempat label tergeser keluar.
        dlg = _shown(width=1024, height=640)
        try:
            label = self._label(dlg)
            bottom = label.mapTo(dlg, QPoint(0, 0)).y() + label.height()
            self.assertLessEqual(bottom, dlg.height())
        finally:
            dlg.close()

    def test_only_one_version_number_is_shown(self):
        # Dua angka yang saling menyangkal (mis. "v1.0.0 (API 2.5.0)") pernah
        # muncul; yang dibaca pengguna harus satu.
        dlg = _shown()
        try:
            text = self._label(dlg).text()
            self.assertNotIn("API", text)
            self.assertEqual(text.count("v"), 1)
        finally:
            dlg.close()


if __name__ == "__main__":
    unittest.main()
