"""Download PDF yang GAGAL setelah siswa keluar tidak boleh membangun lembar jawaban.

Bug — `_on_pdf_error` lupa mengecek `_abandoned`
-------------------------------------------------
`_on_pdf_ready` (`exam_viewer.py`) punya:

    if getattr(self, "_abandoned", False):
        self._discard_pdf()
        return

`_on_pdf_error` tidak punya guard itu, padahal keduanya dipanggil dari
thread yang sama untuk unduhan yang sama. Akibatnya: siswa menekan
"Keluar Ujian" sementara PDF masih diunduh, unduhan baru GAGAL beberapa
detik kemudian → `_build_answer_sheet()` tetap berjalan di jendela yang
sudah ditutup (membangun widget jawaban untuk-soal yang tidak akan pernah
dilihat siapa pun), dan `_sig_status` menembak ke UI yang sudah tidak
terlihat.

Bukan kerentanan kebocoran data (PDF-nya sendiri tidak dirender), tapi
pekerjaan sia-sia pada widget yang sudah ditinggalkan, dengan status yang
ditulis ke dialog konfigurasi yang sedang dipakai siswa berikutnya.

Test memakai viewer sungguhan dan memanggil `_on_pdf_error` dalam dua
keadaan: `_abandoned` True (harus diam) dan False (harus tetap membangun
lembar jawaban — penjagaan tidak boleh mematikan jalur "PDF gagal tapi soal
tetap bisa dijawab").
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

from PyQt5.QtWidgets import QApplication

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer as ev_mod

APP = QApplication.instance() or QApplication([])

QUESTIONS = [
    {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
    {"number": 2, "type": "single_choice", "choices": ["X", "Y"]},
]


class _NoopSecurity:
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


class PdfErrorAfterExitTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r6-pdf-"))
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

        exam = Exam.from_json({
            "id": 7, "name": "Ujian", "status": "active",
            "security_level": "medium", "questions": QUESTIONS,
        })
        self.exam = exam
        self.win = ev_mod.ExamViewerWindow(
            exam=exam, server_url="https://exam.example", token="ABCD1234",
            identity_data={"nama": "Budi"},
        )
        self.addCleanup(self.win.hide)
        self.addCleanup(self.win.deleteLater)
        APP.processEvents()
        # Lembar jawaban dikosongkan lebih dulu supaya yang diukur benar-benar
        # panggilan `_build_answer_sheet`, bukan sisa pekerjaan thread unduhan.
        self.win._answer_sheet = _FreshSheet()

    def test_a_failed_download_after_exit_does_not_build_the_sheet(self):
        self.win._abandoned = True
        with mock.patch.object(self.win, "_build_answer_sheet") as build:
            self.win._on_pdf_error("timeout jaringan")
        build.assert_not_called()

    def test_a_failed_download_after_exit_keeps_the_window_quiet(self):
        # `_discard_pdf()` menandai `_abandoned`; status tidak boleh ditulis
        # ke label milik jendela yang sudah ditinggalkan.
        self.win._abandoned = True
        self.win._lbl_status = _RecordingLabel()
        self.win._on_pdf_error("timeout jaringan")
        self.assertEqual(
            self.win._lbl_status.text(), "",
            "status unduhan yang gagal ditulis ke jendela yang sudah "
            "ditinggalkan siswa — dialog konfigurasi milik siswa berikutnya "
            "yang menanggung pesannya",
        )

    def test_the_partial_download_is_still_discarded_after_exit(self):
        # `_load_pdf_thread` sudah menaruh `dest` di `self._pdf_path` SEBELUM
        # download; berkas separuh selesai dari kegiatan yang sudah ditinggalkan
        # tidak boleh menunggu di %TEMP% untuk PC lab berikutnya.
        path = Path(tempfile.mkdtemp(prefix="examvan-r6-partial-")) / "exam.pdf"
        path.write_bytes(b"%PDF-1.4 parsial")
        self.addCleanup(shutil.rmtree, path.parent, True)
        self.win._pdf_path = str(path)
        self.win._abandoned = True

        self.win._on_pdf_error("terputus di tengah")

        self.assertFalse(
            path.exists(),
            "berkas PDF separuh yang gagal diunduh tetap tertinggal di "
            "%TEMP% setelah siswa keluar",
        )

    def test_a_failed_download_while_the_exam_runs_still_builds_the_sheet(self):
        # Penjagaan tidak boleh mematikan jalur ini: PDF gagal bukan alasan
        # siswa kehilangan lembar jawabannya.
        self.win._abandoned = False
        with mock.patch.object(self.win, "_build_answer_sheet") as build:
            self.win._on_pdf_error("timeout jaringan")
        build.assert_called_once_with()

    def test_a_failed_download_while_the_exam_runs_still_explains_itself(self):
        self.win._abandoned = False
        self.win._lbl_status = _RecordingLabel()
        self.win._on_pdf_error("timeout jaringan")
        self.assertIn("timeout jaringan", self.win._lbl_status.text())
        self.assertIn("Gagal", self.win._lbl_status.text())


class _FreshSheet:
    """Pengganti lembar jawaban yang mencatat `build_from_questions`."""

    def __init__(self) -> None:
        self.built = []

    def build_from_questions(self, questions):
        self.built.append(questions)

    def duplicate_question_numbers(self):
        return []

    def broken_question_numbers(self):
        return []


class _RecordingLabel:
    def __init__(self) -> None:
        self._text = ""

    def setText(self, text):  # noqa: N802 (Qt API)
        self._text = text

    def text(self):
        return self._text


if __name__ == "__main__":
    unittest.main()