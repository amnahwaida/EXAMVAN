"""Waiting approval dialog."""

from __future__ import annotations

import logging
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
from ..utils import build_attempt_key, get_device_label, map_identity_to_standard

# H4: `exam.name` dan `resp.message` dari server tidak pernah divalidasi,
# sementara default `QLabel.textFormat()` adalah `Qt.AutoText` — markup dari
# server jadi rich text, dan `<img src="file://...">` membuat
# `QTextDocument` membuka berkas lokal sinkron di thread GUI. Sanitizer-nya
# SATU-SATUNYA di repo (`congratulations`, tidak boleh diedit ronde ini):
# di-IMPORT, bukan disalin, supaya tidak lahir dua versi yang berbeda.
from .congratulations import _sanitize_server_text

log = logging.getLogger(__name__)


class WaitingApprovalDialog(QDialog):
    """Dialog that polls the server for approval."""

    _sig_status = pyqtSignal(str, str, str)  # status_type (approved, rejected, pending, error), title, message

    def __init__(self, exam: Exam, server_url: str, identity_data: dict, token: str = "", parent=None):
        super().__init__(parent)
        self.exam = exam
        self.server_url = server_url
        self.identity_data = identity_data
        self._token = token  # exam token — required by the server (anti-spam)
        
        # Tiga field dihitung dari SATU pemanggilan map_identity_to_standard
        # — implementasi tunggal yang sama dipakai submit (exam_viewer),
        # recovery, dan approval. Dulu dialog ini punya penebak sendiri
        # (_extract_field, substring per keyword) yang memetakan
        # `nama_peserta` ke exam_number sementara submit memetakannya ke
        # nama: baris approval dan baris submission tidak match.
        _std = map_identity_to_standard(identity_data)
        self.student_name = str(_std.get("student_name", ""))
        self.exam_number = str(_std.get("exam_number", ""))
        self.student_class = str(_std.get("student_class", ""))

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
        self._poll_generation = 0
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
        # Warna kartu mengikuti `styles.app_theme_dark()`, bukan
        # tema sistem: kartu gelap di dalam jendela terang (atau
        # sebaliknya) adalah regresi butir 1 di
        # `tests/test_styles_dark_regression.py`.
        card.setStyleSheet("QWidget#waitingCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")

        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(10)
        card_layout.setContentsMargins(40, 40, 40, 40)

        self.icon_label = QLabel("⏳")
        self.icon_label.setStyleSheet("font-size: 48px;")
        self.icon_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self.icon_label)

        self.title_label = QLabel("Menunggu Persetujuan")
        # Judul dan subtitle ditulis ulang dari teks server di bawah ini,
        # jadi keduanya harus PlainText sejak awal (lihat catatan import).
        self.title_label.setTextFormat(Qt.PlainText)
        self.title_label.setStyleSheet("font-size: 20px; font-weight: bold;")
        self.title_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self.title_label)

        self.subtitle_label = QLabel(self._pending_subtitle_text())
        self.subtitle_label.setTextFormat(Qt.PlainText)
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

    def _pending_subtitle_text(self) -> str:
        """Teks subtitle default — nama ujian disanitasi (H4).

        Satu helper untuk `_setup_ui` dan `_retry_approval`: keduanya
        menulis teks yang sama ke label yang sama, dan kalau hanya salah
        satu yang disanitasi, jalur retry menjadi celahnya.
        """
        return _sanitize_server_text(
            f"Ujian: {self.exam.name}\n"
            "Silakan tunggu pengawas menyetujui akses Anda."
        )

    def _start_polling(self):
        # Hanya boleh ada SATU poller. Kalau ada yang masih hidup, hentikan
        # dulu dan tunggu — kalau tidak, retry akan menghasilkan dua thread
        # yang keduanya memanggil request-approval.
        self._stop_polling()
        # Generasi naik SETIAP poller baru. Poller lama memegangnya, dan
        # keluar begitu melihat generasinya sudah bukan yang terbaru.
        #
        # Ini yang menutup race yang tidak bisa ditutup dengan join saja:
        # `request_approval` punya timeout 10 detik, jadi `join(2s)` SELALU
        # melepas thread lama sementara ia masih di dalam panggilan HTTP.
        # Poller lama akan keluar 10 detik kemudian -- satu siklus
        # persetujuan kemudian -- dan `reset=True`-nya bisa me-reset approval
        # yang baru saja diberikan pengawas.
        self._poll_generation += 1
        self._poll_stop.clear()
        self._poll_thread_obj = threading.Thread(
            target=self._poll_thread,
            args=(self._poll_generation,),
            daemon=True,
        )
        self._poll_thread_obj.start()

    def _stop_polling(self) -> None:
        """Set stop token lalu naikkan generasi. TIDAK join.

        Join pernah dipakai karenaFear poller lama bertahan hidup: satu
        `api.request_approval` bisa memblokir 10 detik (HTTP timeout),
        jadi join 2 detik selalu gagal. Naikkan ke 12 detik dan poller
        lama tidak lagi bocor -- tapi karena `_stop_polling()` dipanggil
        dari `reject()`, `closeEvent()`, dan "Minta Izin Lagi", join itu
        berjalan di THREAD GUI.

        Jaringan mati -> thread poll sedang memblokir di socket ->
        GUI membeku 10-12 detik: tanpa repaint, tanpa input, dan tanpa
        enforcement keamanan (timer `SecurityEnforcer` juga butuh event
        loop). `reject()` baru mencapai `super()` sesudah join, jadi
        dialog benar-benar terlihat beku.

        Join tidak diperlukan. `_poll_stop.set()` memberi tahu poller
        untuk keluar, dan generation counter membuat poller yang keluar
        terlambat langsung `return` tanpa menyentuh widget dialog yang
        sudah ditutup -- persis jaring pengaman yang dulu jadi alasan
        join dipakai.
        """
        self._poll_stop.set()
        # Naikkan generasi: poller yang sedang memblokir di socket akan
        # melihat nomor basi dan berhenti begitu ia kembali, tanpa
        # sempat menembak sinyal ke dialog yang sudah ditutup.
        self._poll_generation += 1
        self._poll_thread_obj = None

    def _poll_thread(self, generation: int = 0):
        first_check = True
        while not self._poll_stop.is_set():
            # Poller basi: ada yang lebih baru. Keluar tanpa menebak.
            if generation and generation != self._poll_generation:
                return
            try:
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
            except Exception as e:
                # request_approval sendiri best-effort, tapi Mock/patch di
                # test atau bug tak terduga tetap bisa melempar ke sini.
                # Tanpa ini thread poll mati diam-diam dan dialog menunggu
                # selamanya tanpa pesan. Tampilkan sekali, lalu berhenti.
                if generation and generation != self._poll_generation:
                    return
                self._sig_status.emit(
                    "error", "Koneksi Terganggu",
                    f"Mencoba menghubungkan ulang...\n{e}")
                break
            first_check = False

            if self._poll_stop.is_set():
                break

            # Poller lama yang kembali TERLAMBAT (10 detik di dalam HTTP
            # sementara retry sudah jalan): generasinya basi — jangan
            # sentuh UI, apalagi me-reset approval yang baru diberikan.
            if generation and generation != self._poll_generation:
                return

            # Verdict permanen 401/403/404 (token salah, akses ditolak,
            # ujian tidak ada): server tidak akan berubah pikiran, jadi
            # polling lagi tiap 5 detik SELAMANYA hanya memutar loop
            # "Koneksi Terganggu". Perlakukan sebagai penolakan final
            # dengan pesan ASLI server, lalu berhenti.
            if getattr(resp, "http_status", None) in (401, 403, 404):
                msg = resp.message or "Permintaan akses ditolak server."
                self._sig_status.emit("rejected", "Akses Ditolak", msg)
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
                    # Segarkan koreksi waktu server SETELAH persetujuan, SEBELUM
                    # siswa diberi tahu ujian dimulai.
                    #
                    # `skew` dihitung sekali saat tombol Hubungkan. Di antara
                    # Hubungkan dan titik ini ada dialog identitas + menunggu
                    # persetujuan (bisa menit), dan selama itu belum ada
                    # keyboard hook / fullscreen yang menahan siswa — jadi
                    # memundurkan jam OS di fase ini menggeser deadline awal
                    # yang dipinjam viewer (monotonic) menjadi lebih panjang.
                    # `/api/health` menghitung ulang skew terhadap jam
                    # PERANGKAT SEKARANG, sehingga penundaan itu netral.
                    # Best-effort: kegagalan tidak boleh menahan ujian, dan
                    # server tetap menegakkan deadline lewat 403 pada submit.
                    try:
                        api.check_health(self.server_url)
                    except Exception:
                        log.warning(
                            "gagal menyegarkan skew waktu server saat approval",
                            exc_info=True,
                        )
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
                    # Jangan menyentuh widget Qt dari thread poll:
                    # QWidget::show() bukan thread-safe, dan dialog ini
                    # sedang di-showMaximized() persis di detik pertama
                    # thread berjalan -- maximize + thread = race paling
                    # rawan di Windows. `_on_status_update` (slot GUI)
                    # sudah menampilkan tombolnya, jadi baris ini tidak
                    # diperlukan sama sekali.
                    self.is_waiting = True
                else:
                    self._sig_status.emit("pending", "Menunggu Persetujuan", "Silakan tunggu pengawas menyetujui akses Anda.")

            # `wait()` supaya tombol Batal / "Minta Izin Lagi" bisa
            # menghentikan thread tanpa menunggu 5 detik penuh.
            self._poll_stop.wait(5)

    @pyqtSlot(str, str, str)
    def _on_status_update(self, status_type: str, title: str, message: str):
        # `title`/`message` datang dari server (`resp.message` bisa juga
        # berasal dari proxy/WAF), jadi disanitasi + PlainText — bukan
        # hanya plain, tapi bebas karakter tersembunyi/bidi yang dipakai
        # menyamarkan isi pesan dari mata siswa.
        self.title_label.setText(_sanitize_server_text(title))
        self.subtitle_label.setText(_sanitize_server_text(message))

        if status_type == "approved":
            self.icon_label.setText("✅")
            self.btn_cancel.setEnabled(False)
            self.btn_retry.hide()
            # Auto close and proceed after a short delay — tapi hanya kalau
            # poller yang memicu approval ini masih yang terbaru. Tanpa
            # cek generasi, accept dari poller basi bisa menutup dialog
            # yang sudah di-retry untuk siklus berikutnya.
            #
            # Generasi WAJIB ditangkap DI SINI, saat penjadwalan. Versi lama
            # menulis `self._poll_generation` di dalam lambda, jadi nilainya
            # dibaca 1,5 detik kemudian — setelah `_retry_approval`/
            # `reject()` menaikkan generasi — sehingga
            # `_on_approved_delay` selalu menerima angka yang SAHAM dengan
            # `self._poll_generation` dan penjaganya tidak pernah berarti
            # apa pun. Poller basi pun lalu bisa menutup dialog yang sedang
            # dipakai untuk siklus berikutnya, dan `__main__` langsung
            # membuka jendela ujian tanpa persetujuan untuk percobaan itu.
            _approved_generation = self._poll_generation
            QTimer.singleShot(
                1500, lambda: self._on_approved_delay(_approved_generation),
            )
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

    @pyqtSlot(int)
    def _on_approved_delay(self, generation: int) -> None:
        """Tutup dialog 1,5 detik setelah approval — hanya bila masih relevan.

        Poller lama yang approval-nya datang terlambat (generasi basi)
        atau dialog yang sudah dibatalkan/di-retry tidak boleh di-accept.
        """
        if generation != self._poll_generation:
            return
        if not self.is_waiting:
            return
        self.accept()

    def _retry_approval(self):
        # Stop dulu yang lama, baru buka yang baru — lihat _start_polling.
        self._stop_polling()
        self.is_waiting = True
        self.btn_retry.hide()
        self.btn_cancel.setText("Batal")
        self.icon_label.setText("⏳")
        self.title_label.setText("Menunggu Persetujuan")
        self.subtitle_label.setText(self._pending_subtitle_text())
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
