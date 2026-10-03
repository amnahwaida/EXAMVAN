"""MEDIUM — pruning di `restore_answers` tidak pernah jalan (lembar belum ada).

Bug
---
`AnswerSheetWidget.restore_answers` punya dua bagian: mengisi `_answers`
dari disk, lalu PRUNING — membuang nilai yang tidak ada lagi di pilihan
sekarang (`single`/`truefalse` di luar opsi, `multi` yang tersisa kosong,
entri `matching` yang item kanannya hilang). Bagian kedua hanya bisa jalan
kalau `_answer_widgets` sudah terisi.

Di produksi `restore_answers` dijadwalkan `QTimer.singleShot(500, …)` dari
konstruktor viewer, sedangkan lembar jawaban baru dibangun setelah PDF siap
(`_build_answer_sheet`). PDF butuh network — jadi pruning berjalan saat
`_answer_widgets` masih KOSONG dan mengiterasi nol widget.

Diukur: widget yang ada saat restore = 0; jawaban tersimpan `C` untuk soal
yang opsinya kini `[A, B]` tetap dikirim apa adanya dan dinilai SALAH, tanpa
pesan apa pun ke siswa. Kode di `restore_answers` sendiri secara eksplisit
beralasan soal perlindungan ini — hanya saja tidak pernah aktif.

Test di bawah memakai viewer sungguhan + unduhan PDF yang devour ke
lewat signal `_sig_pdf_ready` (event loop nyata), lalu memeriksa apa yang
benar-benar terkirim dan berapa widget yang ada saat pruning jalan.
"""

from __future__ import annotations

import contextlib
import importlib
import os
import shutil
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, QTimer, pyqtSignal
from PyQt5.QtWidgets import QApplication

from examvan import api, config
from examvan.models import Exam
from examvan.utils import build_student_key

APP = QApplication.instance() or QApplication([])

EXAM_ID = 615
TOKEN = "ABCD1234"
IDENTITY = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
STUDENT_KEY = build_student_key(IDENTITY, TOKEN)

# Pilihan yang SEKARANG ada; jawaban tersimpan "C" sudah tidak valid.
QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]
STALE_PAYLOAD = {"1": "C"}


class _FakeSecurity(QObject):
    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        pass

    @contextlib.contextmanager
    def pause_focus_guard(self):
        yield

    def reassert_capture_protection(self) -> None:
        pass

    def clear_clipboard_now(self) -> None:
        pass

    def protect_window(self, window) -> bool:
        return True


def _pump_until(done, timeout_ms: int = 8000) -> bool:
    watcher = QTimer()
    watcher.setInterval(20)
    state = {"ok": False}

    def _tick():
        if done():
            state["ok"] = True
            watcher.stop()
            APP.quit()

    watcher.timeout.connect(_tick)
    hard_stop = QTimer()
    hard_stop.setSingleShot(True)
    hard_stop.timeout.connect(APP.quit)
    hard_stop.setInterval(timeout_ms)
    watcher.start()
    hard_stop.start()
    try:
        APP.exec_()
    finally:
        watcher.stop()
        hard_stop.stop()
    return state["ok"]


class AnswerSheetPruningUnitTestCase(unittest.TestCase):
    """Logika pruning-nya sendiri (sudah benar, tapi tidak pernah dipanggil)."""

    def _sheet(self):
        from examvan.ui.answer_sheet import AnswerSheetWidget

        sheet = AnswerSheetWidget()
        self.addCleanup(sheet.deleteLater)
        sheet.build_from_questions(QUESTIONS)
        return sheet

    def test_a_stale_single_choice_answer_is_dropped_after_building(self):
        sheet = self._sheet()
        sheet.restore_answers(dict(STALE_PAYLOAD))
        self.assertEqual(
            sheet.get_answers(), {},
            "nilai single-choice yang tidak ada di pilihan harus dibuang, "
            "bukan dikirim apa adanya dan dinilai 0 tanpa pesan",
        )

    def test_a_valid_answer_survives(self):
        sheet = self._sheet()
        sheet.restore_answers({"1": "A"})
        self.assertEqual(sheet.get_answers(), {"1": "A"},
                         "pruning ikut membuang jawaban yang SAH")


class ViewerRestorePruningTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-restore-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        for patcher in (
            mock.patch.object(config, "_CONFIG_DIR", self.tmp),
            mock.patch.object(config, "_CONFIG_FILE", self.tmp / "config.json"),
            mock.patch.object(api, "send_access_log", return_value=False),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

        # Jawaban + sidecar milik SISWA YANG SEDANG BEKERJA (kunci cocok).
        config.save_answers(EXAM_ID, dict(STALE_PAYLOAD))
        config.save_answers_owner(EXAM_ID, STUDENT_KEY, "Andi")

        self.pdf_path = self.tmp / f"exam_{EXAM_ID}.pdf"
        self.pdf_path.write_bytes(b"%PDF-1.4\n%%EOF\n")

        # Unduhan yang LAMBAT — persis kondisi lapangan. Kalau unduhan
        # selesai dalam beberapa milidetik (loopback lokal), lembar jawaban
        # kebetulan dibangun sebelum timer 500 ms dan bug-nya tidak terlihat;
        # di ruang kelas PDF butuh-detik, jadi timer selalu menang.
        download_seconds = 1.2

        def _download(*args, **kwargs):
            time.sleep(download_seconds)
            dest = kwargs.get("dest")
            if dest:
                dest.write_bytes(self.pdf_path.read_bytes())
            return str(dest or self.pdf_path)

        self.module = importlib.import_module("examvan.ui.exam_viewer")
        for patcher in (
            mock.patch.object(self.module, "SecurityEnforcer", _FakeSecurity),
            mock.patch.object(self.module, "ExamWebSocket"),
            mock.patch.object(api, "download_pdf", side_effect=_download),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)

    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def _viewer(self):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low", "questions": QUESTIONS,
        })
        viewer = self.module.ExamViewerWindow(
            exam=exam, server_url="https://exam.example", token=TOKEN,
            identity_data=dict(IDENTITY),
        )
        self.addCleanup(viewer.hide)
        self.addCleanup(viewer.deleteLater)
        return viewer

    def test_restore_runs_after_the_answer_sheet_exists(self):
        viewer = self._viewer()
        sheet = viewer._answer_sheet
        seen_widgets = []
        real_restore = sheet.restore_answers

        def _spy(saved):
            seen_widgets.append(len(sheet._answer_widgets))
            return real_restore(saved)

        with mock.patch.object(sheet, "restore_answers", _spy):
            built = _pump_until(
                lambda: len(sheet._answer_widgets) > 0
                and viewer._lbl_status.text() in ("PDF siap",)
            ) or len(sheet._answer_widgets) > 0
            # Beri waktu untuk restore yang dijadwalkan 500 ms.
            watcher = QTimer()
            watcher.setSingleShot(True)
            watcher.timeout.connect(APP.quit)
            watcher.setInterval(1200)
            watcher.start()
            APP.exec_()
            watcher.stop()

        self.assertTrue(
            built or sheet._answer_widgets,
            "lembar jawaban tidak pernah dibangun — test tidak menguji apa pun",
        )
        self.assertTrue(
            seen_widgets,
            "restore_answers tidak pernah dipanggil di viewer ini",
        )
        self.assertTrue(
            all(count > 0 for count in seen_widgets),
            f"restore berjalan saat lembar jawaban masih KOSONG "
            f"(widget={seen_widgets}) — pruning mengiterasi nol widget, "
            f"jadi nilai yang sudah tidak valid tetap ikut dikirim",
        )
        self.assertEqual(
            sheet.get_answers(), {},
            f"jawaban basi ikut terkirim: {sheet.get_answers()} — "
            "pruning di restore_answers tidak pernah tereksekusi",
        )


if __name__ == "__main__":
    unittest.main()