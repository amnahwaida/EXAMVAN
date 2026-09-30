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

# Alamat server default untuk deployment sekolah.
#
# Satu konstanta, bukan string yang disalin ke beberapa tempat: dipakai juga
# oleh `windows/README.md` dan dialog, supaya tidak bisa melenceng.
# Bentuk KANONIK (https://, tanpa trailing slash) karena `_on_connect`
# menormalkan input dengan asumsi itu.
DEFAULT_SERVER_URL = "https://examvan.my.id"


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

        # Nomor versi -- DI DALAM kartu, bukan di layout luar.
        #
        # Dulu label ini ditambahkan ke `outer` setelah addStretch(2), jadi
        # tertorong ke tepi bawah jendela. Di jendela maximized tepi bawah itu
        # berimpit dengan taskbar dan tepi layar: label tertutup atau
        # terpotong -- padahal ini satu-satunya cara memastikan build mana
        # yang sedang terpasang. Di dalam kartu, posisinya ikut isi kartu:
        # selalu terlihat, tidak pernah tertutup.
        #
        # Satu nomor, bukan dua. Label pernah menampilkan dua angka yang
        # saling menyangkal di depan mata pengguna.
        self._ver_label = QLabel(f"v{APP_VERSION}")
        self._ver_label.setStyleSheet("font-size: 12px;")
        self._ver_label.setAlignment(Qt.AlignCenter)
        card_layout.addWidget(self._ver_label)

        card_layout.addSpacing(16)

        # Server URL
        lbl_url = QLabel("Server URL")
        self.input_url = QLineEdit()
        self.input_url.setPlaceholderText(DEFAULT_SERVER_URL)
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


    def _load_saved(self) -> None:
        """Isi form dari config, atau default server bila belum pernah diisi.

        `remember_url` akhirnya DICHIBAHKAN, yang sebelumnya tidak berlaku
        sama sekali: nilainya dibaca tapi tidak pernah dipakai sebagai
        syarat, sehingga URL dan token selalu di-pre-fill apa pun yang
        siswa pilih. Sekarang kontraknya jelas:

          dicentang   -> isi lagi di komputer ini (atau pakai default server)
          tidak       -> jangan sentuh; form dikosongkan

        Yang TIDAK berubah: token tetap ditulis ke disk walau tidak
        dicentang, karena `_xor_obfuscate` memakainya sebagai kunci decode
        jawaban yang tersimpan (`config.py:32-39`). Menghapusnya akan
        membuat recovery "Kirim Lagi" gagal decode. Yang dikontrol remember
        adalah pre-fill, bukan penyimpanan.
        """
        remember = bool(config.get("remember_url", True))
        self.chk_remember.setChecked(remember)
        if not remember:
            return

        url = config.get("server_url", "") or DEFAULT_SERVER_URL
        token = config.get("exam_token", "")
        if url:
            self.input_url.setText(url)
        if token:
            self.input_token.setText(token.upper())

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
        # Status checkbox dibaca di thread GUI, DI SINI. `_connect_thread`
        # berjalan di worker dan tidak boleh menyentuh QCheckBox -- baca
        # widget Qt dari thread lain tidak thread-safe, dan di Windows
        # dialog ini bisa sedang di-maximize bersamaan.
        remember_url = self.chk_remember.isChecked()
        threading.Thread(
            target=self._connect_thread,
            args=(url, token, remember_url),
            daemon=True,
        ).start()

    def _connect_thread(
        self, url: str, token: str, remember_url: bool = True
    ) -> None:
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
        config.set("remember_url", remember_url)

        # Tidak ada gerbang "sudah dikerjakan" di titik mana pun.
        # Lihat _offer_pending_recovery() untuk penjelasan policies-nya.

        self._exam = resp.exam
        self._server_url = url

        # Show identity dialog on UI thread via signal
        self._sig_show_identity.emit()

    # --- Slots (run on UI thread, connected via signals) ---

    def enable_connect(self) -> None:
        """Hidupkan lagi tombol "Hubungkan" dan bersihkan status.

        Dipanggil `__main__.on_exam_selected` ketika siswa membatalkan dialog
        persetujuan. Tanpa ini, `_on_connect` men-disable tombol (`:208`),
        jalur sukses hanya meng-emit `_sig_show_identity` (`:273`) dan tidak
        pernah `_sig_enable_btn` — jadi dialog konfigurasi kembali dengan
        tombol mati dan siswa tidak bisa mengulang tanpa menutup aplikasi.
        """
        self.btn_connect.setEnabled(True)
        self.lbl_status.setText("")

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
        from ..utils import build_attempt_key, get_device_label, map_identity_to_standard

        # Token dibaca dari config, BUKAN dari QLineEdit: pemanggilan ini
        # berjalan di worker thread, dan membaca widget Qt dari luar
        # thread GUI tidak thread-safe. `config.set("exam_token", ...)`
        # sudah menyimpan token yang sama sebelum dialog ini muncul.
        token = str(config.get("exam_token", "") or "").strip().upper()
        identity = config.get("identity_data", {}) or {}
        answers = config.load_answers(exam.id) or {}
        std = map_identity_to_standard(identity)
        start_time = config.load_start_time(exam.id)
        # Scoped dengan token + identitas yang sama seperti ExamViewer, jadi
        # jawaban yang dikirim ulang menimpa baris yang sama di server.
        # Tanpa scope, label mesin tidak akan cocok dengan label kursi yang
        # dipakai saat ujian berjalan dan muncul baris kedua.
        mac = get_device_label(build_attempt_key(token, identity))
        try:
            resp = api.submit_with_retry(
                self._server_url, exam.id,
                std.get("student_name", ""),
                std.get("exam_number", ""),
                std.get("student_class", ""),
                answers, start_time, mac, identity,
                # Tanpa header ini server menolak dengan 401 "Token tidak
                # disertakan" — kirim ulang dari layar ini pun selalu gagal.
                token=token,
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

    def _offer_pending_recovery(self) -> bool:
        """True bila siswa boleh lanjut. Tidak pernah menolak.

        Dulu method ini memblokir dengan pesan "Ujian ini sudah
        dikumpulkan pada perangkat ini" begitu ada jawaban yang pernah
        dikirim dari PC ini. Aturan itu dihapus: satu PC lab dipakai
        banyak siswa, dan memblokir berdasarkan "sudah pernah" menutup
        jalan bagi siswa yang memang belum sempat mengerjakan -- dan bagi
        yang memang harus mengulang.

        Pembatasan dipindahkan ke sisi server, di halaman pengawas, di
        mana pengawas bisa melihat dan memutuskan. Client bukan tempat
        untuk aturan yang tidak bisa diaudit.

        Yang tersisa di sini BUKAN pembatasan, tapi pemulihan data: kalau
        submit sebelumnya gagal dan jawaban masih tersimpan di disk, itu
        milik siswa dan harus dikirim ulang. Itu urusan `_show_recovery`,
        tidak ada hubungannya dengan "sudah pernah mengerjakan".
        """
        assert self._exam is not None
        if config.load_answers(self._exam.id):
            self._sig_recovery_available.emit(self._exam)
            return False
        return True

    @pyqtSlot()
    def _show_identity_dialog(self) -> None:
        """Show identity dialog after successful token lookup."""
        from .identity_dialog import IdentityDialog

        assert self._exam is not None
        saved_identity = config.get("identity_data", {})

        dlg = IdentityDialog(self._exam, saved_data=saved_identity, parent=self)
        # Sekadar `showMaximized()`. Sebelumnya ada setGeometry(
        # availableGeometry()) di antara show() dan showMaximized() -- itu
        # sia-sia, karena showMaximized() menimpanya, dan hanya membuat
        # pembaca mengira ukuran dialog diatur di sini padahal tidak.
        # Dialog lain di app ini juga cukup dengan showMaximized(); yang
        # fullscreen pakai _maximize_window(fullscreen=True).
        dlg.showMaximized()
        QApplication.processEvents()
        if dlg.exec_() == QDialog.Accepted:
            identity = dlg.get_identity_data()
            # Identitas disimpan DULU, sebelum recovery dicek.
            #
            # `_sig_recovery_available.connect(self._show_recovery)` tanpa
            # `Qt.QueuedConnection`, jadi `_offer_pending_recovery()`
            # memanggil `_show_recovery()` secara SYNCHRONOUS -- dan di
            # situulah thread `_recovery_submit_thread` dijalankan. Thread
            # itu membaca identitas dari `config.get("identity_data")`.
            #
            # Urutan lama (set -> return -> set) berarti worker membaca
            # store yang masih kosong: server membalas 400 "Identitas
            # 'Nama' wajib diisi" dan fitur "Kirim Lagi" tidak pernah bisa
            # bekerja. Kerusakan kedua: `build_attempt_key(token, {})`
            # menghasilkan label mesin yang BERBEDA dari ujian, jadi
            # pengiriman ulang akan membuat baris kedua dan placeholder
            # aslinya menggantung "in progress" selamanya.
            #
            # Pemulihan jawaban yang belum terkirim. Bukan pembatasan:
            # siswa boleh mengulang, tapi jawaban yang masih tertinggal di
            # disk milik dia dan jangan sampai hilang diam-diam.
            config.set("identity_data", identity)

            if not self._offer_pending_recovery():
                return
            self.exam_selected.emit(self._exam, self._server_url, identity)
            self.accept()
        else:
            self.btn_connect.setEnabled(True)
            self.lbl_status.setText("")
