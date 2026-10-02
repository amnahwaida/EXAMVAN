"""MEDIUM + monitor ganda: pemberitahuan harus SEBELUM persetujuan dibakar.

Bug (MEDIUM) — peringatan datang belakangan
------------------------------------------
`__main__.on_exam_selected` menjalankan `waiting_dlg.exec_()` lebih dulu, baru
menampilkan peringatan multi-monitor medium. Efeknya di kelas: pengawas
dipanggil, dialog persetujuan dibuka, dan siswa baru diberi tahu PC-nya punya dua
layar SETELAH persetujuan itu diberikan. Strict punya gate awal di
`server_config._strict_monitor_ok()` (fail-CLOSED, menolak mulai), medium tidak
punya apa pun.

Medium memang "warn and continue" — jadi gate awalnya harus NON-BLOCKING
(`QMessageBox.information`), bukan menolak, dan harus fail-OPEN bila detektor
error: tidak diketahui ≠ lebih dari satu layar.

Test menjalankan `main()` sungguhan dengan backend pencatat dan mencatat
URUTAN permintaan dialog terhadap pembuatan dialog persetujuan. `_Recording
MessageBox` juga mencatat "dialog dibangun", jadi urutan yang diukur adalah
urutan yang benar-benar terjadi, bukan urutan baris sumber.

Bonus (item 12): `__main__` pernah membandingkan tier dengan literal
`exam.level == "medium"`. `Exam.level` sudah kanonik (`normalize_level`), jadi
branch ini diuji dengan kata server yang SEBENARNYA dipakai ("medium") dan
dengan "high" — yang tidak pernah sampai ke branch medium karena `Exam.level`
memetakan "high" → "strict", dan gate strict-lah yang harus tetap menyala.
"""

from __future__ import annotations

import importlib
import os
import time
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QTimer, pyqtSignal
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
    QLineEdit,
    QMessageBox,
    QVBoxLayout,
    QWidget,
)

from examvan import __main__ as main_mod
from examvan.models import Exam

APP = QApplication.instance() or QApplication([])

_QUESTIONS = [{"number": 1, "type": "single_choice", "choices": ["A", "B"]}]


# ---------------------------------------------------------------------------
# Test double
# ---------------------------------------------------------------------------


class _MonitorBackend:
    """Backend dengan jumlah monitor yang bisa dikontrol."""

    def __init__(self, monitors: int = 1, fail: bool = False) -> None:
        self.monitors = monitors
        self.fail = fail
        self.queries = 0

    def has_multiple_monitors(self) -> bool:
        self.queries += 1
        if self.fail:
            raise RuntimeError("EnumDisplayMonitors gagal")
        return self.monitors > 1

    def set_capture_protection(self, window) -> None:
        pass

    def __getattr__(self, name):
        raise AssertionError(f"backend dipanggil untuk {name!r}, tak terduga")


class _FakeConfigDialog(QWidget):
    exam_selected = pyqtSignal(object, object, object)

    def __init__(self) -> None:
        super().__init__()
        layout = QVBoxLayout(self)
        self.input_token = QLineEdit(self)
        self.input_token.setText("ABCD1234")
        layout.addWidget(self.input_token)

    def enable_connect(self) -> None:
        pass

    @property
    def validated_token(self) -> str:
        return self.input_token.text().strip().upper()


class _RecordingMessageBox(QMessageBox):
    """Mencatat urutan: pesan statik yang diminta, dan box yang dibangun."""

    trace: list = []

    @classmethod
    def _note(cls, what):
        cls.trace.append(what)

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        _RecordingMessageBox._note("box:" + self.windowTitle())

    def exec_(self):  # noqa: N802 (Qt API)
        return QMessageBox.Yes

    @classmethod
    def information(cls, parent, title, text, *args, **kwargs):
        cls._note("information:" + title)
        return QMessageBox.Ok

    @classmethod
    def warning(cls, parent, title, text, *args, **kwargs):
        cls._note("warning:" + title)
        return QMessageBox.Ok

    @classmethod
    def question(cls, parent, title, text, *args, **kwargs):
        cls._note("question:" + title)
        return QMessageBox.No


class _ApprovedWaitingDialog(QWidget):
    def __init__(self, exam=None, server_url=None, identity_data=None,
                 token=None, parent=None, **kwargs):
        super().__init__(parent)
        _RecordingMessageBox._note("waiting-dialog")

    def exec_(self):  # noqa: N802 (Qt API)
        self.hide()
        return QDialog.Accepted


class _RecordingViewer(QWidget):
    closed = pyqtSignal()
    all_done = pyqtSignal()

    def __init__(self, exam=None, server_url=None, token=None,
                 identity_data=None, kiosk_mode=False, parent=None, **kwargs):
        super().__init__(parent)
        _RecordingMessageBox._note("viewer")


class _ExistingApplication:
    def __new__(cls, *args, **kwargs):
        return APP

    setAttribute = staticmethod(QApplication.setAttribute)
    instance = staticmethod(QApplication.instance)
    processEvents = staticmethod(QApplication.processEvents)
    clipboard = staticmethod(QApplication.clipboard)
    activePopupWidget = staticmethod(QApplication.activePopupWidget)
    topLevelWidgets = staticmethod(QApplication.topLevelWidgets)
    primaryScreen = staticmethod(QApplication.primaryScreen)


def _drain(seconds: float = 2.0) -> int:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        APP.processEvents()
        time.sleep(0.002)
    return 0


def _viewer_module():
    return importlib.import_module("examvan.ui.exam_viewer")


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


class _FlowTestCase(unittest.TestCase):
    def setUp(self) -> None:
        _purge_top_levels()
        _RecordingMessageBox.trace = []
        self._made = []

    def tearDown(self) -> None:
        for w in self._made:
            try:
                w.hide()
                w.deleteLater()
            except Exception:
                pass
        _purge_top_levels()

    def _run_flow(self, security_level: str, backend) -> list:
        exam = Exam.from_json({
            "id": 11, "name": "Ujian", "status": "active",
            "security_level": security_level, "questions": _QUESTIONS,
        })
        dialog = _FakeConfigDialog()
        self._made.append(dialog)
        _RecordingMessageBox.trace = []
        QTimer.singleShot(
            0, lambda: dialog.exam_selected.emit(
                exam, "https://exam.example", {"nama": "Budi"}))

        with mock.patch.object(main_mod, "_setup_logging"), \
             mock.patch.object(main_mod, "_recover_gnome_settings"), \
             mock.patch.object(main_mod, "_recover_windows_settings"), \
             mock.patch.object(main_mod, "_sweep_stale_exam_pdfs"), \
             mock.patch.object(main_mod, "atexit"), \
             mock.patch.object(main_mod, "signal"), \
             mock.patch("sys.exit"), \
             mock.patch("sys.argv", ["examvan"]), \
             mock.patch("PyQt5.QtWidgets.QApplication",
                        _ExistingApplication), \
             mock.patch.object(QApplication, "exec_",
                               staticmethod(_drain)), \
             mock.patch("examvan.ui.styles.apply_theme"), \
             mock.patch("examvan.ui.styles.is_system_dark",
                        return_value=False), \
             mock.patch("examvan.ui.server_config.ServerConfigDialog",
                        return_value=dialog), \
             mock.patch("PyQt5.QtWidgets.QMessageBox", _RecordingMessageBox), \
             mock.patch(
                 "examvan.ui.waiting_approval.WaitingApprovalDialog",
                 _ApprovedWaitingDialog), \
             mock.patch.object(_viewer_module(), "ExamViewerWindow",
                               _RecordingViewer), \
             mock.patch("examvan.security.get_backend",
                        return_value=backend):
            main_mod.main()
        return list(_RecordingMessageBox.trace)


# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------


class MediumMultiMonitorNoticeTestCase(_FlowTestCase):
    """Peringatan medium harus diminta sebelum persetujuan dikuras."""

    def test_notice_is_requested_before_the_approval_dialog_is_built(self):
        trace = self._run_flow("medium", _MonitorBackend(monitors=2))

        self.assertIn(
            "information:Monitor Ganda Terdeteksi", trace,
            "peringatan multi-monitor medium tidak pernah diminta sama "
            "sekali — kelas yang paling banyak dipakai tidak punya "
            "peringatan apa pun",
        )
        self.assertLess(
            trace.index("information:Monitor Ganda Terdeteksi"),
            trace.index("waiting-dialog"),
            "peringatan baru muncul SETELAH persetujuan pengawas "
            "dikonsumsi — siswa menunda pengawas tanpa tahu masalahnya",
        )

    def test_the_exam_still_starts_after_the_notice(self):
        # Medium = warn-and-continue. Peringatan tidak boleh memblokir alur.
        trace = self._run_flow("medium", _MonitorBackend(monitors=2))

        self.assertIn("viewer", trace,
                      "medium + dua monitor tidak boleh berhenti sebelum "
                      "jendela ujian dibuat — medium bersifat warn-and-continue")
        self.assertNotIn(
            "question:Konfirmasi Pengumpulan", trace,
            "gate multi-monitor medium berubah menjadi dialog yang "
            "menunggu jawaban, bukan informasi yang sekali baca",
        )

    def test_no_notice_on_a_single_monitor(self):
        trace = self._run_flow("medium", _MonitorBackend(monitors=1))

        self.assertEqual(
            [t for t in trace if t.startswith("information:")], [],
            "peringatan multi-monitor muncul di mesin yang cuma punya satu "
            "layar",
        )
        self.assertIn("viewer", trace)

    def test_a_failing_detector_fails_open_and_the_exam_starts(self):
        # Tidak diketahui ≠ lebih dari satu layar. Menahan medium karena
        # detektor error akanesus semua PC yang Smbus-nya bermasalah.
        trace = self._run_flow("medium", _MonitorBackend(fail=True))

        self.assertIn("viewer", trace,
                      "detektor multi-monitor yang exception menahan ujian "
                      "medium — fail-OPEN, bukan fail-closed")
        self.assertEqual(
            [t for t in trace if t.startswith("warning:")], [],
            "kegagalan detektor tidak boleh dipasang jadi peringatan "
            "yang menakut-nakuti siswa tanpa bukti",
        )


class MediumBranchLevelTestCase(unittest.TestCase):
    """Branch medium ditentukan konstanta tier, bukan literal string."""

    def _exam(self, raw_level: str) -> Exam:
        return Exam.from_json({
            "id": 1, "name": "Ujian", "status": "active",
            "security_level": raw_level, "questions": _QUESTIONS,
        })

    def test_the_server_word_medium_takes_the_notice_branch(self):
        self.assertTrue(
            main_mod._exam_needs_monitor_notice(self._exam("medium")),
            "ujian dengan security_level 'medium' (kata yang benar-benar "
            "dipakai server) tidak masuk branch peringatan",
        )

    def test_the_server_word_high_never_takes_the_notice_branch(self):
        # "high" adalah strict. Kalau ia ikut cabang medium, strict akan
        # dapat peringatan non-blocking — bukan penolakan yang memang
        # Become rancangannya. `Exam.level` sudah memetakannya ke "strict".
        exam = self._exam("high")
        self.assertEqual(exam.level, "strict")
        self.assertFalse(
            main_mod._exam_needs_monitor_notice(exam),
            "level server 'high' ikut masuk branch medium — dua tier "
            "dengan perilaku berbeda jadi satu cabang",
        )

    def test_an_unreadable_level_warns_instead_of_skipping_silently(self):
        # `normalize_level` memetakan apa pun yang tidak dikenal ke
        # DEFAULT_LEVEL (= medium) dengan sengaja: kebijakan yang tak
        # dikenal tidak boleh diam-diam berarti "tanpa peringatan".
        # Catatan: versi pertama test ini mengharapkan "False"
        # untuk level yang tak terbaca — itu salah; `normalize_level` memang
        # mengembalikan DEFAULT_LEVEL-nya, dan itu arah fail-secure yang benar.
        class _NoLevel:
            pass

        self.assertTrue(
            main_mod._exam_needs_monitor_notice(_NoLevel()),
            "objek ujian tanpa level dilewati tanpa peringatan — "
            "kebijakan yang tidak dikenal tidak boleh berarti 'tanpa "
            "peringatan'",
        )
        self.assertTrue(
            main_mod._exam_needs_monitor_notice(None),
            "level None harus diperlakukan sebagai DEFAULT_LEVEL "
            "(medium), bukan dilewati diam-diam",
        )


class StrictStillGatesTestCase(_FlowTestCase):

    def test_strict_with_two_monitors_still_blocks_and_never_opens_the_exam(self):
        trace = self._run_flow("high", _MonitorBackend(monitors=2))

        self.assertIn(
            "warning:Monitor Ganda Terdeteksi", trace,
            "gate multi-monitor strict hilang saat cabang medium dipindah",
        )
        self.assertNotIn(
            "information:Monitor Ganda Terdeteksi", trace,
            "strict memakai peringatan NON-BLOCKING yang sama dengan medium "
            "— strict memang harus menolak mulai, bukan warn-and-continue",
        )
        self.assertNotIn(
            "viewer", trace,
            "ujian strict dengan dua monitor tetap berjalan — gate "
            "multi-monitor strict harus menolak mulai",
        )

    def test_strict_with_a_broken_detector_is_refused_not_allowed(self):
        # Kebalikan dari medium: strict fail-CLOSED. Detektor yang tidak bisa
        # menjawab "berapa layar" berarti kita TIDAK TAHU pengawas sedang
        # menutupi layar kedua — releasing strict dalam keadaan itu lebih
        # buruk daripada menolak mulai.
        trace = self._run_flow("high", _MonitorBackend(fail=True))

        self.assertIn(
            "warning:Tidak Dapat Memeriksa Layar", trace,
            "strict dengan detektor rusak dianggap aman — itu fail-OPEN di "
            "tier yang paling ketatnya",
        )
        self.assertNotIn("viewer", trace)


if __name__ == "__main__":
    unittest.main()