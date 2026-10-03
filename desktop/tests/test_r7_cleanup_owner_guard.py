"""C2 (TINGGI) — submit yang telat tidak boleh menghapus jawaban siswa BERIKUTNYA.

Bug
---
`_cleanup_after_submit` (jalur submit MANUAL) memanggil
`config.clear_answers(self._exam.id)` TANPA penjagaan pemilik. Dua call
site lain sudah mendapat penjagaan — commit ini menambahkan
`answers_match_disk()` ke `_background_submit_thread` dan ke
`ServerConfigDialog._recovery_submit_thread` — tapi call site KETIGA ini
terlewat.

Alur dunia nyata (low tier, PC lab):

    siswa A selesai menjawab  -> auto-submit -> layar "Mengumpulkan…"
    A menekan Alt+F4 / klik X  -> viewer ditutup sementara submit masih jalan
    siswa B masuk, autosave jawaban B menulis ke disk yang SAMA
    submit A akhirnya sukses    -> clear_answers() menghapus jawaban B
                                -> halaman selamat A fullscreen di atas
                                   dialog konfigurasi yang sedang dipakai B

Dua hal rusak sekaligus: nilai B hilang permanen, dan B kehilangan layar
yang sedang dikerjakannya.

Test di sini memakai alur NYATA dengan event loop NYATA (`app.exec_()`),
dengan hanya jaringan/storage/backend security yang dipalsukan. Isi disk
ditimpa "percobaan baru" tepat di tengah submit — persis timeline di atas.
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

from PyQt5.QtCore import QObject, QTimer, Qt, pyqtSignal
from PyQt5.QtWidgets import (
    QApplication,
    QMessageBox,
    QVBoxLayout,
    QWidget,
)

from examvan import api, config, notify
from examvan.models import Exam, SubmitResponse

APP = QApplication.instance() or QApplication([])

EXAM_ID = 41
QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]

# Jawaban yang dikirim oleh attempt ini (siswa A).
OLD_PAYLOAD = {"1": "A", "2": "B"}
# Jawaban attempt BERIKUTNYA (siswa B) — harus selamat apa pun yang terjadi.
NEW_ATTEMPT = {"1": "C", "2": "C", "3": "D"}
STUDENT_B_KEY = "kunci-siswa-B"


def _live_viewer_class():
    """Kelas ExamViewerWindow yang SEDANG hidup di `sys.modules`."""
    return importlib.import_module("examvan.ui.exam_viewer").ExamViewerWindow


def _visible_top_levels() -> list:
    return [w for w in APP.topLevelWidgets() if w.isVisible()]


def _purge_top_levels() -> None:
    """Sembunyikan + hapus semua window top-level antar-test."""
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


class _FakeSecurityEnforcer(QObject):
    """Antarmuka yang sama dengan SecurityEnforcer, tanpa sentuhan platform."""

    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()
        self.deactivate_calls = 0
        self.protected = []

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        self.deactivate_calls += 1

    @contextlib.contextmanager
    def pause_focus_guard(self):
        yield

    def protect_window(self, window) -> bool:
        self.protected.append(window)
        return True

    def reassert_capture_protection(self) -> None:
        pass

    def clear_clipboard_now(self) -> None:
        pass


class _FakeBackend:
    def __init__(self) -> None:
        self.capture_protected = []

    def has_multiple_monitors(self) -> bool:
        return False

    def set_capture_protection(self, window) -> None:
        self.capture_protected.append(window)


class _ConfigDialogStandIn(QWidget):
    """`ServerConfigDialog` versi test: dipakai `all_done` untuk kembali."""

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("EXAMVAN — Konfigurasi")
        QVBoxLayout(self)


class _Sandbox(unittest.TestCase):
    """config sementara + jaringan/dialog dipalsukan + QMessageBox di-stub."""

    def setUp(self) -> None:
        _purge_top_levels()
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r7-cleanup-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
            mock.patch.object(
                importlib.import_module("examvan.ui.exam_viewer"),
                "SecurityEnforcer", _FakeSecurityEnforcer,
            ),
            mock.patch.object(
                importlib.import_module("examvan.ui.exam_viewer"),
                "ExamWebSocket",
            ),
            mock.patch.object(api, "download_pdf", side_effect=OSError("offline")),
            mock.patch.object(api, "send_access_log", return_value=False),
            mock.patch.object(api, "complete_exam", return_value=True),
            mock.patch.object(api, "poll_queued_result"),
            mock.patch.object(notify, "send_notification", return_value=True),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

        # Dialog TIDAK PERNAH boleh memblokir test headless: `QMessageBox`
        # asli menjalankan nested event loop dan tidak akan pernah kembali
        # tanpa klik. Stub ini menjawab langsung (default: "Tidak" — jangan
        # menutup apa pun karena tidak ada yang mengklik).
        self._msgbox_patch = mock.patch("examvan.ui.exam_viewer.QMessageBox")
        self._msgbox = self._msgbox_patch.start()
        self.addCleanup(self._msgbox_patch.stop)
        self._msgbox.return_value.exec_.return_value = QMessageBox.No

    def tearDown(self) -> None:
        _purge_top_levels()

    # -- helper ------------------------------------------------------------

    def _make_viewer(self, level="low", answers=None):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": level, "questions": QUESTIONS,
        })
        viewer = _live_viewer_class()(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(viewer.hide)
        viewer._answer_sheet.build_from_questions(exam.questions)
        viewer._answer_sheet.restore_answers(
            OLD_PAYLOAD if answers is None else answers
        )
        viewer.show()
        APP.processEvents()
        return viewer

    @staticmethod
    def _simulate_newer_attempt() -> None:
        """Siswa B autosave menimpa disk di tengah submit A yang masih jalan."""
        config.save_answers(EXAM_ID, NEW_ATTEMPT)
        config.save_answers_owner(EXAM_ID, STUDENT_B_KEY, "Siswa B")


# ---------------------------------------------------------------------------
# Auto-submit (low tier) — event loop sungguhan
# ---------------------------------------------------------------------------


class AutoSubmitLateSuccessOwnerTestCase(_Sandbox):
    """Sukses yang telat tidak boleh menyentuh milik percobaan berikutnya."""

    HARD_STOP_MS = 12000
    SUBMIT_DELAY = 0.8

    def _run_flow(self):
        state = {"done": False, "finished": False, "congrats_visible": False,
                 "config_visible": False, "submit_calls": 0}

        viewer = self._make_viewer("low")
        dialog = _ConfigDialogStandIn()
        self.addCleanup(dialog.hide)
        viewer.all_done.connect(dialog.show)
        self.addCleanup(viewer._close_progress_window)

        def _fake_submit(*args, **kwargs):
            state["submit_calls"] += 1
            # Jaringan lambat (retry + polling 202 = puluhan detik di
            # lapangan).
            time.sleep(self.SUBMIT_DELAY)
            #_autosave siswa B masuk DI TENGAH submit yang masih berjalan.
            self._simulate_newer_attempt()
            state["finished"] = True
            return SubmitResponse(
                success=True, status="done", message="ok",
                congrats_message="Selamat, Budi!",
            )

        def _watch():
            page = getattr(viewer, "_congrats_ref", None)
            if page is not None:
                state["congrats_visible"] = True
                try:
                    page.close()
                except RuntimeError:
                    pass
                watcher.stop()
                state["config_visible"] = dialog.isVisible()
                state["done"] = True
                APP.quit()
                return
            if dialog.isVisible():
                watcher.stop()
                state["config_visible"] = True
                state["done"] = True
                APP.quit()

        watcher = QTimer()
        watcher.setInterval(20)
        watcher.timeout.connect(_watch)
        hard_stop = QTimer()
        hard_stop.setSingleShot(True)
        hard_stop.setInterval(self.HARD_STOP_MS)
        hard_stop.timeout.connect(lambda: APP.quit())

        with mock.patch(
            "examvan.security.get_backend", return_value=_FakeBackend()
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(),
        ), mock.patch.object(
            api, "submit_with_retry", side_effect=_fake_submit
        ):
            watcher.start()
            hard_stop.start()
            try:
                viewer._auto_submit_and_exit()
                APP.exec_()
            finally:
                state["done"] = True
                watcher.stop()
                hard_stop.stop()
        state["viewer"] = viewer
        state["disk"] = config.load_answers(EXAM_ID)
        state["owner"] = config.load_answers_owner(EXAM_ID)
        return state

    def test_a_newer_attempt_on_disk_survives_the_late_success(self):
        state = self._run_flow()
        self.assertTrue(
            state["finished"],
            "thread submit tidak pernah selesai — test tidak menguji apa pun",
        )
        self.assertEqual(
            state["disk"], NEW_ATTEMPT,
            "jawaban siswa berikutnya dihapus oleh submit yang sukses telat — "
            "nilai B hilang permanen karena A menekan Alt+F4 duluan",
        )
        self.assertEqual(
            (state["owner"] or {}).get("student_key"), STUDENT_B_KEY,
            "sidecar pemilik jawaban B ikut terhapus/tertimpa",
        )

    def test_the_congratulations_page_is_not_stacked_over_the_next_student(self):
        state = self._run_flow()
        self.assertFalse(
            state["congrats_visible"],
            "halaman selamat siswa A muncul fullscreen di atas layar yang "
            "sed dipakai siswa B — hasil yang basi menimpa layar orang lain",
        )
        self.assertFalse(
            getattr(state["viewer"], "_auto_submit_pending", True),
            "alur auto-submit tidak pernah selesai: progress window masih "
            "menunggu hasil yang sudah datang",
        )
        self.assertTrue(
            state["config_visible"],
            "setelah submit telat diabaikan, tidak ada window yang terlihat "
            "— aplikasi mati di depan siswa",
        )


# ---------------------------------------------------------------------------
# Submit manual — `_cleanup_after_submit` (call site yang terlewat)
# ---------------------------------------------------------------------------


class CleanupAfterSubmitOwnerGuardTestCase(_Sandbox):
    """`_cleanup_after_submit` wajib memakai penjagaan yang sama."""

    def _submit_manually(self, viewer, *, newer_attempt):
        """Jalur submit manual sungguhan, lewat `_do_submit` + signal."""
        def _fake_submit(*args, **kwargs):
            if newer_attempt:
                self._simulate_newer_attempt()
            return SubmitResponse(
                success=True, status="done", message="ok",
                congrats_message="Selamat, Budi!",
            )

        with mock.patch(
            "examvan.security.get_backend", return_value=_FakeBackend()
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(),
        ), mock.patch.object(
            api, "submit_with_retry", side_effect=_fake_submit
        ):
            viewer._do_submit()
            deadline = time.monotonic() + 5.0
            while time.monotonic() < deadline and not viewer._submitted:
                APP.processEvents()
                time.sleep(0.01)

    def _congrats_shown(self, viewer):
        page = getattr(viewer, "_congrats_ref", None)
        return page is not None

    def test_a_newer_attempt_survives_the_manual_cleanup(self):
        viewer = self._make_viewer("low")
        self._submit_manually(viewer, newer_attempt=True)
        self.assertTrue(
            viewer._submitted,
            "submit manual tidak pernah selesai — test tidak menguji apa pun",
        )
        self.assertEqual(
            config.load_answers(EXAM_ID), NEW_ATTEMPT,
            "`_cleanup_after_submit` menghapus jawaban yang bukan miliknya — "
            "call site ketiga yang tidak memakai `answers_match_disk`",
        )
        self.assertFalse(
            self._congrats_shown(viewer),
            "halaman selamat muncul padahal isinya bukan yang baru saja "
            "dikirim — hasil basi menimpa layar siswa berikutnya",
        )

    def test_our_own_payload_is_still_cleared_and_the_page_shown(self):
        # Kontrol positif: penjagaan tidak boleh membuat jalur sukses normal
        # ikut gagal — jawaban terkumpul harus benar hilang dan halaman
        # selamat harus tetap tampil untuk siswa yang sedang tesnya.
        viewer = self._make_viewer("low")
        self._submit_manually(viewer, newer_attempt=False)
        self.assertTrue(
            viewer._submitted,
            "submit manual tidak pernah selesai — test tidak menguji apa pun",
        )
        self.assertEqual(
            config.load_answers(EXAM_ID), None,
            "jawaban milik sendiri tidak dihapus setelah submit sukses — "
            "PC lab mewarisi jawaban lama untuk siswa berikutnya",
        )
        self.assertTrue(
            self._congrats_shown(viewer),
            "halaman selamat tidak tampil setelah submit manual sukses",
        )


if __name__ == "__main__":
    unittest.main()