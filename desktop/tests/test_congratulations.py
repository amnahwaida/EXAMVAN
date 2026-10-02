"""Halaman selamat setelah submit (parity Android `CongratulationsActivity`).

Kesenjangan yang ditutup file ini
---------------------------------
Klien Android punya `CongratulationsActivity`: pesan ucapan selamat yang
di-custom guru, badge nama ujian, baris identitas siswa, dan tombol
"Copy Link" untuk short-link `{serverUrl}/{examToken}` yang me-redirect ke
`/hasil/<token>`.

Desktop dulu menampilkan `QMessageBox`, lalu diperbaiki menjadi dialog modal
-- dan modal pun masih terasa POP-UP: kotak kecil di atas jendela ujian,
countdown tetap kelihatan di belakangnya, X kecil satu-satunya jalan keluar.
Sekarang ini HALAMAN tersendiri (`CongratulationsWindow`, QMainWindow
non-modal fullscreen) yang menggantikan jendela ujian; jendela ujian baru
ditutup saat halaman ini ditutup siswa, supaya dialog konfigurasi siswa
berikutnya tidak menimpa halaman yang sedang dibaca. Server menyediakan
targetnya (`GET /hasil/:token` plus short-link redirect `/<token>` di
`webui/cmd/server/main.go`); format URL mengikuti `ResultsLinkPolicy`
Android.

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

import os
import time
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QDialog, QMainWindow

from examvan.models import Exam
from examvan.ui.congratulations import (
    CLIPBOARD_CLEAR_SECONDS,
    CongratulationsWindow,
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


class CongratulationsContentTestCase(unittest.TestCase):
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
        dlg = CongratulationsWindow(**params)
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

    Menghitung prediksi `time.time` secara manual rapuh --
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
            "examvan.ui.congratulations.time.time", self
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
        dlg = CongratulationsWindow(**params)
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


class ItIsAPageNotAPopupTestCase(unittest.TestCase):
    """Laporan lapangan: "jadikan halaman sendiri, bukan pop-up".

    Dua hal yang membuat versi lama terasa pop-up:
      1. bentuknya QDialog modal di atas jendela ujian;
      2. satu-satunya jalan keluar adalah X kecil di title bar.
    Kontrak sekarang: QMainWindow non-modal dengan tombol "Selesai" yang
    jelas, dan menutup halaman mengumumkan `page_closed` ke pemanggil.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _page(self, **kwargs):
        params = dict(
            server_url="https://examvan.my.id",
            exam_token="ABCD1234",
            exam_name="Ujian",
            student_name="SITI",
        )
        params.update(kwargs)
        page = CongratulationsWindow(**params)
        self.addCleanup(page.deleteLater)
        return page

    def test_it_is_a_standalone_window_not_a_modal_dialog(self):
        page = self._page()
        self.assertIsInstance(page, QMainWindow)
        self.assertNotIsInstance(page, QDialog)
        self.assertFalse(
            page.isModal(),
            "halaman selamat tidak boleh memblokir jendela lain -- "
            "itu definisi pop-up yang dikeluhkan",
        )

    def test_there_is_a_visible_finish_button(self):
        page = self._page()
        page.show()
        self.app.processEvents()
        self.assertTrue(page.finish_button().isVisible())
        self.assertTrue(page.finish_button().isEnabled())

    def test_the_finish_button_closes_the_page(self):
        page = self._page()
        page.show()
        self.app.processEvents()
        closed = []
        page.page_closed.connect(lambda: closed.append(True))
        page.finish_button().click()
        self.app.processEvents()
        self.assertTrue(closed, "tombol Selesai menutup halaman")
        self.assertFalse(page.isVisible())
        self.assertTrue(page.is_closed())

    def test_closing_via_the_title_bar_also_announces_page_closed(self):
        # X di title bar, Alt+F4, atau WM close harus berperilaku sama
        # dengan tombol Selesai -- siswa tidak boleh terjebak di halaman
        # yang tidak bisa ditutup lewat cara standar Windows.
        page = self._page()
        closed = []
        page.page_closed.connect(lambda: closed.append(True))
        page.close()
        self.assertEqual(closed, [True])

    def test_page_closed_fires_exactly_once(self):
        # Penerima sinyal (ExamViewer) menutup dirinya begitu halaman
        # ditutup; sinyal ganda berarti alur berikutnya (dialog konfigurasi)
        # dijalankan dua kali.
        page = self._page()
        closed = []
        page.page_closed.connect(lambda: closed.append(True))
        page.close()
        page.close()
        self.assertEqual(closed, [True])


class ViewerHandsOverToThePageTestCase(unittest.TestCase):
    """Alur `_cleanup_after_submit`: halaman tampil, viewer sembunyi dulu.

    `__main__` menampilkan ServerConfigDialog siswa berikutnya dari sinyal
    `closed` viewer. Kalau viewer di-`close()` BERSAMAAN dengan menampilkan
    halaman selamat, `closed` menembak sinkron dan dialog konfigurasi
    menimpa halamannya -- persis keluhan "pop-up" dalam bentuk baru.
    Kontrak (C1): halaman dulu + viewer `hide()` (tetap direferensi supaya
    tidak ter-GC); viewer `close()` saat halaman ditutup siswa
    (`page_closed`); baru saat itu `closed` menembak dan dialog berikutnya
    tampil.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _viewer(self):
        import threading

        from examvan.ui import exam_viewer as ev

        win = ev.ExamViewerWindow.__new__(ev.ExamViewerWindow)
        win._exam = Exam(id=1, name="Ujian", status="active")
        win._token = "ABCD1234"
        win._server_url = "https://exam.example"
        win._identity_data = {"nama": "Andi", "nomor_ujian": "N01"}
        win._submitted = True
        win._submitting = False
        win._submit_lock = threading.Lock()
        win._security = None
        win._timer_widget = mock.Mock()
        win._btn_submit = mock.Mock()
        win._pdf_viewer = mock.Mock()
        win._pdf_path = None
        win._std_identity = {"student_name": "Andi"}
        win._device_label = "DESKTOP:test"
        win._presence_active = False
        win._heartbeat_timer = mock.Mock()
        win._ws = mock.Mock()
        return win

    def test_viewer_hides_while_congrats_page_shows(self):
        from examvan.ui import exam_viewer as ev

        win = self._viewer()
        viewer_closed = []
        hidden = []
        # Spy di-level Python, bukan `win.closed.connect(...)`: stub ini
        # melewati QWidget.__init__, jadi sinyal Qt-nya tidak bisa dipakai.
        # Yang diuji adalah PEMANGGILAN self.hide() (bukan self.close()),
        # dan close() yang tertunda sampai halaman ditutup.
        win.close = lambda *a, **k: viewer_closed.append(True)
        win.hide = lambda *a, **k: hidden.append(True)

        with mock.patch.object(ev.config, "clear_answers"), \
                mock.patch.object(ev.config, "mark_submitted"), \
                mock.patch.object(ev.api, "complete_exam"):
            win._cleanup_after_submit("Hebat!")

        self.app.processEvents()
        page = win._congrats_ref
        self.assertTrue(page.isVisible(), "halaman selamat tidak tampil")
        self.assertEqual(
            hidden, [True],
            "viewer harus disembunyikan (hide), bukan ditutup (close): "
            "close() menembakkan `closed` sinkron dan dialog konfigurasi "
            "siswa berikutnya menimpa halaman selamat",
        )
        self.assertEqual(
            viewer_closed, [],
            "viewer ditutup bersamaan dengan halaman — dialog konfigurasi "
            "akan menimpa halaman selamat (bug C1)",
        )
        self.assertEqual(page.congrats_text(), "Hebat!")
        # Identitas halaman dari pemetaan kanonik.
        self.assertIn("Andi", page.identity_text())
        page.close()
        self.addCleanup(page.deleteLater)

    def test_viewer_closes_when_the_page_is_closed(self):
        from examvan.ui import exam_viewer as ev

        win = self._viewer()
        viewer_closed = []
        win.close = lambda *a, **k: viewer_closed.append(True)
        win.hide = lambda *a, **k: None

        with mock.patch.object(ev.config, "clear_answers"), \
                mock.patch.object(ev.config, "mark_submitted"), \
                mock.patch.object(ev.api, "complete_exam"):
            win._cleanup_after_submit("Hebat!")

        page = win._congrats_ref
        self.addCleanup(page.deleteLater)
        page.close()
        self.app.processEvents()
        self.assertEqual(
            viewer_closed, [True],
            "menutup halaman selamat harus menutup viewer (page_closed)",
        )


class PublicResultsGateTestCase(unittest.TestCase):
    """H6+H7: gating publikasi nilai + wording jujur cakupan kelas."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

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

    def test_default_is_public(self):
        page = self._page()
        page.show()
        self.app.processEvents()
        self.assertTrue(page.copy_button().isVisible())
        self.assertTrue(page.copy_button().isEnabled())

    def test_unpublished_hides_the_copy_button(self):
        page = self._page(public_results=False)
        page.show()
        self.app.processEvents()
        self.assertTrue(page.copy_button().isHidden())
        self.assertFalse(page.copy_button().isEnabled())

    def test_unpublished_note_is_honest(self):
        from PyQt5.QtWidgets import QLabel

        page = self._page(public_results=False)
        notes = [lab.text() for lab in page.findChildren(QLabel)]
        self.assertTrue(
            any("tidak dipublikasikan" in t.lower() for t in notes),
            "catatan harus jujur bahwa nilai tidak dipublikasikan",
        )

    def test_note_and_tooltip_state_the_class_wide_scope(self):
        from PyQt5.QtWidgets import QLabel

        page = self._page()
        self.assertIn("seluruh kelas", page.copy_button().toolTip())
        notes = [lab.text() for lab in page.findChildren(QLabel)]
        self.assertTrue(
            any("seluruh kelas" in t for t in notes),
            "catatan link harus menyebut cakupan seluruh kelas",
        )

    def test_exam_model_parses_public_results(self):
        from examvan.models import Exam

        self.assertTrue(Exam.from_json({"public_results": 1}).public_results)
        self.assertFalse(Exam.from_json({"public_results": 0}).public_results)
        # Absen → True (perilaku lama).
        self.assertTrue(Exam.from_json({}).public_results)


class CongratsPolishTestCase(unittest.TestCase):
    """M4/M5/L-congrats: wrap, ukuran minimum, sanitasi link, guard URL."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

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

    def test_identity_values_wrap(self):
        from PyQt5.QtWidgets import QLabel

        page = self._page(student_name="Nama Yang Sangat Panjang Sekali")
        vals = [
            lab for lab in page.findChildren(QLabel)
            if lab.text() == "Nama Yang Sangat Panjang Sekali"
        ]
        self.assertTrue(vals)
        for val in vals:
            self.assertTrue(val.wordWrap())

    def test_minimum_size_is_small(self):
        page = self._page()
        self.assertLessEqual(page.minimumWidth(), 360)
        self.assertLessEqual(page.minimumHeight(), 280)

    def test_format_chars_are_stripped(self):
        # M8: Cf (U+200B zero-width), Co (U+E000 private use),
        # Cs (lone surrogate) — semua dibuang, whitespace sah dipertahankan.
        page = self._page(congrats_message="A\u200bB\ue000C\ud800D")
        text = page.congrats_text()
        self.assertEqual(text, "ABCD")
        self.assertNotIn("\u200b", text)

    def test_link_label_is_sanitized_without_changing_the_url(self):
        page = self._page()
        self.assertIn(page.result_url(), page._link_label.text())
        self.assertEqual(
            page._link_label.text(),
            f"Link hasil: {page.result_url()}",
        )

    def test_closing_without_url_leaves_the_clipboard_alone(self):
        page = self._page(exam_token="")
        QApplication.clipboard().setText("catatan pribadi")
        page.close()
        self.assertEqual(
            QApplication.clipboard().text(), "catatan pribadi")


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
                {"nama": "SITI"}, "ABCD1234",
            )

        self.assertTrue(poll.called, "polling tidak dipanggil sama sekali")
        self.assertEqual(
            poll.call_args.kwargs.get("initial_congrats"),
            "Hebat, kerja bagus!",
            "_submit_thread tidak meneruskan congrats_message dari respons "
            "202, sehingga halaman selamat menampilkan teks bawaan server",
        )


class ServerTextSafetyTestCase(unittest.TestCase):
    """Teks server dirender sebagai plain-text yang dibersihkan."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _page(self, **kwargs):
        from PyQt5.QtCore import Qt

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

    def test_server_labels_use_plain_text_format(self):
        from PyQt5.QtCore import Qt

        page = self._page(congrats_message="<b>Hai</b>")
        self.assertEqual(page._congrats.textFormat(), Qt.PlainText)
        self.assertEqual(page._exam_badge.textFormat(), Qt.PlainText)
        self.assertEqual(page._link_label.textFormat(), Qt.PlainText)

    def test_html_in_teacher_message_is_not_rendered(self):
        page = self._page(congrats_message="<b>Hebat</b>")
        self.assertIn("<b>Hebat</b>", page.congrats_text())

    def test_teacher_message_is_capped(self):
        page = self._page(congrats_message="A" * 5000)
        self.assertLessEqual(len(page.congrats_text()), 2000)

    def test_control_and_bidi_chars_are_stripped(self):
        page = self._page(congrats_message="Ha\x00bat\u202eX")
        text = page.congrats_text()
        self.assertNotIn("\x00", text)
        self.assertNotIn("\u202e", text)
        self.assertEqual(text, "HabatX")

    def test_link_label_is_not_mouse_selectable(self):
        from PyQt5.QtCore import Qt

        page = self._page()
        # Tombol Copy tetap satu-satunya jalan salin.
        self.assertEqual(
            page._link_label.textInteractionFlags(), Qt.NoTextInteraction)
        self.assertTrue(page.copy_button().isEnabled())

    def test_card_has_min_and_max_width_not_fixed(self):
        from PyQt5.QtWidgets import QWidget

        page = self._page()
        card = page.findChild(QWidget, "congratsCard")
        self.assertIsNotNone(card)
        self.assertEqual(card.maximumWidth(), 560)
        self.assertEqual(card.minimumWidth(), 420)

    def test_note_and_version_inherit_theme_color(self):
        from PyQt5.QtWidgets import QLabel

        page = self._page()
        for label in page.findChildren(QLabel):
            style = label.styleSheet() or ""
            if "font-size: 11px" in style:
                self.assertNotIn("#94a3b8", style)

    def test_null_icon_pixmap_leaves_an_empty_label(self):
        from PyQt5.QtGui import QPixmap
        from PyQt5.QtWidgets import QLabel, QStyle

        with mock.patch.object(
            QStyle, "standardIcon", return_value=mock.Mock(
                pixmap=mock.Mock(return_value=QPixmap()))
        ):
            fresh = CongratulationsWindow(
                server_url="https://examvan.my.id", exam_token="T")
            self.addCleanup(fresh.deleteLater)
        # Label ikon kosong (tidak ada emoji fallback/tofu).
        self.assertTrue(
            any(not lab.text().strip() for lab in fresh.findChildren(QLabel)),
        )

    def test_reject_is_gone(self):
        self.assertFalse(hasattr(CongratulationsWindow, "reject"))

    def test_hide_clears_the_clipboard_and_stops_countdown(self):
        from PyQt5.QtGui import QHideEvent

        clock = _FakeClock().patch(self)
        QApplication.clipboard().clear()
        page = self._page()
        page.show()
        self.app.processEvents()
        page.copy_link()
        self.assertEqual(
            QApplication.clipboard().text(), page.result_url())
        page.hide()
        self.app.processEvents()
        self.assertEqual(QApplication.clipboard().text(), "")
        timer = getattr(page, "_timer", None)
        if timer is not None:
            self.assertFalse(timer.isActive())

    def test_show_restarts_the_timer_while_a_copy_is_guarded(self):
        # Salin selagi halaman hidden (timer lalu mati, mis. suspend),
        # lalu tampilkan: showEvent harus me-restart karena _copied_at set.
        _FakeClock().patch(self)
        QApplication.clipboard().clear()
        page = self._page()
        page.copy_link()
        page._stop_timer()
        self.assertIsNotNone(page._copied_at)
        page.show()
        self.app.processEvents()
        timer = getattr(page, "_timer", None)
        self.assertIsNotNone(timer)
        self.assertTrue(timer.isActive())
        page.close()

    def test_fractional_elapsed_does_not_truncate(self):
        # 29,9 detik berlalu: versi `int()` menulis sisa "1s" padahal
        # tinggal 0,1 detik; float compare tetap menampilkan hitung mundur
        # dan BELUM membersihkan.
        clock = _FakeClock().patch(self)
        QApplication.clipboard().clear()
        page = self._page()
        page.copy_link()
        clock.advance(CLIPBOARD_CLEAR_SECONDS - 0.1)
        page._tick()
        self.assertEqual(
            QApplication.clipboard().text(), page.result_url())
        self.assertIn("Copy Link (", page.copy_button().text())


if __name__ == "__main__":
    unittest.main()
