"""Token yang dipakai di seluruh alur harus PERSIS yang tervalidasi.

Bug — `__main__` membaca ulang QLineEdit, bukan `validated_token`
---------------------------------------------------------------
`ServerConfigDialog.validated_token` didokumentasikan sebagai kontrak
(`server_config.py:66-70`):

    "__main__ memakai validated_token (bukan membaca ulang QLineEdit saat
     dialog persetujuan/viewer dibuat) supaya token yang dipakai di seluruh
     alur PERSIS yang tervalidasi — bukan apa pun yang kebetulan ada di
     kotak input detik itu."

Dua call site melanggarnya:

    __main__.py:282  WaitingApprovalDialog(token=dialog.input_token.text()…)
    __main__.py:357  ExamViewerWindow(token=dialog.input_token.text()…)

Yang membuatnya benar-benar bisa berubah: `_enable_connect_ui()` berjalan
SEBELUM `exam_selected.emit()`, jadi tombol dan kedua input sudah hidup lagi
pada saat widget dibaca. `_validated_token` masih ada, tapi tidak dipakai —
dan gate PDF, submit, serta presence semuanya memakai nilai yang dibaca ulang
dari widget, bukan nilai yang server benar-benar akui.

Test di sini menjalankan `main_mod.main()` sungguhan dengan
`ServerConfigDialog` palsu yang punya `validated_token`, lalu MENGUBAH isi
kotak input sesudah validasi — persis celah yang dipakai bug ini.
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


class _FakeBackend:
    """Backend security: satu monitor, tidak ada yang perlu dilindungi."""

    def has_multiple_monitors(self) -> bool:
        return False

    def set_capture_protection(self, window) -> None:
        pass

    def __getattr__(self, name):
        raise AssertionError(f"backend dipanggil untuk {name!r}, tak terduga")


class _FakeConfigDialog(QWidget):
    """ServerConfigDialog secukupnya: input token + tombol hubungkan."""

    exam_selected = pyqtSignal(object, object, object)

    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("EXAMVAN — Konfigurasi")
        layout = QVBoxLayout(self)
        self.input_token = QLineEdit(self)
        layout.addWidget(self.input_token)
        self.enable_connect_calls = 0

    def enable_connect(self) -> None:
        self.enable_connect_calls += 1


class _ValidatedConfigDialog(_FakeConfigDialog):
    """Dialog yang sudah lewat `_on_connect` (sama seperti produksi).

    `_validated_token` diisi waktu validasi; sesudahnya `_enable_connect_ui()`
    menghidupkan lagi input, jadi isi kotak TIDAK lagi sama dengan yang
    tervalidasi — persis keadaan di titik `exam_selected.emit()`.
    """

    def __init__(self, validated: str = "", widget_text: str = "") -> None:
        super().__init__()
        self._validated_token = validated
        self.input_token.setText(widget_text or validated)

    @property
    def validated_token(self) -> str:
        if self._validated_token:
            return self._validated_token
        return self.input_token.text().strip().upper()


class _LegacyConfigDialog(_FakeConfigDialog):
    """Dialog versi lama: tidak punya properti `validated_token` sama sekali."""

    @property
    def validated_token(self):  # sengaja dihapus lewat patch di test
        raise AttributeError("validated_token")


class _ApprovedWaitingDialog(QWidget):
    """WaitingApprovalDialog yang mencatat token lalu langsung disetujui."""

    log: list = []

    def __init__(self, exam=None, server_url=None, identity_data=None,
                 token=None, parent=None, **kwargs):
        super().__init__(parent)
        self.token = token
        type(self).log.append(("waiting", token))

    def exec_(self):  # noqa: N802 (Qt API)
        self.hide()
        return QDialog.Accepted


class _RecordingViewer(QWidget):
    """ExamViewerWindow palsu yang mencatat token yang diterimanya."""

    log: list = []

    closed = pyqtSignal()
    all_done = pyqtSignal()

    def __init__(self, exam=None, server_url=None, token=None,
                 identity_data=None, kiosk_mode=False, parent=None, **kwargs):
        super().__init__(parent)
        self._token = token
        type(self).log.append(("viewer", token))


def _drain(seconds: float = 2.0) -> int:
    """Pemompa event berbatas waktu — pengganti `app.exec_()`.

    Event loop sungguhan tidak bisa dipakai: test tidak punya cara memberhentikan
    `exec_()` selain `QApplication.quit()` dari timer lain, dan kegagalan
    schedulernya menggantung seluruh test suite. Yang dipalsukan hanya event
    loop-nya — `main()`, `on_exam_selected`, dan seluruh widget tetap
    sungguhan.
    """
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        APP.processEvents()
        time.sleep(0.002)
    return 0


class _ExistingApplication:
    """`QApplication` selama `main()` — mengembalikan instance yang sudah ada.

    PyQt5 hanya mengizinkan SATU instance per proses, jadi `main()` harus
    memakai QApplication milik test — bukan Mock (Mock membuat
    `_maximize_window` gagal: `primaryScreen()` jadi MagicMock).
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


def _viewer_module():
    """Modul viewer yang SEDANG hidup di `sys.modules`.

    `tests/test_admin_password.py` me-reload `examvan.ui.exam_viewer`, dan
    reload membuat objek kelas BARU: nama yang diimpor di atas jadi kelas
    lama, dan patching nama itu tidak menyentuh yang diimpor `__main__`
    (impor occur saat `main()` dipanggil). Produksi selalu resolving lewat
    `sys.modules` — test harus sama.
    """
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


# ---------------------------------------------------------------------------
# Harness
# ---------------------------------------------------------------------------


class _FlowTestCase(unittest.TestCase):
    def setUp(self) -> None:
        _purge_top_levels()
        _ApprovedWaitingDialog.log = []
        _RecordingViewer.log = []
        self._made = []

    def tearDown(self) -> None:
        for w in self._made:
            try:
                w.hide()
                w.deleteLater()
            except Exception:
                pass
        _purge_top_levels()

    def _make_dialog(self, cls, *args, **kwargs) -> QWidget:
        dlg = cls(*args, **kwargs)
        self._made.append(dlg)
        return dlg

    def _run_flow(self, dialog, security_level: str = "medium") -> None:
        """Jalankan `main()` penuh dengan dialog konfigurasi yang diberikan."""
        exam = Exam.from_json({
            "id": 11, "name": "Ujian", "status": "active",
            "security_level": security_level,
            "questions": _QUESTIONS,
        })
        _ApprovedWaitingDialog.log = []
        _RecordingViewer.log = []

        def _emit():
            dialog.exam_selected.emit(
                exam, "https://exam.example", {"nama": "Budi"})

        QTimer.singleShot(0, _emit)

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
             mock.patch(
                 "examvan.ui.waiting_approval.WaitingApprovalDialog",
                 _ApprovedWaitingDialog), \
             mock.patch.object(_viewer_module(), "ExamViewerWindow",
                               _RecordingViewer), \
             mock.patch("examvan.security.get_backend",
                        return_value=_FakeBackend()):
            main_mod.main()


# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------


class ValidatedTokenFlowTestCase(_FlowTestCase):
    """`__main__` harus memakai `validated_token`, bukan isi QLineEdit."""

    def test_approval_dialog_receives_the_validated_token(self):
        # Validasi "ABCD1234", lalu siswa (atau apa pun) mengubah kotak input
        # sebelum approval dibuka. `_enable_connect_ui()` sudah menghidupkan
        # input lagi pada titik ini.
        dlg = self._make_dialog(_ValidatedConfigDialog,
                                validated="ABCD1234")
        dlg.input_token.setText("BAWAH12")

        self._run_flow(dlg)

        self.assertEqual(
            _ApprovedWaitingDialog.log, [("waiting", "ABCD1234")],
            "dialog persetujuan dibangun dari isi QLineEdit yang sudah berubah, "
            "bukan token yang tervalidasi — gate PDF/approval memakai token "
            "yang tidak pernah diack server",
        )

    def test_viewer_receives_the_validated_token(self):
        dlg = self._make_dialog(_ValidatedConfigDialog,
                                validated="ABCD1234")
        dlg.input_token.setText("BAWAH12")

        self._run_flow(dlg)

        self.assertEqual(
            _RecordingViewer.log, [("viewer", "ABCD1234")],
            "jendela ujian memakai token dari QLineEdit — submit dan presence "
            "mengirim kredensial yang berbeda dari yang dipakai approval",
        )

    def test_both_sites_get_the_same_token_even_if_the_widget_changes(self):
        # Widget hidup kembali saat dialog persetujuan dibangun: kalau
        # `__main__` membaca ulang di dua tempat, keduanya bisa berbeda.
        # Fix membaca SATU kali di awal, jadi keduanya identik.
        class _MutatingWaitingDialog(_ApprovedWaitingDialog):
            def __init__(self, *args, **kwargs):
                super().__init__(*args, **kwargs)
                dlg.input_token.setText("GANTI999")

        _ApprovedWaitingDialog.log = []
        _RecordingViewer.log = []
        dlg = self._make_dialog(_ValidatedConfigDialog,
                                validated="ABCD1234")

        exam = Exam.from_json({
            "id": 11, "name": "Ujian", "status": "active",
            "security_level": "medium", "questions": _QUESTIONS,
        })
        QTimer.singleShot(
            0, lambda: dlg.exam_selected.emit(
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
                        return_value=dlg), \
             mock.patch("examvan.ui.waiting_approval.WaitingApprovalDialog",
                        _MutatingWaitingDialog), \
             mock.patch.object(_viewer_module(), "ExamViewerWindow",
                               _RecordingViewer), \
             mock.patch("examvan.security.get_backend",
                        return_value=_FakeBackend()):
            main_mod.main()

        self.assertEqual(_ApprovedWaitingDialog.log,
                         [("waiting", "ABCD1234")])
        self.assertEqual(
            _RecordingViewer.log, [("viewer", "ABCD1234")],
            "jendela ujian membaca ulang QLineEdit yang SUKAR sudah diubah "
            "dialog persetujuan → approval dan submit memakai dua token berbeda",
        )

    def test_falls_back_to_the_widget_and_logs_when_the_property_is_missing(self):
        # Dialog versi lama / test double tidak punya `validated_token`.
        # Alur harus tetap jalan (fail-open padaATRIBUT, bukan pada token),
        # dan kejujuran soal fallback harus terlihat di log.
        dlg = self._make_dialog(_FakeConfigDialog)
        dlg.input_token.setText("abcd1234")
        with mock.patch.object(
            type(dlg), "validated_token", mock.PropertyMock(
                side_effect=AttributeError("tidak ada properti ini")),
            create=True,
        ):
            with self.assertLogs("examvan.__main__", level="WARNING"):
                self._run_flow(dlg)

        self.assertEqual(
            _ApprovedWaitingDialog.log, [("waiting", "ABCD1234")],
            "fallback ke QLineEdit tidak boleh mengubah format token "
            "(strip + upper) — token lowercase tidak akan cocok di server",
        )


if __name__ == "__main__":
    unittest.main()