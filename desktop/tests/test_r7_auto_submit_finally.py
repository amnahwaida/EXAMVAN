"""H6 (TINGGI) — `_on_auto_submit_done` tidak boleh meninggalkan zombie.

Bug
---
`_on_auto_submit_done` tidak punya `try/finally` sama sekali, dan
`_close_progress_window()` adalah statement TERAKHIR:

    if ok:
        congrats = self._show_congratulations(msg)   # bisa melempar
        ...
    else:
        self.all_done.emit()                          # bisa melempar
    self._close_progress_window()                     # TIDAK PERNAH terjadi

Satu exception dari langkah berikutnya (halaman selamat gagal dibangun,
`all_done` → `_maximize_window(dialog)` gagal, apa pun) berarti layar
"Mengumpulkan jawaban…" TETAP di layar selamanya. PyQt5 melanjutkan
setelah traceback (bukan crash — sudah diuji di lingkungan ini), jadi ini
adalah zombie: prosesnya hidup, `_auto_submit_pending` sudah False, tapi
tidak ada lagi yang terjadi. Siswa duduk di depan "sedang mengumpulkan…"
yang tidak akan pernah selesai; satu-satunya jalan keluar Task Manager.

Test memakai event loop SUNGGUHAN (`app.exec_()`) lewat `main()` asli:
yang sedang diuji justru topologi window — apakah ada momen tanpa satu pun
window terlihat (`lastWindowClosed` → aplikasi menutup diri sendiri), dan
apakah layar pengumpulan masih ada di akhir alur. Keduanya mustahil dinilai
dari test yang hanya memompa event dengan tangan.
"""

from __future__ import annotations

import contextlib
import importlib
import os
import shutil
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, QTimer, pyqtSignal
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
    QLineEdit,
    QVBoxLayout,
    QWidget,
)

from examvan import __main__ as main_mod
from examvan import api, config, notify
from examvan.models import Exam, SubmitResponse

APP = QApplication.instance() or QApplication([])

QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]

SUCCESS = SubmitResponse(
    success=True, status="done", message="ok", congrats_message="Selamat, Budi!",
)
FAILURE = SubmitResponse(success=False, message="jaringan mati")


def _live_viewer_class():
    """Kelas ExamViewerWindow yang SEDANG hidup di `sys.modules`."""
    return importlib.import_module("examvan.ui.exam_viewer").ExamViewerWindow


class _FakeConfigDialog(QWidget):
    """ServerConfigDialog secukupnya untuk alur `on_exam_selected`."""

    exam_selected = pyqtSignal(object, object, object)

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("EXAMVAN — Konfigurasi")
        layout = QVBoxLayout(self)
        self.input_token = QLineEdit(self)
        self.input_token.setText("ABCD1234")
        layout.addWidget(self.input_token)
        self.enable_connect_calls = 0

    def enable_connect(self) -> None:
        self.enable_connect_calls += 1


class _ApprovedWaitingDialog(QWidget):
    def exec_(self):  # noqa: N802 (Qt API)
        self.hide()
        return QDialog.Accepted


class _FakeSecurityEnforcer(QObject):
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
    def has_multiple_monitors(self) -> bool:
        return False

    def set_capture_protection(self, window) -> None:
        pass


class _ExistingQApplication:
    """`QApplication` selama `main()` — mengembalikan instance yang ada."""

    def __new__(cls, *args, **kwargs):
        return APP

    setAttribute = staticmethod(QApplication.setAttribute)
    instance = staticmethod(QApplication.instance)
    processEvents = staticmethod(QApplication.processEvents)
    clipboard = staticmethod(QApplication.clipboard)
    activePopupWidget = staticmethod(QApplication.activePopupWidget)
    topLevelWidgets = staticmethod(QApplication.topLevelWidgets)
    primaryScreen = staticmethod(QApplication.primaryScreen)


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


class _FlowBase(unittest.TestCase):
    """`main()` + `app.exec_()` sungguhan, dengan sabotase langkah terakhir."""

    HARD_STOP_MS = 14000

    def setUp(self) -> None:
        _purge_top_levels()
        self._tmp = tempfile.mkdtemp(prefix="examvan-r7-finally-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp)),
            mock.patch.object(
                config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
            ),
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
        ]
        for p in self._patches:
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self.addCleanup(_purge_top_levels)

    def _run_flow(self, *, submit_response=SUCCESS, submit_delay=0.6,
                  sabotage=None, ceiling_ms=None):
        """Alur nyata; `sabotage` = "congrats" / "all_done" / None."""
        state = {
            "visible_samples": [],
            "last_window_closed": [],
            "congrats_visible": False,
            "config_visible": False,
            "config_enable_connect_calls": 0,
            "done": False,
            "viewer": None,
            "submit_calls": 0,
        }
        exam = Exam.from_json({
            "id": 51, "name": "Ujian", "status": "active",
            "security_level": "low", "questions": QUESTIONS,
        })
        dialog = _FakeConfigDialog()
        self.addCleanup(dialog.hide)
        timers = []
        thread_done = threading.Event()

        def _fake_submit(*args, **kwargs):
            state["submit_calls"] += 1
            time.sleep(submit_delay)
            try:
                return submit_response
            finally:
                thread_done.set()

        def _find_viewer():
            for w in APP.topLevelWidgets():
                if isinstance(w, _live_viewer_class()):
                    return w
            return None

        def _start_exam():
            starter.stop()
            dialog.exam_selected.emit(
                exam, "https://exam.example", {"nama": "Budi"}
            )

        def _drive():
            viewer = _find_viewer()
            if viewer is None:
                return
            driver.stop()
            state["viewer"] = viewer
            viewer._answer_sheet.build_from_questions(exam.questions)
            viewer._answer_sheet.restore_answers({"1": "A"})
            viewer._auto_submit_and_exit()

        def _sabotage_all_done():
            viewer = _find_viewer()
            if viewer is None:
                return
            breaker.stop()
            boom = mock.Mock()
            boom.emit.side_effect = RuntimeError(
                "dialog konfigurasi gagal tampil"
            )
            viewer.all_done = boom

        def _sample():
            state["visible_samples"].append(len(_visible_top_levels()))

        def _finish():
            state["config_visible"] = dialog.isVisible()
            state["config_enable_connect_calls"] = dialog.enable_connect_calls
            state["done"] = True
            hard_stop.stop()
            APP.quit()

        def _watch():
            page = getattr(state["viewer"], "_congrats_ref", None)
            if page is not None and page.isVisible():
                state["congrats_visible"] = True
                watcher.stop()
                _finish()
                return
            if dialog.isVisible():
                watcher.stop()
                _finish()

        starter = QTimer()
        starter.setInterval(20)
        starter.timeout.connect(_start_exam)
        timers.append(starter)

        driver = QTimer()
        driver.setInterval(20)
        driver.timeout.connect(_drive)
        timers.append(driver)

        breaker = QTimer()
        breaker.setInterval(20)
        breaker.timeout.connect(_sabotage_all_done)
        timers.append(breaker)

        sampler = QTimer()
        sampler.setInterval(20)
        sampler.timeout.connect(_sample)
        timers.append(sampler)

        watcher = QTimer()
        watcher.setInterval(20)
        watcher.timeout.connect(_watch)
        timers.append(watcher)

        hard_stop = QTimer()
        hard_stop.setSingleShot(True)
        hard_stop.setInterval(self.HARD_STOP_MS)
        hard_stop.timeout.connect(lambda: None if state["done"] else APP.quit())
        timers.append(hard_stop)

        sabotage_patches = []
        if sabotage == "congrats":
            sabotage_patches.append(
                mock.patch.object(
                    _live_viewer_class(), "_show_congratulations",
                    side_effect=RuntimeError("halaman selamat gagal dibangun"),
                )
            )
        if ceiling_ms is not None:
            sabotage_patches.append(
                mock.patch(
                    "examvan.ui.exam_viewer._AUTOSUBMIT_WATCHDOG_MS",
                    ceiling_ms, create=True,
                )
            )

        APP.lastWindowClosed.connect(lambda: state["last_window_closed"].append(1))
        for p in sabotage_patches:
            p.start()
        try:
            with mock.patch.object(main_mod, "_setup_logging"), \
                 mock.patch.object(main_mod, "_recover_gnome_settings"), \
                 mock.patch.object(main_mod, "_recover_windows_settings"), \
                 mock.patch.object(main_mod, "_sweep_stale_exam_pdfs"), \
                 mock.patch.object(main_mod, "atexit"), \
                 mock.patch.object(main_mod, "signal"), \
                 mock.patch.object(sys, "exit"), \
                 mock.patch.object(sys, "argv", ["examvan"]), \
                 mock.patch("PyQt5.QtWidgets.QApplication",
                            _ExistingQApplication), \
                 mock.patch("examvan.ui.styles.apply_theme"), \
                 mock.patch("examvan.ui.styles.is_system_dark",
                            return_value=False), \
                 mock.patch("examvan.ui.server_config.ServerConfigDialog",
                            return_value=dialog), \
                 mock.patch(
                     "examvan.ui.waiting_approval.WaitingApprovalDialog",
                     return_value=_ApprovedWaitingDialog()), \
                 mock.patch("examvan.security.get_backend",
                            return_value=_FakeBackend()), \
                 mock.patch("examvan.security.enforcer.get_backend",
                            return_value=_FakeBackend()), \
                 mock.patch.object(api, "submit_with_retry",
                                   side_effect=_fake_submit):
                for t in timers:
                    if t is breaker and sabotage != "all_done":
                        continue
                    t.start()
                main_mod.main()
        finally:
            state["done"] = True
            for t in timers:
                t.stop()
            for p in sabotage_patches:
                p.stop()
            try:
                APP.lastWindowClosed.disconnect()
            except (RuntimeError, TypeError):
                pass
        # Biarkan worker selesai (dan hasil telatnya sampai) supaya tidak
        # bocor ke test berikutnya.
        deadline = time.monotonic() + 5.0
        while not thread_done.is_set() and time.monotonic() < deadline:
            APP.processEvents()
            time.sleep(0.01)
        APP.processEvents()
        progress = getattr(state["viewer"], "_progress_ref", None)
        state["progress_ref"] = progress
        state["progress_visible"] = (
            progress.isVisible() if progress is not None else False
        )
        return state


class AutoSubmitDoneMustAlwaysFinishTest(_FlowBase):
    """Layar pengumpulan tidak boleh pernah menjadi layar terakhir."""

    def test_a_broken_congratulations_page_does_not_leave_a_zombie(self):
        state = self._run_flow(sabotage="congrats")
        self.assertEqual(
            state["submit_calls"], 1,
            "submit tidak pernah dijalankan — test tidak menguji apa pun",
        )
        self.assertFalse(
            state["congrats_visible"],
            "halaman selamat tetap tampil walau `_show_congratulations` "
            "dijadikan melempar — sabotase tidak aktif",
        )
        self.assertIsNone(
            state["progress_ref"],
            "layar 'Mengumpulkan jawaban…' masih hidup setelah langkah "
            "berikutnya gagal — siswa menganggur di depan layar yang tidak "
            "akan pernah selesai (H6)",
        )
        self.assertFalse(
            state["progress_visible"],
            "layar pengumpulan masih TERLIHAT di layar",
        )
        self.assertTrue(
            state["config_visible"],
            "dialog konfigurasi tidak tampil — langkah berikutnya yang gagal "
            "tidak dialihkan ke `all_done`, dan tidak ada layar lain untuk "
            "ditampilkan",
        )
        self.assertGreaterEqual(
            min(state["visible_samples"]), 1,
            "ada momen tanpa satu pun window yang terlihat — Qt menutup "
            "aplikasinya sendiri di titik itu",
        )
        self.assertEqual(
            state["last_window_closed"], [],
            "lastWindowClosed menembak di tengah alur — aplikasi ikut "
            "tertutup",
        )

    def test_a_broken_all_done_does_not_leave_a_zombie_either(self):
        # Jalur `else` (`all_done`). Kalau `all_done` pun gagal, tidak ada
        # layar lain yang bisa ditampilkan — yang diuji di sini bukan
        # "halaman mana", tapi "alur tidak menggantung dan jejaknya ada".
        state = self._run_flow(
            submit_response=FAILURE, submit_delay=0.4, sabotage="all_done",
        )
        self.assertIsNone(
            state["progress_ref"],
            "layar pengumpulan tetap menggantung setelah `all_done` "
            "melempar — tidak ada yang menyelesaikan alur (H6)",
        )
        self.assertFalse(
            state["progress_visible"],
            "layar pengumpulan masih TERLIHAT di layar",
        )
        self.assertFalse(
            getattr(state["viewer"], "_auto_submit_pending", True),
            "`_auto_submit_pending` masih True: alur auto-submit tidak "
            "pernah diselesaikan",
        )


class AutoSubmitWatchdogTest(_FlowBase):
    """Hasil yang tidak pernah datang tidak boleh menggantung selamanya."""

    def test_a_result_that_never_arrives_is_finished_off(self):
        # Batas keras normally ±110 s (retry ~7 + polling ~77 + notifikasi
        # ~18). Di sini dipotong supaya test tidak menunggu 110 detik.
        state = self._run_flow(
            submit_response=SUCCESS,
            submit_delay=3.0,          # jauh lewat batas watchdog
            ceiling_ms=500,
        )
        viewer = state["viewer"]
        self.assertFalse(
            viewer._auto_submit_pending,
            "hasil submit tidak pernah datang dan tidak ada yang menyelesaikan "
            "alur — `_auto_submit_pending` masih True setelah 14 detik",
        )
        self.assertIsNone(
            getattr(viewer, "_progress_ref", None),
            "layar pengumpulan masih hidup padahal hasilnya tidak akan "
            "pernah datang",
        )
        self.assertTrue(
            state["config_visible"],
            "dialog konfigurasi tidak kembali — PC lab mengunci begitu saja",
        )
        self.assertFalse(
            state["congrats_visible"],
            "hasil basi yang telat tetap memunculkan halaman selamat di atas "
            "layar siswa berikutnya — watchdog seharusnya sudah menyelesaikan "
            "alur sebelum itu terjadi",
        )


if __name__ == "__main__":
    unittest.main()