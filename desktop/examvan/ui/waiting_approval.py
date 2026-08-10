"""Waiting approval dialog."""

from __future__ import annotations

import threading
import time
from typing import Optional

from PyQt5.QtCore import Qt, pyqtSignal, pyqtSlot, QTimer
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
    QLabel,
    QPushButton,
    QVBoxLayout,
    QWidget,
)

from .. import api
from ..models import Exam
from ..utils import get_mac_address


class WaitingApprovalDialog(QDialog):
    """Dialog that polls the server for approval."""

    _sig_status = pyqtSignal(str, str, str)  # status_type (approved, rejected, pending, error), title, message

    def __init__(self, exam: Exam, server_url: str, identity_data: dict, token: str = "", parent=None):
        super().__init__(parent)
        self.exam = exam
        self.server_url = server_url
        self.identity_data = identity_data
        self._token = token  # exam token — required by the server (anti-spam)
        
        # Extract basic info
        self.student_name = self._extract_field("nama", "name", "student_name")
        self.exam_number = self._extract_field("nomor", "no", "nis", "number", "exam_number")
        self.student_class = self._extract_field("kelas", "class", "student_class")

        self.mac_address = get_mac_address()
        self.is_waiting = True

        self._sig_status.connect(self._on_status_update)

        self._setup_ui()
        self._start_polling()

    def _extract_field(self, *keywords) -> str:
        for k, v in self.identity_data.items():
            kl = k.lower()
            for kw in keywords:
                if kw in kl and v:
                    return str(v)
        return ""

    def _setup_ui(self):
        self.setWindowTitle("Menunggu Persetujuan")
        self.setWindowFlags(self.windowFlags() & ~Qt.WindowContextHelpButtonHint)
        self.setModal(True)

        layout = QVBoxLayout(self)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addStretch(1)

        card = QWidget()
        card.setFixedWidth(440)
        card.setObjectName("waitingCard")
        from .styles import is_system_dark
        if is_system_dark():
            card.setStyleSheet("QWidget#waitingCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
        else:
            card.setStyleSheet("QWidget#waitingCard { background-color: #ffffff; border: 1px solid #ccd0da; border-radius: 12px; }")

        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(10)
        card_layout.setContentsMargins(40, 40, 40, 40)

        self.icon_label = QLabel("⏳")
        self.icon_label.setStyleSheet("font-size: 48px;")
        self.icon_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self.icon_label)

        self.title_label = QLabel("Menunggu Persetujuan")
        self.title_label.setStyleSheet("font-size: 20px; font-weight: bold;")
        self.title_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self.title_label)

        self.subtitle_label = QLabel(f"Ujian: {self.exam.name}\nSilakan tunggu pengawas menyetujui akses Anda.")
        self.subtitle_label.setStyleSheet("font-size: 13px; color: #6c7086;")
        self.subtitle_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self.subtitle_label)

        card_layout.addSpacing(20)

        self.btn_retry = QPushButton("Minta Izin Lagi")
        self.btn_retry.clicked.connect(self._retry_approval)
        self.btn_retry.hide()
        card_layout.addWidget(self.btn_retry, alignment=Qt.AlignCenter)

        self.btn_cancel = QPushButton("Batal")
        self.btn_cancel.clicked.connect(self.reject)
        card_layout.addWidget(self.btn_cancel, alignment=Qt.AlignCenter)

        layout.addWidget(card, alignment=Qt.AlignHCenter)
        layout.addStretch(1)

    def _start_polling(self):
        threading.Thread(target=self._poll_thread, daemon=True).start()

    def _poll_thread(self):
        first_check = True
        while self.is_waiting:
            resp = api.request_approval(
                self.server_url,
                self.exam.id,
                self.student_name,
                self.exam_number,
                self.student_class,
                self.identity_data,
                self.mac_address,
                reset=first_check,
                token=self._token
            )
            first_check = False

            if not self.is_waiting:
                break

            if not resp.success and resp.status == "pending" and resp.message:
                # Connection error
                self._sig_status.emit("error", "Koneksi Terganggu", f"Mencoba menghubungkan ulang...\n{resp.message}")
            else:
                if resp.status == "approved":
                    self._sig_status.emit("approved", "Disetujui!", "Akses Anda telah disetujui. Memulai ujian...")
                    break
                elif resp.status == "rejected":
                    self._sig_status.emit("rejected", "Akses Ditolak", "Pengawas menolak permintaan akses Anda.")
                    break
                else:
                    self._sig_status.emit("pending", "Menunggu Persetujuan", "Silakan tunggu pengawas menyetujui akses Anda.")

            time.sleep(5)

    @pyqtSlot(str, str, str)
    def _on_status_update(self, status_type: str, title: str, message: str):
        self.title_label.setText(title)
        self.subtitle_label.setText(message)

        if status_type == "approved":
            self.icon_label.setText("✅")
            self.btn_cancel.setEnabled(False)
            self.btn_retry.hide()
            # Auto close and proceed after a short delay
            QTimer.singleShot(1500, self.accept)
        elif status_type == "rejected":
            self.icon_label.setText("🚫")
            self.btn_cancel.setText("Kembali")
            self.btn_retry.show()
            self.is_waiting = False
        elif status_type == "error":
            self.icon_label.setText("⚠️")
        else:
            self.icon_label.setText("⏳")
            self.btn_retry.hide()
            self.btn_cancel.setText("Batal")

    def _retry_approval(self):
        self.is_waiting = True
        self.btn_retry.hide()
        self.btn_cancel.setText("Batal")
        self.icon_label.setText("⏳")
        self.title_label.setText("Menunggu Persetujuan")
        self.subtitle_label.setText(f"Ujian: {self.exam.name}\nSilakan tunggu pengawas menyetujui akses Anda.")
        self._start_polling()

    def reject(self):
        self.is_waiting = False
        super().reject()
