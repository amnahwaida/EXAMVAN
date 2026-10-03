"""LOW ronde 7 — lima kebocoran kecil yang survived review sebelumnya.

L1. `_poll_focus` memanggil `raise_()` + `activateWindow()` pada jendela
    ujian yang SUDAH DISEMBUNYIKAN, tiap 500 ms, selama menunggu hasil
    auto-submit. Akibatnya layar pengumpulan tidak pernah jadi window
    aktif, focus-loss selalu terbaca "siswa keluar", dan tiap
    `_on_focus_timeout` mengosongkan episode — jadi cap 60 detik yang
    seharusnya memaksa auto-submit tidak pernah bisa aktif.
L2. Jalur admin exit dan low-level exit tidak pernah melakukan flush
    jawaban terakhir, jadi ketikan di dalam jendela debounce 500 ms hilang
    tanpa pernah menyentuh disk.
L3. `_build_progress_window` melewati helper modul-level
    `protect_window_capture()` yang justru dibuat untuk layar yang hidup
    tanpa enforcer aktif — dan memasang proteksinya SEBELUM
    `apply_fullscreen()`, urutan yang dikoreksi di tempat lain di file ini.
L4. `_close_progress_window` melepas `_progress_ref` SEBELUM `close()`
    di dalam try: kalau `close()` melempar, layar pengumpulan tetap
    terlihat tanpa siapa pun yang masih memegangnya.
L5. `__main__._exam_needs_monitor_notice` tidak membaca `is_strict`, jadi
    ujian yang sudah ditandai strict dapat dialog informasi medium DAN
    THEN ditolak oleh gate strict — dua perilaku berbeda untuk satu
    Property yang sama.
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

from PyQt5.QtCore import QObject, pyqtSignal
from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import __main__ as main_mod
from examvan import api, config, notify
from examvan.models import Exam, SubmitResponse
from examvan.security import enforcer as enforcer_mod
from examvan.security.enforcer import SecurityEnforcer
from examvan.security_levels import LEVEL_STRICT
from examvan.ui import exam_viewer as ev_mod

APP = QApplication.instance() or QApplication([])

EXAM_ID = 61
QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]


def _visible_top_levels() -> list:
    return [w for w in APP.topLevelWidgets() if w.isVisible()]


def _purge_top_levels() -> None:
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
    """Mencatat perintah "tunggu hasil submit" dari viewer."""

    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()
        self.deactivate_calls = 0
        self.protected = []
        self.waiting_calls = []

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

    def set_waiting_for_submit_result(self, waiting: bool) -> None:
        self.waiting_calls.append(bool(waiting))

    def reassert_capture_protection(self) -> None:
        pass

    def clear_clipboard_now(self) -> None:
        pass


class _FakeBackend:
    def __init__(self, order=None, name="capture"):
        self.capture_protected = []
        self.order = order
        self.name = name

    def has_multiple_monitors(self) -> bool:
        return False

    def set_capture_protection(self, window) -> None:
        self.capture_protected.append(window)
        if self.order is not None:
            self.order.append(("protect", self.name, window))


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        _purge_top_levels()
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r7-low-"))
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
        # QMessageBox tidak boleh memblokir test headless (nested event loop
        # tanpa siapa pun yang mengklik).
        self.msgbox_patch = mock.patch.object(ev_mod, "QMessageBox")
        self.msgbox = self.msgbox_patch.start()
        self.addCleanup(self.msgbox_patch.stop)
        # Bandingkan enum ASLI: kode membandingkan `reply != QMessageBox.Yes`,
        # jadi atribut enum pada stub harus nilai enum yang sama — kalau
        # tidak, dialog selalu terbaca "bukan Yes" dan tidak ada yang terjadi.
        self.msgbox.Yes = QMessageBox.Yes
        self.msgbox.No = QMessageBox.No
        self.msgbox.Warning = QMessageBox.Warning
        self.msgbox.Ok = QMessageBox.Ok
        self.msgbox.return_value.exec_.return_value = QMessageBox.Yes
        self.addCleanup(_purge_top_levels)

    def _make_viewer(self, level="low"):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": level, "questions": QUESTIONS,
        })
        viewer = ev_mod.ExamViewerWindow(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(viewer.hide)
        viewer._answer_sheet.build_from_questions(exam.questions)
        viewer.show()
        APP.processEvents()
        return viewer

    @staticmethod
    def _answer(viewer, num, value):
        """Simulasikan siswa baru saja menjawab (jalur signal sungguhan)."""
        viewer._answer_sheet._on_answer_changed(str(num), value)

    def _pump_until(self, predicate, timeout=5.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.01)
        return False


# ---------------------------------------------------------------------------
# L1 — jangan raise/activate jendela yang disembunyikan saat menunggu submit
# ---------------------------------------------------------------------------


class _RecordingWindow:
    """Bukan QWidget: enforcer hanya butuh `isActiveWindow`/`raise_`/..."""

    def __init__(self):
        self.active = True
        self.calls = []

    def isActiveWindow(self):
        return self.active

    def raise_(self):
        self.calls.append("raise_")

    def activateWindow(self):
        self.calls.append("activateWindow")

    def installEventFilter(self, *a):
        pass

    def removeEventFilter(self, *a):
        pass

    def setWindowFlag(self, *a, **k):
        pass

    def isVisible(self):
        return True


class _RecordingBackend:
    def __init__(self):
        self.confine_calls = 0

    def set_capture_protection(self, window):
        pass

    def confine_pointer(self, window):
        self.confine_calls += 1

    def activate(self):
        pass

    def deactivate(self):
        pass

    def prevent_sleep(self):
        pass

    def allow_sleep(self):
        pass

    def clear_clipboard(self):
        pass

    def has_multiple_monitors(self):
        return False

    def set_strict_mode(self, window):
        pass

    def release_strict_mode(self, window):
        pass

    def release_capture_protection(self, window):
        pass

    def release_pointer(self):
        pass


class PollFocusDuringSubmitTest(unittest.TestCase):
    """`_poll_focus` harus berhenti menaiikkan jendela yang sudah disembunyi."""

    def _enforcer(self):
        patcher = mock.patch.object(
            enforcer_mod, "get_backend", return_value=_RecordingBackend()
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        window = _RecordingWindow()
        enforcer = SecurityEnforcer(
            security_level=LEVEL_STRICT, strict_mode=True, window=window
        )
        self.addCleanup(enforcer.deactivate)
        enforcer.activate()
        return enforcer, window

    def test_the_window_is_still_raised_during_a_normal_exam(self):
        # Kontrol positif: penjagaan hanya berlaku saat menunggu hasil.
        enforcer, window = self._enforcer()
        enforcer._poll_focus()
        self.assertIn("raise_", window.calls, "kontrol tidak menguji apa pun")
        self.assertIn("activateWindow", window.calls)

    def test_a_hidden_window_is_not_raised_while_the_submit_is_in_flight(self):
        enforcer, window = self._enforcer()
        # Flag yang sama dengan yang ditulis `set_waiting_for_submit_result`;
        # ditulis langsung supaya test ini gagal karena PERILAKU polling,
        # bukan karena setter-nya belum ada.
        enforcer._waiting_for_submit = True
        window.calls.clear()
        enforcer._poll_focus()
        self.assertEqual(
            window.calls, [],
            "jendela ujian yang sudah disembunyikan tetap di-raise_/diaktifkan "
            "setiap 500 ms: layar pengumpulan tidak pernah jadi window aktif, "
            "dan tiap `_on_focus_timeout` mengosongkan episode sehingga cap "
            "60 detik tidak pernah bisa aktif",
        )
        self.assertEqual(
            enforcer._backend.confine_calls, 0,
            "ClipCursor masih dikunci ke jendela yang tidak terlihat — "
            "kursor siswa terkunci tanpa alasan yang bisa dilihat",
        )

    def test_no_countdown_is_started_for_a_window_nobody_sees(self):
        enforcer, window = self._enforcer()
        window.active = False          # viewer sudah hide(), progress aktif
        enforcer._waiting_for_submit = True
        enforcer._poll_focus()
        self.assertFalse(
            enforcer._focus_timer.isActive(),
            "countdown auto-submit dinyalakan untuk jendela yang tidak "
            "terlihat — auto-submit bisa menembak di belakang layar "
            "pengumpulan",
        )

    def test_the_public_setter_exists_and_is_what_the_viewer_calls(self):
        enforcer, window = self._enforcer()
        enforcer.set_waiting_for_submit_result(True)
        self.assertTrue(
            enforcer._waiting_for_submit,
            "setter tidak menandai apa pun — viewer memakai setter ini, jadi "
            "polling tetap menaiakkan HWND yang tidak terlihat",
        )
        enforcer.set_waiting_for_submit_result(False)
        self.assertFalse(enforcer._waiting_for_submit)


# ---------------------------------------------------------------------------
# L1 (sisi viewer) — penanda diurus sesuai umur auto-submit
# ---------------------------------------------------------------------------


class ViewerWaitsForSubmitResultTest(_Sandbox):
    def test_the_viewer_flags_the_wait_and_clears_it_afterwards(self):
        viewer = self._make_viewer("low")
        enforcer = viewer._security
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch(
            "examvan.security.get_backend", return_value=_FakeBackend()
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(),
        ), mock.patch.object(api, "submit_with_retry", return_value=resp):
            viewer._auto_submit_and_exit()
            try:
                self.assertEqual(
                    enforcer.waiting_calls, [True],
                    "viewer tidak memberi tahu enforcer bahwa jendela ujian "
                    "sudah disembunyikan — polling tetap menaiikkan HWND "
                    "yang tidak terlihat (L1)",
                )
            finally:
                self._pump_until(lambda: viewer._auto_submit_pending is False, 5.0)
        self.assertEqual(
            enforcer.waiting_calls[-1], False,
            "penanda tidak dilepas setelah hasil tiba — seluruh sisa sesi "
            "polling focus tidak lagi memindahkan fokus",
        )


# ---------------------------------------------------------------------------
# L2 — flush jawaban terakhir di kedua jalur keluar tanpa submit
# ---------------------------------------------------------------------------


class FinalAnswerFlushTest(_Sandbox):
    def test_the_low_tier_exit_flushes_the_last_answer(self):
        viewer = self._make_viewer("low")
        self._answer(viewer, 1, "B")
        self.assertTrue(viewer._answers_dirty)
        self.assertFalse(
            viewer._save_timer.isActive() is False,
            "debounce tidak berjalan — test tidak menguji apa pun",
        )
        viewer.close()                # dialog stub menjawab Yes
        APP.processEvents()
        self.assertEqual(
            config.load_answers(EXAM_ID), {"1": "B"},
            "jawaban terakhir hilang:Autosave punya debounce 500 ms dan "
            "flush 2 detik, dan jalur keluar low-tier memanggil "
            "`_stop_autosave()` tanpa flush sama sekali — ketikan terakhir "
            "siswa tidak pernah sampai ke disk (L2)",
        )

    def test_the_admin_exit_flushes_the_last_answer(self):
        viewer = self._make_viewer("low")
        self._answer(viewer, 1, "B")
        with mock.patch.object(ev_mod, "_ADMIN_PASSWORD", "rahasia-supervisor"), \
             mock.patch(
                 "PyQt5.QtWidgets.QInputDialog.getText",
                 return_value=("rahasia-supervisor", True),
             ):
            viewer._admin_exit_prompt()
        APP.processEvents()
        self.assertEqual(
            config.load_answers(EXAM_ID), {"1": "B"},
            "admin exit membuang jawaban terakhir yang belum sempat "
            "autosave — supervisor menutup ujian dan nilai soal terakhir "
            "siswa hilang (L2)",
        )


# ---------------------------------------------------------------------------
# L3 — helper proteksi capture, dipasang SESUDAH fullscreen
# ---------------------------------------------------------------------------


class ProgressCaptureProtectionTest(_Sandbox):
    def test_the_shared_helper_is_used(self):
        viewer = self._make_viewer("low")
        real_helper = enforcer_mod.protect_window_capture
        calls = []

        def _spy(window):
            calls.append(window)
            return real_helper(window)

        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch.object(
            ev_mod, "protect_window_capture", _spy, create=True
        ), mock.patch(
            "examvan.security.get_backend", return_value=_FakeBackend()
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(),
        ), mock.patch.object(api, "submit_with_retry", return_value=resp):
            viewer._auto_submit_and_exit()
            try:
                self.assertIn(
                    viewer._progress_ref, calls,
                    "layar pengumpulan dipasang proteksinya lewat "
                    "`get_backend()` sendiri, bukan lewat helper "
                    "`protect_window_capture()` yang justru dibuat untuk layar "
                    "yang hidup tanpa enforcer aktif (L3)",
                )
            finally:
                self._pump_until(
                    lambda: viewer._auto_submit_pending is False, 5.0
                )

    def test_protection_is_installed_after_the_final_state_change(self):
        order = []
        real_helper = enforcer_mod.protect_window_capture
        viewer = self._make_viewer("low")

        def _spy(window):
            order.append("protect")
            return real_helper(window)

        def _fake_fullscreen(widget):
            order.append("fullscreen")
            return False

        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch.object(
            ev_mod, "protect_window_capture", _spy, create=True
        ), mock.patch.object(
            ev_mod, "apply_fullscreen", _fake_fullscreen
        ), mock.patch(
            "examvan.security.get_backend",
            return_value=_FakeBackend(order, "backend"),
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(order, "backend"),
        ), mock.patch.object(api, "submit_with_retry", return_value=resp):
            viewer._auto_submit_and_exit()
            try:
                self.assertEqual(
                    order[:2], ["fullscreen", "protect"],
                    "proteksi capture dipasang SEBELUM state akhir jendela "
                    "dibentuk — `SetWindowDisplayAffinity` disimpan per-HWND, "
                    "jadi proteksinya hilang tanpa jejak (L3): %r" % (order,),
                )
            finally:
                self._pump_until(
                    lambda: viewer._auto_submit_pending is False, 5.0
                )


# ---------------------------------------------------------------------------
# L4 — tutup dulu, baru lepas referensinya
# ---------------------------------------------------------------------------


class _ExplodingProgressWindow:
    """`close()` melempar — persis kondisi yang bocor di `_close_progress_window`."""

    def __init__(self, viewer):
        self._viewer = viewer
        self.ref_at_close = "tidak diukur"
        self.hide_calls = 0
        self.visible = True

    def close(self):
        self.ref_at_close = getattr(self._viewer, "_progress_ref", None)
        raise RuntimeError("close gagal")

    def hide(self):
        self.hide_calls += 1
        self.visible = False

    def isVisible(self):
        return self.visible


class CloseProgressWindowOrderTest(_Sandbox):
    def test_the_window_is_closed_before_the_reference_is_dropped(self):
        viewer = self._make_viewer("low")
        stub = _ExplodingProgressWindow(viewer)
        viewer._progress_ref = stub

        viewer._close_progress_window()

        self.assertIs(
            stub.ref_at_close, stub,
            "referensi sudah dilepas SEBELUM `close()` dicoba — kalau "
            "close() melempar, layar pengumpulan tetap terlihat tanpa siapa "
            "pun yang masih memegangnya, dan tidak ada tombol yang bisa "
            "menutupnya (L4)",
        )
        self.assertIsNone(
            viewer._progress_ref,
            "referensi tidak boleh menggantung sampai viewer di-GC",
        )
        self.assertGreaterEqual(
            stub.hide_calls, 1,
            "close() melempar tapi jendela tidak disembunyikan — layar "
            "'Mengumpulkan jawaban…' menutupi semua yang lain selamanya",
        )

    def test_a_normal_close_still_works(self):
        viewer = self._make_viewer("low")
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch(
            "examvan.security.get_backend", return_value=_FakeBackend()
        ), mock.patch(
            "examvan.security.enforcer.get_backend",
            return_value=_FakeBackend(),
        ), mock.patch.object(api, "submit_with_retry", return_value=resp):
            viewer._auto_submit_and_exit()
            progress = viewer._progress_ref
            try:
                self.assertTrue(progress.isVisible())
            finally:
                self._pump_until(lambda: viewer._auto_submit_pending is False, 5.0)
        self.assertIsNone(viewer._progress_ref)


# ---------------------------------------------------------------------------
# L5 — ujian strict tidak boleh mendapat dialog informasi medium
# ---------------------------------------------------------------------------


class StrictMonitorNoticeTest(unittest.TestCase):
    def _exam(self, **overrides):
        data = {
            "id": 1, "name": "Ujian", "status": "active",
            "security_level": "medium", "questions": QUESTIONS,
        }
        data.update(overrides)
        return Exam.from_json(data)

    def test_a_strict_flagged_exam_is_not_given_the_medium_notice(self):
        exam = self._exam(strict_mode=True)
        self.assertTrue(
            exam.is_strict,
            "exam ini tidak strict — test tidak menguji apa pun",
        )
        self.assertFalse(
            main_mod._exam_needs_monitor_notice(exam),
            "ujian yang sudah ditandai strict mendapat dialog informasi "
            "medium DAN lalu ditolak oleh gate strict — dua perilaku "
            "berbeda untuk satu Property yang sama (L5)",
        )

    def test_a_plain_medium_exam_still_gets_the_notice(self):
        # Kontrol positif: perbaikan tidak boleh mematikan peringatan medium.
        exam = self._exam()
        self.assertFalse(exam.is_strict)
        self.assertTrue(
            main_mod._exam_needs_monitor_notice(exam),
            "ujian medium biasa tidak mendapat peringatan multi-monitor",
        )


if __name__ == "__main__":
    unittest.main()