"""Form identitas harus muat di layar yang memang cukup.

Keluhan yang memicu file ini: setelah memasukkan token, form identitas
"tidak tertampil full — harus di-scroll dulu, padahal layar yang ada sangat
cukup untuk menampilkan semuanya sekaligus".

Penyebabnya bukan QScrollArea, melainkan faktor stretch di layout luar:

    outer.addStretch(2)               # atas
    outer.addWidget(self._scroll, 1)  # kartu
    outer.addStretch(2)               # bawah

Extra space dibagi proporsional terhadap faktor stretch, jadi scroll area
hanya mendapat 1 dari 5 bagian. Di layar 1080p viewport-nya ~215 px
sementara kartunya ~404 px — jadi harus di-scroll, dengan ~2/3 layar
kosong. Faktor 1/1/1 masih salah (viewport ~360 px, tetap <
kartu), karena tiga faktor sama besar.

Yang diuji di sini perilaku yang dilihat pengguna, bukan angka layout:
apakah scrollbar vertikal muncul saat isinya sebenarnya muat. Assertion
atas faktor stretch saja akan lulus untuk kombinasi angka yang tetap salah,
jadi keduanya dipakai — yang kedua sebagai penjaga.

Yang TIDAK boleh hilang: form yang benar-benar panjang harus tetap bisa
di-scroll. Itu gunanya QScrollArea di sini; tanpa itu tombol "Masuk Ujian"
tidak terjangkau dan siswa terkunci sebelum ujian dimulai.
"""

from __future__ import annotations

import unittest

from PyQt5.QtWidgets import QApplication

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog

APP = QApplication.instance() or QApplication([])

# Layar 1080p: work area sedikit di bawah 1080 karena taskbar.
SCREEN_H = 1080
SCREEN_W = 1920


def _exam(n_fields: int = 0) -> Exam:
    fields = None
    if n_fields:
        fields = [
            IdentityField(
                key=f"custom_{i}",
                label=f"Kolom Tambahan {i}",
                required=False,
            )
            for i in range(1, n_fields + 1)
        ]
    return Exam(id=1, name="Ujian Matematika", status="active",
                identity_fields=fields)


def _shown(exam: Exam, width: int = SCREEN_W, height: int = SCREEN_H):
    dlg = IdentityDialog(exam, saved_data={})
    dlg.resize(width, height)
    dlg.show()
    APP.processEvents()
    return dlg


class IdentityDialogFitsTest(unittest.TestCase):
    def tearDown(self):
        pass

    def test_default_form_needs_no_scrolling_on_a_1080p_screen(self):
        # Keluhan aslinya. Tiga field default harus terlihat sekaligus.
        dlg = _shown(_exam())
        try:
            bar = dlg._scroll.verticalScrollBar()
            self.assertFalse(
                bar.isVisible(),
                f"form default harus muat tanpa scroll, tapi scrollbar "
                f"muncul: viewport {dlg._scroll.viewport().height()}px "
                f"vs kartu {dlg._scroll.widget().sizeHint().height()}px",
            )
        finally:
            dlg.close()

    def test_scroll_area_gets_most_of_the_height(self):
        # Penjaga langsung untuk faktor stretch: kalau kartu hanya
        # menerima sebagian kecil dari tinggi dialog, form pendek pun
        # akan ter-scroll. Ini yang membuat 1/5 (dan bahkan 1/3) salah.
        dlg = _shown(_exam())
        try:
            dialog_h = dlg.height()
            viewport_h = dlg._scroll.viewport().height()
            self.assertGreater(
                viewport_h, dialog_h * 0.8,
                f"scroll area hanya {viewport_h}/{dialog_h} px dari dialog; "
                f"sisanya ruang kosong yang membuat form perlu di-scroll",
            )
        finally:
            dlg.close()

    def test_a_long_form_still_scrolls(self):
        # QScrollArea itu memang ada karena form bisa panjang. Kalau
        # "perbaikan" ini ikut menghapus ruang scroll, tombol Masuk Ujian
        # jadi tidak terjangkau dan siswa terkunci sebelum ujian dimulai.
        dlg = _shown(_exam(n_fields=12))
        try:
            self.assertTrue(
                dlg._scroll.verticalScrollBar().isVisible(),
                "form 12 kolom di 1080p harus tetap bisa di-scroll",
            )
        finally:
            dlg.close()

    def test_a_longer_screen_needs_no_scrolling_either(self):
        # Di 1440p, form yang di 1080p masih perlu scroll harus muat --
        # itulah arti "layarnya cukup".
        dlg = _shown(_exam(n_fields=6), width=2560, height=1440)
        try:
            self.assertFalse(dlg._scroll.verticalScrollBar().isVisible())
        finally:
            dlg.close()

    def test_submit_button_is_reachable_in_a_short_window(self):
        # Kasus asal mula-mula QScrollArea ditambahkan: windows kecil.
        dlg = _shown(_exam(), width=1024, height=500)
        try:
            from PyQt5.QtCore import QPoint

            corner = dlg._submit_btn.mapTo(dlg, QPoint(0, 0))
            btn_bottom = corner.y() + dlg._submit_btn.height()
            self.assertLessEqual(
                btn_bottom, dlg.height(),
                "tombol Masuk Ujian berada di luar jendela",
            )
        finally:
            dlg.close()

    def test_card_width_stays_fixed_while_scrolling(self):
        # Lebar kartu tetap 440px; yang berubah hanya tinggi. Kalau ini
        # ikut berubah, form jadi lebar penuh dan tidak terbaca.
        dlg = _shown(_exam(n_fields=12))
        try:
            self.assertEqual(dlg._scroll.widget().width(), 440)
        finally:
            dlg.close()


if __name__ == "__main__":
    unittest.main()
