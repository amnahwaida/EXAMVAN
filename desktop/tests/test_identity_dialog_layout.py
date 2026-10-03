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

from PyQt5.QtWidgets import QLabel

from examvan.models import Exam, IdentityField
from examvan.ui import identity_dialog
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

    def test_card_never_stretches_to_the_full_window_width(self):
        # Kartu boleh mengikuti jendela, tapi TIDAK boleh jadi selebar
        # jendela: isian yang melebar penuh tidak enak dibaca, dan di
        # layar 4K form 3840px hanya jadi satu baris di tengah.
        dlg = _shown(_exam(n_fields=12))
        try:
            card = dlg._scroll.widget()
            self.assertLess(card.width(), dlg._scroll.viewport().width())
            self.assertLessEqual(card.width(), identity_dialog._CARD_MAX_WIDTH)
        finally:
            dlg.close()

    def test_card_width_follows_the_window_and_stays_within_bounds(self):
        # Lebar lama dikunci 440px: di jendela 1920px itu kolom kurus
        # dengan 740px ruang kosong di tiap sisi. Sekarang proporsional,
        # dengan batas atas (jangan jadi spanduk) dan batas bawah (jangan
        # jadi kolom kurus di jendela kecil).
        wide = _shown(_exam(), width=1920, height=1000)
        try:
            wide_width = wide._scroll.widget().width()
        finally:
            wide.close()
        narrow = _shown(_exam(), width=900, height=1000)
        try:
            narrow_width = narrow._scroll.widget().width()
        finally:
            narrow.close()
        self.assertLess(
            narrow_width, wide_width,
            "lebar kartu tidak mengikuti jendela",
        )
        self.assertGreaterEqual(
            narrow_width, identity_dialog._CARD_MIN_WIDTH,
            "di jendela sempit kartu ikut menyusut tanpa batas bawah",
        )
        self.assertLessEqual(
            wide_width, identity_dialog._CARD_MAX_WIDTH,
            "di jendela lebar kartu ikut melebar tanpa batas atas",
        )

    def test_card_height_hugs_its_content(self):
        # Di 1080p kartu pernah setinggi 910px sementara isinya
        # 467px: 478px panel kosong di dalam kartu. Tinggi kartu kini
        # melekat pada isinya, di jendela berapa pun.
        for height in (700, 1000, 1400):
            dlg = _shown(_exam(), height=height)
            try:
                card = dlg._scroll.widget()
                self.assertLessEqual(
                    card.height(), card.sizeHint().height() + 2,
                    f"kartu setinggi {card.height()}px untuk isinya "
                    f"{card.sizeHint().height()}px di jendela {height}px: "
                    f"panelnya jadi kolom kosong",
                )
            finally:
                dlg.close()

    def test_required_asterisk_is_explained(self):
        # Label field memakai " *", jadi tanda itu harus dijelaskan di
        # layar -- kalau tidak, siswa baru tahu artinya setelah menekan
        # tombol.
        dlg = _shown(_exam())
        try:
            legends = [w.text() for w in dlg._scroll.widget().findChildren(QLabel)
                       if w.objectName() == "identityLegend"]
            self.assertEqual(len(legends), 1, legends)
            self.assertIn("*", legends[0])
            self.assertIn("wajib diisi", legends[0])
        finally:
            dlg.close()

    def test_submit_button_has_no_manual_space_padding(self):
        # Lebar tombol dulu dipalsukan dengan dua spasi di sekeliling
        # teks. Teksnya sendiri harus apa adanya.
        dlg = _shown(_exam())
        try:
            text = dlg._submit_btn.text()
            self.assertEqual(text, "Masuk Ujian")
            self.assertEqual(text, text.strip())
            self.assertGreater(dlg._submit_btn.minimumWidth(), 0)
        finally:
            dlg.close()


class LabelSlackTest(unittest.TestCase):
    """Slack tinggi viewport TIDAK boleh mendarat di label.

    Test di atas mengunci hal yang dilihat siswa: "form muat tanpa scroll".
    Yang tidak diUJI sana adalah jarak antar label dan kotak isinya --
    dan justru itulah yang rusak.

    Gejalanya: QScrollArea dengan `widgetResizable(True)` meregangkan
    kartu sampai setinggi viewport (di 1080p: 910px, sementara isinya
    432px). Tanpa stretch di `card_layout`, sisa 478px itu dibagikan
    QBoxLayout ke widget yang boleh melar, yaitu QLabel yang
    `setWordWrap(True)`: tiap label jadi 102px padahal teksnya 18px.
    Scrollbar tetap tidak muncul, jadi semua test "form muat" tetap
    hijau -- padahal jarak label ke input membesar jadi ~88px dan
    jarak antar field jadi ~164px.

    Yang dikunci di sini adalah sifat yang salahnya terlihat, bukan
    angka hasil tuning: tinggi label harus sama dengan tinggi yang
    dia benar-benar butuhkan pada lebar yang sedang dipakai
    (`heightForWidth`), dan harus TIDAK berubah ketika tinggi jendela
    berubah.
    """

    # Tinggi jendela dipisah jauh supaya test ini menangkap "slack
    # dibagi ke label": beda tinggi = beda slack = beda tinggi label
    # kalau bug-nya kembali.
    SHORT_H = 700
    TALL_H = 1400

    def _field_labels(self, dlg):
        """Label field (bukan judul, bukan pesan error inline)."""
        from PyQt5.QtWidgets import QLabel

        card = dlg._scroll.widget()
        out = []
        for lbl in card.findChildren(QLabel):
            if lbl.objectName() == "identityFieldError":
                continue
            if lbl.font().bold() and lbl.text().endswith(" *"):
                out.append(lbl)
        return out

    def _label_heights(self, dlg):
        return [lbl.height() for lbl in self._field_labels(dlg)]

    def test_label_height_matches_the_text_it_wraps(self):
        dlg = _shown(_exam(), height=self.TALL_H)
        try:
            for lbl in self._field_labels(dlg):
                needed = lbl.heightForWidth(lbl.width())
                self.assertLessEqual(
                    lbl.height(), needed,
                    f"label {lbl.text()!r} setinggi {lbl.height()}px "
                    f"padahal teksnya cuma perlu {needed}px; slack "
                    f"tinggi viewport bocor ke label",
                )
        finally:
            dlg.close()

    def test_label_height_does_not_depend_on_window_height(self):
        # Inkarnasi langsung dari bug-nya: jendela makin tinggi berarti
        # viewport makin tinggi. Yang boleh berubah adalah viewport --
        # tinggi KARTU dan tinggi label harus tetap, karena slack-nya
        # tidak boleh sampai ke isi form.
        short = _shown(_exam(), height=self.SHORT_H)
        try:
            short_viewport = short._scroll.viewport().height()
            short_heights = self._label_heights(short)
            short_card = short._scroll.widget().height()
        finally:
            short.close()
        tall = _shown(_exam(), height=self.TALL_H)
        try:
            tall_viewport = tall._scroll.viewport().height()
            tall_heights = self._label_heights(tall)
            tall_card = tall._scroll.widget().height()
        finally:
            tall.close()
        self.assertGreater(
            tall_viewport, short_viewport,
            "test ini tidak menguji apa-apa kalau viewportnya sama",
        )
        self.assertEqual(
            tall_heights, short_heights,
            "tinggi label ikut berubah mengikuti tinggi jendela: sisa "
            "ruang dibagikan ke label, bukan diserap di luar kartu",
        )
        self.assertEqual(
            tall_card, short_card,
            "kartu ikut menjulang bersama jendela, jadi jadi kolom kosong",
        )

    def test_each_label_text_sits_right_above_its_input(self):
        # Yang dilihat mata bukan jarak dari BAWAH label, melainkan jarak
        # dari teks label ke kotak isinya. Dan teksnya menempel di ATAS
        # kotak labelnya sendiri, jadi yang diukur harus jarak dari
        # tepi ATAS label ke tepi atas input.
        #
        # Penting: mengukur `input.y - (label.y + label.height)` TIDAK
        # akan menangkap apa pun. Label yang melar tetap berakhir 4px di
        # atas input -- ruang kosongnya ada di BAWAH teks, di dalam kotak
        # label itu sendiri. Bug-nya justru ada di sana.
        dlg = _shown(_exam(), height=1000)
        try:
            card = dlg._scroll.widget()
            labels = self._field_labels(dlg)
            for key, inp in dlg._inputs.items():
                match = None
                for lbl in labels:
                    wanted = inp.placeholderText().replace("Masukkan ", "")
                    if lbl.text().rstrip(" *").lower() == wanted.lower():
                        match = lbl
                        break
                self.assertIsNotNone(match, f"label untuk {key}")
                natural = match.heightForWidth(match.width())
                gap = (inp.mapTo(card, inp.rect().topLeft()).y()
                       - match.mapTo(card, match.rect().topLeft()).y())
                self.assertLessEqual(
                    gap, natural + 12,
                    f"teks label {match.text()!r} berdiri {gap}px di atas "
                    f"kotaknya padahal labelnya cuma perlu {natural}px; "
                    f"ruang kosong menggeser isi kotak label",
                )
        finally:
            dlg.close()

    def test_error_label_gives_its_space_back_when_hidden(self):
        # Label error disembunyikan sampai field benar-benar salah, dan
        # saat disembunyikan ia TIDAK boleh menyisakan ruang: dengan
        # tinggi/minimum-height yang tetap, form yang tadinya rapat
        # akan punya celah di bawah setiap kotak sebelum ada error pun.
        dlg = _shown(_exam(), height=1000)
        try:
            card = dlg._scroll.widget()

            def positions():
                return {k: w.mapTo(card, w.rect().topLeft()).y()
                        for k, w in dlg._inputs.items()}

            for key, err in dlg._error_labels.items():
                self.assertTrue(
                    err.isHidden(),
                    f"pesan error untuk {key} tampil sebelum ada error",
                )
            before = positions()

            # Munculkan satu pesan: layout boleh bergeser ke bawah.
            err = dlg._error_labels["student_name"]
            err.setText("Nama wajib diisi")
            err.show()
            card.layout().activate()
            APP.processEvents()
            during = positions()
            # Yang bertambah adalah jarak ke field BERIKUTNYA: pesan di
            # bawah kotak pertama mendorong semua yang ada di bawahnya.
            # (Kotaknya sendiri justru naik sedikit, karena stretch
            # pemutar ikut menyusut -- itu konsekuensi wajar, bukan bug.)
            gap_before = (before["exam_number"] - before["student_name"])
            gap_during = (during["exam_number"] - during["student_name"])
            self.assertGreater(
                gap_during, gap_before,
                "label error yang tampil tidak menambah ruang di bawahnya",
            )

            # Sembunyikan lagi: jarak antar field harus pulih persis.
            err.hide()
            card.layout().activate()
            APP.processEvents()
            self.assertEqual(
                positions(), before,
                "label error yang disembunyikan menyisakan ruang kosong "
                "di bawah kotaknya",
            )
        finally:
            dlg.close()


if __name__ == "__main__":
    unittest.main()
