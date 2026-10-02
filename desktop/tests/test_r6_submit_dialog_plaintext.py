"""Dialog tertinggi-konsekuensi dalam ujian: teks server SELALU teks polos.

Bug — dua dialog masih me-render teks server sebagai rich text
----------------------------------------------------------
`_on_submit_result` sudah dikeraskan: kotaknya dibangun eksplisit lalu
`setTextFormat(Qt.PlainText)`. Dua dialog lain di posisi yang justru lebih
tinggi belum:

  * `_on_submit` — dialog konfirmasi "Anda telah menjawab N dari M soal", yang
    teksnya menyisipkan `_exam_warnings`. Isi `_exam_warnings` diambil
    VERBATIM dari `exam.questions` milik server (`duplicate_question_numbers()`
    dan `broken_question_numbers()`), jadi nomor soal bertanda `<b>`, `<img …>`,
    atau `<a href=…>` dari salah ketik guru — atau server yang dikonfigurasi
    salah — dirender sebagai markup oleh `QMessageBox.question()` (format
    default `Qt.AutoText`).
  * `closeEvent` level low — dialog "Keluar Ujian".

`_on_submit_result` menampilkan nomor yang sama persis (pesan error), jadi
markup yang lolos ke `_on_submit` tidak akan terlihat sebagai bug terpisah di
lapangan: dialog konfirmasi terlihat "aneh" dan dianggap salah style.

Test memanggil `_on_submit` / `closeEvent` dengan `QMessageBox` sungguhan
(QWidget asli, `exec_` dicatat) supaya yang diperiksa adalah `textFormat()`
yang benar-benar dipakai Qt untuk merender — bukan hasattr.
"""

from __future__ import annotations

import contextlib
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import Qt
from PyQt5.QtGui import QCloseEvent
from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer as ev_mod

APP = QApplication.instance() or QApplication([])

QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]


class _NoopSecurity:
    """Pengganti SecurityEnforcer dengan antarmuka yang sama.

    `pause_focus_guard()` wajib ada: `_modal_dialog_guard()` di viewer
    memanggilnya di setiap dialog modal, dan test double yang tidak memilikinya
    gagal dengan AttributeError yang menyesatkan — seolah ada bug produksi.
    """

    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()

    def activate(self):
        pass

    def deactivate(self):
        pass

    def pause_focus_guard(self):
        return contextlib.nullcontext()

    def reassert_capture_protection(self):
        pass

    def clear_clipboard_now(self):
        pass


class _RecordingMessageBox(QMessageBox):
    """QMessageBox sungguhan yang mencatat dirinya dan TIDAK memblokir.

    Dua jalur masuk dicatat:

      * `question()` statis — bentuk yang dipakai produksi SEBELUM perbaikan.
        Kotak tetap dibangun (dengan default `Qt.AutoText`) supaya test bisa
        memeriksa format yang benar-benar akan dipakai Qt merender, dan supaya
        test tidak menggantung di dialog modal sungguhan.
      * konstruktor + `exec_()` — bentuk yang dipakai produksi SESUDAH
        perbaikan.
    """

    instances: list = []
    reply = QMessageBox.No

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        _RecordingMessageBox.instances.append(self)

    def exec_(self):  # noqa: N802 (Qt API)
        return type(self).reply

    @classmethod
    def question(cls, parent, title, text, buttons=None, default=None,
                 *args, **kwargs):
        box = cls(QMessageBox.Question, title, text, buttons, parent)
        return cls.reply

    @classmethod
    def information(cls, parent, title, text, *args, **kwargs):
        return cls.reply

    @classmethod
    def warning(cls, parent, title, text, *args, **kwargs):
        return cls.reply


class _ConfigSandbox(unittest.TestCase):
    """Arahkan config ke direktori sementara + matikan jaringan/backend."""

    def setUp(self) -> None:
        _RecordingMessageBox.instances = []
        _RecordingMessageBox.reply = QMessageBox.No
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r6-plaintext-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
            mock.patch.object(ev_mod, "SecurityEnforcer", _NoopSecurity),
            mock.patch.object(ev_mod, "ExamWebSocket"),
            mock.patch.object(ev_mod.api, "download_pdf",
                              side_effect=OSError("offline")),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self._windows = []

    def tearDown(self) -> None:
        for box in _RecordingMessageBox.instances:
            try:
                box.close()
                box.deleteLater()
            except Exception:
                pass
        for win in self._windows:
            # hide(), BUKAN close(): close() pada level low memanggil
            # `closeEvent` → dialog keluar sungguhan yang tidak pernah
            # dipatch lagi di sini → test menggantung selamanya.
            try:
                win.hide()
                win.deleteLater()
            except Exception:
                pass
        APP.processEvents()

    def _viewer(self, level: str = "high"):
        exam = Exam.from_json({
            "id": 7, "name": "Ujian", "status": "active",
            "security_level": level, "questions": QUESTIONS,
        })
        win = ev_mod.ExamViewerWindow(
            exam=exam, server_url="https://exam.example", token="ABCD1234",
            identity_data={"nama": "Budi"},
        )
        self._windows.append(win)
        win._auto_submit = mock.Mock()
        APP.processEvents()
        return win, exam

    def _boxes(self) -> list:
        """Box yang benar-benardialog konfirmasi (ada teksnya)."""
        return [b for b in _RecordingMessageBox.instances if b.text()]


class SubmitConfirmPlainTextTestCase(_ConfigSandbox):
    """`_on_submit`: konfirmasi pengumpulan harus PlainText."""

    def test_submit_confirm_box_is_plain_text(self):
        win, _exam = self._viewer()
        with mock.patch.object(ev_mod, "QMessageBox", _RecordingMessageBox):
            win._on_submit()

        boxes = self._boxes()
        self.assertTrue(boxes, "dialog konfirmasi pengumpulan tidak pernah dibangun")
        self.assertEqual(
            boxes[0].textFormat(),
            Qt.PlainText,
            "dialog konfirmasi pengumpulan merender teks server sebagai rich "
            "text — nomor soal dari config server bisa menyuntik markup ke "
            "dialog tertinggi-konsekuensi dalam ujian",
        )

    def test_server_supplied_warning_renders_literally(self):
        # Nomor soal diambil VERBATIM dari config server. Salah ketik guru
        # adalah kasus lapangan yang paling mungkin, jadi persis string itu
        # yang harus muncul di layar — bukan <b> yang dirender jadi tebal.
        win, _exam = self._viewer()
        win._exam_warnings = [
            "Perhatian: soal nomor <b>1</b> & <i>2</i> bernomor ganda. "
            "Satu nomor hanya bisa menyimpan satu jawaban — hubungi pengawas.",
        ]
        with mock.patch.object(ev_mod, "QMessageBox", _RecordingMessageBox):
            win._on_submit()

        boxes = self._boxes()
        self.assertTrue(boxes)
        self.assertIn(
            "<b>1</b> & <i>2</i>", boxes[0].text(),
            "peringatan dari server hilang dari teks dialog konfirmasi",
        )
        self.assertEqual(
            boxes[0].textFormat(), Qt.PlainText,
            "markup di dalam peringatan ikut dirender sebagai HTML",
        )

    def test_a_rejected_confirm_does_not_submit(self):
        # Jaga agar pengerasan format tidak mengubah perilaku: No = batal.
        win, _exam = self._viewer()
        _RecordingMessageBox.reply = QMessageBox.No
        with mock.patch.object(ev_mod, "QMessageBox", _RecordingMessageBox), \
             mock.patch.object(win, "_do_submit") as do_submit:
            win._on_submit()
        do_submit.assert_not_called()


class LowLevelExitPlainTextTestCase(_ConfigSandbox):
    """`closeEvent` low-level: dialog keluar juga PlainText."""

    def _open_exit_dialog(self, win) -> None:
        event = QCloseEvent()
        with mock.patch.object(ev_mod, "QMessageBox", _RecordingMessageBox):
            win.closeEvent(event)
        APP.processEvents()

    def test_low_level_exit_box_is_plain_text(self):
        win, _exam = self._viewer(level="low")
        self._open_exit_dialog(win)

        boxes = self._boxes()
        self.assertTrue(boxes, "dialog keluar low-level tidak pernah dibangun")
        self.assertEqual(
            boxes[0].textFormat(), Qt.PlainText,
            "dialog keluar low-level masih merender teks sebagai rich text",
        )

    def test_the_exit_box_is_plain_text_even_without_any_warning(self):
        # Tanpa `_exam_warnings` teksnya konstan, tapi formatnya tetap harus
        # dikunci supaya pesan yang ditambahkan di masa depan tidak membuka
        # kembali celah yang sama.
        win, _exam = self._viewer(level="low")
        win._exam_warnings = []
        self._open_exit_dialog(win)

        boxes = self._boxes()
        self.assertTrue(boxes)
        self.assertEqual(boxes[0].textFormat(), Qt.PlainText)


if __name__ == "__main__":
    unittest.main()