"""H1 (ronde 5) — autosave harus menulis sidecar pemilik jawaban.

`config.save_answers_owner` hanya dipanggil di dua jalur submit
(`_auto_submit_and_exit` dan `_do_submit`). Semua penulis lain —
`ExamViewerWindow._save_answers` (debounce 500 ms) dan `_flush_answers`
(2 detik) — menulis `answers_<id>.dat` tanpa sidecar, dan KEDUA gerbang
konsumennya gagal-TERBUKA saat sidecar tidak ada:

  * restore jawaban di `ExamViewerWindow.__init__` — `owner is None`
    berarti "milik siapa pun", jadi jawaban restore;
  * `ServerConfigDialog._offer_pending_recovery` — `owner is None` berarti
    recovery sah, jadi jawaban orang lain ditawarkan sebagai milik
    siswa yang baru saja mengetik identitasnya.

Reproduksi nyata (PC lab): siswa A menjawab, proses mati sebelum ada
submit → siswa B masuk → B melihat jawaban A, dan offered "Kirim Lagi"
untuk jawaban A dengan identitas B. Jawaban A tercatat atas nama B.

Test ini memakai fungsi config yang sungguhan (temp config dir), bukan
mock, supaya sidecar yang benar-benar ditulis ke disk yang diperiksa.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer
from examvan.ui.exam_viewer import ExamViewerWindow
from examvan.utils import build_student_key, student_label

APP = QApplication.instance() or QApplication([])

EXAM_ID = 31


class _NoopSecurity:
    """SecurityEnforcer tanpa sentuhan platform (sama seperti test lain)."""

    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()

    def activate(self):
        pass

    def deactivate(self):
        pass

    def pause_focus_guard(self):
        import contextlib

        return contextlib.nullcontext()

    def protect_window(self, window):
        return True

    def reassert_capture_protection(self):
        pass

    def clear_clipboard_now(self):
        pass


class AnswersOwnerBase(unittest.TestCase):
    """Config dir sementara + viewer nyata (jaringan/security dipalsukan)."""

    IDENTITY_A = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
    IDENTITY_B = {"nama": "Budi", "nomor_ujian": "N02", "kelas": "9A"}

    def setUp(self) -> None:
        _purge_top_levels()
        self._tmp = tempfile.mkdtemp(prefix="examvan-r5-owner-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp)),
            mock.patch.object(
                config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
            ),
            mock.patch.object(exam_viewer, "SecurityEnforcer", _NoopSecurity),
            mock.patch.object(exam_viewer, "ExamWebSocket"),
            mock.patch.object(
                exam_viewer.api, "download_pdf", side_effect=OSError("offline")
            ),
            mock.patch.object(exam_viewer.api, "send_access_log",
                              return_value=False),
            mock.patch.object(exam_viewer.api, "complete_exam", return_value=True),
            mock.patch.object(exam_viewer.notify, "send_notification",
                              return_value=True),
        ]
        for p in self._patches:
            p.start()
        config._cache = None

    def tearDown(self) -> None:
        _purge_top_levels()
        for p in reversed(self._patches):
            p.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)
        _purge_top_levels()

    # -- helper -----------------------------------------------------------

    def _viewer(self, identity, token="ABCD1234"):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low", "questions": [],
        })
        win = ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token=token,
            identity_data=identity,
        )
        self.addCleanup(win.hide)
        return win

    def _pump(self, predicate, timeout=3.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.01)
        return False


def _purge_top_levels() -> None:
    """Sembunyikan + hapus window top-level (tanpa `close()` → tanpa
    `lastWindowClosed` → tanpa `app.quit()` untuk test berikutnya)."""
    for _ in range(3):
        for w in APP.topLevelWidgets():
            try:
                w.hide()
            except Exception:
                pass
        APP.processEvents()
    for w in APP.topLevelWidgets():
        try:
            w.deleteLater()
        except Exception:
            pass
    APP.processEvents()


class AnswersOwnerSidecarTest(AnswersOwnerBase):
    """(a) satu `_save_answers` cukup untuk menulis sidecar."""

    def test_autosave_writes_the_owner_sidecar(self):
        win = self._viewer(self.IDENTITY_A)
        key = build_student_key(self.IDENTITY_A, "ABCD1234")
        self.assertEqual(win._student_key, key)

        win._save_answers()

        owner = config.load_answers_owner(EXAM_ID)
        self.assertIsNotNone(
            owner,
            "autosave menulis answers_<id>.dat tanpa sidecar pemilik — "
            "siswa berikutnya/Me-restored akan membaca jawaban ini sebagai "
            "miliknya sendiri",
        )
        self.assertEqual(owner["student_key"], key)
        self.assertEqual(
            owner["label"], student_label(self.IDENTITY_A, "ABCD1234"),
        )

    def test_student_key_is_computed_once_and_matches_submit(self):
        # Satu sumber kebenaran: `self._student_key` yang dipakai restore,
        # sidecar, dan submit. Kalau `_save_answers` menghitung ulang
        # sendiri, satu hari token/identitas bisa tidak cocok.
        win = self._viewer(self.IDENTITY_A)
        win._save_answers()
        self.assertEqual(
            config.load_answers_owner(EXAM_ID)["student_key"],
            win._student_key,
        )
        self.assertEqual(
            win._student_key,
            build_student_key(self.IDENTITY_A, "ABCD1234"),
        )

    def test_flush_timer_path_also_writes_the_sidecar(self):
        # `_flush_answers` -&gt; `_save_answers`: jalur yang dipakai siswa
        # yang mengetik terus-menerus (debounce belum sempat menembak).
        win = self._viewer(self.IDENTITY_A)
        win._answer_sheet._on_answer_changed("1", "A")
        win._flush_answers()
        self.assertIsNotNone(config.load_answers_owner(EXAM_ID))

    def test_debounced_autosave_writes_the_sidecar(self):
        # Jalur debounce 500 ms sungguhan (bukan `_save_answers` langsung).
        win = self._viewer(self.IDENTITY_A)
        win._answer_sheet._on_answer_changed("1", "B")
        self.assertTrue(
            self._pump(lambda: config.load_answers(EXAM_ID) is not None, 3.0),
            "autosave debounce tidak pernah menulis jawaban ke disk",
        )
        self.assertIsNotNone(config.load_answers_owner(EXAM_ID))


class AnswersOwnerGateTest(AnswersOwnerBase):
    """(b)-(d) dua gerbang konsumen, dua siswa, satu PC lab."""

    def _student_a_saves(self):
        win = self._viewer(self.IDENTITY_A)
        win._answer_sheet._on_answer_changed("1", "A")
        win._save_answers()
        win.hide()
        return win

    def _real_config_dialog(self):
        """ServerConfigDialog NYATA (kode recovery yang diuji bukan mock)."""
        from examvan.ui.server_config import ServerConfigDialog

        with mock.patch.object(config, "_CONFIG_DIR"), \
             mock.patch.object(config, "_CONFIG_FILE"):
            config._cache = {"remember_url": True, "server_url": "",
                             "exam_token": "ABCD1234", "identity_data": {}}
            dialog = ServerConfigDialog()
        config._cache = None
        dialog._exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low",
        })
        dialog._validated_token = "ABCD1234"
        # `_show_recovery` (slot produksi) membuka halaman hasil lewat
        # jaringan — Putuskan: yang diuji GERBANG-nya, bukan unduhan.
        try:
            dialog._sig_recovery_available.disconnect(dialog._show_recovery)
        except (TypeError, RuntimeError):
            pass
        return dialog

    def _recovery_offered_to(self, identity):
        """(result, apakah recovery ditawarkan) untuk identitas tersebut.

        `_offer_pending_recovery` memanggil emit
        `_sig_recovery_available`; itu yang dicatat di sini — bukan
        return value-nya (return value hanya "boleh lanjut atau tidak").
        """
        dialog = self._real_config_dialog()
        self.addCleanup(dialog.hide)
        offered = []
        dialog._sig_recovery_available.connect(lambda exam: offered.append(exam))
        result = dialog._offer_pending_recovery(identity)
        return result, bool(offered)

    def test_student_b_resolve_submit_answers_is_empty(self):
        self._student_a_saves()
        self.assertEqual(config.load_answers(EXAM_ID), {"1": "A"})
        key_b = build_student_key(self.IDENTITY_B, "ABCD1234")
        self.assertNotEqual(key_b, build_student_key(self.IDENTITY_A, "ABCD1234"))
        self.assertEqual(
            config.resolve_submit_answers({}, EXAM_ID, key_b), {},
            "resolve_submit_answers memberi jawaban siswa A ke percobaan "
            "siswa B — payload atas nama orang lain",
        )

    def test_recovery_is_not_offered_to_student_b(self):
        self._student_a_saves()
        result, offered = self._recovery_offered_to(self.IDENTITY_B)
        self.assertFalse(
            offered,
            "recovery jawaban siswa A ditawarkan ke siswa B (kunci tidak "
            "cocok) — jawaban A akan dikirim atas nama B",
        )
        self.assertTrue(result, "recovery milik orang lain TIDAK boleh memblokir")

    def test_recovery_is_still_offered_to_student_a(self):
        self._student_a_saves()
        result, offered = self._recovery_offered_to(self.IDENTITY_A)
        self.assertTrue(
            offered,
            "recovery jawaban sendiri tidak ditawarkan lagi — fitur "
            "recovery re-entry untuk siswa yang jawabannya belum terkirim mati",
        )
        self.assertFalse(result, "UI recovery harus dibangun untuk pemilik")


if __name__ == "__main__":
    unittest.main()