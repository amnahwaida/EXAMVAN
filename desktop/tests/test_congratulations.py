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



class TeacherMessageSurvivesTheQueuedPollTestCase(unittest.TestCase):
    """#5 — `congrats_message` hanya ada di respons 202, bukan di `/result`.

    Server mengirim `congrats_message` hanya di dua tempat, keduanya pada
    respons 202 (`exams.go:1204`, `:1237`). Endpoint `/result` yang
    dipanggil `poll_queued_result` tidak pernah mengirimkannya; pesannya
    selalu literal `"Jawaban berhasil disimpan"`.

    Karena `resp = api.poll_queued_result(...)` MENIMPA objek respons 202,
    `resp.congrats_message` selalu `None` di jalur mana pun yang lewat
    antrean -- yaitu jalur NORMAL saat Redis tersedia:

        202 -> congrats_message = "Hebat, kerja bagus!"
        /result -> message = "Jawaban berhasil disimpan"
        (resp ditimpa)
        pesan yang dipakai dialog : 'Jawaban berhasil disimpan'

    Commit `a58bdf5` menjadikan pesan guru itu SELURUH isi headline
    halaman selamat, jadi fiturnya praktis mati di produksi.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    @staticmethod
    def _exam(name="Ujian"):
        from examvan.models import Exam
        return Exam(id=7, name=name, status="active")

    def test_the_polling_carries_the_teacher_message_through(self):
        from examvan import api
        from examvan.models import SubmitResponse

        queued = SubmitResponse(
            success=True, status="queued", job_id="j1",
            congrats_message="Hebat, kerja bagus!",
        )
        durable = SubmitResponse(
            success=True, status="done",
            message="Jawaban berhasil disimpan",
        )
        with mock.patch.object(api, "exam_result", return_value=durable), \
             mock.patch.object(api.time, "sleep"):
            resp = api.poll_queued_result(
                "https://x", 7, "T", "M", "j1",
                initial_congrats=queued.congrats_message,
            )
        self.assertEqual(
            resp.congrats_message, queued.congrats_message,
            "pesan guru hilang saat polling: `/result` hanya mengirim "
            "message generik dan objek respons ditimpa",
        )

    def test_the_submit_path_actually_forwards_the_teacher_message(self):
        """Pemanggil produksi harus meneruskannya.

        Test pertama hanya membuktikan polling bisa meneruskan pesan kalau
        DIBERIKAN -- tidak membuktikan `_submit_thread` benar-benar
       .memberikannya. Kalau pemanggilnya lupa, fiturnya mati dan testnya
        tetap hijau.
        """
        import threading

        from examvan.ui import exam_viewer as ev
        from examvan.models import SubmitResponse

        win = ev.ExamViewerWindow.__new__(ev.ExamViewerWindow)
        win._exam = self._exam("Ujian")
        win._token = "ABCD1234"
        win._server_url = "https://examvan.my.id"
        win._identity_data = {"nama": "SITI", "nomor_ujian": "N02"}
        win._security = None
        win._save_lock = threading.Lock()
        win._answer_sheet = mock.Mock()
        win._answer_sheet.get_answers.return_value = {"1": "A"}
        win._sig_status = mock.Mock()
        win._sig_submit_result = mock.Mock()
        win._on_download_progress = mock.Mock()
        win._submit_lock = threading.Lock()
        win._submitting = False
        win._submitted = False

        queued = SubmitResponse(
            success=True, status="queued", job_id="j1",
            congrats_message="Hebat, kerja bagus!",
        )
        durable = SubmitResponse(
            success=True, status="done",
            message="Jawaban berhasil disimpan",
        )
        with mock.patch.object(ev.api, "submit_with_retry", return_value=queued), \
             mock.patch.object(ev.api, "poll_queued_result",
                               return_value=durable) as poll, \
             mock.patch.object(ev.config, "save_start_time"), \
             mock.patch.object(ev.config, "load_start_time", return_value="t"), \
             mock.patch.object(ev, "build_attempt_key", return_value="k"):
            ev.ExamViewerWindow._submit_thread(
                win, "https://examvan.my.id", 7, "SITI", "N02", "9B",
                {"1": "A"}, "2026-09-30T07:00:00Z", "DESKTOP:abc",
                {"nama": "SITI"},
            )

        self.assertTrue(poll.called, "polling tidak dipanggil sama sekali")
        self.assertEqual(
            poll.call_args.kwargs.get("initial_congrats"),
            "Hebat, kerja bagus!",
            "_submit_thread tidak meneruskan congrats_message dari respons "
            "202, sehingga halaman selamat menampilkan teks bawaan server",
        )


if __name__ == "__main__":
    unittest.main()
