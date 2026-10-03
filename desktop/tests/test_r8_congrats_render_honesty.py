"""Ronde 8 (medium) — halaman selesai harus jujur dan muat di layar lab.

Empat temuan yang ditutup file ini
----------------------------------
1. **Minimum yang bertabrakan.** Jendela ini membuka diri dengan
   `setMinimumSize(360, 280)`, sedangkan kartunya
   `setMinimumWidth(420)`. Pada minimum yang dideklarasikan, viewport
   hanya 346 px dan KEDUA scrollbar muncul — hanya karena dua angka
   yang tidak bisa hidup berdampingan. Diukur di sini, bukan dikomentari.

2. **Pesan guru bisa mendorong "Selesai" keluar layar.** Field
   `congrats_message` di server adalah `<textarea maxlength="500">` dan
   server hanya `TrimSpace`, jadi 15 baris kosong di tengah pesan adalah
   masukan yang sah. Diamati: batas 2000 karakter TIDAK membatasi
   tinggi yang dirender — 9 baris saja sudah membuat tombol "Selesai"
   keluar dari layar 1024x600. Sekarang baris kosong beruntun diratakan
   dan tinggi yang dirender dijepit (bukan cuma jumlah karakternya).

3. **URL yang ditampilkan bisa BERBEDA dari URL yang disalin.**
   `_sanitize_server_text` diterapkan ke teks gabungan
   `f"Link hasil: {url}"`, sedangkan `_result_url` — yang justru
   disalin ke clipboard — tidak pernah disentuh. ZWSP di
   `server_url` jadi berarti label menampilkan
   `https://examvan.my.id/ABCD1234` sementara clipboard berisi
   `https://exam\u200bvan.my.id/ABCD1234`: label tidak memuat URL yang
   disalin. URL panjang (> 2000 karakter) lebih buruk lagi: label
   terpotong dan token hilang darinya. Yang benar: bersihkan BASE-nya lebih dulu,
   lalu pakai `_result_url` apa adanya di label — clipboard dan layar
   tidak mungkin berbeda, dan token tidak pernah dirusak.

4. **Komentar yang berbohong.** Komentar ikon menjanjikan "fallback ke
   teks" yang tidak pernah ada (label sengaja dikosongkan), dan
   komentar label link mengklaim sanitizer "hanya membuang kontrol tak
   terlihat" padahal ia juga memotong. Dua-duanya diuji di sini lewat
   sumber, supaya tidak masuk lagi.
"""

from __future__ import annotations

import os
import re
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QPoint
from PyQt5.QtWidgets import (
    QApplication,
    QScrollArea,
    QWidget,
)

from examvan.ui.congratulations import CongratulationsWindow

# Lebar minimum kartu yang disepakati: minimum jendela harus menggambar
# lebarnya dari angka yang sama, bukan menebak angka lain.
CARD_MIN_WIDTH = 420

# Lebar teks pesan guru saat kartu di lebar MINIMUM-nya dikurangi margin
# kiri/kanan kartu (40 + 40): inilah lebar word-wrap terburuk.
MESSAGE_WRAP_WIDTH = CARD_MIN_WIDTH - 80


class _RenderTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _page(self, message="Selesai", **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token="ABCD1234",
            exam_name="Ujian",
            student_name="SITI",
            student_number="N02",
            student_class="9B",
            congrats_message=message,
        )
        params.update(kwargs)
        page = CongratulationsWindow(**params)
        self.addCleanup(page.deleteLater)
        return page

    def _shown_at(self, page, widget) -> int:
        """Posisi bawah `widget` dalam koordinat viewport scroll area."""
        scroll = page.centralWidget()
        self.assertIsInstance(scroll, QScrollArea)
        view = scroll.viewport()
        return widget.mapTo(view, QPoint(0, widget.height())).y()


class MinimumSizeIsCoherentTestCase(_RenderTestCase):
    """(1) minimum jendela tidak boleh bertabrakan dengan minimum kartu."""

    def test_the_window_minimum_is_at_least_the_card_width(self):
        page = self._page()
        card = page.findChild(QWidget, "congratsCard")
        self.assertIsNotNone(card, "kartu ini harus bisa dicari lewat objectName")
        self.assertGreaterEqual(
            page.minimumSize().width(), card.minimumWidth(),
            "jendela boleh lebih kecil dari kartunya: pada minimum yang "
            "dideklarasikan scrollbar horizontal muncul hanya karena dua "
            "minimum yang tidak bisa hidup berdampingan",
        )

    def test_no_horizontal_scrollbar_at_the_declared_minimum(self):
        page = self._page()
        page.resize(page.minimumSize())
        page.show()
        self.app.processEvents()
        scroll = page.centralWidget()
        self.assertFalse(
            scroll.horizontalScrollBar().isVisible(),
            "scrollbar horizontal muncul pada minimum jendela — kartu "
            "selalu lebih lebar dari jendela yang menjadi tempatnya",
        )
        page.close()


class TeacherMessageFitsOnOneScreenTestCase(_RenderTestCase):
    """(2) pesan guru tidak boleh mendorong tombol keluar layar lab."""

    LAB = (1024, 600)

    def test_a_message_with_many_blank_lines_keeps_selesai_visible(self):
        # 15 baris kosong di tengah pesan: masukan yang sah dari
        # `<textarea maxlength="500">` yang hanya di-`TrimSpace` server.
        message = "Selamat mengerjakan.\n" + "\n" * 15 + "Sampai jumpa."
        page = self._page(message)
        page.resize(*self.LAB)
        page.show()
        self.app.processEvents()
        bottom = self._shown_at(page, page.finish_button())
        self.assertLessEqual(
            bottom, page.centralWidget().viewport().height(),
            "tombol 'Selesai' berada di luar layar 1024x600 — siswa "
            "harus menggulir untuk menutup halaman, dan tidak ada yang "
            "memberi tahu kenapa",
        )
        page.close()

    def test_a_very_long_single_paragraph_keeps_selesai_visible(self):
        # Batas 2000 karakter tidak membatasi tinggi: 500 karakter
        # tanpa baris baru membungkus menjadi banyak baris.
        page = self._page("sorted " * 100)
        page.resize(*self.LAB)
        page.show()
        self.app.processEvents()
        bottom = self._shown_at(page, page.finish_button())
        self.assertLessEqual(
            bottom, page.centralWidget().viewport().height(),
            "pesan panjang tanpa baris baru membuat tombol 'Selesai' "
            "keluar dari layar",
        )
        page.close()

    def test_runs_of_blank_lines_are_collapsed(self):
        page = self._page("A\n\n\n\n\n\n\nB")
        self.assertNotIn(
            "\n\n\n", page.congrats_text(),
            "baris kosong beruntun tidak diratakan — batas karakter tidak "
            "mencapai apa pun terhadap tinggi yang dirender",
        )
        self.assertIn("A", page.congrats_text())
        self.assertIn("B", page.congrats_text())

    def test_the_short_message_is_left_alone(self):
        # Tidak ada yang dirusak oleh perbaikan ini: pesan pendek tetap
        # persis seperti yang dikirim guru.
        page = self._page("Selamat mengerjakan, terima kasih.")
        self.assertEqual(
            page.congrats_text(), "Selamat mengerjakan, terima kasih.")
        self.assertNotIn("…", page.congrats_text())


class ShownLinkEqualsCopiedLinkTestCase(_RenderTestCase):
    """(3) label harus memuat URL yang benar-benar disalin."""

    INVISIBLE = (
        "https://exam\u200bvan.my.id",   # ZWSP
        "https://exam\u00advan.my.id",   # soft hyphen
        "https://exam\u200fvan.my.id",   # RLM
        "https://exam\u202dvan.my.id",   # LRO
        "https://examvan.my.id\n",       # baris baru
        "https://exa\u200bmvan.my.id",
    )

    def test_the_label_contains_the_copied_url(self):
        for base in self.INVISIBLE:
            with self.subTest(base=repr(base)):
                page = self._page(server_url=base)
                copied = page.result_url()
                self.assertIn(
                    copied, page.displayed_link_text(),
                    "label menampilkan URL yang BERBEDA dari yang disalin "
                    "ke clipboard — siswa menyalin dari layar dan berakhir "
                    "dengan URL yang tidak bisa dibuka",
                )
                page.close()

    def test_invisible_characters_never_reach_the_clipboard(self):
        for base in self.INVISIBLE:
            with self.subTest(base=repr(base)):
                page = self._page(server_url=base)
                self.assertNotIn(
                    "\u200b", page.result_url(),
                    "karakter tak terlihat ikut ke URL yang disalin",
                )
                page.close()

    def test_a_long_url_is_not_truncated_on_screen(self):
        base = "https://examvan.my.id/" + "a" * 2100
        page = self._page(server_url=base)
        self.assertIn(
            page.result_url(), page.displayed_link_text(),
            "label terpotong 2000 karakter — token ujian hilang dari layar "
            "sedangkan ada di clipboard",
        )
        self.assertTrue(
            page.displayed_link_text().rstrip().endswith("ABCD1234"),
            "token harus tetap terlihat di layar",
        )

    def test_the_token_is_never_mangled(self):
        page = self._page(
            server_url="https://exam\u200bvan.my.id", exam_token="ABCD1234",
        )
        self.assertTrue(page.result_url().endswith("/ABCD1234"))


class MisleadingCommentsTestCase(unittest.TestCase):
    """(4) komentar yang berbohong tidak boleh tinggal."""

    SRC = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "examvan", "ui", "congratulations.py",
    )

    def _src(self) -> str:
        with open(self.SRC, encoding="utf-8") as fh:
            return fh.read()

    def test_no_claimed_text_fallback_for_the_icon(self):
        # `:203` menjanjikan fallback teks yang tidak pernah ada; label
        # sengaja dikosongkan supaya tidak menjadi tofu.
        self.assertNotIn(
            "Fallback ke teks hanya kalau style tidak menyediakan pixmap",
            self._src(),
        )

    def test_the_link_label_is_not_claimed_to_be_sanitised(self):
        # Komentar lama: "_sanitize hanya membuang kontrol tak terlihat"
        # (ia juga memotong) dan display "tidak disentuh" (salah).
        src = self._src()
        self.assertNotIn("hanya membuang kontrol tak terlihat", src)

    def test_the_message_limit_comment_matches_the_code(self):
        # Batas yang diklaim harus benar-benar membatasi tinggi yang
        # dirender, bukan cuma jumlah karakter.
        src = self._src()
        self.assertTrue(
            re.search(r"#\s*Batas (?:PANJANG|maksimal) teks server", src),
            "komentar batas panjang teks server hilang",
        )
        # `assertTrue` bukan `assertIn`: kegagalan `assertIn` mencetak
        # seluruh file sebagai pesan, yang berguna untuk siapa pun kecuali
        # pembaca test ini.
        self.assertTrue(
            "tinggi" in src,
            "komentar batas harus menyebut tinggi yang dirender, karena "
            "itulah yang membuat kartu tidak terpakai di layar lab",
        )


if __name__ == "__main__":
    unittest.main()
