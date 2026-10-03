"""Ronde 8 (H6/H6b) — saruan pembersih clipboard harus DILEPAS setelah
keputusan yang sah, bukan sebelumnya.

Bug yang ditutup file ini
-------------------------
`CongratulationsWindow._clear_clipboard_if_ours` menaruh
`self._cleared = True` + `self._copied_at = None` SEBELUM
`clipboard.text()` dibaca. Satu pembacaan yang gagal (clipboard sedang
dikuasai proses lain, pemilik selection X11 mati, Wayland tidak
mengizinkan baca) berarti:

    _tick() -> _clear_clipboard_if_ours() -> read melempar -> return
    _tick() berikutnya: `if self._copied_at is None or self._cleared: return`

Countdown mati permanen, tombolnya masih menampilkan sisa detik yang
salah, dan satu-satunya pembersih yang tersisa adalah `closeEvent` /
`hideEvent`. Di PC lab itu berarti token ujian — kredensial hasil SELURUH
KELAS pada mode static — tinggal di clipboard tanpa ada yang menjaganya,
dan tidak ada satu pun QString di layar yang menyatakannya.

Perbaikan: saruan (countdown) baru dilepas setelah pembacaan BERHASIL dan
keputusan diambil. Kegagalan dibaca ulang pada tick berikutnya, dibatasi
supaya tidak berputar selamanya, dan tombolnya jujur (bukan "Copy Link"
padahal token masih ada di sana).

Aturan "jangan hapus yang bukan milik kita" harus tahan dua kasus yang
sulit dibedakan:

  * baca mengembalikan TEKS LAIN -> milik siswa, jangan dihapus (sudah
    ada sebelumnya, dikunci lagi di sini);
  * baca mengembalikan `""` -> kosong tidak berarti "milik kita sudah
    hilang", dan tidak berarti boleh mengosongkan clipboard orang. Di
    Wayland, atau saat pemilik selection X11 sudah mati, pembacaan
    kosong terjadi karena tidak ada yang memegang selection — bukan
    karena isinya memang kosong. Jadi `clear()` TIDAK boleh dipanggil,
    dan hitung mundur juga TIDAK boleh menggantung mencoba terus.

H6b — cabang restart timer di `showEvent` tidak terjangkau
---------------------------------------------------------
`hideEvent` dulu memanggil `_clear_clipboard_if_ours()` yang melepas
saruan, jadi kondisi `self._copied_at is not None and not self._cleared`
di `showEvent` tidak pernah benar setelah hide yang jujur. Test lama
mencapainya dengan memanggil `_stop_timer()` langsung, dan komentarnya
("timer lalu mati, mis. suspend") tidak benar: QTimer tidak dimatikan
suspend, dia resume lalu berbunyi.

PUTUSAN: countdown harus BERTAHAN lewat hide/show yang sementara.
`hideEvent` tetap menyapu clipboard SEKARANG (fail-safe, tidak melemah),
tapi tidak membuang deadline-nya, sehingga:

  * hide tidak lagi membuang satu-satunya naganya;
  * show melanjutkan hitung mundur dari sisa waktu yang sama;
  * clipboard manager yang mengembalikan salinan lama di balik (X11
    selection restore saat fokus) disapu lagi di deadline yang sama.

Test di sini mencapai cabang itu lewat `hide()` lalu `show()` yang
jujur, bukan dengan menyentuh atribut private.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.congratulations import (
    CLIPBOARD_CLEAR_SECONDS,
    CongratulationsWindow,
)

# Batas percobaan baca ulang yang diharapkan di produksi. Ditulis di sini,
# bukan diimpor, supaya test menguji batas yang disepakati dan bukan
# sekadar mengikuti angka implementasi apa pun.
_RETRY_BOUND = 5


class _FakeClock:
    """Jam dinding yang dikendalikan test (lihat test_congratulations)."""

    def __init__(self) -> None:
        self.now = 1000.0

    def __call__(self) -> float:
        return self.now

    def advance(self, seconds: float) -> None:
        self.now += seconds

    def patch(self, test):
        patcher = mock.patch(
            "examvan.ui.congratulations.time.time", self
        )
        patcher.start()
        test.addCleanup(patcher.stop)
        return self


# Penanda skrip: "baca isi clipboard yang sungguhan".
REAL = object()


class _ScriptedClipboard:
    """Clipboard palsu: `text()`/`clear()` mengikuti skrip test.

    `read_script` adalah daftar hasil pembacaan (string, atau exception
    yang harus dilempar). Setelah skrip habis: kalau `sticky_read` diisi,
    exception itu terus dilempar (clipboard yang benar-benar mati),
    kalau tidak, pembacaan diteruskan ke clipboard sungguhan.

    `clear_script` berlaku untuk `clear()`. Operasi lain diteruskan ke
    clipboard sungguhan, jadi isi clipboard sistem tetap bisa diperiksa
    (offscreen Qt tetap punya satu).
    """

    def __init__(self, real, read_script=(), clear_script=(),
                 sticky_read=None):
        self._real = real
        self._read_script = list(read_script)
        self._clear_script = list(clear_script)
        self._sticky_read = sticky_read
        self.reads = 0
        self.clears = 0

    def _step(self, step):
        if isinstance(step, BaseException):
            raise step
        if step is REAL:
            return self._real.text()
        return step

    def text(self):
        self.reads += 1
        if self._read_script:
            return self._step(self._read_script.pop(0))
        if self._sticky_read is not None:
            raise self._sticky_read
        return self._real.text()

    def clear(self):
        self.clears += 1
        if self._clear_script:
            self._step(self._clear_script.pop(0))
            return
        return self._real.clear()

    def setText(self, text):  # noqa: N802 (Qt API)
        return self._real.setText(text)


class ClipboardScrubArmTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.clock = _FakeClock().patch(self)
        # Disimpan SEBELUM patch: asersi harus membaca clipboard
        # sungguhan, bukan skrip test.
        self.real_clipboard = QApplication.clipboard()
        self.real_clipboard.clear()
        self.addCleanup(self.real_clipboard.clear)

    LINK = "https://examvan.my.id/ABCD1234"

    def _page(self, **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token="ABCD1234",
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

    def _scripted(self, read_script=(), clear_script=(), sticky_read=None):
        fake = _ScriptedClipboard(
            self.real_clipboard, read_script, clear_script, sticky_read,
        )
        patcher = mock.patch.object(
            QApplication, "clipboard", staticmethod(lambda: fake)
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        return fake

    # ------------------------------------------------------------------
    # H6: kegagalan membaca TIDAK boleh melepas saruan
    # ------------------------------------------------------------------

    def test_a_failed_read_keeps_the_countdown_armed(self):
        page = self._page()
        fake = self._scripted(
            read_script=[RuntimeError("clipboard sedang dikuasai proses lain")],
        )
        page.copy_link()

        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()

        self.assertTrue(
            page._copied_at is not None and not page._cleared,
            "satuan hitung mundur dilepas SEBELUM clipboard dibaca: satu "
            "pembacaan yang gagal membuat token tinggal di clipboard "
            "selamanya dan tidak ada lagi yang membersihkannya",
        )
        self.assertEqual(fake.reads, 1, "clipboard harusnya sudah dicoba")
        self.assertNotEqual(
            page.copy_button().text(), "Copy Link",
            "tombol berbohong: menampilkan 'Copy Link' padahal token "
            "masih mungkin ada di clipboard",
        )

    def test_the_token_is_eventually_cleared_after_a_transient_failure(self):
        page = self._page()
        fake = self._scripted(read_script=[
            RuntimeError("gagal baca"),
            RuntimeError("gagal baca"),
            REAL,  # tick ketiga: baca berhasil -> link kita -> clear()
        ])
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)

        page._tick()
        page._tick()
        self.assertEqual(
            self.real_clipboard.text(), self.LINK,
            "sebelum pembacaan berhasil, clipboard harus dibiarkan",
        )

        page._tick()
        self.assertEqual(
            self.real_clipboard.text(), "",
            "pembacaan yang berhasil tidak membersihkan clipboard — retry "
            "pada tick berikutnya tidak pernah berhasil",
        )
        self.assertEqual(fake.clears, 1)
        self.assertEqual(page.copy_button().text(), "Copy Link")

    def test_a_failing_clear_is_retried_too(self):
        # Baca berhasil dan isinya link kita, tapi `clear()` yang gagal:
        # clipboard masih memegang token, jadi saruan jangan dilepas.
        page = self._page()
        fake = self._scripted(
            read_script=[self.LINK, self.LINK],
            clear_script=[RuntimeError("clear ditolak")],
        )
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)

        page._tick()
        self.assertTrue(
            page._copied_at is not None and not page._cleared,
            "clear() yang gagal berarti token masih di clipboard, tapi "
            "satuan sudah dilepas",
        )
        page._tick()
        self.assertEqual(self.real_clipboard.text(), "")
        self.assertEqual(fake.clears, 2)
        self.assertEqual(page.copy_button().text(), "Copy Link")

    def test_the_retry_is_bounded(self):
        # Clipboard yang TIDAK PERNAH bisa dibaca tidak boleh membuat
        # halaman berputar tanpa henti; setelah batas, potong saja dan
        # biarkan close/hide sebagai pembersih terakhir.
        page = self._page()
        self._scripted(sticky_read=RuntimeError("mati"))
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)

        with self.assertLogs("examvan.ui.congratulations", level="WARNING"):
            for _ in range(_RETRY_BOUND + 5):
                page._tick()

        self.assertTrue(page._cleared)
        self.assertIsNone(page._copied_at)
        self.assertNotEqual(
            page.copy_button().text(), "Copy Link",
            "setelah batas percobaan tombol harus jujur: token belum "
            "terbukti hilang",
        )

    def test_closing_still_scrubs_after_the_retry_budget_is_spent(self):
        page = self._page()
        fake = self._scripted(sticky_read=RuntimeError("mati"))
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        with self.assertLogs("examvan.ui.congratulations", level="WARNING"):
            for _ in range(_RETRY_BOUND + 1):
                page._tick()

        # closeEvent adalah kesempatan terakhir: anggaran bacaannya
        # di-reset, jadi satu clipboard yang sedang tidak terbaca tidak
        # membekukan pembersih terakhir ini.
        fake._sticky_read = None
        page.close()
        self.assertEqual(
            self.real_clipboard.text(), "",
            "menutup halaman tidak lagi menjadi kesempatan membersihkan "
            "token setelah satu bacaan gagal",
        )

    # ------------------------------------------------------------------
    # "jangan hapus yang bukan milik kita"
    # ------------------------------------------------------------------

    def test_the_happy_path_is_unchanged(self):
        page = self._page()
        fake = self._scripted()
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()
        self.assertEqual(self.real_clipboard.text(), "")
        self.assertEqual(fake.clears, 1)
        self.assertEqual(page.copy_button().text(), "Copy Link")
        self.assertTrue(page._cleared)

    def test_data_the_student_copied_is_preserved(self):
        page = self._page()
        fake = self._scripted(read_script=["koreksi jawaban saya"])
        page.copy_link()
        self.real_clipboard.setText("koreksi jawaban saya")
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()
        self.assertEqual(fake.clears, 0, "clipboard siswa dihapus")
        self.assertEqual(
            self.real_clipboard.text(), "koreksi jawaban saya")
        self.assertTrue(
            page._cleared, "keputusan sudah diambil, jangan berputar")

    def test_an_empty_read_is_not_permission_to_clear(self):
        # Baca kosong = tidak ada yang memegang selection. Itu BUKAN izin
        # mengosongkan clipboard orang.
        page = self._page()
        fake = self._scripted(read_script=[""])
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()
        self.assertEqual(
            fake.clears, 0,
            "baca kosong diperlakukan sebagai 'boleh mengosongkan "
            "clipboard' — di Wayland/X11 itu bisa menghapus milik orang",
        )

    def test_an_empty_read_does_not_wedge_the_loop(self):
        page = self._page()
        self._scripted(read_script=[""])
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        page._tick()
        self.assertTrue(
            page._cleared and page._copied_at is None,
            "baca kosong membuat hitung mundur menggantung mencoba lagi "
            "selamanya",
        )
        # Tidak ada busy loop: tick berikutnya diam-diam kembali.
        page._tick()
        page._tick()
        self.assertEqual(page.copy_button().text(), "Copy Link")

    def test_empty_read_keeps_data_that_appeared_meanwhile(self):
        # Balapan: clipboard dibaca kosong, lalu siswa menyalin sesuatu.
        # Pembersih tidak boleh menyentuh apa pun.
        page = self._page()
        fake = self._scripted(read_script=[""])
        page.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        self.real_clipboard.setText("catatan siswa")
        page._tick()
        self.assertEqual(fake.clears, 0)
        self.assertEqual(self.real_clipboard.text(), "catatan siswa")


class HideShowKeepsTheCountdownTestCase(unittest.TestCase):
    """H6b — cabang restart di `showEvent` harus terjangkau lewat hide/show."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.clock = _FakeClock().patch(self)
        self.real_clipboard = QApplication.clipboard()
        self.real_clipboard.clear()
        self.addCleanup(self.real_clipboard.clear)

    def _page(self):
        page = CongratulationsWindow(
            server_url="https://examvan.my.id", exam_token="ABCD1234",
            exam_name="Ujian", student_name="SITI", student_number="N02",
            student_class="9B", congrats_message="Selesai",
        )
        self.addCleanup(page.deleteLater)
        return page

    def test_hide_scrubs_now_but_keeps_the_deadline(self):
        page = self._page()
        page.show()
        self.app.processEvents()
        page.copy_link()
        page._tick()
        deadline = page._copied_at
        self.assertIsNotNone(deadline)

        page.hide()
        self.app.processEvents()
        self.assertEqual(
            self.real_clipboard.text(), "",
            "hide harus tetap menyapu clipboard sekarang",
        )
        timer = getattr(page, "_timer", None)
        self.assertIsNotNone(timer)
        self.assertFalse(timer.isActive())
        self.assertEqual(
            page._copied_at, deadline,
            "hide membuang deadline countdown: showEvent tidak punya apa pun "
            "untuk dilanjutkan dan cabang restart-nya mati",
        )
        self.assertFalse(page._cleared)

    def test_show_after_hide_restarts_the_countdown(self):
        page = self._page()
        page.show()
        self.app.processEvents()
        page.copy_link()
        page.hide()
        self.app.processEvents()
        self.assertFalse(page._cleared)

        page.show()
        self.app.processEvents()
        timer = getattr(page, "_timer", None)
        self.assertIsNotNone(timer)
        self.assertTrue(
            timer.isActive(),
            "showEvent tidak me-restart hitung mundur setelah hide/show "
            "yang jujur — cabang restart tidak pernah terjangkau",
        )
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS)
        page._tick()
        self.assertTrue(page._cleared)
        self.assertEqual(page.copy_button().text(), "Copy Link")
        page.close()


if __name__ == "__main__":
    unittest.main()
