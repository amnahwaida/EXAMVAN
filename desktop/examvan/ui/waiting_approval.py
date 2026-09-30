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
from ..utils import build_attempt_key, get_device_label


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

        # Identitas PERANGKAT tunggal (DESKTOP:<hash>) — HARUS sama persis
        # dengan yang dipakai download PDF (X-Device-Id) dan submit
        # (mac_address). Sebelumnya di sini dipakai get_mac_address() (MAC
        # mentah) sedangkan submit memakai get_device_label() → baris approval
        # (kunci gate PDF) dan baris submission (kunci upsert) tidak match →
        # siswa tampil 2× di monitoring dan approval tidak pernah di-revoke
        # worker. Satu identitas untuk ketiga endpoint (pola DEVICE:<AndroidId>
        # di Android).
        #
        # Di-scope dengan build_attempt_key (token + identitas siswa) supaya
        # label menandai KURSI yang sedang dipakai, bukan mesin fisik. Tanpa
        # itu, satu PC lab hanya bisa dipakai satu kali: server menegakkan
        # "satu perangkat satu percobaan" dan siswa berikutnya diblokir.
        # Kuncinya identik dengan yang dipakai ExamViewer karena keduanya
        # dibangun dari token + identity_data yang sama.
        self.mac_address = get_device_label(
            build_attempt_key(self._token, self.identity_data)
        )
        self.is_waiting = True
        # Stop token untuk thread poll.
        #
        # `is_waiting` dulu dipakai ganda: sebagai kondisi loop DAN sebagai
        # perintah berhenti. Itu causes race saat "Minta Izin Lagi": status
        # "rejected" menyetelnya False, tapi thread lama masih di
        # time.sleep(5). Kalau siswa menekan retry sebelum thread itu bangun,
        # is_waiting=True lagi dan thread lama melanjutkan loop-nya — dua
        # poller request-approval berjalan bersamaan untuk satu siklus
        # persetujuan, yang satu reset=True dan yang lain reset=False.
        # Event terpisah membuat "berhenti" irreversible, jadi tidak ada
        # jalan bagi loop lama untuk hidup kembali.
        self._poll_stop = threading.Event()
        self._poll_thread_obj: Optional[threading.Thread] = None

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
        # Hanya boleh ada SATU poller. Kalau ada yang masih hidup, hentikan
        # dulu dan tunggu — kalau tidak, retry akan menghasilkan dua thread
        # yang keduanya memanggil request-approval.
        self._stop_polling()
        self._poll_stop.clear()
        self._poll_thread_obj = threading.Thread(
            target=self._poll_thread, daemon=True
        )
        self._poll_thread_obj.start()

    def _stop_polling(self, timeout: float = 2.0) -> None:
        """Set stop token lalu tunggu thread poll benar-benar berhenti."""
        self._poll_stop.set()
        thread = self._poll_thread_obj
        if thread is not None and thread.is_alive():
            thread.join(timeout=timeout)
        self._poll_thread_obj = None

    def _poll_thread(self):
        first_check = True
        while not self._poll_stop.is_set():
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

            if self._poll_stop.is_set():
                break

            if not resp.success and resp.status in ("pending", "error") and resp.message:
                # Koneksi terganggu, atau server membalas sesuatu yang bukan
                # JSON (halaman blok proxy, captive portal, WAF).
                #
                # Status "error" HARUS ditangani di sini. Kalau tidak, ia
                # jatuh ke cabang `else` yang menampilkan "Menunggu
                # Persetujuan" -- dan polling berjalan tiap 5 detik
                # SELAMANYA tanpa satu pesan pun, sehingga siswa duduk
                # menghadap layar yang tidak akan berubah dan mengira
                # hanya menunggu tanpa tahu jaringannya bermasalah.
                self._sig_status.emit("error", "Koneksi Terganggu", f"Mencoba menghubungkan ulang...\n{resp.message}")
            else:
                if resp.status == "approved":
                    self._sig_status.emit("approved", "Disetujui!", "Akses Anda telah disetujui. Memulai ujian...")
                    break
                elif resp.status == "rejected":
                    self._sig_status.emit("rejected", "Akses Ditolak", "Pengawas menolak permintaan akses Anda.")
                    break
                elif resp.status == "repeat_required":
                    # Server menolak karena siswa ini sudah pernah
                    # mengirim jawaban untuk ujian yang sama. Berbeda dengan
                    # "rejected", ini BUKAN keputusan pengawas -- dan bisa
                    # berubah kapan saja begitu pengawas menekan
                    # "Izinkan Mengulang" di halaman pengawasan.
                    #
                    # Karena itu polling DIJALANKAN TERUS, bukan di-break.
                    # Kalau di-break, siswa harus menutup dan membuka ulang
                    # aplikasi untuk mencoba lagi, dan tidak ada yang tahu
                    # kalau izinnya sudah diberikan 30 detik yang lalu.
                    self._sig_status.emit(
                        "repeat_required",
                        "Sudah Dikerjakan",
                        "Ujian ini sudah Anda kerjakan.\n\n"
                        "Hubungi pengawas bila perlu izin mengulang.\n"
                        'Klik "Periksa Lagi" setelah mendapat izin.',
                    )
                    self.is_waiting = True
                    self.btn_retry.show()
                else:
                    self._sig_status.emit("pending", "Menunggu Persetujuan", "Silakan tunggu pengawas menyetujui akses Anda.")

            # `wait()` supaya tombol Batal / "Minta Izin Lagi" bisa
            # menghentikan thread tanpa menunggu 5 detik penuh.
            self._poll_stop.wait(5)

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
        elif status_type == "repeat_required":
            self.icon_label.setText("🔁")
            self.btn_retry.setText("Periksa Lagi")
            self.btn_retry.show()
            self.is_waiting = True
        elif status_type == "error":
            self.icon_label.setText("⚠️")
        else:
            self.icon_label.setText("⏳")
            self.btn_retry.hide()
            self.btn_cancel.setText("Batal")

    def _retry_approval(self):
        # Stop dulu yang lama, baru buka yang baru — lihat _start_polling.
        self._stop_polling()
        self.is_waiting = True
        self.btn_retry.hide()
        self.btn_cancel.setText("Batal")
        self.icon_label.setText("⏳")
        self.title_label.setText("Menunggu Persetujuan")
        self.subtitle_label.setText(f"Ujian: {self.exam.name}\nSilakan tunggu pengawas menyetujui akses Anda.")
        self._start_polling()

    def reject(self):
        self.is_waiting = False
        self._stop_polling()
        super().reject()

    def closeEvent(self, event) -> None:
        # Menutup dialog dengan cara lain (Alt+F4, task manager, WM close)
        # harus meninggalkan thread poll yang sedang berjalan, kalau tidak
        # ia tetap calls request-approval untuk exam yang sudah ditinggalkan.
        self._stop_polling()
        super().closeEvent(event)
