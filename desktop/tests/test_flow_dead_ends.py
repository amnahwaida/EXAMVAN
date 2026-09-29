"""Tiga dead end / kehilangan data di alur ujian Windows.

Temuan ronde 4 (30 September 2026), dari review alur end-to-end
`config dialog -> identity -> approval -> exam -> answer -> submit`.

P1 (HIGH) — PDF gagal: siswa tidak bisa menjawab sama sekali
------------------------------------------------------------
`AnswerSheetWidget.build_from_questions()` dipanggil HANYA di dalam cabang
sukses `_on_pdf_ready` (`exam_viewer.py:328-331`):

    if self._pdf_path and self._pdf_viewer.load_pdf(self._pdf_path):
        self._lbl_status.setText("PDF siap")
        self._answer_sheet.build_from_questions(self._exam.questions)
    else:
        self._lbl_status.setText("Gagal memuat PDF")

Lembar jawaban sebenarnya tidak butuh PDF sama sekali — ia dibangun dari
`exam.questions`. Jadi satuhiccup jaringan saat download, atau satu file PDF
korup, membuat:

  * 0 widget jawaban although the exam punya N soal,
  * `get_answered_count()` -> (0, 0), jadi dialog konfirmasi mengarang
    "Anda telah menjawab 0 dari 0 soal",
  * tombol submit TETAP aktif -> siswa bisa mengirim jawaban kosong.

Bukti eksekusi (2 soal di config):

    answer widgets dibangun : 0
    get_answered_count()    : (0, 0)
    tombol submit enabled  : True

Untuk siswa itu akhir ujiannya: tidak ada yang bisa diklik, tidak ada
tombol retry download, dan jalan keluar yang satu-satunya adalah
mengirim jawaban kosong.

P2 (HIGH) — Batal di dialog persetujuan = buntu total
------------------------------------------------------
`_on_connect` men-disable `btn_connect` (`server_config.py:208`). Jalur sukses
hanya meng-emit `_sig_show_identity` (`:273`) — TIDAK pernah
`_sig_enable_btn`. Jadi ketika siswa menekan "Batal" di
`WaitingApprovalDialog`, `on_exam_selected` melakukan `dialog.show()` dengan
tombol "Hubungkan" MASIH nonaktif. Tidak ada cancel/close, tidak ada reset:

    dialog.show() dipanggil?      True
    btn_connect masih enabled?    False

Siswa harus menutup aplikasi dan mengetik ulang. Meminta izin lagi dari
dialog persetujuan tidak mungkin karena dialog itu sudah tertutup.

P3 (MEDIUM) — nomor soal duplikat = satu blok jawaban dibuang diam-diam
------------------------------------------------------------------------
`_answer_widgets` di-key dengan `str(int(q["number"]))`, dan
`build_from_questions` menimpa kunci yang sama. Dua soal bernomor "1" →
DUA blok tergambar di layar, tapi hanya SATU yang tercatat:

    widgets di _answer_widgets: {'1': ('single', <QButtonGroup ...>)}
    jumlah blok di layar      : 2

Jawaban blok pertama menimpa / ditimpa blok kedua, dan `restore_answers`
juga tidak bisa memulihkannya karena entri pertamanya sudah hilang.
Server hanya memvalidasi kunci jawaban (`validateQuestionKeys`,
admin/exams.go:1694) — nomor duplikat tidak pernah dicek, jadi guru bisa
menyimpannya dan tidak ada yang memberi tahu.
"""

from __future__ import annotations

import os
import sys
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QDialog, QWidget

from examvan import __main__ as main_mod
from examvan.models import Exam
from examvan.ui import exam_viewer
from examvan.ui.answer_sheet import AnswerSheetWidget
from examvan.ui.exam_viewer import ExamViewerWindow

APP = QApplication.instance() or QApplication([])

QUESTIONS = [
    {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
    {"number": 2, "type": "single_choice", "choices": ["X", "Y"]},
    {"number": 3, "type": "matching", "left_items": ["a", "b"],
     "right_items": ["p", "q", "r"]},
]


class _NoopSecurity:
    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()

    def activate(self):
        pass

    def deactivate(self):
        pass


def _make_viewer(questions=None, download_error=None, load_ok=True):
    exam = Exam.from_json({
        "id": 7, "name": "Ujian", "status": "active",
        "security_level": "high",
        "questions": questions if questions is not None else QUESTIONS,
    })
    with mock.patch("examvan.ui.exam_viewer.SecurityEnforcer", _NoopSecurity), \
         mock.patch("examvan.ui.exam_viewer.ExamWebSocket"), \
         mock.patch("examvan.ui.exam_viewer.api.download_pdf",
                    side_effect=download_error):
        win = ExamViewerWindow(
            exam=exam, server_url="https://exam.example",
            token="ABCD1234", identity_data={"nama": "Budi"},
        )
    win._auto_submit = mock.Mock()
    win._pdf_viewer.load_pdf = mock.Mock(return_value=load_ok)
    return win, exam


# ---------------------------------------------------------------------------
# P1 — PDF gagal tidak boleh membuat lembar jawaban kosong
# ---------------------------------------------------------------------------


class PdfFailureStillAllowsAnsweringTest(unittest.TestCase):
    def tearDown(self):
        for w in getattr(self, "_windows", []):
            w.close()

    def _viewer(self, **kw):
        win, exam = _make_viewer(**kw)
        self._windows = getattr(self, "_windows", []) + [win]
        return win, exam

    def test_download_failure_still_builds_the_answer_sheet(self):
        win, exam = self._viewer(download_error=OSError("timeout jaringan"))
        APP.processEvents()
        win._on_pdf_error("timeout jaringan")
        APP.processEvents()

        self.assertEqual(
            len(win._answer_sheet._answer_widgets), len(exam.questions),
            "siswa tidak bisa menjawab karena download PDF gagal",
        )

    def test_download_failure_never_reports_zero_questions(self):
        win, exam = self._viewer(download_error=OSError("timeout"))
        APP.processEvents()
        win._on_pdf_error("timeout")
        APP.processEvents()
        answered, total = win._answer_sheet.get_answered_count()
        self.assertEqual(
            total, len(exam.questions),
            "dialog konfirmasi akan mengarang '0 dari 0 soal'",
        )

    def test_corrupt_pdf_still_builds_the_answer_sheet(self):
        # Download sukses tapi file tidak bisa dirender (PDF rusak, dipotong
        # oleh proxy, dll.). Cabang `else` lama sama sekali tidak membangun
        # lembar jawaban.
        win, exam = self._viewer(load_ok=False)
        win._pdf_path = "/tmp/examvan-not-a-real.pdf"
        APP.processEvents()
        win._on_pdf_ready()
        APP.processEvents()
        self.assertEqual(len(win._answer_sheet._answer_widgets), len(exam.questions))

    def test_student_can_answer_without_a_pdf(self):
        win, _ = self._viewer(download_error=OSError("timeout"))
        APP.processEvents()
        win._on_pdf_error("timeout")
        APP.processEvents()
        win._answer_sheet._on_answer_changed("1", "A")
        self.assertEqual(win._answer_sheet.get_answers()["1"], "A")

    def test_pdf_status_and_answer_sheet_are_independent(self):
        # Status PDF boleh menderrık, tapi lembar jawaban harus tetap utuh.
        win, exam = self._viewer(download_error=OSError("boom"))
        APP.processEvents()
        win._on_pdf_error("boom")
        APP.processEvents()
        self.assertIn("Gagal", win._lbl_status.text())
        self.assertEqual(len(win._answer_sheet._answer_widgets), len(exam.questions))

    def test_build_is_idempotent(self):
        # Dipanggil ulang tidak boleh menghapus jawaban yang sudah masuk.
        win, exam = self._viewer()
        APP.processEvents()
        win._on_pdf_error("boom")
        win._answer_sheet._on_answer_changed("1", "A")
        win._answer_sheet.build_from_questions(exam.questions)
        self.assertEqual(win._answer_sheet.get_answers().get("1"), "A")


# ---------------------------------------------------------------------------
# P2 — batal di dialog persetujuan tidak boleh jadi buntu
# ---------------------------------------------------------------------------


class _Sig:
    def __init__(self):
        self.slots = []

    def connect(self, slot):
        self.slots.append(slot)


def _real_config_dialog():
    from examvan import config
    from examvan.ui.server_config import ServerConfigDialog

    with mock.patch.object(config, "_CONFIG_DIR"), \
         mock.patch.object(config, "_CONFIG_FILE"):
        config._cache = {"remember_url": True, "server_url": "",
                         "exam_token": "ABCD1234", "identity_data": {}}
        dlg = ServerConfigDialog()
    config._cache = None
    #-condition after a successful connect: button disabled.
    dlg.btn_connect.setEnabled(False)
    dlg.exam_selected = _Sig()
    return dlg


class CancelApprovalTest(unittest.TestCase):
    def _run_rejected_approval(self):
        dialog = _real_config_dialog()
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        waiting = mock.Mock()
        waiting.exec_.return_value = QDialog.Rejected

        with mock.patch.object(main_mod, "_setup_logging"), \
             mock.patch.object(main_mod, "_recover_gnome_settings"), \
             mock.patch.object(main_mod, "_recover_windows_settings"), \
             mock.patch.object(main_mod, "atexit"), \
             mock.patch.object(main_mod, "signal"), \
             mock.patch("PyQt5.QtWidgets.QApplication"), \
             mock.patch("examvan.ui.styles.apply_theme"), \
             mock.patch("examvan.ui.styles.is_system_dark", return_value=False), \
             mock.patch("examvan.ui.server_config.ServerConfigDialog",
                        return_value=dialog), \
             mock.patch("examvan.ui.exam_viewer.ExamViewerWindow") as viewer_cls, \
             mock.patch("examvan.ui.waiting_approval.WaitingApprovalDialog",
                        return_value=waiting), \
             mock.patch.object(sys, "exit"), \
             mock.patch.object(sys, "argv", ["examvan"]), \
             mock.patch.object(main_mod, "_maximize_window") as mw:
            main_mod.main()
            dialog.exam_selected.slots[0](exam, "https://x", {"nama": "Budi"})
        return dialog, viewer_cls, mw

    def test_no_exam_window_is_opened(self):
        _, viewer_cls, _ = self._run_rejected_approval()
        self.assertFalse(viewer_cls.called)

    def test_config_dialog_is_presented_again(self):
        dialog, _, mw = self._run_rejected_approval()
        self.assertTrue(
            any(c.args and c.args[0] is dialog for c in mw.call_args_list),
            "dialog konfigurasi tidak dikembalikan ke siswa",
        )

    def test_connect_button_is_re_enabled(self):
        # Tanpa ini tombol "Hubungkan" tetap mati: tidak ada cancel, tidak
        # ada reset, tidak ada jalan lain kecuali menutup aplikasi.
        dialog, _, _ = self._run_rejected_approval()
        self.assertTrue(
            dialog.btn_connect.isEnabled(),
            "siswa membuntu setelah membatalkan dialog persetujuan",
        )

    def test_stale_status_is_cleared(self):
        dialog, _, _ = self._run_rejected_approval()
        dialog.lbl_status.setText("Menunggu Persetujuan")
        dialog.enable_connect()
        self.assertEqual(dialog.lbl_status.text(), "")

    def test_enable_connect_is_safe_to_call_twice(self):
        dialog, _, _ = self._run_rejected_approval()
        dialog.enable_connect()
        dialog.enable_connect()
        self.assertTrue(dialog.btn_connect.isEnabled())

    def test_token_field_keeps_its_value_for_a_retry(self):
        dialog, _, _ = self._run_rejected_approval()
        self.assertTrue(dialog.input_token.text().strip())


# ---------------------------------------------------------------------------
# P3 — nomor soal duplikat
# ---------------------------------------------------------------------------


class DuplicateQuestionNumberTest(unittest.TestCase):
    def test_every_question_gets_its_own_answer_slot(self):
        sheet = AnswerSheetWidget()
        sheet.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        blocks = sheet._container_layout.count() - 1     # dikurangi addStretch
        self.assertEqual(
            len(sheet._answer_widgets), blocks,
            "ada blok di layar yang jawabannya tidak akan pernah dikirim",
        )

    def test_duplicate_numbers_are_reported_not_silently_merged(self):
        # Dulu: dua blok tergambar, satu entri _answer_widgets, jawaban
        # blok pertama hilang diam-diam. Guru harus diberi tahu.
        sheet = AnswerSheetWidget()
        dupes = sheet.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        self.assertTrue(
            dupes, "nomor soal duplikat harus dilaporkan, bukan digabung diam-diam"
        )

    def test_normal_questions_report_nothing(self):
        sheet = AnswerSheetWidget()
        dupes = sheet.build_from_questions(QUESTIONS)
        self.assertFalse(dupes)

    def test_answers_of_both_duplicate_blocks_are_kept(self):
        sheet = AnswerSheetWidget()
        sheet.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        nums = list(sheet._answer_widgets)
        self.assertEqual(len(nums), 2)
        self.assertEqual(len(set(nums)), 2)

    def test_duplicate_is_surfaced_to_the_student(self):
        # Kalau hanya di-log, guru dan siswa tidak pernah tahu. Peringatan
        # muncul di dialog konfirmasi pengumpulan — titik terakhir di mana
        # siswa masih bisa melapor sebelum jawaban terkirim.
        win, _ = _make_viewer(questions=[
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        try:
            APP.processEvents()
            win._on_pdf_error("x")
            APP.processEvents()
            self.assertTrue(win._exam_warnings)
            self.assertIn("1", win._exam_warnings[0])
        finally:
            win.close()

    def test_no_warning_for_a_healthy_exam(self):
        win, _ = _make_viewer()
        try:
            APP.processEvents()
            win._on_pdf_error("x")
            APP.processEvents()
            self.assertEqual(win._exam_warnings, [])
        finally:
            win.close()

    def test_warning_reaches_the_confirm_dialog(self):
        win, _ = _make_viewer(questions=[
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        try:
            APP.processEvents()
            win._on_pdf_error("x")
            APP.processEvents()
            win._on_submit = win._on_submit
            with mock.patch.object(exam_viewer.QMessageBox, "question") as qb:
                qb.return_value = exam_viewer.QMessageBox.No
                win._on_submit()
            text = qb.call_args.args[2]
            self.assertIn("1", text)
        finally:
            win.close()

    def test_submitted_payload_keeps_unique_keys(self):
        # Kontrak server: answers adalah map {nomor: nilai}. Kunci yang sama
        # dua kali berarti satu menimpa yang lain.
        sheet = AnswerSheetWidget()
        sheet.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 1, "type": "single_choice", "choices": ["X", "Y"]},
        ])
        answers = sheet.get_answers()
        self.assertEqual(len(answers), len(set(answers)))


if __name__ == "__main__":
    unittest.main()
