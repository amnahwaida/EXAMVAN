"""Main exam viewer window — PDF + answer sheet + timer + submit."""

from __future__ import annotations

import logging
import os
import tempfile
import threading
from typing import Any, Dict, List, Optional

# Admin exit password — REQUIRED. Set env EXAMVAN_ADMIN_PASSWORD before launch.
# Without this, admin exit is DISABLED (no any-password fallback).
_ADMIN_PASSWORD = os.environ.get("EXAMVAN_ADMIN_PASSWORD")

from PyQt5.QtCore import Qt, QTimer, pyqtSignal, pyqtSlot
from PyQt5.QtGui import QCloseEvent, QKeyEvent
from PyQt5.QtWidgets import (
    QApplication,
    QHBoxLayout,
    QLabel,
    QMainWindow,
    QMessageBox,
    QPushButton,
    QSplitter,
    QVBoxLayout,
    QWidget,
)

from .. import api, config
from ..models import Exam
from ..security.enforcer import SecurityEnforcer
from ..utils import clear_clipboard, get_device_label, map_identity_to_standard
from .answer_sheet import AnswerSheetWidget
from .pdf_viewer import PdfWidget
from .timer import ElapsedTimerWidget
from .styles import SECURITY_COLORS

log = logging.getLogger(__name__)


class ExamViewerWindow(QMainWindow):
    """Main exam window."""

    closed = pyqtSignal()

    # Thread-safe signals for background -> UI updates
    _sig_status = pyqtSignal(str)
    _sig_pdf_ready = pyqtSignal()
    _sig_pdf_error = pyqtSignal(str)
    _sig_submit_result = pyqtSignal(bool, str)

    def __init__(
        self,
        exam: Exam,
        server_url: str,
        token: str,
        identity_data: Dict[str, str],
        kiosk_mode: bool = False,
        parent=None,
    ):
        super().__init__(parent)
        self._exam = exam
        self._server_url = server_url
        self._token = token
        self._identity_data = identity_data
        self._kiosk_mode = kiosk_mode

        # Thread-safe submission guards
        self._submit_lock = threading.Lock()
        self._submitted = False
        self._submitting = False
        self._close_in_progress = False  # prevent spam close events

        self._security: Optional[SecurityEnforcer] = None
        self._pdf_path: Optional[str] = None
        self._admin_exit_count = 0
        self._admin_exit_timer = QTimer(self)
        self._admin_exit_timer.setSingleShot(True)
        self._admin_exit_timer.setInterval(2000)
        self._admin_exit_timer.timeout.connect(lambda: setattr(self, '_admin_exit_count', 0))

        # Connect signals to slots
        self._sig_status.connect(self._on_status)
        self._sig_pdf_ready.connect(self._on_pdf_ready)
        self._sig_pdf_error.connect(self._on_pdf_error)
        self._sig_submit_result.connect(self._on_submit_result)

        self._setup_ui()
        self._init_security()  # Activate security BEFORE PDF loads
        self._load_pdf_async()

        # Restore saved answers
        saved = config.load_answers(exam.id)
        if saved:
            QTimer.singleShot(500, lambda: self._answer_sheet.restore_answers(saved))

    def _setup_ui(self) -> None:
        # Window title: generic, no exam name leakage
        self.setWindowTitle("EXAMVAN")
        self.setMinimumSize(1024, 700)

        central = QWidget()
        self.setCentralWidget(central)
        main_layout = QVBoxLayout(central)
        main_layout.setContentsMargins(0, 0, 0, 0)
        main_layout.setSpacing(0)

        # Top bar
        top_bar = QWidget()
        top_bar.setObjectName("examTopBar")
        top_bar.setStyleSheet("#examTopBar { padding: 8px 16px; }")
        top_layout = QHBoxLayout(top_bar)
        top_layout.setContentsMargins(8, 4, 8, 4)

        # Exam title
        self._lbl_title = QLabel(self._exam.name)
        self._lbl_title.setStyleSheet("font-size: 16px; font-weight: bold;")
        top_layout.addWidget(self._lbl_title, 1)

        # Security banner
        mode = "strict" if self._exam.is_strict else self._exam.security_level
        bg, fg = SECURITY_COLORS.get(mode, SECURITY_COLORS["low"])
        self._lbl_security = QLabel(f"\U0001f512 {mode.upper()}")
        self._lbl_security.setStyleSheet(
            f"background-color: {bg}; color: {fg}; padding: 4px 12px;"
            f"border-radius: 4px; font-weight: bold; font-size: 12px;"
        )
        top_layout.addWidget(self._lbl_security)

        # Timer (countdown if end_time, else elapsed)
        self._timer_widget = ElapsedTimerWidget(end_time=self._exam.end_time)
        self._timer_widget.time_up.connect(self._auto_submit)
        top_layout.addWidget(self._timer_widget)

        main_layout.addWidget(top_bar)

        # Content: PDF + Answer sheet
        splitter = QSplitter(Qt.Horizontal)
        splitter.setHandleWidth(3)

        # PDF viewer
        self._pdf_viewer = PdfWidget(panel_color=self._exam.panel_color)
        splitter.addWidget(self._pdf_viewer)

        # Answer sheet panel
        self._answer_sheet = AnswerSheetWidget(
            panel_color=self._exam.panel_color
        )
        self._answer_sheet.setMinimumWidth(280)
        self._answer_sheet.setMaximumWidth(420)
        splitter.addWidget(self._answer_sheet)

        splitter.setSizes([700, 300])

        main_layout.addWidget(splitter, 1)

        # Bottom bar
        bottom_bar = QWidget()
        bottom_bar.setObjectName("examBottomBar")
        bottom_bar.setStyleSheet("#examBottomBar { padding: 8px 16px; }")
        bottom_layout = QHBoxLayout(bottom_bar)
        bottom_layout.setContentsMargins(8, 4, 8, 4)

        # Status
        self._lbl_status = QLabel("Mengunduh PDF...")
        self._lbl_status.setStyleSheet("font-size: 12px;")
        bottom_layout.addWidget(self._lbl_status, 1)

        # Toggle answer sheet
        pc = self._exam.panel_color
        self._btn_toggle = QPushButton(" Lembar Jawaban")
        self._btn_toggle.setCheckable(True)
        self._btn_toggle.setChecked(True)
        self._btn_toggle.setStyleSheet(
            f"QPushButton {{ background-color: {pc}; color: #ffffff; font-weight: bold;"
            f"padding: 8px 18px; border-radius: 6px; }}"
            f"QPushButton:checked {{ background-color: {pc}; }}"
        )
        self._btn_toggle.clicked.connect(
            lambda checked: self._answer_sheet.setVisible(checked)
        )
        bottom_layout.addWidget(self._btn_toggle)

        # Submit button
        self._btn_submit = QPushButton(" Kumpulkan Jawaban")
        self._btn_submit.setStyleSheet(
            f"QPushButton {{ background-color: {pc}; color: #ffffff; font-weight: bold;"
            f"padding: 10px 24px; border-radius: 6px; }}"
        )
        self._btn_submit.clicked.connect(self._on_submit)
        bottom_layout.addWidget(self._btn_submit)

        main_layout.addWidget(bottom_bar)

        # Auto-save timer (every 500ms debounce)
        self._save_timer = QTimer(self)
        self._save_timer.setSingleShot(True)
        self._save_timer.setInterval(500)
        self._save_timer.timeout.connect(self._save_answers)
        self._answer_sheet.answer_changed.connect(lambda: self._save_timer.start())

    def _init_security(self) -> None:
        """Initialize security enforcer BEFORE exam starts.

        Activating here covers the PDF download period so there's
        no unprotected gap between dialog and viewer.
        """
        self._security = SecurityEnforcer(
            security_level=self._exam.security_level,
            strict_mode=self._exam.is_strict,
            window=self,
            kiosk_mode=self._kiosk_mode,
        )
        self._security.auto_submit.connect(self._auto_submit)
        self._security.activate()
        log.info("Security activated before PDF load")

    # -------------------------------------------------------------------
    # PDF download (background thread)
    # -------------------------------------------------------------------

    def _load_pdf_async(self) -> None:
        threading.Thread(target=self._load_pdf_thread, daemon=True).start()

    def _load_pdf_thread(self) -> None:
        try:
            dest = os.path.join(
                tempfile.gettempdir(), f"examvan_exam_{self._exam.id}.pdf"
            )
            self._pdf_path = api.download_pdf(
                self._server_url,
                self._exam.id,
                self._token,
                dest,
                progress_cb=self._on_download_progress,
            )
            self._sig_pdf_ready.emit()
        except Exception as e:
            self._sig_pdf_error.emit(str(e))

    def _on_download_progress(self, read_bytes: int, total: int) -> None:
        if total > 0:
            pct = int(read_bytes * 100 / total)
            self._sig_status.emit(f"Mengunduh PDF... {pct}%")

    # -------------------------------------------------------------------
    # Slots (run on UI thread)
    # -------------------------------------------------------------------

    @pyqtSlot(str)
    def _on_status(self, text: str) -> None:
        self._lbl_status.setText(text)

    @pyqtSlot()
    def _on_pdf_ready(self) -> None:
        if self._pdf_path and self._pdf_viewer.load_pdf(self._pdf_path):
            self._lbl_status.setText("PDF siap")
            self._answer_sheet.build_from_questions(self._exam.questions)
        else:
            self._lbl_status.setText("Gagal memuat PDF")

    @pyqtSlot(str)
    def _on_pdf_error(self, error: str) -> None:
        self._lbl_status.setText(f"Error: {error}")

    @pyqtSlot(bool, str)
    def _on_submit_result(self, success: bool, message: str) -> None:
        with self._submit_lock:
            self._submitting = False
            if success:
                self._submitted = True

        if success:
            self._cleanup_after_submit(message)
        else:
            self._btn_submit.setEnabled(True)
            QMessageBox.warning(
                self,
                "Gagal",
                f"Gagal mengumpulkan jawaban:\n{message}\n\nSilakan coba lagi.",
            )

    def _cleanup_after_submit(self, message: str) -> None:
        """Clean up after successful submit."""
        config.clear_answers(self._exam.id)
        if self._security:
            self._security.deactivate()

        self._timer_widget.stop()
        self._btn_submit.setEnabled(False)
        self._btn_submit.setText("✅ Terkumpul")

        # Wipe PDF temp file
        self._pdf_viewer.cleanup()
        if self._pdf_path:
            try:
                if os.path.exists(self._pdf_path):
                    os.remove(self._pdf_path)
                    log.info("PDF temp file removed: %s", self._pdf_path)
            except OSError:
                pass

        QMessageBox.information(
            self,
            "Berhasil",
            f"Jawaban berhasil dikumpulkan!\n\n{message}",
        )
        self.closed.emit()
        self.close()

    # -------------------------------------------------------------------
    # Answer persistence
    # -------------------------------------------------------------------

    def _save_answers(self) -> None:
        if self._submitted:
            return
        answers = self._answer_sheet.get_answers()
        config.save_answers(self._exam.id, answers)

    # -------------------------------------------------------------------
    # Submit
    # -------------------------------------------------------------------

    def _on_submit(self) -> None:
        # Quick pre-check (volatile read — race window handled by _do_submit lock)
        if self._submitted or self._submitting:
            return

        answered, total = self._answer_sheet.get_answered_count()
        reply = QMessageBox.question(
            self,
            "Konfirmasi Pengumpulan",
            f"Anda telah menjawab {answered} dari {total} soal.\n\n"
            f"Apakah yakin ingin mengumpulkan jawaban?",
            QMessageBox.Yes | QMessageBox.No,
            QMessageBox.No,
        )
        if reply != QMessageBox.Yes:
            return

        self._do_submit()

    def _auto_submit(self) -> None:
        self._on_status("Auto-submit: jawaban dikumpulkan otomatis...")
        self._do_submit()

    def _do_submit(self) -> None:
        """Thread-safe submit gate. Only one submit runs at a time."""
        with self._submit_lock:
            if self._submitted or self._submitting:
                return
            self._submitting = True

        self._btn_submit.setEnabled(False)

        answers = self._answer_sheet.get_answers()

        # Map dynamic identity keys to standard keys expected by Go backend
        std = map_identity_to_standard(self._identity_data)

        threading.Thread(
            target=self._submit_thread,
            args=(
                self._server_url,
                self._exam.id,
                std.get("student_name", ""),
                std.get("exam_number", ""),
                std.get("student_class", ""),
                answers,
                self._timer_widget.get_start_time_iso(),
                get_device_label(),
                self._identity_data,
            ),
            daemon=True,
        ).start()

    def _submit_thread(
        self, base_url, exam_id, name, number, sclass, answers, start_time, mac, identity
    ) -> None:
        resp = api.submit_with_retry(
            base_url, exam_id, name, number, sclass, answers, start_time, mac, identity,
            on_retry=lambda attempt, total: self._sig_status.emit(
                f"Submit gagal, percobaan {attempt}/{total}..."
            ),
        )
        self._sig_submit_result.emit(resp.success, resp.message)

    # -------------------------------------------------------------------
    # Window events
    # -------------------------------------------------------------------

    def closeEvent(self, event: QCloseEvent) -> None:
        # Guard against repeated closeEvent spam
        if self._close_in_progress:
            event.ignore()
            return
        self._close_in_progress = True

        with self._submit_lock:
            already_done = self._submitted

        if already_done:
            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            self.closed.emit()
            event.accept()
            self._close_in_progress = False
            return

        mode = self._exam.security_level
        if mode in ("medium",) or self._exam.is_strict:
            # Auto-submit on close attempt (once)
            with self._submit_lock:
                if self._submitted or self._submitting:
                    self._close_in_progress = False
                    event.ignore()
                    return
                self._submitting = True
            self._close_in_progress = False
            event.ignore()
            self._auto_submit()
            return

        # Low mode: confirm close
        reply = QMessageBox.question(
            self,
            "Keluar Ujian",
            "Apakah yakin ingin keluar? Jawaban belum dikumpulkan.",
            QMessageBox.Yes | QMessageBox.No,
            QMessageBox.No,
        )
        if reply == QMessageBox.Yes:
            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            self.closed.emit()
            with self._submit_lock:
                self._submitted = True
            event.accept()
        else:
            event.ignore()
        self._close_in_progress = False

    def keyPressEvent(self, event: QKeyEvent) -> None:
        if self._exam.is_strict and self._security:
            # Admin exit backdoor: Ctrl+Shift+Alt+Q
            if (
                event.modifiers() == (Qt.ControlModifier | Qt.ShiftModifier | Qt.AltModifier)
                and event.key() == Qt.Key_Q
            ):
                self._admin_exit_count += 1
                self._admin_exit_timer.start()
                if self._admin_exit_count >= 3:
                    self._admin_exit_count = 0
                    self._admin_exit_prompt()
                    return

            # Block dangerous keys — keyboard hook handles most but
            # Qt-level block as defense-in-depth (works even if hook fails)
            blocked = {
                Qt.Key_Tab: bool(event.modifiers() & Qt.AltModifier),
                Qt.Key_F4: bool(event.modifiers() & Qt.AltModifier),
                Qt.Key_Escape: True,
                Qt.Key_Super_L: True,
                Qt.Key_Super_R: True,
                Qt.Key_Menu: True,
            }
            if blocked.get(event.key(), False):
                event.ignore()
                return

            if event.key() == Qt.Key_Print:
                clear_clipboard()
                event.ignore()
                return

        super().keyPressEvent(event)

    def _admin_exit_prompt(self) -> None:
        from PyQt5.QtWidgets import QInputDialog, QLineEdit

        password, ok = QInputDialog.getText(
            self,
            "Admin Exit",
            "Masukkan password supervisor:",
            QLineEdit.Password,
        )
        if ok and password:
            # Validate against env var — fail-closed
            if _ADMIN_PASSWORD is None:
                QMessageBox.warning(
                    self, "Tidak Diizinkan",
                    "Admin exit tidak dikonfigurasi.\n"
                    "Set environment EXAMVAN_ADMIN_PASSWORD.",
                )
                return
            if password != _ADMIN_PASSWORD:
                QMessageBox.warning(self, "Akses Ditolak", "Password salah.")
                return

            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            with self._submit_lock:
                self._submitted = True
            self.closed.emit()
            self.close()
