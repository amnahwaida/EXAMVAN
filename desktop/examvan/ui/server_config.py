"""Server configuration dialog — URL + token input, health check."""

from __future__ import annotations

import logging
import threading
from typing import Dict, Optional

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
from ..models import Exam, SubmitResponse
from ..utils import (
    build_attempt_key,
    build_student_key,
    get_device_label,
    map_identity_to_standard,
)
from .exam_viewer import answers_match_disk

log = logging.getLogger(__name__)

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
        # Audit 2 Okt 2026 (HIGH H3): guard koneksi ganda. `_on_connect`
        # bisa terpicu DUA sumber sekaligus — klik tombol dan Enter pada
        # salah satu QLineEdit (`returnPressed` tetap terhubung meski tombol
        # di-disable) — dan dulu tidak ada apa pun yang mencegah dua thread
        # `_connect_thread` paralel untuk satu klik ganda/Enter+klik.
        self._connect_in_flight = False
        # Token yang sudah LOLOS validasi, disimpan saat tombol ditekan.
        # `__main__` memakai `validated_token` (bukan membaca ulang
        # QLineEdit saat dialog persetujuan/viewer dibuat) supaya token yang
        # dipakai di seluruh alur PERSIS yang tervalidasi — bukan apa pun
        # yang kebetulan ada di kotak input detik itu.
        self._validated_token = ""
        # Identitas yang dipakai worker recovery — di-capture saat dialog
        # recovery muncul (audit HIGH H13), bukan dibaca ulang dari config
        # di thread, supaya `clear_identity()` / re-entry siswa berikutnya
        # tidak mengubah identitas di tengah pengiriman ulang.
        self._recovery_identity: Dict[str, str] = {}

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

    @property
    def validated_token(self) -> str:
        """Token yang sudah lolos validasi di `_on_connect`.

        Fallback ke isi QLineEdit bila belum pernah validasi (dipakai test
        yang meng-emit `exam_selected` tanpa lewat `_on_connect`).
        """
        if self._validated_token:
            return self._validated_token
        return self.input_token.text().strip().upper()

    def _on_connect(self) -> None:
        # Satu koneksi pada satu waktu. Tanpa ini, Enter di QLineEdit
        # (yang TETAP terhubung meski tombol disabled) + klik tombol
        # menghasilkan dua `_connect_thread` paralel: dua health check,
        # dua token lookup, dua dialog identitas, dan `config.set` yang
        # saling menimpa. Lihat catatan `_connect_in_flight` di __init__.
        if self._connect_in_flight:
            return

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
        # Input dikunci selama koneksi berjalan: sumber keduanya (Enter pada
        # input) memicu `_on_connect` lagi. Dengan guard in-flight + input
        # mati, tidak ada jalur kedua yang tersisa.
        self.input_url.setEnabled(False)
        self.input_token.setEnabled(False)
        self._connect_in_flight = True
        self._validated_token = token
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
        """Background thread: health check → token lookup.

        Audit 2 Okt 2026 (HIGH H1): thread ini dulu TANPA try/except sama
        sekali. Satu exception apa pun yang lolos dari `api.*` atau
        `config.set` (HTTPException dari proxy, OSError saat menulis config
        di disk penuh) mematikan thread tanpa sinyal apa pun: tombol
        "Hubungkan" tetap mati, status tetap "Menghubungkan...", dan siswa
        tidak punya jalan lain selain menutup aplikasi. Semua jalur keluar
        harus meng-emit `_sig_status` + `_sig_enable_btn`.
        """
        try:
            # Step 1: Health check
            health = api.check_health(url)
            if not health.success:
                self._sig_status.emit(
                    f"Gagal terhubung ke server:\n{health.status}", True)
                self._sig_enable_btn.emit()
                return

            # Fingerprint sertifikat yang dilaporkan server — dipantau
            # (audit MEDIUM): perubahan fingerprint untuk server yang sama
            # bisa berarti rotasi sertifikat yang sah, tapi juga MITM di
            # proxy sekolah. Beda → peringatan yang terlihat siswa/pengawas,
            # TANPA memblokir: klien tidak punya daftar fingerprint sah
            # untuk memutuskan, dan memblokir semua rotasi mematikan app
            # tepat saat sekolah memutar sertifikatnya.
            fingerprint = getattr(health, "certificate_fingerprint", None)
            if fingerprint:
                self._check_certificate_fingerprint(url, str(fingerprint))

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
        except Exception as exc:  # noqa: BLE001 — jaring apapun harus sampai ke UI
            log.warning("connect thread crashed", exc_info=True)
            self._sig_status.emit(
                f"Koneksi gagal: {exc}\nCoba lagi atau hubungi pengawas.", True
            )
            self._sig_enable_btn.emit()

    def _check_certificate_fingerprint(self, url: str, fingerprint: str) -> None:
        """Bandingkan fingerprint sertifikat dengan kunjungan sebelumnya.

        Disimpan per URL (kunci `cert_fp_<url>`). Kunjungan pertama: simpan,
        tanpa peringatan. Kunjungan berikutnya yang berbeda: peringatan di
        dialog + log — guru/pengawas yang melihatnya bisa memutuskan.
        """
        try:
            key = f"cert_fp_{url}"
            previous = str(config.get(key, "") or "")
            if previous and previous != fingerprint:
                log.warning(
                    "certificate fingerprint changed for %s: %s -> %s",
                    url, previous[:16], fingerprint[:16],
                )
                self._sig_status.emit(
                    "Peringatan: sertifikat server berubah sejak "
                    "kunjungan terakhir. Bisa jadi rotasi sah, bisa juga "
                    "proxy mencurigakan. Hubungi pengawas bila ragu.",
                    True,
                )
                return
            if not previous:
                config.set(key, fingerprint)
        except Exception:
            # Pemantauan fingerprint tidak boleh menggagalkan koneksi.
            log.debug("fingerprint check failed", exc_info=True)

    # --- Slots (run on UI thread, connected via signals) ---

    def reject(self) -> None:
        # Escape/X MENUTUP window, bukan menyembunyikannya: dialog ini
        # satu-satunya window (quitOnLastWindowClosed) — hide diam-diam
        # meninggalkan aplikasi zombie tanpa jendela.
        self.close()

    def enable_connect(self) -> None:
        """Hidupkan lagi tombol "Hubungkan" dan bersihkan status.

        Dipanggil `__main__.on_exam_selected` ketika siswa membatalkan dialog
        persetujuan. Tanpa ini, `_on_connect` men-disable tombol (`:208`),
        jalur sukses hanya meng-emit `_sig_show_identity` (`:273`) dan tidak
        pernah `_sig_enable_btn` — jadi dialog konfigurasi kembali dengan
        tombol mati dan siswa tidak bisa mengulang tanpa menutup aplikasi.
        """
        self._enable_connect_ui()

    def _enable_connect_ui(self) -> None:
        """Kembalikan seluruh UI koneksi ke keadaan siap-dipakai lagi.

        Tombol DAN kedua input (yang dikunci selama koneksi berjalan),
        plus guard in-flight. Semua jalur selesai — gagal, batal, maupun
        kembali dari dialog persetujuan — lewat sini, jadi tidak ada
        keadaan "tombol hidup tapi input mati" atau sebaliknya.
        """
        self._connect_in_flight = False
        self.btn_connect.setEnabled(True)
        self.input_url.setEnabled(True)
        self.input_token.setEnabled(True)
        self.lbl_status.setText("")

    @pyqtSlot(str, bool)
    def _set_status_slot(self, msg: str, is_error: bool) -> None:
        color = "#e53935" if is_error else "#2e7d32"
        self.lbl_status.setStyleSheet(f"color: {color};")
        self.lbl_status.setText(msg)

    @pyqtSlot()
    def _enable_btn_slot(self) -> None:
        self._enable_connect_ui()

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
        # Audit 2 Okt 2026 (HIGH H13): identitas di-capture SEKARANG, sebelum
        # thread jalan — bukan dibaca ulang dari config di dalam worker.
        # Dulu worker membaca `config.get_identity_data()` saat thread
        # berjalan; kalau di tengah pengiriman identitas di config
        # dibersihkan (clear_identity) atau ditimpa siswa berikutnya,
        # worker mengirim 400 / jawaban tercatat atas nama orang yang
        # salah, dan halaman selamat tampil kosong.
        self._recovery_identity = dict(config.get_identity_data() or {})
        if reply != QMessageBox.Yes:
            self._enable_connect_ui()
            self.lbl_status.setText("")
            return

        self.btn_connect.setEnabled(False)
        self.lbl_status.setStyleSheet("color: #2e7d32;")
        self.lbl_status.setText("Mengirim ulang jawaban...")
        threading.Thread(
            target=self._recovery_submit_thread,
            args=(exam, dict(self._recovery_identity)),
            daemon=True,
        ).start()

    def _recovery_submit_thread(
        self, exam, identity: Dict[str, str]
    ) -> None:
        """Kirim ulang jawaban tersimpan di background — TIDAK menyentuh Qt.

        `identity` DITERIMA SEBAGAI ARGUMEN (lihat `_show_recovery`), bukan
        dibaca ulang dari config: nilainya harus PERSIS yang ditawarkan di
        dialog recovery, apa pun yang terjadi pada config selama proses.
        """
        # Token dibaca dari config, BUKAN dari QLineEdit: pemanggilan ini
        # berjalan di worker thread, dan membaca widget Qt dari luar
        # thread GUI tidak thread-safe. `config.set("exam_token", ...)`
        # sudah menyimpan token yang sama sebelum dialog ini muncul.
        token = str(config.get("exam_token", "") or "").strip().upper()
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
            if resp.status == "queued":
                if resp.job_id:
                    # M2: teruskan congrats 202 (mirror _submit_thread) —
                    # `/result` tidak pernah mengirimkannya.
                    queued_congrats = resp.congrats_message or ""
                    resp = api.poll_queued_result(
                        self._server_url, exam.id, token, mac, resp.job_id, identity,
                        initial_congrats=queued_congrats,
                    )
                else:
                    # M7: antre TANPA job_id = tidak ada konfirmasi yang
                    # bisa di-poll — perlakukan sebagai GAGAL, jangan pernah
                    # tampilkan halaman hijau atas dasar 202 mentah.
                    log.warning(
                        "recovery queued tanpa job_id untuk exam %s — "
                        "dianggap gagal", exam.id,
                    )
                    resp = SubmitResponse(
                        success=False,
                        status="queued",
                        message="Server mengantre jawaban tetapi tidak "
                        "memberikan konfirmasi (job_id kosong). Jawaban "
                        "tetap tersimpan — coba lagi.",
                    )
            if resp.success:
                # Owner guard yang SAMA dengan jalur background
                # (`exam_viewer._background_submit_thread`): worker ini
                # bisa berjalan ~84 dtk (retry 7 + polling 202), dan
                # selama itu siswa bisa re-entry dan memulai percobaan
                # baru yang menulis autosave-nya sendiri. Tanpa guard,
                # sukses TERLAMBAT dari percobaan lama menghapus jawaban
                # percobaan baru — persis yang sudah diperbaiki di sisi
                # viewer. Isi disk yang berbeda berarti milik percobaan
                # lain, bukan urusan thread ini.
                if answers_match_disk(exam.id, answers):
                    config.clear_answers(exam.id)
                else:
                    log.info(
                        "skip clear_answers di recovery: disk berisi jawaban "
                        "percobaan lain untuk exam %s (payload ini sudah durable)",
                        exam.id,
                    )
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
        # C1: kembalikan SELURUH UI koneksi dulu (tombol + input + flag),
        # bukan hanya tombolnya — thread recovery mematikannya saat mulai.
        self._enable_connect_ui()
        # Sama seperti jalur submit biasa: layar penuh berisi pesan guru,
        # identitas, dan link hasil -- bukan message box. Di recovery ini
        # siswa mungkin sudah keluar dari ruang ujian jadi menampilkan
        # link hasil jauh lebih berguna daripada "Pesan Berhasil".
        #
        # Halaman non-modal tersendiri, bukan dialog modal di atas dialog
        # konfigurasi. Referensinya ditahan di `self._congrats_ref` supaya
        # tidak ter-GC saat slot ini kembali, dan WA_DeleteOnClose membuat
        # Qt menghapusnya ketika siswa menekan "Selesai".
        from .congratulations import CongratulationsWindow

        # Identitas yang DIPAKAI worker recovery (audit HIGH H13) — bukan
        # apa pun yang kebetulan ada di config saat halaman ini tampil.
        identity = self._recovery_identity or config.get_identity_data()
        # H2: identitas via pemetaan kanonik yang sama dengan submit dan
        # approval — bukan lookup mentah "nama"/"nomor_ujian".
        std = map_identity_to_standard(identity)
        # Token yang tervalidasi, BUKAN `config.get("exam_token")`: config
        # masih menyimpan token dari percobaan sebelumnya kalau siswa
        # mengetik ulang token di kotak yang sama (recovery dibaca ulang
        # pada connect berikutnya), dan link yang ditampilkan harus milik
        # ujian yang benar-benar dikerjakan — bukan milik token basi.
        exam_token = self.validated_token
        congrats = CongratulationsWindow(
            server_url=self._server_url,
            exam_token=exam_token,
            exam_name=getattr(self._exam, "name", ""),
            student_name=str(std.get("student_name", "")),
            student_number=str(std.get("exam_number", "")),
            student_class=str(std.get("student_class", "")),
            congrats_message=msg,
            public_results=bool(getattr(self._exam, "public_results", True)),
        )
        congrats.setAttribute(Qt.WA_DeleteOnClose)
        congrats.show_fullscreen()
        self._congrats_ref = congrats

        # H2: halaman ini menaruh token ujian — di mode static-token itu
        # kredensial hasil SELURUH KELAS — SETELAH `deactivate()` sudah
        # melepas WDA_MONITOR, keyboard hook, ClipCursor, dan sweeper
        # clipboard. Tanpa baris ini, jalur recovery (justru yang dipakai
        # saat auto-submit background gagal) adalah satu-satunya halaman
        # hasil yang bisa difoto bebas. Helper modul-level dipakai karena
        # di titik ini tidak ada enforcer yang hidup: `_active`-nya sudah
        # False, jadi `protect_window` versi enforcer akan jadi no-op.
        from ..security.enforcer import protect_window_capture

        if not protect_window_capture(congrats):
            log.warning(
                "proteksi capture halaman hasil recovery gagal — token "
                "ujian bisa terekam lewat PrintScreen"
            )

        # M-token-leak: kotak token dikosongkan begitu ujian benar-benar
        # selesai. Pada jalur ini tidak pernah ada ExamViewerWindow yang
        # dibuat, jadi pembersihan `__main__` (`_on_viewer_closed`) tidak
        # pernah jalan — dan kotak yang sudah ter-prefill adalah satu
        # klik (atau satu Enter) dari memakai token kelas lagi di PC lab
        # yang dipakai bersama. `config["exam_token"]` SENGAJA dibiarkan:
        # token itu kunci XOR decode jawaban tersimpan (lihat
        # `_recovery_submit_thread`), dan `remember_url` sudah mengatur
        # apakah token boleh di-prefill pada kunjungan berikutnya.
        try:
            self.input_token.clear()
        except Exception:
            log.debug("could not clear token input", exc_info=True)

    def _offer_pending_recovery(self, identity: Dict[str, str]) -> bool:
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

        Yang tersisa di sini BUKAN pembatasan, tapi pemulihan data — dan
        pemulihan itu ber-scoped per SISWA, bukan per ujian (audit 2 Okt
        2026, HIGH H14): sidecar owner (config.save_answers_owner) mencatat
        siapa yang menulis jawaban tersimpan. Hanya identitas yang SAMA
        yang ditawarkan pengiriman ulang:

        * siswa A re-entry dan mengetik ulang identitasnya → kunci cocok
          → recovery ditawarkan (jalur utama fitur ini, tetap utuh);
        * siswa B masuk di PC yang sama sebelum A sempat kembali → kunci
          berbeda → jawaban A TIDAK ditawarkan, apalagi dikirim atas nama
          B. Berkasnya dibiarkan di disk supaya A masih bisa memulihkannya
          sendiri; pelanggarannya hanya dilog.
        * owner tidak dikenal (jawaban ditulis versi app lama tanpa
          sidecar) → fail-open: tawarkan seperti perilaku lama. Menolak
          recovery yang sah hanya karena upgrade app lebih buruk daripada
          risiko salah kirim satu kali di masa transisi.
        """
        assert self._exam is not None
        if config.load_answers(self._exam.id):
            owner = config.load_answers_owner(self._exam.id)
            if owner is not None:
                owner_key = str(owner.get("student_key", "") or "")
                current_key = build_student_key(
                    identity, self.validated_token
                )
                if owner_key and owner_key != current_key:
                    # Jawaban milik siswa lain. Tidak ditawarkan, tidak
                    # dikirim, tidak dihapus — dan labelnya TIDAK dibocorkan
                    # ke pengetik sekarang (bukan urusannya siapa pemiliknya).
                    log.warning(
                        "recovery untuk exam %s milik percobaan lain "
                        "(owner key != current key); tidak ditawarkan",
                        self._exam.id,
                    )
                    return True
            self._sig_recovery_available.emit(self._exam)
            return False
        return True

    @pyqtSlot()
    def _show_identity_dialog(self) -> None:
        """Show identity dialog after successful token lookup."""
        from .identity_dialog import IdentityDialog

        assert self._exam is not None
        # M-6: cek monitor ganda strict LEBIH AWAL di sini — sebelum dialog
        # identitas, sebelum approval membakar waktu pengawas. Backstop di
        # `__main__.on_exam_selected` tetap ada untuk jalur lain.
        if self._exam.is_strict and not self._strict_monitor_ok():
            return
        # H8 anti-prefill-silangan: identitas tersimpan hanya dipakai bila
        # konteksnya (ujian + token) sama dengan yang sedang dibuka.
        # Tanpa ini, identitas siswa A (dari PC/re-entry sebelumnya)
        # mengisi form siswa B dan Enter saja cukup menjawab atas namanya.
        saved_identity = self._prefill_identity_if_same_exam()
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
            # Ronde 6 (item 3): identitas DAN konteksnya disimpan sebagai
            # SATU kunci lewat satu `_save()` (config.set_identity_session).
            # Dua `config.set()` terpisah berarti dua penulisan penuh file,
            # dan crash di antaranya menyisakan identitas siswa A dengan
            # konteks yang menunjuk ujian lain — persis kondisi yang harus
            # mustahil, karena konteks itulah penjaga anti-prefill-silangan
            # (H8).
            #
            # Identitas disimpan DULU, sebelum recovery dicek.
            #
            # `_sig_recovery_available.connect(self._show_recovery)` tanpa
            # `Qt.QueuedConnection`, jadi `_offer_pending_recovery()`
            # memanggil `_show_recovery()` secara SYNCHRONOUS -- dan di
            # situulah thread `_recovery_submit_thread` dijalankan. Thread
            # itu membaca identitas dari `config.get_identity_data()`.
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
            try:
                token_now = self.validated_token
            except Exception:
                token_now = ""
            # H8: ikat identitas tersimpan ke ujian + token ini supaya
            # prefill berikutnya tidak dipakai siswa/ujian lain.
            config.set_identity_session(identity, {
                "exam_id": self._exam.id,
                "token": str(token_now or "").strip().upper(),
            })

            if not self._offer_pending_recovery(identity):
                # UJIAN TIDAK DIMULAI. Identitas harus dibersihkan.
                #
                # `exam_selected` tidak pernah emit, jadi tidak ada viewer
                # yang dibuat dan tidak ada `closed` signal -- artinya
                # `config.clear_identity()` yang ada di `__main__` TIDAK
                # terjangkau dari sini. Tanpa baris di bawah, identitas
                # siswa ini tinggal di config dan dipakai mengisi form
                # siswa berikutnya; `IdentityDialog` menutup diri lewat
                # `last_input.returnPressed -> _on_submit`, jadi Enter
                # saja sudah cukup menjawab atas nama orang lain.
                #
                # Ini kebocoran yang sama yang ditutup untuk jalur
                # pembatalan layar persetujuan; jalur recovery membuka
                # hole yang sama dari arah lain.
                #
                # `clear_identity()` sekarang membersihkan identitas DAN
                # konteksnya sekaligus, jadi tidak ada lagi `set
                # ("identity_context", {})` terpisah di sini.
                config.clear_identity()
                dlg.deleteLater()
                return
            # UI koneksi dikembalikan SEBELUM emit: __main__.on_exam_selected
            # menjalankan dialog persetujuan SYNCHRONOUS — bila ia kembali
            # (batal), dialog ini muncul lagi dan tombolnya harus sudah hidup.
            # JANGAN accept() di sini: on_exam_selected mengelola visibilitas
            # sendiri (hide saat mulai, show saat batal); accept() menutup
            # dialog yang baru saja ditampilkan lagi (zombie tanpa jendela).
            #
            # Ronde 6 (item 5): baru setelah recovery (yang lebih mendesak)
            # menawarkan pilihan eksplisit soal marker "sudah terkumpul".
            if not self._offer_resubmit_choice(identity):
                dlg.deleteLater()
                return
            self._enable_connect_ui()
            self.exam_selected.emit(self._exam, self._server_url, identity)
        else:
            self._enable_connect_ui()
            self.lbl_status.setText("")
        # L2: dialog identitas dibuang setelah dipakai — widget + inputnya
        # (nama siswa) tidak boleh tertinggal di memori proses untuk sesi
        # berikutnya.
        try:
            dlg.deleteLater()
        except Exception:
            log.debug("deleteLater IdentityDialog gagal", exc_info=True)

    def _offer_resubmit_choice(self, identity: Dict[str, str]) -> bool:
        """Tawarkan pilihan eksplisit bila identitas ini SUDAH pernah submit.

        Ronde 6 (item 5). `config.is_submitted` adalah penanda "percobaan ini
        sudah mengumpulkan jawaban di mesin ini" (`mark_submitted` di
        `_cleanup_after_submit`). ENTITY-nya punya penulis tapi tidak punya
        pembaca produksi: `ui/exam_viewer.py` memberi tahu pembaca bahwa
        "`ServerConfigDialog.is_submitted`" memblokir re-entry, dan itu tidak
        pernah terjadi. Gate yang sebenarnya ada di server
        (`repeat_required`).

        Yang diubah di sini BUKAN policy. Policy tetap: TIDAK ADA yang
        diblokir di client — `_offer_pending_recovery` tetap `return True`
        tanpa syarat, dan ini juga tidak menolak. Yang ditambahkan adalah
        INFORMASI: siswa yang kembali dengan identitas yang sama diberi tahu
        jawabannya sudah tercatat, lalu memilih sendiri.

        Kenapa hanya marker, bukan "ada jawaban di disk": file jawaban adalah
        fallback yang dipakai identitas LAIN bila autosave/autosubmit-nya
        gagal, dan pengaman kepemiliknya sudah ada di
        `_offer_pending_recovery`. Menoffer "kirim ulang" berdasarkan file
        akan membuka overwrite terhadap baris yang sudah benar.

        Battasi ke identitas + ujian yang sama: `is_submitted` sudah ber-key
        `build_student_key`, jadi marker siswa lain di PC yang sama tidak
        tersentuh. Kegagalan membaca marker TIDAK boleh menutup jalan: bersihkan
        identitas dan membiarkan siswa masuk — client bukan tempat aturan
        yang tidak bisa diaudit.
        """
        assert self._exam is not None
        try:
            attempt = build_student_key(identity, self.validated_token)
            if not config.is_submitted(self._exam.id, attempt):
                return True
        except Exception:
            log.warning("gagal membaca marker submit", exc_info=True)
            return True
        # `question()` static hanya menyediakan label "Yes"/"No", yang
        # mengikuti locale sistem. Dua label eksplisit
        # ("Kerjakan Ulang" / "Kembali") lebih jujur untuk keputusan
        # yang membuang pekerjaan ujian yang sudah dikerjakan.
        box = QMessageBox(self)
        box.setIcon(QMessageBox.Question)
        box.setWindowTitle("Jawaban Sudah Terkumpul")
        box.setText(
            "Jawaban untuk identitas ini sudah tercatat di perangkat ini.\n\n"
            "Masuk lagi berarti mengerjakan ujian dari awal — jawaban yang "
            "sudah terkirim tetap ada di server.\n\n"
            "Kerjakan ulang sekarang?"
        )
        box.addButton("Kerjakan Ulang", QMessageBox.YesRole)
        back_btn = box.addButton("Kembali", QMessageBox.NoRole)
        # Default = "Kembali": satu Enter yang tidak sengaja — persis
        # yang `_on_submit` di dialog identitas lakukan untuk menutup
        # form — tidak boleh ikut memulai percobaan baru.
        box.setDefaultButton(back_btn)
        # `exec_()` mengembalikan StandardButton yang diklik (documented
        # untuk QMessageBox), jadi keputusan tidak bergantung pada
        # `clickedButton()`.
        reply = box.exec_()
        if reply == QMessageBox.Yes:
            return True
        log.info(
            "siswa memilih kembali: marker submit untuk exam %s, identitas %s",
            self._exam.id, attempt,
        )
        # UJIAN TIDAK DIMULAI. Identitas yang baru diketik tidak boleh
        # tertinggal: tidak ada viewer yang dibuat, jadi tidak ada sinyal
        # `closed` yang memanggil `clear_identity()`.
        config.clear_identity()
        self._enable_connect_ui()
        return False

    def _prefill_identity_if_same_exam(self) -> Dict[str, str]:
        """Identitas tersimpan HANYA bila konteksnya cocok (H8).

        `identity_session` (lihat `config.set_identity_session`) menyimpan
        identitas DAN konteksnya (exam_id + token saat itu) sebagai satu
        pasangan, jadi keduanya tidak bisa terpisah oleh crash di tengah
        penulisan. Konteks beda → kembalikan {} supaya form kosong untuk
        siswa/ujian berikutnya.

        `config.get_identity_session()` sudah melemahkan pasangan: bentuk
        yang tidak lengkap terbaca sebagai tidak ada, jadi tidak mungkin
        ada identitas tanpa konteks yang lolos ke form.
        """
        try:
            token_now = self.validated_token
        except Exception:
            token_now = ""
        token_now = str(token_now or "").strip().upper()
        try:
            session = config.get_identity_session()
        except Exception:
            return {}
        ctx = session["context"]
        stored = session["identity_data"]
        if (
            ctx.get("exam_id") == self._exam.id
            and str(ctx.get("token") or "").strip().upper() == token_now
            and token_now
        ):
            return dict(stored) if isinstance(stored, dict) else {}
        return {}

    def _strict_monitor_ok(self) -> bool:
        """Gate monitor-ganda strict lebih awal (M-6, fail-closed).

        True → boleh lanjut. False → pesan sudah ditampilkan ke siswa dan
        UI koneksi sudah dikembalikan; pemanggil cukup `return`.
        """
        try:
            from ..security import get_backend

            multi = bool(get_backend().has_multiple_monitors())
            detect_ok = True
        except Exception:
            log.warning("deteksi multi-monitor gagal (strict) — tolak",
                        exc_info=True)
            multi = False
            detect_ok = False
        if not detect_ok:
            # Fail-closed: detector exception berarti kita TIDAK TAHU
            # berapa layar terpasang — menolak lebih aman daripada melepas
            # ujian strict tanpa pengawasan.
            QMessageBox.warning(
                self,
                "Tidak Dapat Memeriksa Layar",
                "Ujian ini berjalan dalam mode ketat dan aplikasi tidak "
                "dapat memastikan hanya satu layar yang terpasang.\n\n"
                "Pastikan hanya satu layar terhubung lalu coba lagi, "
                "atau hubungi pengawas.",
            )
            self._enable_connect_ui()
            self.lbl_status.setText("Pemeriksaan layar gagal — coba lagi.")
            return False
        if multi:
            QMessageBox.warning(
                self,
                "Monitor Ganda Terdeteksi",
                "Ujian ini berjalan dalam mode ketat dan hanya boleh "
                "menggunakan satu layar.\n\nLepaskan monitor kedua "
                "lalu coba lagi.",
            )
            self._enable_connect_ui()
            self.lbl_status.setText(
                "Lepaskan monitor kedua lalu coba lagi.")
            return False
        return True
