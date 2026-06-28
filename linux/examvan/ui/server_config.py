"""Server configuration dialog — URL + token input, health check."""

from __future__ import annotations

import threading
from typing import Optional

from PyQt5.QtCore import Qt, QTimer, pyqtSignal, pyqtSlot
from PyQt5.QtWidgets import (
    QApplication,
    QCheckBox,
    QDialog,
    QLabel,
    QLineEdit,
    QMessageBox,
    QPushButton,
    QVBoxLayout,
    QWidget,
)

from .. import APP_VERSION, __version__
from .. import api, config
from ..models import Exam


class ServerConfigDialog(QDialog):
    """Initial dialog: enter server URL and exam token."""

    exam_selected = pyqtSignal(object, str, object)  # (Exam, server_url, identity_data)

    # Signals for thread-safe UI updates
    _sig_status = pyqtSignal(str, bool)      # (message, is_error)
    _sig_enable_btn = pyqtSignal()
    _sig_show_identity = pyqtSignal()

    def __init__(self, kiosk_mode: bool = False, parent=None):
        super().__init__(parent)
        self.kiosk_mode = kiosk_mode
        self._exam: Optional[Exam] = None
        self._server_url = ""

        # Connect signals to slots
        self._sig_status.connect(self._set_status_slot)
        self._sig_enable_btn.connect(self._enable_btn_slot)
        self._sig_show_identity.connect(self._show_identity_dialog)

        self._setup_ui()
        self._load_saved()

    def _setup_ui(self) -> None:
        self.setWindowTitle("EXAMVAN — Server Configuration")
        self.setWindowFlags(self.windowFlags() & ~Qt.WindowContextHelpButtonHint)

        # Outer layout centers the form card
        outer = QVBoxLayout(self)
        outer.setContentsMargins(0, 0, 0, 0)

        # Spacer top
        outer.addStretch(2)

        # Centered card container
        card = QWidget()
        card.setFixedWidth(440)
        card.setObjectName("loginCard")
        from .styles import is_system_dark
        if is_system_dark():
            card.setStyleSheet("QWidget#loginCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
        else:
            card.setStyleSheet("QWidget#loginCard { background-color: #ffffff; border: 1px solid #ccd0da; border-radius: 12px; }")
        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(10)
        card_layout.setContentsMargins(40, 40, 40, 40)

        # Title
        title = QLabel("EXAMVAN")
        title.setStyleSheet("font-size: 28px; font-weight: bold;")
        title.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(title)

        subtitle = QLabel("Sistem Ujian Digital — Linux Client")
        subtitle.setStyleSheet("font-size: 13px;")
        subtitle.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(subtitle)

        card_layout.addSpacing(16)

        # Server URL
        lbl_url = QLabel("Server URL")
        self.input_url = QLineEdit()
        self.input_url.setPlaceholderText("http://192.168.1.100:80 atau localhost")
        card_layout.addWidget(lbl_url)
        card_layout.addWidget(self.input_url)

        # Token
        lbl_token = QLabel("Token Ujian")
        self.input_token = QLineEdit()
        self.input_token.setPlaceholderText("8 karakter (contoh: ABCD1234)")
        self.input_token.setMaxLength(8)
        card_layout.addWidget(lbl_token)
        card_layout.addWidget(self.input_token)

        # Remember
        self.chk_remember = QCheckBox("Simpan URL & Token")
        self.chk_remember.setChecked(True)
        card_layout.addWidget(self.chk_remember)

        card_layout.addSpacing(8)

        # Connect button
        self.btn_connect = QPushButton("  Hubungkan  ")
        self.btn_connect.clicked.connect(self._on_connect)
        card_layout.addWidget(self.btn_connect, alignment=Qt.AlignCenter)

        # Enter key triggers connect
        self.input_token.returnPressed.connect(self._on_connect)
        self.input_url.returnPressed.connect(self._on_connect)

        # Status
        self.lbl_status = QLabel("")
        self.lbl_status.setStyleSheet("color: #e53935;")
        self.lbl_status.setAlignment(Qt.AlignCenter)
        self.lbl_status.setWordWrap(True)
        card_layout.addWidget(self.lbl_status)

        outer.addWidget(card, alignment=Qt.AlignHCenter)

        # Spacer bottom
        outer.addStretch(2)

        # Version label
        ver_label = QLabel(f"v{__version__} (API {APP_VERSION})")
        ver_label.setStyleSheet("font-size: 11px;")
        ver_label.setAlignment(Qt.AlignRight)
        outer.addWidget(ver_label)

    def _load_saved(self) -> None:
        url = config.get("server_url", "")
        token = config.get("exam_token", "")
        remember = config.get("remember_url", True)
        if url:
            self.input_url.setText(url)
        if token:
            self.input_token.setText(token.upper())
        self.chk_remember.setChecked(remember)

    def _on_connect(self) -> None:
        url = self.input_url.text().strip()
        token = self.input_token.text().strip().upper()

        if not url:
            self.lbl_status.setStyleSheet("color: #e53935;")
            self.lbl_status.setText("Masukkan URL server")
            self.input_url.setFocus()
            return

        # Auto-prepend http:// if no scheme, strip trailing slashes
        if not url.startswith("http://") and not url.startswith("https://"):
            url = "http://" + url
        url = url.rstrip("/")
        self.input_url.setText(url)

        if not token or len(token) != 8:
            self.lbl_status.setStyleSheet("color: #e53935;")
            self.lbl_status.setText("Token harus 8 karakter")
            self.input_token.setFocus()
            return

        self.input_token.setText(token)
        self.btn_connect.setEnabled(False)
        self.lbl_status.setStyleSheet("color: #6c7086;")
        self.lbl_status.setText("Menghubungkan...")
        self._server_url = url

        # Run health check + token lookup in background thread
        threading.Thread(
            target=self._connect_thread,
            args=(url, token),
            daemon=True,
        ).start()

    def _connect_thread(self, url: str, token: str) -> None:
        """Background thread: health check → token lookup."""
        # Step 1: Health check
        health = api.check_health(url)
        if not health.success:
            self._sig_status.emit(f"Gagal terhubung ke server:\n{health.status}", True)
            self._sig_enable_btn.emit()
            return

        # Step 2: Token lookup
        resp = api.get_exam_by_token(url, token)
        if not resp.success or not resp.exam:
            msg = resp.message or resp.error or "Token tidak valid"
            self._sig_status.emit(msg, True)
            self._sig_enable_btn.emit()
            return

        # Save config
        if self.chk_remember.isChecked():
            config.set("server_url", url)
            config.set("exam_token", token)
            config.set("remember_url", True)
        else:
            config.set("remember_url", False)

        self._exam = resp.exam
        self._server_url = url

        # Show identity dialog on UI thread via signal
        self._sig_show_identity.emit()

    # --- Slots (run on UI thread, connected via signals) ---

    @pyqtSlot(str, bool)
    def _set_status_slot(self, msg: str, is_error: bool) -> None:
        color = "#e53935" if is_error else "#2e7d32"
        self.lbl_status.setStyleSheet(f"color: {color};")
        self.lbl_status.setText(msg)

    @pyqtSlot()
    def _enable_btn_slot(self) -> None:
        self.btn_connect.setEnabled(True)

    @pyqtSlot()
    def _show_identity_dialog(self) -> None:
        """Show identity dialog after successful token lookup."""
        from .identity_dialog import IdentityDialog

        assert self._exam is not None
        saved_identity = config.get("identity_data", {})

        dlg = IdentityDialog(self._exam, saved_data=saved_identity, parent=self)
        dlg.show()
        screen = self.screen()
        if screen:
            dlg.setGeometry(screen.availableGeometry())
        dlg.showMaximized()
        QApplication.processEvents()
        if dlg.exec_() == QDialog.Accepted:
            identity = dlg.get_identity_data()
            config.set("identity_data", identity)
            self.exam_selected.emit(self._exam, self._server_url, identity)
            self.accept()
        else:
            self.btn_connect.setEnabled(True)
            self.lbl_status.setText("")
