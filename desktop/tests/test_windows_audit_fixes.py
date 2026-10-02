"""Enam temuan dari audit ulang Windows, masing-masing dengan testnya.

Temuan yang ditutup file ini:

1. Thread submit tidak punya guard -- satu exception mematikan thread tanpa
   menyalakan `_sig_submit_result`, jadi `_submitting` mengunci True
   selamanya: tombol submit mati, `closeEvent` menolak menutup, deadline
   auto-submit ditolak penjaganya. Satu-satunya jalan: Task Manager.

2. Deadline bisa meledak DI DALAM dialog konfirmasi, karena
   `QMessageBox.question` menjalankan nested event loop dan QTimer tetap
   delivers di dalamnya. Ujian terkumpul sementara siswa masih membaca
   dialog dan belum menekan apa pun.

3. `_stop_polling(join 2 detik)` lebih pendek dari HTTP timeout 10 detik,
   jadi `join` selalu meninggalkan poller lama hidup. Poller basi lalu
   membangun kembali dan `reset=True`-nya bisa me-reset approval yang
   baru saja diberikan pengawas.

4. Tiga tempat membaca widget Qt dari worker thread.

5. Test lama mengarang atribut Win32 yang hilang dengan `create=True`,
   sehingga ia hijau justru karena memasang pengganti fungsi yang tidak
   pernah ada.

6. `short_answer` menulis langsung ke `_answer_widgets`, jadi SETIAP soal
   short_answer dilaporkan sebagai nomor bentrok: peringatan palsu ke
   siswa dan `log.error` ke guru, sekali per submit.
"""

from __future__ import annotations

import os
import pathlib
import re
import threading
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QDialog, QMessageBox

from examvan.models import Exam

REPO = pathlib.Path(__file__).resolve().parents[2]
from examvan.ui.answer_sheet import AnswerSheetWidget
from examvan.ui.exam_viewer import ExamViewerWindow

APP = QApplication.instance() or QApplication([])


class _FakeSignal:
    """Signal PyQt yang cukup untuk test: mencatat emit dan jadi sinkron."""

    def __init__(self):
        self.emitted = []

    def connect(self, slot):
        self._slot = slot

    def emit(self, *args):
        self.emitted.append(args)
        slot = getattr(self, "_slot", None)
        if slot is not None:
            slot(*args)


class _FakeTimer:
    def __init__(self, active=True):
        self._active = active

    def isActive(self):
        return self._active

    def stop(self):
        self._active = False

    def start(self):
        self._active = True


class _FakeTimerWidget:
    def __init__(self):
        self._timer = _FakeTimer()
        self._fired_time_up = False
        self.refreshed = 0

    def refresh_deadline(self):
        self.refreshed += 1


def _viewer() -> ExamViewerWindow:
    """Window-exam TANPA konstruktor sungguhan.

    `ExamViewerWindow(...)` memulai monitoring focus-loss yang hitung
    mundur 3 detik lalu auto-submit, jadi test yang memakainya akan
    menjalankan seluruh alur itu. Yang diuji di sini cuma state machine
    submit dan timer widget -- keduanya cukup dari state minimal.
    """
    win = ExamViewerWindow.__new__(ExamViewerWindow)
    win._exam = Exam(id=1, name="Ujian", status="active")
    win._token = "ABCD1234"
    win._identity_data = {"nama": "Andi", "nomor": "N01"}
    win._submitted = False
    win._submitting = False
    win._exam_warnings = []
    win._submit_lock = threading.Lock()
    win._sig_submit_result = _FakeSignal()
    win._sig_status = _FakeSignal()
    win._timer_widget = _FakeTimerWidget()
    win._answer_sheet = mock.Mock()
    win._answer_sheet.get_answered_count.return_value = (1, 2)
    win._btn_submit = mock.Mock()
    win._do_submit = mock.Mock()
    # `_modal_dialog_guard()` mengUJI bool ini sebelum memakai
    # `_security.pause_focus_guard()`. None berarti "tanpa enforcer",
    # yang memang keadaan sah; yang tidak sah adalah atribut yang tidak
    # ada sama sekali, karena stub melewati __init__.
    win._security = None
    # `_modal_dialog_guard()` hanya menghidupkan countdown kembali kalau
    # jendela masih terlihat -- supaya timer 1 Hz tidak terus berjalan
    # pada jendela yang sudah ditutup. Stub melewati __init__ QWidget, jadi
    # `isVisible()` aslinya melempar RuntimeError; di sini, berperilaku
    # sebagai jendela yang sedang tampil, sesuai skenario yang diuji.
    win.isVisible = lambda: True
    win._cleanup_after_submit = mock.Mock()
    return win


class SubmitThreadNeverStrandsTest(unittest.TestCase):
    """#1 — `_submitting` tidak boleh pernah terkunci selamanya."""

    def _drive_with_crash(self, boom):
        win = _viewer()
        results = []
        win._sig_submit_result._slot = lambda ok, msg: results.append((ok, msg))
        with mock.patch(
            "examvan.ui.exam_viewer.api.submit_with_retry", side_effect=boom
        ):
            win._submit_thread(
                "https://exam.example", 1, "Andi", "N01", "9A", {"1": "A"},
                "2026-01-01T00:00:00Z", "DESKTOP:h", {"nama": "Andi"},
                "ABCD1234",
            )
        return win, results

    def test_unicode_error_from_a_non_utf8_body_is_reported_not_swallowed(self):
        # Byte non-UTF-8 pernah mematikan thread submit. `_submitting`
        # lalu mengunci True: tombol mati, jendela tidak bisa ditutup,
        # dan deadline auto-submit ditolak penjaganya.
        win, results = self._drive_with_crash(
            UnicodeDecodeError("utf-8", b"\xff", 0, 1, "invalid start byte")
        )
        self.assertEqual(len(results), 1, "tidak ada hasil submit yang sampai")
        success, message = results[0]
        self.assertFalse(success)
        self.assertIn("Gagal mengirim", message)

    def test_the_student_is_told_the_answers_are_safe(self):
        # Pesan yang salah akan membuat siswa mengulang dari nol padahal
        # jawabannya masih ada di disk.
        win, results = self._drive_with_crash(RuntimeError("boom"))
        self.assertIn("tidak hilang", results[0][1].lower())

    def test_the_submit_flag_is_released_so_the_student_can_retry(self):
        win, results = self._drive_with_crash(RuntimeError("boom"))
        # `_submitting` di-reset oleh slot `_on_submit_result`; karena
        # sinyalnya sampai, guard di closeEvent/_auto_submit akan melepas.
        # `_submitting` dilepas oleh slot `_on_submit_result`, yang
        # hanya-situ. Karena sinyalnya sampai, flag itu ada di tangan
        # slot -- bukan terkunci selamanya.
        win._submitting = True
        try:
            win._on_submit_result(False, "gagal")
        except Exception:
            pass  # bagian UI belum ada di stub; yang diuji flag-nya
        self.assertFalse(
            win._submitting,
            "_submitting harus dilepas supaya tombol submit hidup lagi "
            "dan siswa bisa mencoba ulang",
        )


class ConfirmDialogStopsTheCountdownTest(unittest.TestCase):
    """#2 — dialog konfirmasi tidak boleh membiarkan deadline meledak."""

    def test_timer_is_stopped_while_the_dialog_is_open(self):
        win = _viewer()
        stopped_during_dialog = {}

        def fake_question(*_a, **_k):
            stopped_during_dialog["running"] = win._timer_widget._timer.isActive()
            return QMessageBox.No

        with mock.patch(
            "examvan.ui.exam_viewer.QMessageBox.question", side_effect=fake_question
        ):
            win._on_submit()

        self.assertFalse(
            stopped_during_dialog["running"],
            "countdown harus berhenti selama dialog konfirmasi terbuka; "
            "kalau tidak, deadline bisa mencapai nol dan jawaban terkirim "
            "sambil siswa masih membaca dialog",
        )

    def test_countdown_is_restarted_after_the_dialog(self):
        win = _viewer()
        with mock.patch(
            "examvan.ui.exam_viewer.QMessageBox.question",
            return_value=QMessageBox.No,
        ):
            win._on_submit()
        self.assertTrue(
            win._timer_widget._timer.isActive(),
            "countdown harus hidup lagi setelah dialog ditutup",
        )

    def test_countdown_is_restarted_even_if_the_dialog_raises(self):
        win = _viewer()
        with mock.patch(
            "examvan.ui.exam_viewer.QMessageBox.question",
            side_effect=RuntimeError("dialog gagal"),
        ):
            with self.assertRaises(RuntimeError):
                win._on_submit()
        self.assertTrue(
            win._timer_widget._timer.isActive(),
            "countdown harus hidup lagi walau dialog melempar -- kalau tidak, "
            "siswa kehilangan sisa waktu tanpa tahu",
        )


class _PollerHost:
    """Pengganti dialog untuk menguji siklus polling tanpa UI."""

    def __init__(self):
        self.generation = 0
        self.stop = threading.Event()
        self.started = threading.Event()


class StalePollerQuitsTest(unittest.TestCase):
    """#3 — poller basi harus keluar, bukan membangun kembali."""

    def test_stopping_the_poller_never_blocks_the_gui_thread(self):
        # #3 ditutup dengan join 12 s -- tapi join itu berjalan di THREAD
        # GUI (dipanggil dari reject / closeEvent / "Minta Izin Lagi").
        # Jaringan mati -> poller memblokir 10 s di socket -> UI beku dan
        # `SecurityEnforcer` ikut mati karena butuh event loop yang sama.
        #
        # Yang benar: `set()` + naikkan generasi. Poll basi keluar sendiri
        # begitu socket-nya selesai, tanpa menyentuh dialog.
        import inspect

        from examvan.ui.waiting_approval import WaitingApprovalDialog

        src = inspect.getsource(WaitingApprovalDialog._stop_polling)
        self.assertNotIn(".join(", src)
        self.assertIn("self._poll_generation += 1", src)
        self.assertIn("self._poll_stop.set()", src)

    def test_a_stale_generation_returns_immediately(self):
        from examvan.ui.waiting_approval import WaitingApprovalDialog

        dlg = WaitingApprovalDialog.__new__(WaitingApprovalDialog)
        dlg._poll_stop = threading.Event()
        dlg._poll_generation = 5
        dlg._poll_thread_obj = None
        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval"
        ) as req:
            # Generasi 1 sudah basi karena ada poller 5.
            dlg._poll_thread(1)
        req.assert_not_called()
        self.assertFalse(dlg._poll_stop.is_set())


def _function_body(src: str, name: str) -> str:
    """Body satu fungsi: baris setelah `def` sampai indentasi <= indentasi `def`."""
    lines = src.splitlines(keepends=True)
    head = None
    for i, line in enumerate(lines):
        m = re.match(r"^(\s*)def " + re.escape(name) + r"\(", line)
        if m:
            head = i
            indent = len(m.group(1))
            break
    if head is None:
        raise AssertionError(f"{name} tidak ditemukan")
    # Signatures bisa wielah: `) -> None:` berindentasi sama dengan `def`
    # dan akan salah dianggap akhir fungsi kalau tidak dilewati dulu.
    out, in_signature = [], True
    for line in lines[head + 1:]:
        if in_signature:
            out.append(line)
            if line.rstrip().endswith(":"):
                in_signature = False
            continue
        if line.strip() and (len(line) - len(line.lstrip())) <= indent:
            break
        out.append(line)
    return "".join(out)


class NoQtFromWorkerThreadTest(unittest.TestCase):
    """#4 — worker thread tidak boleh menyentuh widget Qt.

    Diuji sebagai aturan sumber: yang dilarang adalah NAMA atribut yang
    hanya boleh disentuh dari thread GUI. Menyebutkannya persis di dalam
    fungsi worker adalah flag merah, dan pemeriksaan sumber menangkapnya
    tanpa perlu membangun dialog sungguhan.

    `QWidget::show()` dan `QLineEdit::text()` dari thread lain tidak
    thread-safe. Di Windows dialog sedang di-showMaximized() persis di
    detik pertama thread berjalan, jadi maximize + worker = race yang paling
    sering meledak sebagai access violation.
    """

    CASES = [
        ("desktop/examvan/ui/server_config.py", "_connect_thread",
         ("chk_remember", "input_token", "btn_connect", "lbl_status")),
        ("desktop/examvan/ui/server_config.py", "_recovery_submit_thread",
         ("input_token", "chk_remember", "btn_connect", "lbl_status")),
        ("desktop/examvan/ui/waiting_approval.py", "_poll_thread",
         ("btn_retry", "btn_cancel", "title_label", "subtitle_label",
          "icon_label")),
    ]

    def test_worker_threads_touch_no_qt_widgets(self):
        offenders = []
        for rel, fn, widgets in self.CASES:
            body = _function_body((REPO / rel).read_text(encoding="utf-8"), fn)
            for widget in widgets:
                if re.search(rf"self\.{re.escape(widget)}\b", body):
                    offenders.append(f"{rel}:{fn} -> self.{widget}")
        self.assertEqual(
            offenders, [],
            "worker thread menyentuh widget Qt: " + ", ".join(offenders)
            + ". Baca widget dari thread GUI sebelum menypawn worker, atau "
            "kirim sinyal.",
        )

    def test_the_checkbox_read_moved_to_the_gui_thread(self):
        # Bukti perbaikan, bukan sekadar larangan: nilai checkbox dibaca
        # di `_on_connect` (GUI) lalu diteruskan ke worker.
        src = (REPO / "desktop/examvan/ui/server_config.py").read_text(
            encoding="utf-8")
        body = _function_body(src, "_on_connect")
        self.assertIn("self.chk_remember.isChecked()", body)
        self.assertIn("remember_url", body)

    def test_recovery_token_comes_from_config_not_the_widget(self):
        src = (REPO / "desktop/examvan/ui/server_config.py").read_text(
            encoding="utf-8")
        body = _function_body(src, "_recovery_submit_thread")
        self.assertIn('config.get("exam_token"', body)
        self.assertNotIn("self.input_token", body)


class NoFabricatedWin32AttributesTest(unittest.TestCase):
    """#5 — test tidak boleh mengarang atribut Win32 yang tidak ada."""

    def test_clipboard_test_no_longer_creates_the_attributes(self):
        import pathlib

        src = pathlib.Path(__file__).with_name(
            "test_clipboard_performance.py"
        ).read_text(encoding="utf-8")
        self.assertNotIn(
            '_OpenClipboard", opened, create=True', src,
            "create=True mengarang atribut yang hilang -- test jadi hijau "
            "justru karena memasang pengganti fungsi yang tidak pernah ada",
        )

    def test_the_prototypes_really_exist_after_bind(self):
        import ctypes

        from examvan.security import windows_backend as wb
        from tests.test_windows_backend_binding import (
            REQUIRED_PROTOTYPES, _fake_windll,
        )

        for name in REQUIRED_PROTOTYPES:
            wb.__dict__.pop(name, None)
        wb._bound = False
        try:
            with mock.patch.object(ctypes, "windll", _fake_windll(), create=True):
                wb._bind()
            missing = [n for n in REQUIRED_PROTOTYPES if n not in wb.__dict__]
            self.assertEqual(missing, [])
        finally:
            for name in REQUIRED_PROTOTYPES:
                wb.__dict__.pop(name, None)
            wb._bound = False


class ShortAnswerIsNotADuplicateTest(unittest.TestCase):
    """#6 — short_answer tidak boleh dilaporkan sebagai nomor bentrok."""

    def _build(self, questions):
        sheet = AnswerSheetWidget()
        sheet.build_from_questions(questions)
        return sheet

    def test_a_single_short_answer_reports_no_duplicate(self):
        sheet = self._build([
            {"number": 1, "type": "single_choice", "question": "a",
             "options": ["A", "B"]},
            {"number": 2, "type": "short_answer", "question": "b"},
        ])
        self.assertEqual(
            sheet._duplicates, [],
            "setiap soal short_answer dilaporkan sebagai nomor bentrok: "
            "siswa melihat peringatan palsu dan guru mendapat log.error",
        )

    def test_several_short_answers_report_no_duplicate(self):
        sheet = self._build([
            {"number": 1, "type": "short_answer", "question": "a"},
            {"number": 2, "type": "short_answer", "question": "b"},
            {"number": 3, "type": "short_answer", "question": "c"},
        ])
        self.assertEqual(sheet._duplicates, [])

    def test_a_real_duplicate_is_still_reported(self):
        # Perbaikannya tidak boleh mematikan deteksi duplikat yang sungguhan.
        sheet = self._build([
            {"number": 1, "type": "single_choice", "question": "a",
             "options": ["A", "B"]},
            {"number": 1, "type": "true_false", "question": "a"},
        ])
        self.assertEqual(sheet._duplicates, ["1"])

    def test_short_answer_still_stores_and_restores_its_text(self):
        # `_pending_entry` harus diteruskan ke `_answer_widgets` oleh
        # build_from_questions, kalau tidak jawabannya tidak pernah
        # ikut ter-restore saat siswa me-reload.
        sheet = self._build([{"number": 2, "type": "short_answer",
                              "question": "b"}])
        self.assertIn("2", sheet._answer_widgets)
        sheet._answer_widgets["2"][1].setText("jawaban saya")
        self.assertEqual(sheet._answers["2"], "jawaban saya")


if __name__ == "__main__":
    unittest.main()
