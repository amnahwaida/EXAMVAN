"""Main exam viewer window — PDF + answer sheet + timer + submit."""

from __future__ import annotations

import logging
import os
import sys
import tempfile
import threading
from typing import Any, Dict, List, Optional

# Admin exit password — Wajib ada untuk fitur admin exit aktif.
#
# Urutan sumber (pertama yang ada menang):
#   1. env EXAMVAN_ADMIN_PASSWORD — cara lama, tetap didukung
#   2. %LOCALAPPDATA%\EXAMVAN\admin_password.txt — ditulis installer
#      EXAMVAN-Setup.exe, supaya "1 klik langsung jalan" benar-benar
#      tanpa langkah set env var manual.
#   3. tidak ada → fitur admin exit NONAKTIF (fail-closed, TIDAK ada
#      fallback password apa pun).
#
# File di #2 dibaca dengan stripping newline (SaveStringToFile/Edit
# menulis persis apa yang diketik user, tanpa newline — tapi file yang
# diedit manual di Notepad bisa punya CRLF, jadi tetap di-strip).
def _load_admin_password() -> Optional[str]:
    pw = os.environ.get("EXAMVAN_ADMIN_PASSWORD")
    if pw:
        return pw

    # Hanya Windows yang punya installer EXAMVAN-Setup.exe; di Linux
    # password tetap via env var saja (LokalAppData tidak ada).
    local_appdata = os.environ.get("LOCALAPPDATA")
    if not local_appdata:
        return None
    try:
        with open(
            os.path.join(local_appdata, "EXAMVAN", "admin_password.txt"),
            "r",
            encoding="utf-8",
        ) as f:
            pw = f.read().strip()
            return pw or None
    except OSError:
        return None


_ADMIN_PASSWORD = _load_admin_password()

from PyQt5.QtCore import QEvent, Qt, QTimer, pyqtSignal, pyqtSlot
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

from .. import APP_VERSION, api, config, notify
from ..models import Exam
from ..security.enforcer import SecurityEnforcer
from ..utils import get_device_label, map_identity_to_standard
from ..ws import ExamWebSocket
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
        # Re-entrancy guard for _enforce_fullscreen, which is driven by a
        # window-state change that showFullScreen() itself produces.
        self._fullscreen_reasserting = False
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
        # Persist start_time sejak awal (mirror Android loadExamContent) —
        # dipakai recovery re-entry saat mengirim ulang jawaban dari disk.
        config.save_start_time(self._exam.id, self._timer_widget.get_start_time_iso())
        self._init_security()  # Activate security BEFORE PDF loads
        self._load_pdf_async()

        # Presence (login/heartbeat/logout) — mirror Android WebSocketManager.
        # Heartbeat HTTP berjalan selama sesi (interval 60 dtk) supaya siswa
        # tampil ONLINE di dashboard monitoring pengawas; login dikirim sekali
        # di awal, logout/complete saat ujian selesai.
        self._std_identity = map_identity_to_standard(identity_data)
        self._device_label = get_device_label()
        self._presence_active = True
        self._send_access_log("login")
        self._heartbeat_timer = QTimer(self)
        self._heartbeat_timer.setInterval(60000)
        self._heartbeat_timer.timeout.connect(lambda: self._send_access_log("heartbeat"))
        self._heartbeat_timer.start()

        # WebSocket real-time (mirror Android WebSocketManager): menerima
        # event pengawas, terutama `exam_terminated` → auto-submit segera
        # (ujian dihentikan pengawas). Receive-only; presence via HTTP di atas.
        self._ws = ExamWebSocket(self)
        self._ws.event_received.connect(self._on_ws_event)
        self._ws.connect(self._server_url, self._exam.id, self._token)

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

        # Security banner. display_level is the canonical tier, so this can
        # never raise (the raw server string used to be `.upper()`d here,
        # which crashed the whole window on `"security_level": null`) and
        # never renders a locked-down exam in the low-tier colour.
        mode = self._exam.display_level
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
                # Same device identity used at request-approval time — the
                # server approval gate requires an approved exam_approvals row
                # for this device (14 Agustus 2026). Identitas HARUS sama
                # persis dengan approval (WaitingApprovalDialog) dan submit
                # (mac_address): DESKTOP:<hash>. Sebelumnya di sini dipakai
                # get_mac_address() (MAC mentah) → gate PDF tidak match baris
                # approval → unduhan ditolak.
                device_id=get_device_label(),
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
        # Sticky marker (F2, mirror Android): ujian ini sudah SELESAI di
        # perangkat ini. Re-entry berikutnya diblokir oleh gate join
        # (ServerConfigDialog.is_submitted) — mencegah re-entry dalam window
        # grace server mengirim submit kosong yang MENIMPA jawaban asli.
        config.mark_submitted(self._exam.id)
        # Presence: hapus heartbeat Redis (siswa tampil OFFLINE segera).
        self._stop_presence(completed=True)
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
        # closeEvent already handles self.closed.emit() when _submitted
        self.close()

    # -------------------------------------------------------------------
    # WebSocket events
    # -------------------------------------------------------------------

    def _on_ws_event(self, event: str, data: dict) -> None:
        if event == "exam_terminated":
            log.warning("exam_terminated received — auto-submitting")
            self._sig_status.emit("Ujian dihentikan pengawas. Mengumpulkan jawaban...")
            self._auto_submit()
        # student_update / notification: diagnostik, tidak perlu aksi di desktop.

    # -------------------------------------------------------------------
    # Presence (access-log)
    # -------------------------------------------------------------------

    def _send_access_log(self, event: str) -> None:
        """Kirim event presence (login/heartbeat/logout) — best-effort."""
        if not self._presence_active:
            return
        try:
            threading.Thread(
                target=api.send_access_log,
                args=(
                    self._server_url,
                    self._exam.id,
                    self._token,
                    self._device_label,
                    event,
                ),
                kwargs={
                    "student_name": self._std_identity.get("student_name", ""),
                    "exam_number": self._std_identity.get("exam_number", ""),
                    "student_class": self._std_identity.get("student_class", ""),
                    "device_info": f"EXAMVAN-Desktop/{APP_VERSION} {sys.platform}",
                    "identity_data": self._identity_data,
                },
                daemon=True,
            ).start()
        except Exception:
            pass

    def _stop_presence(self, completed: bool = False) -> None:
        """Hentikan heartbeat & laporkan kepergian siswa (best-effort).

        `completed=True` → POST /complete (hapus presence Redis, tampil
        offline segera); selain itu kirim logout (presence habis via TTL).
        """
        self._presence_active = False
        try:
            self._heartbeat_timer.stop()
        except Exception:
            pass
        try:
            self._ws.disconnect()
        except Exception:
            pass
        try:
            threading.Thread(
                target=api.complete_exam if completed else api.send_access_log,
                args=(
                    self._server_url,
                    self._exam.id,
                    self._token,
                    self._device_label,
                ) if completed else (
                    self._server_url,
                    self._exam.id,
                    self._token,
                    self._device_label,
                    "logout",
                ),
                kwargs={
                    "student_name": self._std_identity.get("student_name", ""),
                    "exam_number": self._std_identity.get("exam_number", ""),
                    "student_class": self._std_identity.get("student_class", ""),
                } if not completed else {},
                daemon=True,
            ).start()
        except Exception:
            pass

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
        # Guard init-race: time_up bisa menembak DI KONSTRUKTOR timer saat
        # deadline sudah lewat (window baru dibuka) — di _setup_ui, timer
        # dibuat sebelum _answer_sheet, jadi submit akan crash. Tunda
        # sampai _setup_ui selesai (singleShot; time_up hanya menembak sekali,
        # jadi tidak ada pengulangan tak terbatas).
        if not hasattr(self, '_answer_sheet'):
            QTimer.singleShot(100, self._auto_submit)
            return
        self._on_status("Auto-submit: jawaban dikumpulkan otomatis...")
        self._auto_submit_and_exit()

    def _auto_submit_and_exit(self) -> None:
        """Mirror Android autoSubmitAndExit: tutup window SEGERA, submit di background.

        Alur lama menahan window tetap terbuka (dan di strict mode tetap
        terkunci) sampai hasil jaringan tiba — jaringan mati → siswa terjebak
        di layar terkunci tanpa jalan keluar. Alur baru (parity Android):
          1. gate submit tunggal (lock);
          2. marker sticky "sudah selesai" + flush jawaban TERBARU ke disk
             (proses mati / gagal jaringan → recovery re-entry);
          3. lepas kunci + tutup window SEGERA (jangan menunggu jaringan);
          4. submit + polling /result di background;
          5. sukses → clear jawaban + complete presence + notifikasi;
             gagal  → jawaban tetap di disk → layar recovery "Kirim Lagi"
             saat re-entry (ServerConfigDialog).
        """
        with self._submit_lock:
            if self._submitted or self._submitting:
                return
            self._submitted = True
            self._submitting = True

        # 1. Sticky marker + flush jawaban terkini ke disk SEBELUM window
        #    mati. Flush memakai resolve_submit_answers (F1): memori kosong
        #    (deadline menembak sebelum restore dari disk pada re-entry)
        #    → JAWABAN DISK TIDAK BOLEH ditimpa {}. Kalau memori tidak kosong
        #    → dipakai apa adanya (siswa mungkin baru mengubah jawaban
        #    setelah auto-save terakhir).
        config.mark_submitted(self._exam.id)
        memory = (
            self._answer_sheet.get_answers()
            if hasattr(self, '_answer_sheet') else {}
        )
        answers = config.resolve_submit_answers(memory, self._exam.id)
        config.save_answers(self._exam.id, answers)

        # 2. Presence: logout segera (TTL Redis); `complete` saat submit sukses.
        self._stop_presence(completed=False)

        # 3. Lepas kunci & tutup window SEGERA.
        self._timer_widget.stop()
        self._btn_submit.setEnabled(False)
        self._btn_submit.setText("Mengumpulkan...")
        self._pdf_viewer.cleanup()
        if self._pdf_path:
            try:
                if os.path.exists(self._pdf_path):
                    os.remove(self._pdf_path)
            except OSError:
                pass
        if self._security:
            self._security.deactivate()
        self.close()

        # 4. Submit di background — thread MURNI: setelah window ditutup
        #    semua nilai di-capture sebagai argumen biasa, thread TIDAK
        #    menyentuh Qt (window bisa di-GC oleh _on_viewer_closed).
        std = map_identity_to_standard(self._identity_data)
        threading.Thread(
            target=self._background_submit_thread,
            args=(
                self._server_url,
                self._exam.id,
                self._token,
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

    def _background_submit_thread(
        self, base_url, exam_id, token, name, number, sclass,
        answers, start_time, mac, identity,
    ) -> None:
        """Submit background setelah window ditutup. TIDAK menyentuh Qt.

        Sukses hanya setelah durable (sync ATAU worker mengonfirmasi via
        polling /result untuk jalur 202 queued) — baru jawaban lokal di-clear.
        Gagal → jawaban tetap di disk; re-entry menawarkan "Kirim Lagi"
        (server idempoten, retry tidak menduplikasi baris).
        """
        try:
            resp = api.submit_with_retry(
                base_url, exam_id, name, number, sclass,
                answers, start_time, mac, identity,
            )
            if resp.status == "queued" and resp.job_id:
                resp = api.poll_queued_result(
                    base_url, exam_id, token, mac, resp.job_id, identity,
                )
            if resp.success:
                config.clear_answers(exam_id)
                try:
                    api.complete_exam(base_url, exam_id, token, mac)
                except Exception:
                    pass
                msg = resp.congrats_message or resp.message or "Jawaban berhasil dikumpulkan."
                notify.send_notification("EXAMVAN — Ujian Terkumpul", msg)
            else:
                notify.send_notification(
                    "EXAMVAN — Pengumpulan Gagal",
                    "Jawaban belum terkirim: " + (resp.message or "terjadi kesalahan") +
                    "\nBuka aplikasi dan pilih ujian ini untuk mengirim ulang.",
                    urgency="critical",
                )
        except Exception as e:
            notify.send_notification(
                "EXAMVAN — Pengumpulan Gagal",
                "Jawaban belum terkirim: " + str(e) +
                "\nBuka aplikasi dan pilih ujian ini untuk mengirim ulang.",
                urgency="critical",
            )

    def _do_submit(self) -> None:
        """Thread-safe submit gate. Only one submit runs at a time."""
        with self._submit_lock:
            if self._submitted or self._submitting:
                return
            self._submitting = True

        self._btn_submit.setEnabled(False)

        # F1 fallback (mirror fix Android): saat deadline sudah lewat pada
        # re-entry (proses mati), timer time_up menembak SEBELUM restore
        # jawaban dari disk (QTimer.singleShot 500ms) → memori kosong → tanpa
        # fallback ini submit KOSONG menimpa jawaban asli dalam window grace
        # server (end_time + 60 dtk) dan clear_answers menghapusnya. Memori
        # tidak kosong → dipakai apa adanya (siswa mungkin baru mengubah
        # jawaban setelah auto-save terakhir).
        answers = config.resolve_submit_answers(
            self._answer_sheet.get_answers(), self._exam.id
        )

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

        # Async path (202): server hanya MENGANTRI jawaban — belum durable.
        # Poll /result sampai worker mengonfirmasi ("done") sebelum menganggap
        # sukses; copy lokal TIDAK boleh di-clear pada 202 mentah (mirror
        # Android: clearSavedAnswers hanya setelah konfirmasi durable). Jika
        # worker gagal → resp.success=False → _on_submit_result menampilkan
        # error dan TIDAK menghapus jawaban (re-entry memulihkan dari disk).
        if resp.status == "queued" and resp.job_id:
            self._sig_status.emit("Jawaban diterima server, menunggu konfirmasi...")
            resp = api.poll_queued_result(
                base_url,
                exam_id,
                self._token,
                mac,
                resp.job_id,
                identity,
            )

        # Fix #2 (parity Android): tampilkan `congrats_message` custom guru
        # saat sukses (bukan hanya resp.message bawaan server).
        if resp.success:
            message = resp.congrats_message or resp.message or "Jawaban berhasil dikumpulkan."
        else:
            message = resp.message
        self._sig_submit_result.emit(resp.success, message)

    # -------------------------------------------------------------------
    # Window events
    # -------------------------------------------------------------------

    @property
    def is_strict(self) -> bool:
        """True when this exam runs with the strictest lockdown.

        Read by __main__.main() to decide between showFullScreen() and
        showMaximized() when presenting the window, and by _enforce_fullscreen.
        """
        return self._exam.is_strict

    def _enforce_fullscreen(self) -> None:
        """Re-assert fullscreen if the window state leaked away from it.

        The enforcer sets fullscreen once, at activation. That is not
        enough on its own: any later showMaximized(), restore or resize
        silently drops a strict exam out of fullscreen, and the caller that
        does it does not know the exam is strict. Rather than trusting every
        presentational call site, the window defends itself whenever its own
        state changes while the exam is strict.
        """
        if not self._exam.is_strict or self._fullscreen_reasserting:
            return
        if self.isFullScreen():
            return
        self._fullscreen_reasserting = True
        try:
            self.showFullScreen()
        except Exception:
            log.warning("could not re-assert fullscreen", exc_info=True)
        finally:
            self._fullscreen_reasserting = False

    def changeEvent(self, event) -> None:
        """Hitung ulang deadline saat window aktif kembali (mirror onResume).

        Fix #3: `time.monotonic()` (CLOCK_MONOTONIC) TIDAK termasuk waktu
        suspend — laptop ditutup 30 menit → countdown membeku 30 menit →
        tampilan "sisa waktu" menyesatkan setelah resume. `refresh_deadline`
        menghitung ulang _end_mono dari end_time ABSOLUT + skew server saat
        window di-restore / diaktifkan kembali.
        """
        try:
            if event.type() == QEvent.WindowStateChange:
                if not self.isMinimized():
                    self._timer_widget.refresh_deadline()
                    self._enforce_fullscreen()
            elif event.type() == QEvent.ActivationChange:
                if self.isActiveWindow():
                    self._timer_widget.refresh_deadline()
        except Exception:
            # refresh_deadline aman gagal (deadline tetap dihitung ulang pada
            # tick berikutnya / server tetap otoritas end_time).
            pass
        super().changeEvent(event)

    def closeEvent(self, event: QCloseEvent) -> None:
        # Already submitted — always allow close, bypass all guards
        with self._submit_lock:
            already_done = self._submitted
        if already_done:
            if self._security:
                self._security.deactivate()
            self._pdf_viewer.cleanup()
            self.closed.emit()
            event.accept()
            return

        # Guard against repeated closeEvent spam (re-entrant calls from
        # QMessageBox or self.close() during cleanup)
        if self._close_in_progress:
            event.ignore()
            return
        self._close_in_progress = True

        # Medium and strict: a close attempt is an auto-submit, never an
        # exit. blocks_free_exit reads the canonical level, so the server's
        # "high" is covered here — previously this compared the raw string
        # against ("medium",) and a "Tinggi" exam fell through to the low
        # branch below, where a "Yes" answer closed the exam unsubmitted.
        if self._exam.blocks_free_exit:
            # Auto-submit on close attempt
            # _do_submit() handles its own _submitting guard under lock,
            # so we don't set it here.
            if self._submitted or self._submitting:
                self._close_in_progress = False
                event.ignore()
                return
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
            # Presence: siswa keluar tanpa submit — kirim logout (presence
            # habis via TTL Redis; tidak ada submit yang menghapusnya).
            self._stop_presence(completed=False)
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
            alt_held = bool(event.modifiers() & Qt.AltModifier)
            blocked = {
                Qt.Key_Tab: alt_held,              # Alt+Tab
                Qt.Key_F2: alt_held,               # Alt+F2 (Run Command)
                Qt.Key_F4: alt_held,               # Alt+F4 (Close)
                Qt.Key_F7: alt_held,               # Alt+F7 (Move)
                Qt.Key_F8: alt_held,               # Alt+F8 (Resize)
                Qt.Key_Space: alt_held,            # Alt+Space (Window menu)
                Qt.Key_Escape: True,                # Alone Escape
                Qt.Key_Super_L: True,               # Left Win
                Qt.Key_Super_R: True,               # Right Win
                Qt.Key_Menu: True,                  # Context menu
            }
            if blocked.get(event.key(), False):
                event.ignore()
                return

            if event.key() == Qt.Key_Print:
                # Go through the enforcer, not utils.clear_clipboard(): the
                # enforcer clears the Qt side here on the GUI thread and
                # hands the platform side to its worker. utils did the
                # platform part inline, which on Linux forks xsel/xclip
                # while the keystroke handler is still running.
                self._security.clear_clipboard_now()
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
            # Fail-closed: tanpa password terkonfigurasi, TIDAK ada
            # jalur keluar selain kill process dari Task Manager.
            if _ADMIN_PASSWORD is None:
                QMessageBox.warning(
                    self, "Tidak Diizinkan",
                    "Admin exit tidak dikonfigurasi.\n\n"
                    "Cara mengaktifkan:\n"
                    "• Saat instalasi, centang \"Konfigurasi password admin exit\",\n"
                    "  atau\n"
                    "• Set environment EXAMVAN_ADMIN_PASSWORD sebelum aplikasi jalan.\n\n"
                    "Lokasi file: %LOCALAPPDATA%\\EXAMVAN\\admin_password.txt",
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
            # closeEvent handles self.closed.emit() when _submitted
            self.close()
