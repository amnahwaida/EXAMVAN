"""Halaman "Selesai": link hasil dan hitung mundur clipboard (ronde 5).

M-public -- `public_results=False` masih mencetak token di layar
---------------------------------------------------------------
Menyembunyikan tombol Copy saja tidak cukup: label link dirender dari
`_result_url` yang sudah terisi, jadi token kelas -- kredensial hasil
SELURUH KELAS pada mode static-token -- tetap tercetak di layar PC lab
(rekaman layar depan kelas, foto kamera ruang pengawas, screenshot siswa
berikutnya yang menumpuk di folder Unduhan). Guru yang mematikan publikasi
nilai sudah memutuskan link itu tidak boleh dibuka siapa pun; menampilkannya
di layar sama saja membocorkannya.

Perbaikannya: `public_results=False` berarti TIDAK ADA url sama sekali
(`_result_url` kosong), dan label memakai pesan jujur yang sama dengan kasus
token kosong. Dengan begitu tidak ada satu pun label yang memuat token, dan
tidak ada yang bisa disalin lewat pilih-teks atau dibaca kamera.

M-clock -- hitung mundur clipboard memakai jam dinding
------------------------------------------------------
`_tick()` mengukur `time.time() - self._copied_at`. Jam dinding bisa
dimundurkan administrator (koreksi NTP, pengaturan tanggal manual, PC lab
yang sakelar waktunya dikoreksi hari itu juga) -- dan begitu jam mundur,
`elapsed` jadi negatif, syarat `elapsed < CLIPBOARD_CLEAR_SECONDS` selalu
benar, dan token yang sudah disalin tertinggal di clipboard PC lab sampai jam
menyusul satu jam kemudian. Tombolnya ikut berbohong: "Copy Link (3629s)".

Jendela harus diukur dari jam yang TIDAK bisa dimundurkan
(`time.monotonic()`), DAN tetap harus berakhir kalau jam dinding melompat
maju -- itulah kasus suspend yang jadi alasan asli pemakaian jam dinding:
`monotonic` membeku saat mesin tidur, jadi token yang disalin sebelum
suspend tidak boleh menetap selamanya sesudahnya. Keduanya dipakai: jendela
berakhir pada jam mana pun yang lebih dulu lewat, dan sisa waktu yang
ditampilkan selalu dijepit ke [0, CLIPBOARD_CLEAR_SECONDS].
"""

from __future__ import annotations

import os
import re
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QLabel

from examvan.ui.congratulations import (
    CLIPBOARD_CLEAR_SECONDS,
    CongratulationsWindow,
)


class _FakeClocks:
    """Jam monotonic DAN jam dinding yang keduanya dikendalikan test.

    `_tick()` membaca keduanya, jadi menambal hanya satu jam membuat test
    menebak: jam yang tidak ditambal bergerak nyata (~0 detik), persis
    seperti kasus "hanya satu jam yang dimanipulasi" yang sedang diuji.
    """

    def __init__(self) -> None:
        self.mono = 1000.0
        self.wall = 1_700_000_000.0

    def monotonic(self) -> float:
        return self.mono

    def time(self) -> float:
        return self.wall

    def advance(self, seconds: float) -> None:
        """Waktu NYATA mengalir normal: kedua jam ikut maju."""
        self.mono += seconds
        self.wall += seconds

    def rewind_wall(self, seconds: float) -> None:
        """Hanya jam dinding yang dimundurkan (koreksi NTP / tanggal)."""
        self.wall -= seconds

    def jump_wall(self, seconds: float) -> None:
        """Hanya jam dinding yang melompat (suspend, koreksi NTP)."""
        self.wall += seconds

    def patch(self, test):
        for name, fake in (
            ("monotonic", self.monotonic),
            ("time", self.time),
        ):
            patcher = mock.patch(
                f"examvan.ui.congratulations.time.{name}", fake)
            patcher.start()
            test.addCleanup(patcher.stop)
        return self


def _remaining_seconds(page: CongratulationsWindow):
    """Sisa detik yang tertulis di tombol Copy, atau None kalau sudah idle."""
    match = re.search(r"\((\d+)s\)", page.copy_button().text())
    return int(match.group(1)) if match else None


class ResultLinkHonestyTestCase(unittest.TestCase):
    """M-public: guru mematikan publikasi -> tidak ada url di layar."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    TOKEN = "ABCD1234"
    LINK = "https://examvan.my.id/ABCD1234"

    def _page(self, **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token=self.TOKEN,
            exam_name="Ujian",
            student_name="SITI",
            student_number="N02",
            student_class="9B",
            congrats_message="Selesai",
        )
        params.update(kwargs)
        page = CongratulationsWindow(**params)
        self.addCleanup(page.deleteLater)
        return page

    def _label_texts(self, page):
        return [lab.text() for lab in page.findChildren(QLabel)]

    def test_unpublished_results_expose_no_url(self):
        page = self._page(public_results=False)
        self.assertEqual(
            page.result_url(), "",
            "public_results=False harus berarti tidak ada url sama sekali, "
            "bukan url yang tombol salinnya disembunyikan saja",
        )

    def test_unpublished_results_print_no_token_anywhere_on_the_page(self):
        page = self._page(public_results=False)
        for text in self._label_texts(page):
            self.assertNotIn(
                self.TOKEN, text,
                "token kelas masih tercetak di halaman yang tidak boleh "
                "mempublikasikan hasil",
            )
            self.assertNotIn(
                "http", text.lower(),
                "url hasil masih tercetak walau tombolnya disembunyikan",
            )

    def test_unpublished_results_show_the_honest_unavailable_message(self):
        page = self._page(public_results=False)
        self.assertTrue(
            any("tidak tersedia" in t.lower()
                for t in self._label_texts(page)),
            "tidak ada yang menjelaskan kenapa linknya tidak ada",
        )

    def test_unpublished_results_keep_the_honest_teacher_note(self):
        page = self._page(public_results=False)
        self.assertTrue(
            any("tidak dipublikasikan" in t.lower()
                for t in self._label_texts(page)),
            "catatan 'nilai tidak dipublikasikan guru' harus tetap ada",
        )

    def test_published_results_are_unchanged(self):
        page = self._page(public_results=True)
        self.assertEqual(page.result_url(), self.LINK)
        self.assertTrue(
            any(self.LINK in t for t in self._label_texts(page)),
            "link hasil harus tetap dirender saat guru mempublikasikan nilai",
        )
        self.assertTrue(page.copy_button().isEnabled())

    def test_an_empty_token_stays_empty_too(self):
        page = self._page(exam_token="")
        self.assertEqual(page.result_url(), "")
        self.assertTrue(
            any("tidak tersedia" in t.lower()
                for t in self._label_texts(page)),
        )


class ClipboardCountdownClockTestCase(unittest.TestCase):
    """M-clock: jendela hitung mundur tidak bisa dipanjang/dipotong jam."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.clocks = _FakeClocks().patch(self)
        QApplication.clipboard().clear()
        self.addCleanup(QApplication.clipboard().clear)

    LINK = "https://examvan.my.id/ABCD1234"

    def _page(self, **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token="ABCD1234",
            exam_name="Ujian",
            student_name="SITI",
            congrats_message="Selesai",
        )
        params.update(kwargs)
        page = CongratulationsWindow(**params)
        self.addCleanup(page.deleteLater)
        return page

    # -- (a) jam dinding mundur ----------------------------------------

    def test_a_backwards_wall_clock_step_does_not_extend_the_window(self):
        page = self._page()
        page.copy_link()
        self.assertEqual(QApplication.clipboard().text(), self.LINK)

        self.clocks.advance(5)
        page._tick()
        before = _remaining_seconds(page)
        self.assertIsNotNone(before)

        # Jam dinding dimundurkan satu jam (koreksi NTP / tanggal manual).
        self.clocks.rewind_wall(3600)
        page._tick()

        self.assertEqual(
            QApplication.clipboard().text(), self.LINK,
            "token hilang sebelum waktunya -- jam mundur tidak boleh "
            "memperpendek jendela pembersih",
        )
        self.assertLessEqual(
            _remaining_seconds(page), before,
            "jam dinding mundur menambah sisa waktu yang ditampilkan "
            "(label jadi 'Copy Link (3629s)'): token tertinggal di PC lab "
            "sampai jam menyusul satu jam kemudian",
        )

    def test_a_backwards_wall_clock_step_still_expires_on_real_time(self):
        page = self._page()
        page.copy_link()
        self.clocks.rewind_wall(3600)

        # Waktu NYATA (monotonic) tetap mengalir 30 detik.
        self.clocks.advance(CLIPBOARD_CLEAR_SECONDS)
        page._tick()

        self.assertEqual(
            QApplication.clipboard().text(), "",
            "meski jam dinding mundur, 30 detik waktu nyata tetap harus "
            "membersihkan token",
        )

    # -- (b) jam dinding maju (suspend) --------------------------------

    def test_a_forward_wall_clock_step_still_clears(self):
        # Kasus suspend yang jadi alasan asli pemakaian jam dinding:
        # monotonic membeku, jam dinding terus berjalan.
        page = self._page()
        page.copy_link()

        self.clocks.jump_wall(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()

        self.assertEqual(
            QApplication.clipboard().text(), "",
            "mesin tidur lalu bangun tidak boleh membekukan pembersih "
            "clipboard -- token kelas menetap di PC lab",
        )

    # -- (c) sisa waktu selalu dijepit ---------------------------------

    def test_the_displayed_remaining_is_clamped_to_the_window(self):
        page = self._page()
        page.copy_link()
        self.clocks.rewind_wall(1_000_000)
        page._tick()

        remaining = _remaining_seconds(page)
        self.assertIsNotNone(remaining, "tombol harus menampilkan hitung mundur")
        self.assertGreaterEqual(remaining, 0)
        self.assertLessEqual(
            remaining, CLIPBOARD_CLEAR_SECONDS,
            f"sisa waktu {remaining}s lebih dari jendela "
            f"{CLIPBOARD_CLEAR_SECONDS}s -- tombol menjanjikan jendela yang "
            "tidak pernah berakhir",
        )

    def test_the_displayed_remaining_never_leaves_the_clamped_range(self):
        page = self._page()
        page.copy_link()
        for step in (-7.0, -100.0, 1.0, 12.5, -1000.0, 29.9, 0.1, 5.0):
            with self.subTest(wall_jump=step):
                self.clocks.jump_wall(step)
                if step < 0:
                    # waktu nyata juga berjalan maju (jam monotonic tidak
                    # bisa dimundurkan -- itu justru inti bug-nya)
                    self.clocks.mono -= step
                page._tick()
                remaining = _remaining_seconds(page)
                if remaining is None:
                    continue  # sudah dibersihkan -> tombol kembali idle
                self.assertGreaterEqual(remaining, 0)
                self.assertLessEqual(remaining, CLIPBOARD_CLEAR_SECONDS)


if __name__ == "__main__":
    unittest.main()