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
    _sig_recovery_available = pyqtSignal(object)  # Exam — jawaban belum terkirim
    _sig_recovery_done = pyqtSignal(str)          # (message) — kirim ulang sukses

    def __init__(self, kiosk_mode: bool = False, parent=None):
        super().__init__(parent)
        self.kiosk_mode = kiosk_mode
        self._exam: Optional[Exam] = None
        self._server_url = ""

        # Connect signals to slots
        self._sig_status.connect(self._set_status_slot)
        self._sig_enable_btn.connect(self._enable_btn_slot)
        self._sig_show_identity.connect(self._show_identity_dialog)
        self._sig_recovery_available.connect(self._show_recovery)
        self._sig_recovery_done.connect(self._recovery_done_slot)

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

        subtitle = QLabel("Sistem Ujian Digital — Desktop Client")
        subtitle.setStyleSheet("font-size: 13px;")
        subtitle.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(subtitle)

        card_layout.addSpacing(16)

        # Server URL
        lbl_url = QLabel("Server URL")
        self.input_url = QLineEdit()
        self.input_url.setPlaceholderText("https://examvan.my.id")
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

        # Cloud-only: default HTTPS, strip trailing slashes. Cleartext HTTP not supported.
        if not url.startswith("http://") and not url.startswith("https://"):
            url = "https://" + url
        url = url.rstrip("/")
        self.input_url.setText(url)

        if url.startswith("http://"):
            self.lbl_status.setStyleSheet("color: #e53935;")
            self.lbl_status.setText("Koneksi HTTP (cleartext) tidak didukung. Gunakan HTTPS.")
            self.input_url.setFocus()
            return

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

        # Save config SEBELUM gate submitted — decode jawaban di disk memakai
        # token sebagai kunci XOR (_xor_obfuscate); token yang baru diketik
        # harus tersimpan dulu agar recovery bisa membaca jawaban tersimpan.
        # URL+token selalu disimpan (token juga dipakai sebagai kunci decode);
        # `remember_url` hanya mengontrol apakah di-reload ke input berikutnya.
        config.set("server_url", url)
        config.set("exam_token", token)
        config.set("remember_url", self.chk_remember.isChecked())

        # Step 2b: Sticky "already submitted" gate (F2, mirror Android).
        # Setelah submit SUKSES durable, re-entry ujian yang sama diblokir di
        # sini — sebelum approval/PDF — sehingga watchdog deadline tidak bisa
        # mengirim submit kosong yang MENIMPA jawaban asli dalam window grace
        # server (end_time + 60 dtk).
        if config.is_submitted(resp.exam.id):
            # Recovery (mirror Android hasPendingAnswers): submit otomatis
            # background sebelumnya GAGAL — jawaban masih tersimpan di disk
            # (clear hanya saat submit durable) → tawarkan kirim ulang.
            pending = config.load_answers(resp.exam.id)
            if pending:
                self._exam = resp.exam
                self._server_url = url
                self._sig_recovery_available.emit(resp.exam)
                return
            self._sig_status.emit(
                "Ujian ini sudah dikumpulkan pada perangkat ini. "
                "Hubungi pengawas bila Anda memerlukan izin mengulang.",
                True,
            )
            self._sig_enable_btn.emit()
            return

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

    # --- Recovery re-entry (auto-submit background gagal) ---

    @pyqtSlot(object)
    def _show_recovery(self, exam) -> None:
        """Layar recovery (mirror Android showPendingSubmitRecoveryScreen).

        Jawaban masih tersimpan di disk karena auto-submit background tidak
        pernah dikonfirmasi durable. Tawarkan kirim ulang — server idempoten
        (upsert per exam+mac), retry tidak menduplikasi baris. Sukses →
        jawaban lokal dihapus; gagal → tetap di disk, tombol kembali aktif.
        """
        reply = QMessageBox.question(
            self,
            "Jawaban Belum Terkirim",
            "Pengumpulan otomatis sebelumnya tidak sampai ke server. "
            "Jawaban masih tersimpan di perangkat ini.\n\n"
            "Kirim ulang sekarang?",
            QMessageBox.Yes | QMessageBox.No,
            QMessageBox.Yes,
        )
        if reply != QMessageBox.Yes:
            self.btn_connect.setEnabled(True)
            self.lbl_status.setText("")
            return

        self.btn_connect.setEnabled(False)
        self.lbl_status.setStyleSheet("color: #2e7d32;")
        self.lbl_status.setText("Mengirim ulang jawaban...")
        threading.Thread(
            target=self._recovery_submit_thread,
            args=(exam,),
            daemon=True,
        ).start()

    def _recovery_submit_thread(self, exam) -> None:
        """Kirim ulang jawaban tersimpan di background — TIDAK menyentuh Qt."""
        from ..utils import get_device_label, map_identity_to_standard

        token = self.input_token.text().strip().upper()
        identity = config.get("identity_data", {}) or {}
        answers = config.load_answers(exam.id) or {}
        std = map_identity_to_standard(identity)
        start_time = config.load_start_time(exam.id)
        mac = get_device_label()
        try:
            resp = api.submit_with_retry(
                self._server_url, exam.id,
                std.get("student_name", ""),
                std.get("exam_number", ""),
                std.get("student_class", ""),
                answers, start_time, mac, identity,
            )
            if resp.status == "queued" and resp.job_id:
                resp = api.poll_queued_result(
                    self._server_url, exam.id, token, mac, resp.job_id, identity,
                )
            if resp.success:
                config.clear_answers(exam.id)
                try:
                    api.complete_exam(self._server_url, exam.id, token, mac)
                except Exception:
                    pass
                msg = resp.congrats_message or resp.message or "Jawaban berhasil dikumpulkan."
                self._sig_recovery_done.emit(msg)
            else:
                self._sig_status.emit(
                    "Pengiriman ulang gagal: "
                    + (resp.message or "terjadi kesalahan")
                    + ". Jawaban tetap tersimpan — coba lagi.",
                    True,
                )
                self._sig_enable_btn.emit()
        except Exception as e:
            self._sig_status.emit(
                "Pengiriman ulang gagal: " + str(e)
                + ". Jawaban tetap tersimpan — coba lagi.",
                True,
            )
            self._sig_enable_btn.emit()

    @pyqtSlot(str)
    def _recovery_done_slot(self, msg: str) -> None:
        self.btn_connect.setEnabled(True)
        QMessageBox.information(
            self,
            "Berhasil",
            f"Jawaban berhasil dikirim ulang!\n\n{msg}",
        )

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
