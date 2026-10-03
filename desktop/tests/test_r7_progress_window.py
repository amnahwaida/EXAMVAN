"""C1 (KRITIS) — layar "Mengumpulkan jawaban…" tidak boleh bisa ditutup siswa.

Bug
---
`_build_progress_window()` membuat `QWidget(None)` polos: hanya
`Qt.WindowStaysOnTopHint` + `WA_DeleteOnClose`. Tidak ada `closeEvent`,
tidak ada `keyPressEvent`, tidak modal, tidak frameless.

Selama budget submit (±84 detik: retry ~7 dtk + polling 202 ~77 dtk) jendela
ini adalah SATU-SATUNYA top-level yang terlihat — dialog konfigurasi sudah
`hide()`-kan di `__main__.on_exam_selected`, viewer di-`hide()`kan. Maka:

    siswa Alt+F4 / klik X / tekan Esc
      -> close() menutup top-level terakhir
      -> lastWindowClosed
      -> app.quit()  (setQuitOnLastWindowClosed tidak pernah di-override)
      -> app.exec_() kembali
      -> thread submit yang baru saja dimulai MATI di tengah HTTP

Jawaban tidak pernah sampai server, tidak ada halaman selamat, tidak ada
notifikasi: siswa menganggur di depan layar mati.

Test `StudentCloseAttemptKeepsAppAliveTest` memakai TOPOLOGI ASLI + EVENT LOOP
ASLI: `main_mod.main()` sungguhan, `on_exam_selected` sungguhan, viewer
sungguhan, `app.exec_()` sungguhan (rem keras `QTimer`). Yang dipalsukan hanya
jaringan, storage, dan backend security. Test yang cuma memompa event dengan
tangan tidak akan pernah melihat `lastWindowClosed` — dua bug kritis terakhir
di proyek ini sama-sama tidak terlihat di sana.
"""

from __future__ import annotations

import contextlib
import importlib
import logging
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

from PyQt5.QtCore import QEvent, QObject, QTimer, Qt, pyqtSignal
from PyQt5.QtGui import QCloseEvent, QKeyEvent
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
    QLabel,
    QLineEdit,
    QProgressBar,
    QVBoxLayout,
    QWidget,
)

from examvan import __main__ as main_mod
from examvan import api, config, notify
from examvan.models import Exam, SubmitResponse

APP = QApplication.instance() or QApplication([])

QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]


def _live_viewer_class():
    """Kelas ExamViewerWindow yang SEDANG hidup di `sys.modules`.

    `tests/test_admin_password.py` me-reload modul ini, dan reload membuat
    objek KELAS BARU. Produksi selalu resolving lewat `sys.modules` — test
    harus melakukan hal yang sama.
    """
    return importlib.import_module("examvan.ui.exam_viewer").ExamViewerWindow


# ---------------------------------------------------------------------------
# Test double: hanya lapisan yang BOLEH dipalsukan (jaringan/backend/dialog)
# ---------------------------------------------------------------------------


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
    """WaitingApprovalDialog yang langsung disetujui."""

    def exec_(self):  # noqa: N802 (Qt API)
        self.hide()
        return QDialog.Accepted


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
    """Backend security yang mencatat proteksi capture per window."""

    def __init__(self) -> None:
        self.capture_protected = []

    def has_multiple_monitors(self) -> bool:
        return False

    def set_capture_protection(self, window) -> None:
        self.capture_protected.append(window)


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


# ---------------------------------------------------------------------------
# Kebersihan window antar-test
# ---------------------------------------------------------------------------


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


class _FlowBase(unittest.TestCase):
    """Tumpukan bersama: config dir sementara + jaringan/dialog dipalsukan."""

    def setUp(self) -> None:
        _purge_top_levels()
        self._tmp = tempfile.mkdtemp(prefix="examvan-r7-progress-")
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
        config._cache = None

    def tearDown(self) -> None:
        _purge_top_levels()
        for p in reversed(self._patches):
            p.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)
        _purge_top_levels()


# ---------------------------------------------------------------------------
# C1 — siswa menutup layar pengumpulan, event loop sungguhan
# ---------------------------------------------------------------------------


class _FlowResult:
    """Catatan hasil satu kali jalannya `main()` + `app.exec_()`."""

    def __init__(self) -> None:
        self.last_window_closed: list = []
        self.visible_samples: list = []
        self.progress_visible_samples: list = []
        self.submit_finished = threading.Event()
        self.submit_calls = 0
        self.congrats_visible = False
        self.config_visible_at_end = False
        self.exec_ran_seconds = 0.0
        self.attack_done = False
        self.attack_warnings: list = []
        self.viewer = None
        self.exam_started = False
        self.auto_submit_started = False
        self.done = False


class _WarningWatcher(logging.Handler):
    """Kumpulkan pesan WARNING dari logger viewer selama alur berjalan.

    Penolakan close/penelan tombol harus TERLIHAT: di lapangan tidak ada
    yang bisa memastikan Alt+F4 memang ditahan, atau hanya tidak berhasil
    karena kebetulan. Log adalah satu-satunya jejaknya.
    """

    def __init__(self) -> None:
        super().__init__(level=logging.WARNING)
        self.records: list = []

    def emit(self, record) -> None:
        try:
            self.records.append(record.getMessage())
        except Exception:
            pass


class StudentCloseAttemptKeepsAppAliveTest(_FlowBase):
    """Layar pengumpulan harus menolak close Alt+F4 / Esc / tombol X."""

    HARD_STOP_MS = 14000
    # Jaringan lambat: justru kasus lapangan yang paling sering terjadi
    # (retry sampai ~7 dtk + polling 202 sampai ~77 dtk).
    SUBMIT_DELAY = 1.5
    ATTACK_AFTER = 0.35

    def _run_real_flow(self, *, attack, expect_congrats=True):
        state = _FlowResult()

        exam = Exam.from_json({
            "id": 31, "name": "Ujian", "status": "active",
            "security_level": "medium",
            "questions": QUESTIONS,
        })
        dialog = _FakeConfigDialog()
        backend = _FakeBackend()
        timers = []
        attacked_at = {}

        def _fake_submit(*args, **kwargs):
            state.submit_calls += 1
            time.sleep(self.SUBMIT_DELAY)
            state.submit_finished.set()
            return SubmitResponse(
                success=True, status="done", message="ok",
                congrats_message="Selamat, Budi!",
            )

        def _find_viewer():
            for w in APP.topLevelWidgets():
                if isinstance(w, _live_viewer_class()):
                    return w
            return None

        def _drive_flow():
            if not state.exam_started:
                state.exam_started = True
                dialog.exam_selected.emit(
                    exam, "https://exam.example", {"nama": "Budi"}
                )
                return
            viewer = _find_viewer()
            if viewer is None:
                return                       # viewer belum ada — coba lagi
            driver.stop()
            state.viewer = viewer
            viewer._answer_sheet.build_from_questions(exam.questions)
            viewer._answer_sheet.restore_answers({"1": "A"})
            state.auto_submit_started = True
            viewer._auto_submit_and_exit()
            attacked_at["at"] = time.monotonic()

        def _attack_once():
            if state.auto_submit_started and "at" in attacked_at:
                if time.monotonic() - attacked_at["at"] < self.ATTACK_AFTER:
                    return
                attacker.stop()
                state.attack_done = True
                attack(state.viewer, state.viewer._progress_ref)

        def _sample():
            state.visible_samples.append(
                (time.monotonic(), len(_visible_top_levels()))
            )
            progress = getattr(state.viewer, "_progress_ref", None)
            if progress is not None:
                try:
                    state.progress_visible_samples.append(progress.isVisible())
                except RuntimeError:
                    # Objek C++ sudah dihapus WA_DeleteOnClose.
                    state.progress_visible_samples.append(False)

        def _finish():
            state.config_visible_at_end = dialog.isVisible()
            state.done = True
            hard_stop.stop()
            APP.quit()

        def _watch_flow():
            viewer = state.viewer
            if viewer is None or not state.auto_submit_started:
                return
            page = getattr(viewer, "_congrats_ref", None)
            if expect_congrats and page is not None and page.isVisible():
                state.congrats_visible = True
                page.close()
                watcher.stop()
                _finish()
                return
            if not expect_congrats and dialog.isVisible():
                watcher.stop()
                _finish()

        driver = QTimer()
        driver.setInterval(20)
        driver.timeout.connect(_drive_flow)
        timers.append(driver)

        attacker = QTimer()
        attacker.setInterval(20)
        attacker.timeout.connect(_attack_once)
        timers.append(attacker)

        sampler = QTimer()
        sampler.setInterval(20)
        sampler.timeout.connect(_sample)
        timers.append(sampler)

        watcher = QTimer()
        watcher.setInterval(20)
        watcher.timeout.connect(_watch_flow)
        timers.append(watcher)

        hard_stop = QTimer()
        hard_stop.setSingleShot(True)
        hard_stop.setInterval(self.HARD_STOP_MS)
        hard_stop.timeout.connect(lambda: None if state.done else APP.quit())
        timers.append(hard_stop)

        last_window_closed = []
        APP.lastWindowClosed.connect(
            lambda: last_window_closed.append(time.monotonic())
        )
        watcher_log = _WarningWatcher()
        logging.getLogger("examvan.ui.exam_viewer").addHandler(watcher_log)
        started = time.monotonic()
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
                 mock.patch("examvan.security.get_backend", return_value=backend), \
                 mock.patch.object(api, "submit_with_retry",
                                   side_effect=_fake_submit):
                driver.start()
                attacker.start()
                sampler.start()
                watcher.start()
                hard_stop.start()
                main_mod.main()
                state.exec_ran_seconds = time.monotonic() - started
        finally:
            state.done = True          # timer sisa jadi tidak bocor ke test lain
            state.last_window_closed = list(last_window_closed)
            state.attack_warnings = list(watcher_log.records)
            logging.getLogger("examvan.ui.exam_viewer").removeHandler(
                watcher_log
            )
            for t in timers:
                t.stop()
            try:
                APP.lastWindowClosed.disconnect()
            except (RuntimeError, TypeError):
                pass
        return state

    # -- assertions bersama ------------------------------------------------

    def _assert_flow_survived(self, state, *, how):
        self.assertTrue(state.attack_done, f"serangan {how} tidak pernah dikirim")
        self.assertEqual(
            state.last_window_closed, [],
            f"{how}: lastWindowClosed menembak → app.quit() → app.exec_() "
            f"kembali → thread submit background dibunuh di tengah HTTP "
            f"(jawaban tidak pernah sampai server)",
        )
        self.assertTrue(
            state.progress_visible_samples,
            "tidak ada sampel jendela pengumpulan sama sekali",
        )
        self.assertTrue(
            all(state.progress_visible_samples),
            f"{how}: layar 'Mengumpulkan jawaban…' hilang dari layar — "
            f"siswa menganggur depan layar mati",
        )
        self.assertGreaterEqual(
            min(count for _t, count in state.visible_samples), 1,
            f"{how}: ada momen tanpa satu pun window yang terlihat selama "
            f"auto-submit — di titik itu Qt menutup aplikasi",
        )
        self.assertTrue(
            state.submit_finished.is_set(),
            f"{how}: thread submit background tidak pernah selesai: event loop "
            f"sudah kembali (%.2fs) sementara thread masih jalan"
            % state.exec_ran_seconds,
        )
        self.assertEqual(state.submit_calls, 1)
        self.assertTrue(
            state.congrats_visible,
            f"{how}: halaman selamat tidak pernah tampil — alur berhenti di "
            f"tengah jalan",
        )
        self.assertTrue(
            state.config_visible_at_end,
            f"{how}: dialog konfigurasi untuk siswa berikutnya tidak kembali",
        )

    # -- (a) Alt+F4 --------------------------------------------------------

    def test_alt_f4_while_collecting_does_not_kill_the_submit(self):
        def _alt_f4(_viewer, progress):
            # Alt+F4 persis seperti yang dikirim Windows: QKeyEvent dengan
            # modifier Alt. Tanpa handler, tidak ada satu pun yang menahan —
            # dan pada top-level yang sedang terlihat itu berarti tutup.
            ev = QKeyEvent(QEvent.KeyPress, Qt.Key_F4, Qt.AltModifier, "F4")
            QApplication.sendEvent(progress, ev)

        state = self._run_real_flow(attack=_alt_f4)
        self._assert_flow_survived(state, how="Alt+F4")
        self.assertTrue(
            any("ditelan" in line for line in state.attack_warnings),
            "Alt+F4 tidak ditelan apa pun oleh layar pengumpulan — tidak "
            "ada handler yang menahannya: %r" % (state.attack_warnings,),
        )

    # -- (b) tombol close / ESC -------------------------------------------

    def test_closing_the_progress_window_does_not_kill_the_submit(self):
        state = self._run_real_flow(
            attack=lambda _viewer, progress: progress.close()
        )
        self._assert_flow_survived(state, how="close()")

    # -- (c) keduanya, supaya tidak ada handler yang saling meniadakan ---

    def test_close_and_escape_together_still_do_not_kill_the_submit(self):
        def _both(_viewer, progress):
            progress.close()
            for key, mods, text in (
                (Qt.Key_Escape, Qt.NoModifier, "Esc"),
                (Qt.Key_Menu, Qt.NoModifier, "Menu"),
                (Qt.Key_F4, Qt.AltModifier, "F4"),
            ):
                QApplication.sendEvent(
                    progress,
                    QKeyEvent(QEvent.KeyPress, key, mods, text),
                )

        state = self._run_real_flow(attack=_both)
        self._assert_flow_survived(state, how="close()+Esc/Menu/Alt+F4")


# ---------------------------------------------------------------------------
# C1 — unit: handler-nya benar-benar ada dan berperilaku benar
# ---------------------------------------------------------------------------


class ProgressWindowHandlersTest(_FlowBase):
    """`closeEvent` menolak, `keyPressEvent` menelan — di SEMUA tier."""

    def _make_viewer(self):
        exam = Exam.from_json({
            "id": 32, "name": "Ujian", "status": "active",
            "security_level": "medium", "questions": QUESTIONS,
        })
        win = _live_viewer_class()(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(win.hide)
        win._answer_sheet.build_from_questions(exam.questions)
        win._answer_sheet.restore_answers({"1": "A"})
        return win

    def _start_auto_submit(self, win):
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=_FakeBackend()), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win._auto_submit_and_exit()

    def _pump_until(self, predicate, timeout=5.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.01)
        return False

    def _pending_progress(self):
        win = self._make_viewer()
        self._start_auto_submit(win)
        progress = win._progress_ref
        self.assertIsNotNone(
            progress,
            "tidak ada layar pengumpulan sama sekali — test tidak menguji apa pun",
        )
        self.addCleanup(
            lambda: self._pump_until(lambda: win._auto_submit_pending is False, 5.0)
        )
        return win, progress

    # -- closeEvent --------------------------------------------------------

    def test_close_event_is_refused_while_the_submit_is_pending(self):
        _win, progress = self._pending_progress()
        ev = QCloseEvent()
        with self.assertLogs("examvan.ui.exam_viewer", level="WARNING") as logs:
            progress.closeEvent(ev)
        self.assertFalse(
            ev.isAccepted(),
            "closeEvent menerima menutupnya — Alt+F4 / tombol X mematikan "
            "seluruh aplikasi di tengah submit (C1)",
        )
        self.assertTrue(
            any("pengumpulan" in line for line in logs.output),
            "penolakan close tidak dicatat sama sekali — di lapangan tidak "
            "ada jejak bahwa siswa menekan Alt+F4: %r" % (logs.output,),
        )
        self.assertTrue(progress.isVisible())

    def test_progress_window_has_no_title_bar_button_to_click(self):
        _win, progress = self._pending_progress()
        flags = progress.windowFlags()
        self.assertTrue(
            flags & Qt.FramelessWindowHint,
            "layar pengumpulan tidak frameless — tombol X di title bar "
            "tetap bisa diklik siswa",
        )
        self.assertEqual(
            progress.windowModality(), Qt.ApplicationModal,
            "layar pengumpulan harus application-modal: window lain tidak "
            "boleh menerima klik selama submit berjalan",
        )
        self.assertTrue(flags & Qt.WindowStaysOnTopHint)

    # -- keyPressEvent -----------------------------------------------------

    def test_close_and_menu_keys_are_swallowed(self):
        _win, progress = self._pending_progress()
        for key, mods, text in (
            (Qt.Key_F4, Qt.AltModifier, "F4"),
            (Qt.Key_F4, Qt.NoModifier, "F4"),
            (Qt.Key_Escape, Qt.NoModifier, "Esc"),
            (Qt.Key_Menu, Qt.NoModifier, "Menu"),
        ):
            ev = QKeyEvent(QEvent.KeyPress, key, mods, text)
            with self.assertLogs("examvan.ui.exam_viewer", level="WARNING"):
                QApplication.sendEvent(progress, ev)
            self.assertFalse(
                ev.isAccepted(),
                "tombol %s tidak ditelan — jalan menutup jendela tetap terbuka"
                % text,
            )

    def test_a_regular_key_is_not_swallowed(self):
        # Penelan SELALU berlaku adalah bug yang sama besarnya: siswa tidak
        # boleh terkunci total, dan handler bawaan harus tetap jalan.
        _win, progress = self._pending_progress()
        ev = QKeyEvent(QEvent.KeyPress, Qt.Key_A, Qt.NoModifier, "a")
        with mock.patch.object(
            importlib.import_module("examvan.ui.exam_viewer").log, "warning"
        ) as warned:
            QApplication.sendEvent(progress, ev)
        self.assertFalse(
            warned.called,
            "tombol biasa ikut ditelan sebagai kalau Alt+F4 — penalti "
            "yang sama besarnya dengan bug yang diperbaiki",
        )

    # -- jalur internal tetap boleh menutup --------------------------------

    def test_the_internal_close_path_still_closes_the_window(self):
        win, progress = self._pending_progress()
        win._close_progress_window()
        self.assertFalse(
            progress.isVisible(),
            "layar pengumpulan tidak bisa ditutup oleh alur internal — "
            "halaman berikutnya akan tertimpa layar pengumpulan",
        )
        self.assertIsNone(
            win._progress_ref,
            "referensi harus dilepas supaya tidak menggantung sampai viewer di-GC",
        )

    def test_the_result_path_closes_the_window_even_though_closes_are_refused(self):
        win, progress = self._pending_progress()
        fired = []
        win.all_done.connect(lambda: fired.append(True))
        win._on_auto_submit_done(False, "jaringan mati")
        self.assertTrue(fired, "all_done tidak pernah menembak")
        self.assertIsNone(win._progress_ref)
        self.assertFalse(
            progress.isVisible(),
            "layar pengumpulan masih terlihat setelah hasil tiba — closeEvent "
            "menahan juga jalur internal",
        )


if __name__ == "__main__":
    unittest.main()