"""Halaman selamat setelah submit (parity Android `CongratulationsActivity`).

Kesenjangan yang ditutup file ini
---------------------------------
Klien Android punya `CongratulationsActivity`: pesan ucapan selamat yang
di-custom guru, badge nama ujian, baris identitas siswa, dan tombol
"Copy Link" untuk short-link `{serverUrl}/{examToken}` yang me-redirect ke
`/hasil/<token>`.

Desktop hanya menampilkan `QMessageBox.information()` lalu menutup
jendela, jadi siswa tidak pernah mendapatidentitas siswa, nama ujian, maupun
link hasil -- meski server sudah menyediakan targetnya
(`GET /hasil/:token` plus short-link redirect `/<token>` di
`webui/cmd/server/main.go`). formatnya juga terpusat di
`ResultsLinkPolicy` Android; di desktop ini tidak ada padanannya sama
sekali, jadi format URL akan ditulis ulang di beberapa tempat begitu
ditambahkan.

Kenapa clipboard harus DIBERSIHKAN otomatis
-------------------------------------------
PC lab dipakai bersama. `exam_token` adalah kredensial seluruh kelas pada
mode static -- commit sebelumnya menutup kebocorannya ke storage origin,
membuat `config.json` 0600, dan menghentikan kunci jawaban yang mengikuti
token. Menyalin URL yang memuat token ke clipboard dan meninggalkannya
berarti mengembalikan kebocoran itu lewat pintu yang paling mudah:
siswa berikutnya tinggal paste.

`SecurityEnforcer` menyapu clipboard tiap 10 detik, tapi hanya selama
ujian berjalan -- dan `_cleanup_after_submit` sudah memanggil
`deactivate()` sebelum halaman ini tampil. Jadi tanpa pembersih
eksplisit, link itu bertahan selamanya di clipboard mesin.

Karena itu tombol copy punya hitung mundur dan clipboard dikosongkan
setelah 30 detik -- hanya jika isinya MASIH link kita, supaya tidak
menghapus sesuatu yang siswa salin sendiri setelahnya.
"""

from __future__ import annotations

import time
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

from examvan.models import Exam
from examvan.ui.congratulations import (
    CLIPBOARD_CLEAR_SECONDS,
    CongratulationsDialog,
)
from examvan.utils import build_result_link


class ResultLinkFormatTestCase(unittest.TestCase):
    """Format URL harus sama persis dengan `ResultsLinkPolicy` Android."""

    def test_short_link_is_base_slash_token(self):
        self.assertEqual(
            build_result_link("https://examvan.my.id", "ABCD1234"),
            "https://examvan.my.id/ABCD1234",
        )

    def test_trailing_slashes_on_the_base_are_collapsed(self):
        for base in ("https://examvan.my.id/", "https://examvan.my.id//"):
            with self.subTest(base=base):
                self.assertEqual(
                    build_result_link(base, "ABCD1234"),
                    "https://examvan.my.id/ABCD1234",
                )

    def test_the_base_keeps_its_path_prefix(self):
        # Instalasi di sub-path (reverse proxy) tidak boleh kehilangan
        # prefix-nya -- hanya slash ekor yang dirapikan.
        self.assertEqual(
            build_result_link("https://host/examvan/", "ABCD1234"),
            "https://host/examvan/ABCD1234",
        )

    def test_empty_inputs_produce_an_empty_link(self):
        # Mirrors ResultsLinkPolicy.build(): string kosong, bukan "None"
        # atau "/" yang kelihatan benar tapi tidak bisa dipakai.
        self.assertEqual(build_result_link("", "ABCD1234"), "")
        self.assertEqual(build_result_link("https://examvan.my.id", ""), "")
        self.assertEqual(build_result_link("", ""), "")

    def test_surrounding_whitespace_is_ignored(self):
        self.assertEqual(
            build_result_link("  https://examvan.my.id  ", "  ABCD1234  "),
            "https://examvan.my.id/ABCD1234",
        )


class CongratulationsDialogContentTestCase(unittest.TestCase):
    """Isi layar: pesan guru, nama ujian, identitas."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _dialog(self, **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token="ABCD1234",
            exam_name="Ujian Matematika",
            student_name="SITI",
            student_number="N02",
            student_class="9B",
            congrats_message="Good job, kamuHebat!",
        )
        params.update(kwargs)
        dlg = CongratulationsDialog(**params)
        self.addCleanup(dlg.deleteLater)
        return dlg

    def test_the_teachers_message_is_shown(self):
        dlg = self._dialog(congrats_message="Hebat, kerja bagus!")
        self.assertEqual(dlg.congrats_text(), "Hebat, kerja bagus!")

    def test_a_blank_teacher_message_falls_back_to_a_default(self):
        for blank in ("", "   ", None):
            with self.subTest(value=blank):
                dlg = self._dialog(congrats_message=blank)
                text = dlg.congrats_text()
                self.assertTrue(text.strip(), "kalau pesan guru kosong, "
                                             "layar jadi kosong tanpa konteks")
                self.assertNotEqual(text.strip(), "")

    def test_the_exam_name_is_shown(self):
        dlg = self._dialog(exam_name="Ujian Matematika")
        self.assertIn("Ujian Matematika", dlg.exam_name_text())

    def test_a_missing_exam_name_does_not_leave_an_empty_badge(self):
        dlg = self._dialog(exam_name="")
        self.assertTrue(dlg.exam_name_text().strip())

    def test_all_identity_fields_are_shown(self):
        dlg = self._dialog()
        text = dlg.identity_text()
        for expected in ("SITI", "N02", "9B"):
            self.assertIn(expected, text)

    def test_blank_identity_fields_are_omitted_rather_than_shown_empty(self):
        # Field kosong yang tetap dirender terlihat seperti data yang gagal
        # dimuat -- dan pada layar "Selesai" itu membingungkan siswa yang
        # justru sedangmergne.
        dlg = self._dialog(student_number="", student_class="")
        text = dlg.identity_text()
        self.assertIn("SITI", text)
        self.assertNotIn("Nomor Ujian", text)
        self.assertNotIn("Kelas", text)

    def test_identity_with_no_fields_at_all_shows_no_stray_labels(self):
        dlg = self._dialog(student_name="", student_number="", student_class="")
        text = dlg.identity_text()
        for label in ("Nama", "Nomor Ujian", "Kelas"):
            self.assertNotIn(label, text)

    def test_whitespace_only_identity_fields_count_as_blank(self):
        dlg = self._dialog(student_name="   ")
        self.assertNotIn("SITI", dlg.identity_text())
        self.assertNotIn("Nama", dlg.identity_text())

    def test_the_result_link_is_built_from_the_exam_token(self):
        dlg = self._dialog()
        self.assertEqual(dlg.result_url(), "https://examvan.my.id/ABCD1234")

    def test_an_empty_token_yields_no_link_and_a_disabled_button(self):
        dlg = self._dialog(exam_token="")
        self.assertEqual(dlg.result_url(), "")
        self.assertFalse(
            dlg.copy_button().isEnabled(),
            "tombol copy aktif tanpa link; siswa akan menyalin "
            "sesuatu yang tidak berguna",
        )


class _FakeClock:
    """Jam yang dikendalikan test.

    Menghitung prediksi `time.monotonic` secara manual rapuh --
    `copy_link()` sendiri memanggil `_tick()`, jadi satu aksi bisa
   mengonsumsi beberapa pembacaan jam. Jam palsu menghapus tebakan itu.
    """

    def __init__(self) -> None:
        self.now = 1000.0

    def __call__(self) -> float:
        return self.now

    def advance(self, seconds: float) -> None:
        self.now += seconds

    def patch(self, test):
        patcher = mock.patch(
            "examvan.ui.congratulations.time.monotonic", self
        )
        patcher.start()
        test.addCleanup(patcher.stop)
        return self


class CopyLinkAndAutoClearTestCase(unittest.TestCase):
    """Tombol copy + pembersih clipboard otomatis."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.clock = _FakeClock().patch(self)
        QApplication.clipboard().clear()

    LINK = "https://examvan.my.id/ABCD1234"

    def _dialog(self, **kwargs):
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
        dlg = CongratulationsDialog(**params)
        self.addCleanup(dlg.deleteLater)
        return dlg

    def test_copying_puts_the_link_on_the_clipboard(self):
        dlg = self._dialog()
        QApplication.clipboard().setText("sebelum")
        dlg.copy_link()
        self.assertEqual(QApplication.clipboard().text(), self.LINK)

    def test_the_window_is_thirty_seconds(self):
        self.assertEqual(CLIPBOARD_CLEAR_SECONDS, 30)

    def test_the_clipboard_is_cleared_after_the_countdown(self):
        dlg = self._dialog()
        dlg.copy_link()
        self.assertEqual(QApplication.clipboard().text(), self.LINK)

        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        dlg._tick()

        self.assertEqual(
            QApplication.clipboard().text(), "",
            "token ujian masih ada di clipboard PC lab 30 detik setelah "
            "dibuat -- kebocoran yang sama yang commit sebelumnya tutup "
            "dari sisi lain",
        )

    def test_the_clipboard_is_kept_while_the_timer_is_still_running(self):
        dlg = self._dialog()
        dlg.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS - 1)
        dlg._tick()
        self.assertEqual(QApplication.clipboard().text(), self.LINK)

    def test_repeated_copies_restart_the_countdown(self):
        dlg = self._dialog()
        dlg.copy_link()
        self.clock.advance(25)
        dlg._tick()
        dlg.copy_link()          # disalin ulang
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS - 1)
        dlg._tick()
        self.assertEqual(
            QApplication.clipboard().text(), self.LINK,
            "countdown tidak dimulai ulang saat link disalin ulang",
        )

    def test_only_our_own_link_is_cleared(self):
        # Siswa menyalin sesuatu yang lain SETELAH menekan tombol --
        # jangan hapus itu.
        dlg = self._dialog()
        dlg.copy_link()
        QApplication.clipboard().setText("koreksi jawaban saya")
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        dlg._tick()
        self.assertEqual(
            QApplication.clipboard().text(), "koreksi jawaban saya",
            "clipboard dihapus padahal isinya bukan link yang kita buat",
        )

    def test_closing_the_dialog_still_clears_the_clipboard(self):
        # Kalau siswa menekan X sebelum hitung mundur habis, timer ikut
        # mati -- jadi pembersihan harus terjadi di close, bukan hanya
        # lewat tick.
        dlg = self._dialog()
        dlg.copy_link()
        self.assertEqual(QApplication.clipboard().text(), self.LINK)
        dlg.close()
        self.assertEqual(
            QApplication.clipboard().text(), "",
            "menutup layar sebelum hitung mundur habis meninggalkan token "
            "di clipboard",
        )

    def test_closing_without_copying_leaves_the_clipboard_alone(self):
        # Jangan sampai kita menghapus clipboard siswa yang tidak ada
        # hubungannya dengan kita.
        dlg = self._dialog()
        QApplication.clipboard().setText("catatan pribadi")
        dlg.close()
        self.assertEqual(QApplication.clipboard().text(), "catatan pribadi")

    def test_the_button_shows_the_remaining_seconds(self):
        dlg = self._dialog()
        dlg.copy_link()
        self.clock.advance(12)
        dlg._tick()
        self.assertIn("18", dlg.copy_button().text())

    def test_the_button_returns_to_its_idle_text_after_clearing(self):
        dlg = self._dialog()
        dlg.copy_link()
        self.clock.advance(CLIPBOARD_CLEAR_SECONDS + 1)
        dlg._tick()
        self.assertEqual(dlg.copy_button().text(), "Copy Link")
        self.assertTrue(dlg.copy_button().isEnabled())

    def test_the_button_says_it_will_be_cleared(self):
        dlg = self._dialog()
        self.assertIn("30", dlg.copy_button().toolTip())


if __name__ == "__main__":
    unittest.main()


class SubmitFlowShowsThePageTestCase(unittest.TestCase):
    """Layar selamat harus benar-benar dipanggil dari kedua jalur submit.

    Test isi dialog di atas tidak membuktikan apa pun kalau tidak ada
    yang memanggilnya -- dan pemanggilannya justru bagian yang paling
    mudah luput saat ada refactor.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    @staticmethod
    def _exam(name="Ujian"):
        return Exam(id=7, name=name, status="active")

    def test_successful_submit_opens_the_page_with_the_student_identity(self):
        from examvan.ui import exam_viewer as ev

        captured = {}

        def _grab(**kwargs):
            captured.update(kwargs)
            return mock.Mock(exec_=mock.Mock(return_value=0))

        with mock.patch.object(ev, "CongratulationsDialog", side_effect=_grab), \
             mock.patch.object(ev.config, "clear_answers"), \
             mock.patch.object(ev.config, "mark_submitted"), \
             mock.patch.object(ev.config, "load_start_time", return_value="t"), \
             mock.patch.object(ev, "build_attempt_key", return_value="k"), \
             mock.patch.object(ev, "student_label", return_value="l"):
            win = ev.ExamViewerWindow.__new__(ev.ExamViewerWindow)
            win._exam = self._exam("Ujian Matematika")
            win._token = "ABCD1234"
            win._server_url = "https://examvan.my.id"
            win._identity_data = {"nama": "SITI", "nomor_ujian": "N02",
                                  "kelas": "9B"}
            win._security = None
            win._timer_widget = mock.Mock()
            win._btn_submit = mock.Mock()
            win._pdf_viewer = mock.Mock()
            win._pdf_path = None
            win._stop_presence = mock.Mock()
            win.close = mock.Mock()
            win._cleanup_after_submit("Hebat, kerja bagus!")

        self.assertEqual(captured.get("student_name"), "SITI")
        self.assertEqual(captured.get("student_number"), "N02")
        self.assertEqual(captured.get("student_class"), "9B")
        self.assertEqual(captured.get("exam_name"), "Ujian Matematika")
        self.assertEqual(captured.get("exam_token"), "ABCD1234")
        self.assertEqual(captured.get("congrats_message"), "Hebat, kerja bagus!")

    def test_the_security_is_deactivated_before_the_page_is_shown(self):
        # Kalau tidak, `SecurityEnforcer` masih menyapu clipboard tiap 10
        # detik dan link yang disalin di layar ini langsung hilang --
        # atau malah sebaliknya, enforcer masih hidup saat siswa keluar.
        from examvan.ui import exam_viewer as ev

        order = []

        class _Security:
            def deactivate(self):
                order.append("deactivate")

        def _grab(**kwargs):
            order.append("show")
            return mock.Mock(exec_=mock.Mock(return_value=0))

        with mock.patch.object(ev, "CongratulationsDialog", side_effect=_grab), \
             mock.patch.object(ev.config, "clear_answers"), \
             mock.patch.object(ev.config, "mark_submitted"), \
             mock.patch.object(ev, "build_attempt_key", return_value="k"), \
             mock.patch.object(ev, "student_label", return_value="l"):
            win = ev.ExamViewerWindow.__new__(ev.ExamViewerWindow)
            win._exam = self._exam("Ujian")
            win._token = "ABCD1234"
            win._server_url = "https://examvan.my.id"
            win._identity_data = {}
            win._security = _Security()
            win._timer_widget = mock.Mock()
            win._btn_submit = mock.Mock()
            win._pdf_viewer = mock.Mock()
            win._pdf_path = None
            win._stop_presence = mock.Mock()
            win.close = mock.Mock()
            win._cleanup_after_submit("ok")

        self.assertEqual(order, ["deactivate", "show"])
