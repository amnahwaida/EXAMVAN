"""Main exam viewer window — PDF + answer sheet + timer + submit."""

from __future__ import annotations

import os
import tempfile
import threading
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional

from PyQt5.QtCore import Qt, QTimer, pyqtSignal, pyqtSlot
from PyQt5.QtGui import QCloseEvent, QKeyEvent
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
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
from ..utils import get_device_label, map_identity_to_standard
from .answer_sheet import AnswerSheetWidget
from .pdf_viewer import PdfWidget
from .timer import ElapsedTimerWidget
from .styles import SECURITY_COLORS


class ExamViewerWindow(QMainWindow):
    """Main exam window."""

    closed = pyqtSignal()

    # Thread-safe signals for background → UI updates
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
        self._submitted = False
        self._submitting = False
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
        self._load_pdf_async()

        # Restore saved answers
        saved = config.load_answers(exam.id)
        if saved:
            QTimer.singleShot(500, lambda: self._answer_sheet.restore_answers(saved))

    def _setup_ui(self) -> None:
        self.setWindowTitle(f"EXAMVAN — {self._exam.name}")
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
        self._lbl_security = QLabel(f"🔒 {mode.upper()}")
        self._lbl_security.setStyleSheet(
            f"background-color: {bg}; color: {fg}; padding: 4px 12px;"
            f"border-radius: 4px; font-weight: bold; font-size: 12px;"
        )
        top_layout.addWidget(self._lbl_security)

        # Timer
        self._timer_widget = ElapsedTimerWidget()
        top_layout.addWidget(self._timer_widget)

        main_layout.addWidget(top_bar)

        # Content: PDF + Answer sheet
        splitter = QSplitter(Qt.Horizontal)
        splitter.setHandleWidth(3)

        # PDF viewer
        self._pdf_viewer = PdfWidget()
        splitter.addWidget(self._pdf_viewer)

        # Answer sheet panel
        self._answer_sheet = AnswerSheetWidget(
            panel_color=self._exam.panel_color
        )
        self._answer_sheet.setMinimumWidth(280)
        self._answer_sheet.setMaximumWidth(420)
        splitter.addWidget(self._answer_sheet)

        # Default split: 70% PDF, 30% answer sheet
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
        self._btn_toggle = QPushButton("📋 Lembar Jawaban")
        self._btn_toggle.setCheckable(True)
        self._btn_toggle.setChecked(True)
        self._btn_toggle.clicked.connect(
            lambda checked: self._answer_sheet.setVisible(checked)
        )
        bottom_layout.addWidget(self._btn_toggle)

        # Submit button — use exam panel color
        pc = self._exam.panel_color
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

        # Connect answer change to auto-save trigger
        self._answer_sheet.answer_changed.connect(lambda: self._save_timer.start())

        # Initialize security enforcer
        self._security = SecurityEnforcer(
            security_level=self._exam.security_level,
            strict_mode=self._exam.is_strict,
            window=self,
            kiosk_mode=self._kiosk_mode,
        )
        self._security.auto_submit.connect(self._auto_submit)

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
            if self._security:
                self._security.activate()
        else:
            self._lbl_status.setText("Gagal memuat PDF")

    @pyqtSlot(str)
    def _on_pdf_error(self, error: str) -> None:
        self._lbl_status.setText(f"Error: {error}")

    @pyqtSlot(bool, str)
    def _on_submit_result(self, success: bool, message: str) -> None:
        self._submitting = False
        self._submitted = success

        if success:
            config.clear_answers(self._exam.id)
            if self._security:
                self._security.deactivate()
            self._timer_widget.stop()
            self._btn_submit.setEnabled(False)
            self._btn_submit.setText("✅ Terkumpul")

            QMessageBox.information(
                self,
                "Berhasil",
                f"Jawaban berhasil dikumpulkan!\n\n{message}",
            )
            self._submitted = True
            self.closed.emit()
            self.close()
        else:
            self._btn_submit.setEnabled(True)
            QMessageBox.warning(
                self,
                "Gagal",
                f"Gagal mengumpulkan jawaban:\n{message}\n\nSilakan coba lagi.",
            )

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
        if self._submitted or self._submitting:
            return
        self._on_status("Auto-submit: jawaban dikumpulkan otomatis...")
        self._do_submit()

    def _do_submit(self) -> None:
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
        if self._submitted:
            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            self.closed.emit()
            event.accept()
            return

        mode = self._exam.security_level
        if mode in ("medium",) or self._exam.is_strict:
            self._auto_submit()
            event.ignore()
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
            event.accept()
        else:
            event.ignore()

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

            # Block dangerous keys
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
                from ..utils import clear_clipboard
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
            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            self._submitted = True
            self.closed.emit()
            self.close()
