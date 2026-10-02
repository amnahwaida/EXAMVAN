"""C1 + M1 (ronde 5) — auto-submit tidak boleh mematikan seluruh aplikasi.

`_auto_submit_and_exit()` menutup viewer lalu mengirim jawaban di background.

Regresi dari commit a6948a8: viewer ditutup (`self.close()`) sementara
`_on_viewer_closed` menahan langkah ke dialog konfigurasi selama
`_auto_submit_pending` True. Akibatnya viewer adalah top-level TERAKHIR
yang terlihat, `setQuitOnLastWindowClosed` tidak pernah di-override
(default True) → `lastWindowClosed` → `app.quit()` → `app.exec_()`
kembali → thread submit (daemon) mati di tengah jalan. Jawaban tidak
pernah sampai server dan tidak ada yang memberitahu siswa.

Test `AutoSubmitKeepsAppAliveTest` memakai TOPOLOGI ASLI + EVENT LOOP
ASLI: `main_mod.main()` dijalankan sungguhan, `on_exam_selected`
sungguhan, viewer sungguhan, dan `app.exec_()` sungguhan (dengan
QTimer sebagai rem keras supaya test tidak menggantung). Yang dipalsukan
hanya jaringan, storage, dan backend security — bukan alur window-nya.
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

from PyQt5.QtCore import QObject, QTimer, Qt, pyqtSignal
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


def _live_viewer_class():
    """Kelas ExamViewerWindow yang SEDANG hidup di `sys.modules`.

    `tests/test_admin_password.py` memanggil
    `importlib.reload(examvan.ui.exam_viewer)`, dan reload membuat objek
    KELAS BARU. Nama yang diimpor di atas jadi kelas lama: `isinstance`
    miliknya tidak pernah cocok dengan jendela yang benar-benar dibuat
    `__main__.on_exam_selected`, dan harness ini diam-diam hanya
    menunggu batas waktu. Produksi sendiri selalu resolving lewat
    `sys.modules` — test harus melakukan hal yang sama.
    """
    return importlib.import_module("examvan.ui.exam_viewer").ExamViewerWindow


QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]


# ---------------------------------------------------------------------------
# Test double: hanya lapisan yang BOLEH dipalsukan (jaringan/backend/dialog)
# ---------------------------------------------------------------------------


class _FakeConfigDialog(QWidget):
    """ServerConfigDialog secukupnya untuk alur `on_exam_selected`.

    Widget sungguhan (bukan Mock) karena yang sedang diuji justru topologi
    window: `hide()` benar-benar menyembunyikannya dari
    `QApplication.topLevelWidgets()`, dan `lastWindowClosed` tidak
    pernah menembak kalau ada satu pun window lain yang terlihat.
    """

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
    """WaitingApprovalDialog yang langsung disetujui (tanpa dialog sungguhan).

    `exec_()` menyembunyikan dirinya dulu, sama seperti dialog asli yang
    menutup dirinya saat persetujuan datang. Kalau tidak, window ini tetap
    terlihat sepanjang ujian dan menutupi bug C1: `lastWindowClosed` hanya
    menembak kalau tidak ada window lain yang terlihat.
    """

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
    """`QApplication` selama `main()` — mengembalikan instance yang ada.

    `main()` memanggil `QApplication(sys.argv)`, dan PyQt5 hanya mengizinkan
    SATU instance per proses. Test ini butuh event loop sungguhan, jadi
    `main()` harus memakai QApplication yang sudah dibuat test — bukan Mock
    (Mock membuat `_maximize_window` gagal: `primaryScreen()` jadi MagicMock).

    Hanya konstruktor yang dialihkan; static method-nya diteruskan ke kelas
    asli supaya semua helper window tetap jalan persis seperti produksi.
    """

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
    """Sembunyikan + hapus semua window top-level antar-test.

    `hide()` dulu, bukan `close()`: `close()` pada window yang sedang
    terlihat menembakkan `lastWindowClosed` → `app.quit()`, dan flag
    `quitNow` itu masih ada untuk event loop test BERIKUTNYA (loop
    berikutnya langsung kembali tanpa menjalankan apa pun).
    `deleteLater()` tidak memanggil `closeEvent`, jadi tidak ada sinyal
    `closed` yang menembak ke closure `__main__` milik test sebelumnya.
    """
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
        self._tmp = tempfile.mkdtemp(prefix="examvan-r5-autosubmit-")
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
# C1 — topologi + event loop sungguhan
# ---------------------------------------------------------------------------


class _FlowResult:
    """Catatan hasil satu kali jalannya `main()` + `app.exec_()`."""

    def __init__(self) -> None:
        self.last_window_closed: list = []
        self.visible_samples: list = []
        self.submit_finished = threading.Event()
        self.submit_calls = 0
        self.congrats_visible = False
        self.congrats_text = ""
        self.config_visible_at_end = False
        self.config_enable_connect_calls = 0
        self.exec_ran_seconds = 0.0
        self.viewer = None
        self.exam_started = False
        self.auto_submit_started = False
        self.done = False


class AutoSubmitKeepsAppAliveTest(_FlowBase):
    """`app.exec_()` harus tetap hidup sampai hasil submit background tiba."""

    HARD_STOP_MS = 9000

    def _run_real_flow(self, *, submit_response, submit_delay=0.7,
                       expect_congrats=True):
        state = _FlowResult()

        exam = Exam.from_json({
            "id": 11, "name": "Ujian", "status": "active",
            "security_level": "medium",
            "questions": QUESTIONS,
        })
        dialog = _FakeConfigDialog()
        backend = _FakeBackend()
        timers = []

        def _fake_submit(*args, **kwargs):
            state.submit_calls += 1
            # Jaringan lambat: justru kasus lapangan yang paling sering —
            # retry sampai ~7 dtk + polling 202 sampai ~77 dtk.
            time.sleep(submit_delay)
            state.submit_finished.set()
            return submit_response

        def _find_viewer():
            for w in APP.topLevelWidgets():
                if isinstance(w, _live_viewer_class()):
                    return w
            return None

        def _drive_flow():
            # Tick 1: initiate exam lewat sinyal sungguhan
            # (`exam_selected` → `on_exam_selected`).
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

        def _sample():
            state.visible_samples.append(
                (time.monotonic(), len(_visible_top_levels()))
            )

        def _finish():
            state.config_visible_at_end = dialog.isVisible()
            state.config_enable_connect_calls = dialog.enable_connect_calls
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
                state.congrats_text = page.congrats_text()
                # `page.close()` → `page_closed` → `all_done` →
                # `_after_viewer_gone` → dialog konfigurasi. Semua
                # sinkron, jadi pengamatan boleh langsung diambil.
                page.close()
                watcher.stop()
                _finish()
                return
            if not expect_congrats and dialog.isVisible():
                # Dialog konfigurasi = bukti `all_done` sudah sampai:
                # hanya `_after_viewer_gone` yang menampilkannya.
                watcher.stop()
                _finish()

        driver = QTimer()
        driver.setInterval(20)
        driver.timeout.connect(_drive_flow)
        timers.append(driver)

        sampler = QTimer()
        sampler.setInterval(20)
        sampler.timeout.connect(_sample)
        timers.append(sampler)

        watcher = QTimer()
        watcher.setInterval(20)
        watcher.timeout.connect(_watch_flow)
        timers.append(watcher)

        # Rem keras: test tidak boleh menggantung selamanya. Objek QTimer
        # (bukan `singleShot`) supaya bisa dihentikan di `finally` — timer
        # yang bocor ke test berikutnya akan mematikan event loop-nya.
        hard_stop = QTimer()
        hard_stop.setSingleShot(True)
        hard_stop.setInterval(self.HARD_STOP_MS)
        hard_stop.timeout.connect(lambda: None if state.done else APP.quit())
        timers.append(hard_stop)

        last_window_closed = []
        APP.lastWindowClosed.connect(
            lambda: last_window_closed.append(time.monotonic())
        )
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
                sampler.start()
                watcher.start()
                hard_stop.start()
                main_mod.main()
                state.exec_ran_seconds = time.monotonic() - started
        finally:
            state.done = True          # timer sisa jadi tidak bocor ke test lain
            state.last_window_closed = list(last_window_closed)
            for t in timers:
                t.stop()
            try:
                APP.lastWindowClosed.disconnect()
            except (RuntimeError, TypeError):
                pass
        return state

    # -- (a) lastWindowClosed tidak pernah menembak ----------------------

    def test_last_window_closed_never_fires(self):
        state = self._run_real_flow(submit_response=SubmitResponse(
            success=True, status="done", message="ok",
            congrats_message="Selamat, Budi!",
        ))
        self.assertEqual(
            state.last_window_closed, [],
            "lastWindowClosed menembak → app.quit() → app.exec_() kembali "
            "→ thread submit background dibunuh di tengah jalan (jawaban "
            "tidak pernah sampai server)",
        )

    # -- (b) selalu ada satu window terlihat ------------------------------

    def test_a_visible_window_survives_the_whole_submit(self):
        state = self._run_real_flow(submit_response=SubmitResponse(
            success=True, status="done", message="ok",
            congrats_message="Selamat, Budi!",
        ))
        self.assertTrue(state.visible_samples, "tidak ada sampel window sama sekali")
        self.assertEqual(
            min(count for _t, count in state.visible_samples), 1,
            "ada momen tanpa satu pun window yang terlihat selama auto-submit "
            "— di titik itu Qt menembakkan lastWindowClosed dan aplikasi "
            "berhenti",
        )

    # -- (c) submit background benar-benar selesai -------------------------

    def test_background_submit_completes_before_the_app_goes_away(self):
        state = self._run_real_flow(submit_response=SubmitResponse(
            success=True, status="done", message="ok",
            congrats_message="Selamat, Budi!",
        ))
        self.assertTrue(
            state.submit_finished.is_set(),
            "thread submit background tidak pernah selesai: event loop "
            "sudah kembali (%.2fs) sementara thread masih jalan"
            % state.exec_ran_seconds,
        )
        self.assertEqual(state.submit_calls, 1)

    # -- (d) sukses: halaman selamat lalu dialog konfigurasi ---------------

    def test_success_shows_congratulations_then_returns_to_config(self):
        state = self._run_real_flow(submit_response=SubmitResponse(
            success=True, status="done", message="ok",
            congrats_message="Selamat, Budi!",
        ))
        self.assertTrue(
            state.congrats_visible,
            "halaman selamat tidak pernah tampil setelah submit sukses",
        )
        self.assertEqual(state.congrats_text, "Selamat, Budi!")
        self.assertTrue(
            state.config_visible_at_end,
            "setelah halaman selamat ditutup, dialog konfigurasi untuk "
            "siswa berikutnya tidak kembali",
        )

    # -- (e) gagal: all_done lalu dialog konfigurasi ----------------------

    def test_failure_emits_all_done_and_returns_to_config(self):
        state = self._run_real_flow(
            submit_response=SubmitResponse(success=False, message="jaringan mati"),
            expect_congrats=False,
        )
        self.assertTrue(state.submit_finished.is_set())
        self.assertFalse(state.congrats_visible)
        # Jalur gagal menutup layar pengumpulan tepat setelah `all_done` —
        # kalau urutan itu terbalik, `close()`-nya yang menjadi top-level
        # terakhir yang terlihat dan aplikasi ikut tertutup.
        self.assertEqual(
            state.last_window_closed, [],
            "lastWindowClosed menembak di jalur gagal — aplikasi ikut "
            "tertutup sebelum siswa berikutnya bisa masuk",
        )
        self.assertGreaterEqual(
            state.config_enable_connect_calls, 1,
            "all_done tidak pernah sampai → dialog konfigurasi tidak "
            "kembali dan siswa berikutnya tidak bisa masuk",
        )
        self.assertTrue(
            state.config_visible_at_end,
            "dialog konfigurasi tidak dikembalikan setelah submit gagal",
        )
        # Jawaban tetap di disk untuk "Kirim Lagi" (re-entry).
        self.assertEqual(config.load_answers(11), {"1": "A"})


# ---------------------------------------------------------------------------
# C1 + M1 — unit-level: bentuk jendela progress, capture protection, lockdown
# ---------------------------------------------------------------------------


class AutoSubmitProgressWindowTest(_FlowBase):
    """Jendela "Mengumpulkan jawaban…" harus ada, terlihat, dan terlindungi."""

    def _make_viewer(self):
        exam = Exam.from_json({
            "id": 21, "name": "Ujian", "status": "active",
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

    def _pump_until(self, predicate, timeout=5.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.01)
        return False

    def test_auto_submit_leaves_a_visible_window_on_screen(self):
        backend = _FakeBackend()
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            win._auto_submit_and_exit()
            try:
                progress = getattr(win, "_progress_ref", None)
                self.assertIsNotNone(
                    progress,
                    "tidak ada jendela progress: viewer ditutup dan tidak "
                    "satu pun top-level terlihat → lastWindowClosed → "
                    "app.quit() → thread submit mati",
                )
                self.assertIsInstance(progress, QWidget)
                self.assertTrue(
                    progress.isVisible(),
                    "jendela progress tidak terlihat — topologi yang "
                    "menjaga event loop tetap hidup tidak ada",
                )
                self.assertFalse(win.isVisible(), "viewer harus hide, bukan close")
                self.assertTrue(win._auto_submit_pending)
                self.assertTrue(
                    any(w.isVisible() for w in APP.topLevelWidgets()),
                    "tidak ada window yang terlihat selama auto-submit",
                )
            finally:
                self._pump_until(lambda: win._auto_submit_pending is False, 5.0)

    def test_progress_window_shape(self):
        backend = _FakeBackend()
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            win._auto_submit_and_exit()
            try:
                progress = win._progress_ref
                self.assertIsNone(
                    progress.parent(),
                    "progress window harus parent=None supaya tidak ikut "
                    "ikutan viewer yang menyembunyikan diri",
                )
                self.assertTrue(
                    progress.testAttribute(Qt.WA_DeleteOnClose),
                    "WA_DeleteOnClose: window dibuang sendiri saat ditutup",
                )
                self.assertTrue(
                    progress.windowFlags() & Qt.WindowStaysOnTopHint,
                    "layar pengumpulan harus selalu di atas",
                )
                self.assertIsInstance(progress.findChild(QLabel), QLabel)
                self.assertIsInstance(progress.findChild(QProgressBar),
                                      QProgressBar)
                bar = progress.findChild(QProgressBar)
                self.assertEqual(bar.minimum(), 0)
                self.assertEqual(
                    bar.maximum(), 0,
                    "progressbar harus indeterminate (0..0)",
                )
            finally:
                self._pump_until(lambda: win._auto_submit_pending is False, 5.0)

    def test_progress_window_is_capture_protected_while_pending(self):
        # M1: jendela yang tampil selama auto-submit harus memakai proteksi
        # capture yang sama seperti halaman selamat.
        backend = _FakeBackend()
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            win._auto_submit_and_exit()
            try:
                self.assertTrue(
                    backend.capture_protected,
                    "tidak ada proteksi capture yang dipasang selama auto-submit",
                )
                self.assertIn(
                    win._progress_ref, backend.capture_protected,
                    "jendela progress tidak diberi proteksi capture",
                )
            finally:
                self._pump_until(lambda: win._auto_submit_pending is False, 5.0)

    def test_progress_window_is_the_visible_one_while_pending(self):
        # M1 + C1: lockdown sengaja tetap aktif selama menunggu, jadi
        # enforcer masih mem-poll JENDELA UJIAN yang sudah disembunyikan.
        # Yang boleh terlihat hanya layar pengumpulan — kalau jendela ujian
        # muncul kembali di atasnya, siswa melihat naskah ujian (dan tombol
        # yang sudah dimatikan) alih-alih "sedang mengumpulkan".
        backend = _FakeBackend()
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            win._auto_submit_and_exit()
            try:
                visible = [w for w in APP.topLevelWidgets() if w.isVisible()]
                self.assertIn(
                    win._progress_ref, visible,
                    "layar pengumpulan tidak terlihat",
                )
                self.assertNotIn(
                    win, visible,
                    "jendela ujian muncul kembali di atas layar pengumpulan",
                )
            finally:
                self._pump_until(lambda: win._auto_submit_pending is False, 5.0)

    def test_lockdown_is_held_until_the_background_result_arrives(self):
        # M1: `deactivate()` tidak boleh dipanggil pada awal auto-submit —
        # hook/WDA/ClipCursor dibebaskan untuk SELURUH budget submit.
        backend = _FakeBackend()
        resp = SubmitResponse(success=True, status="done", message="ok",
                              congrats_message="Selamat!")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            enforcer = win._security
            win._auto_submit_and_exit()
            try:
                self.assertEqual(
                    enforcer.deactivate_calls, 0,
                    "lockdown dilepas sebelum hasil submit tiba — mesin "
                    "terbuka tanpa pengawasan selama hampir dua menit",
                )
                self.assertTrue(
                    self._pump_until(lambda: enforcer.deactivate_calls >= 1, 5.0),
                    "lockdown tidak pernah dilepas setelah hasil tiba",
                )
            finally:
                self._pump_until(lambda: win._auto_submit_pending is False, 5.0)

    def test_progress_window_is_closed_after_the_result_arrives(self):
        backend = _FakeBackend()
        resp = SubmitResponse(success=False, message="jaringan mati")
        with mock.patch("examvan.security.get_backend", return_value=backend), \
             mock.patch.object(api, "submit_with_retry", return_value=resp):
            win = self._make_viewer()
            fired = []
            win.all_done.connect(lambda: fired.append(True))
            win._auto_submit_and_exit()
            progress = win._progress_ref

            def _progress_gone():
                try:
                    return not progress.isVisible()
                except RuntimeError:
                    # Objek C++ sudah dihapus oleh WA_DeleteOnClose.
                    return True

            self.assertTrue(
                self._pump_until(lambda: bool(fired), 5.0),
                "all_done tidak pernah menembak",
            )
            self.assertTrue(
                self._pump_until(_progress_gone, 5.0),
                "jendela progress tidak ditutup setelah hasil tiba — layar "
                "berikutnya akan tertutup layar pengumpulan",
            )
            self.assertIsNone(
                win._progress_ref,
                "referensi jendela progress harus dilepas supaya tidak "
                "menggantung sampai viewer di-GC",
            )


if __name__ == "__main__":
    unittest.main()