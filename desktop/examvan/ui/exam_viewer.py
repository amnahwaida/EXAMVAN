"""Main exam viewer window — PDF + answer sheet + timer + submit."""

from __future__ import annotations

import base64
import hashlib
import hmac
import logging
import os
import socket
import sys
import tempfile
import threading
from contextlib import contextmanager
from typing import Any, Dict, Iterator, List, Optional

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
# Password admin exit disimpan sebagai PBKDF2-HMAC-SHA256, bukan plaintext.
#
# Alasannya: di lab sekolah semua siswa sering memakai SATU akun
# Windows, dan `%LOCALAPPDATA%\EXAMVAN\admin_password.txt` ada di profil
# yang sama. Plaintext berarti password supervisor bisa dibaca lalu dipakai
# menutup ujian yang sedang berjalan. Installer tidak bisa
# menutup ini lewat ACL: pemilik file selalu dapat memberi akses pada
# dirinya sendiri, dan `PrivilegesRequired=lowest` membuat installer tidak
# punya hak admin untuk mengunci apa pun.
#
# Yang tersisa hanyalah menaikkan palang: file tidak lagi memuat
# passwordnya, dan hash-nya terikat mesin sehingga menyalinnya dari PC lain
# tidak langsung berhasil.
#
# Batasnya harus disebut terang-terang: pada akun yang dipakai bersama,
# siswa masih bisa menulis ulang file dengan hash pilihan sendiri. Tidak
# ada rahasia lokal yang aman dari pemilik akun. Penutup yang sebenarnya
# adalah akun Windows per-siswa (atau ACL admin-only di luar jangkauan
# installer ini), bukan hashing.
_ITERATIONS = 120_000
# Batas klaim iterations yang diterima dari berkas. File ini ada di
# profil akun yang bisa ditulis ulang siswa: klaim 20.000.000 terbukti
# memakan 10 detik per ketikan supervisor (repro nyata, review 30 Sep) —
# siswa menggantung pengawas ~8 menit di dialog keluar. Klaim di atas
# batas ditolak, bukan di-saturasi: kita tidak pernah menulis >120k,
# jadi apa pun di atasnya adalah berkas yang tidak kita buat.
_MAX_ADMIN_ITERATIONS = _ITERATIONS
_SALT_BYTES = 16
_HASH_PREFIX = "pbkdf2_sha256"

# Penanda berkas versi LAMA (plaintext).skrip installer yang sudah
# deployed menulis ini, dan upgrade tidak boleh mengunci supervisor di
# luar kelas saat ujian sedang berjalan.


def _machine_fingerprint() -> str:
    """ID MESIN stabil, dipakai mengikat hash ke satu PC saja.

    USERNAME sengaja TIDAK masuk: hash supervisor adalah milik PC, bukan
    milik nama akun. Lab sekolah sering me-rotate akun per semester
    (siswa2026 → siswa2027); dengan USERNAME di fingerprint, password
    supervisor resmi berhenti bekerja setelah rotasi — supervisor
    terkunci di luar ujian tanpa pesan. Komponen yang dipakai harus
    berubah HANYA ketika mesinnya berubah.
    """
    for key in ("COMPUTERNAME", "PROCESSOR_IDENTIFIER"):
        value = os.environ.get(key, "").strip()
        if value:
            return value
    # socket.gethostname() tidak pernah kosong di Windows dan identik
    # dengan COMPUTERNAME, jadi fallback ini menjaga determinisme mesin
    # tanpa pernah jatuh ke faktor per-pengguna.
    hostname = socket.gethostname().strip()
    if hostname:
        return hostname
    return "unknown-machine"


def _admin_password_path() -> Optional[str]:
    local_appdata = os.environ.get("LOCALAPPDATA")
    if not local_appdata:
        return None
    return os.path.join(local_appdata, "EXAMVAN", "admin_password.txt")


def _machine_salt() -> bytes:
    return hashlib.sha256(
        f"examvan-admin-exit::{_machine_fingerprint()}".encode("utf-8")
    ).digest()


def _derive(password: str, salt: bytes) -> bytes:
    return hashlib.pbkdf2_hmac(
        "sha256",
        password.encode("utf-8"),
        _machine_salt() + salt,
        _ITERATIONS,
    )


def _hash_admin_password(password: str) -> str:
    """`$pbkdf2_sha256$<iterations>$<salt-b64>$<hash-b64>`."""
    salt = os.urandom(_SALT_BYTES)
    digest = _derive(password, salt)
    return "$".join(
        (
            _HASH_PREFIX,
            str(_ITERATIONS),
            base64.b64encode(salt).decode("ascii"),
            base64.b64encode(digest).decode("ascii"),
        )
    )


def _verify_admin_password(stored: str, password: str) -> bool:
    """Verifikasi hash dari berkas. False untuk SEMUA bentuk tidak sah.

    Berkas ini bisa ditulis ulang/dirusak oleh pengguna akun (lihat
    komentar di atas), jadi isinya adalah input musuh: struktur salah,
    iterations bukan angka, base64 rusak, atau klaim iterations di atas
    cap — semuanya berarti 'tidak cocok', bukan exception. ValueError
    yang lepas dari sini menjatuhkan dialog admin exit persis saat
    supervisor membutuhkannya (repro nyata: review 30 Sep 2026).
    """
    if not stored or not stored.startswith(_HASH_PREFIX):
        return False
    try:
        parts = stored.split("$")
        if len(parts) != 4:
            return False
        prefix, iterations, salt_b64, digest_b64 = parts
        if prefix != _HASH_PREFIX:
            return False
        claimed = int(iterations)
        if not (1 <= claimed <= _MAX_ADMIN_ITERATIONS):
            return False
        salt = base64.b64decode(salt_b64, validate=True)
        expected = base64.b64decode(digest_b64, validate=True)
    except (ValueError, TypeError):
        # int('abc'), b64 korup, dst. — semua jalur korup = tolak.
        return False
    if not salt or not expected:
        return False
    candidate = hashlib.pbkdf2_hmac(
        "sha256",
        password.encode("utf-8"),
        _machine_salt() + salt,
        claimed,
    )
    return hmac.compare_digest(candidate, expected)


def _store_admin_password(password: str) -> bool:
    """Tulis password sebagai hash. Return True bila berhasil."""
    path = _admin_password_path()
    if not path or not password:
        return False
    try:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(_hash_admin_password(password))
        return True
    except OSError:
        log.warning("could not write admin password", exc_info=True)
        return False


def _load_admin_password() -> Optional[str]:
    """Password admin exit yang dipakai app, apa adanya bentuknya.

    Yang dikembalikan adalah ISI BERKAS -- hash untuk format baru, atau
    plaintext untuk berkas lama. Perbandingan di `_admin_exit_prompt`
    yang memutuskan, lewat `_password_matches()`.
    """
    pw = os.environ.get("EXAMVAN_ADMIN_PASSWORD")
    if pw:
        return pw

    # Hanya Windows yang punya installer EXAMVAN-Setup.exe; di Linux
    # password tetap via env var saja (LokalAppData tidak ada).
    local_appdata = os.environ.get("LOCALAPPDATA")
    if not local_appdata:
        return None
    _pw_path = os.path.join(local_appdata, "EXAMVAN", "admin_password.txt")
    try:
        with open(_pw_path, "r", encoding="utf-8") as f:
            pw = f.read().strip()
    except (OSError, ValueError):
        # Berkas non-UTF8 (ANSI code page dari Notepad lama): coba encoding
        # lokal sebelum menyerah. UnicodeDecodeError adalah ValueError.
        try:
            import locale
            with open(
                _pw_path, "r",
                encoding=locale.getpreferredencoding(False) or "utf-8",
                errors="strict",
            ) as f:
                pw = f.read().strip()
        except (OSError, ValueError):
            return None
    if not pw:
        return None
    # Migrasi plaintext -> hash: berkas lama berisi password apa adanya;
    # tulis ulang sebagai hash best-effort (tidak pernah raise).
    if not pw.startswith(_HASH_PREFIX):
        try:
            _store_admin_password(pw)
        except Exception:
            log.warning("migrasi admin password ke hash gagal", exc_info=True)
    return pw


def _password_matches(stored: Optional[str], typed: str) -> bool:
    """Bandingkan input supervisor dengan yang tersimpan.

    Dua bentuk didukung: hash baru (diverifikasi) dan plaintext lama
    (dibandingkan langsung, demi kompatibilitas dengan instalasi yang
    sudah ada).
    """
    if not stored:
        return False
    if stored.startswith(_HASH_PREFIX):
        return _verify_admin_password(stored, typed)
    return hmac.compare_digest(stored, typed)


_ADMIN_PASSWORD = _load_admin_password()


from PyQt5.QtCore import QEvent, Qt, QTimer, pyqtSignal, pyqtSlot
from PyQt5.QtGui import QCloseEvent, QKeyEvent
from PyQt5.QtWidgets import (
    QApplication,
    QHBoxLayout,
    QLabel,
    QMainWindow,
    QMessageBox,
    QProgressBar,
    QPushButton,
    QSplitter,
    QVBoxLayout,
    QWidget,
)

from .. import APP_VERSION, api, config, notify
from ..models import Exam, SubmitResponse
from ..security.enforcer import SecurityEnforcer, protect_window_capture
from ..utils import (
    build_attempt_key,
    student_label,
    build_student_key,
    get_device_label,
    map_identity_to_standard,
)
from ..ws import ExamWebSocket
from .answer_sheet import AnswerSheetWidget
from .congratulations import CongratulationsWindow

# SATU-SATUNYA sanitizer teks server di repo ini: `congratulations` sudah
# menyediakannya (bersama `setTextFormat(Qt.PlainText)` di labelnya) untuk
# nama ujian + pesan guru. File itu tidak boleh diedit ronde ini, jadi yang
# dipakai di sini IMPORT (baca-saja) — bukan salinan: dua salinan pasti
# berbeda dalam satu putaran, dan yang lebih 보수atif biasanya yang terlupa
# dipanggil.
from .congratulations import _sanitize_server_text
from .fullscreen import apply_fullscreen, covers_fullscreen
from .pdf_viewer import PdfWidget
from .timer import ElapsedTimerWidget
from .styles import SECURITY_COLORS

log = logging.getLogger(__name__)

# Modifier yang WAJIB ada untuk admin exit (Ctrl+Shift+Alt+Q). Dipakai
# sebagai SUBSET: `event.modifiers() & _ADMIN_EXIT_MODIFIERS` harus sama
# dengan `_ADMIN_EXIT_MODIFIERS`. Membandingkan dengan `==` pada modifier
# mentah akan mematikan pintu darurat begitu ada modifier tambahan apa pun.
_ADMIN_EXIT_MODIFIERS = (
    Qt.ControlModifier | Qt.ShiftModifier | Qt.AltModifier
)

# Batas keras menunggu hasil submit background (H6).
#
# Anggaran terpanjang di `_background_submit_thread`: retry submit ~7 detik
# + polling /result 202 ~77 detik, dan paling lambat setelah itu notifikasi
# desktop. Dipilih 110 detik — di atas anggaran itu, `_on_auto_submit_done`
# TIDAK akan pernah datang (thread mati, signal hilang, atau prosesnya
# sedang disuspensi), dan tanpa batas itu layar "Mengumpulkan jawaban…"
# menggantung selamanya: tidak ada dialog, tidak ada halaman, tidak ada
# tombol, satu-satunya jalan keluar Task Manager.
_AUTOSUBMIT_WATCHDOG_MS = 110_000

# Jeda sebelum perbaikan fullscreen yang dipicu perubahan GEOMETRI.
#
# Dua alasan, keduanya soal WM: (1) saat drag Move/Size masih berjalan,
# memasang ulang geometri di tengah-tengah drag akan membatalkannya;
# (2) pada `show()` pertama Qt masih menghitung ukuran jendela, jadi
# memperbaiki di tengah calculate itu sia-sia — dan `apply_fullscreen()`
# sudah dipanggil oleh jalur show/enforcer. 150 ms cukup untuk keduanya
# selesai, dan event geometri yang beruntun digabung jadi satu perbaikan.
_FULLSCREEN_REPAIR_DELAY_MS = 150


def answers_match_disk(exam_id, answers) -> bool:
    """True HANYA kalau isi disk untuk `exam_id` masih persis `answers`.

    Penjagaan pemilik jawaban, satu implementasi untuk semua jalur yang
    memanggil `config.clear_answers()` setelah submit terkonfirmasi:

      * `_background_submit_thread` (auto-submit) — worker ini berjalan
        sampai ~84 detik (retry ~7 dtk + polling 202 ~77 dtk). Dalam rentang
        itu siswa bisa re-entry dan memulai percobaan baru yang menulis
        autosave-nya sendiri ke disk. Tanpa penjagaan, successes yang
        TERLAMBAT menghapus jawaban percobaan baru — dan kalau sesi baru
        itu mati mendadak sesudahnya, tidak ada yang tersisa untuk
        dipulihkan;
      * `ServerConfigDialog._recovery_submit_thread` (jalur "Kirim Lagi") —
        SUDAH memakai `answers_match_disk` (`server_config.py:943`), tapi
        masih PASANGAN baca-hapus yang tidak atomik: helper yang benar ada
        di bawah (`clear_answers_if_unchanged`). File itu bukan bagian dari
        ronde ini, jadi perbaikannya dilaporkan, bukan dikerjakan — lihat
        bagian "perlu perubahan di luar Roundtable ini" pada laporan.

    Fail-safe: kalau `load_answers` melempar, jawabannya False — "tidak bisa
    dipastikan" tidak boleh berarti "boleh dihapus". Membaca saja, tidak
    pernah mengubah apa pun.

    JANGAN dipakai sendirian sebelum `clear_answers`: fungsi ini hanya
    MEMBACA disk, jadi pasangan "baca lalu hapus" yang dia izinkan tetap
    bisa disela penulis jawaban di antara keduanya. Yang dipakai untuk
    menghapus adalah `clear_answers_if_unchanged`.
    """
    try:
        current = config.load_answers(exam_id)
    except Exception:
        log.warning(
            "tidak bisa memastikan isi disk untuk exam %s — jawaban "
            "dibiarkan (tidak dihapus)", exam_id, exc_info=True,
        )
        return False
    return current == answers


def clear_answers_if_unchanged(exam_id, answers) -> bool:
    """Hapus jawaban `exam_id` HANYA kalau isinya masih persis `answers`.

    Wrapper tipis di atas `config.clear_answers_if_unchanged`. Operasinya
    hidup di `config` karena di sana tempat lock-nya: `_answers_lock` di sana
    yang dipegang `save_answers`/`save_answers_owner`, dan hanya dari sana
    read-decide-delete bisa dibuat atomik terhadap penulis jawaban (H9).
    Detailnya dan bukti racing-nya ada di docstring helper di `config`.

    Dipakai di ketiga call site setelah submit terkonfirmasi:
    `_background_submit_thread`, `_cleanup_after_submit`, dan (lewat
    `config`) jalur recovery di `server_config`. `_cleanup_after_submit`
    sekaligus memakai hasil kembaliannya untuk memutuskan apakah halaman
    selamat masih boleh ditampilkan.

    Mengembalikan True HANYA kalau penghapusan benar-benar dijalankan;
    selain itu fail-closed (kunci hilang, `load_answers` melempar, isi
    berbeda) karena menghapus jawaban milik percobaan lain jauh lebih
    merusak daripada membiarkannya.
    """
    return config.clear_answers_if_unchanged(exam_id, answers)


class _ProgressWindow(QWidget):
    """Layar "Mengumpulkan jawaban…" — top-level yang tidak bisa ditutup.

    C1 (KRITIS). Jendela ini adalah SATU-SATUNYA top-level yang terlihat
    selama budget submit (±84 detik: retry ~7 dtk + polling 202 ~77 dtk):
    dialog konfigurasi sudah `hide()`-kan, viewer di-`hide()`-kan. Jadi
    `QWidget(None)` polos di sini berarti satu Alt+F4 dari siswa menutup
    SELURUH aplikasi:

        Alt+F4 (atau klik X, atau Esc)
          -> close() menutup top-level terakhir
          -> lastWindowClosed  (setQuitOnLastWindowClosed default True)
          -> app.quit()
          -> app.exec_() kembali
          -> thread submit yang baru saja dimulai MATI di tengah HTTP

    Jawaban tidak pernah sampai server, tidak ada halaman selamat, tidak
    ada notifikasi — siswa menganggur di depan layar mati dan menyimpulkan
    aplikasinya sudah tertutup. Di tier low dan medium ini sama sekali
    gratis: `_start_keyboard_hook` hanya dipanggil dari `set_strict_mode`,
    dan tabel blok tombol tingkat-Qt ada di `ExamViewerWindow`, bukan di
    jendela ini. Strict pun tetap terbuka karena
    `windows_backend.py:834` dengan sengaja MENERIMA kegagalan pasang
    hook.

    Tiga lapis, semuanya wajib (satu lapisan saja bisa dilewati per
    platform):

      * `FramelessWindowHint` — tidak ada tombol X di title bar untuk
        diklik. Lapisan ini yang paling sering terlewat, karena yang
        dipakai siswa bukan Alt+F4 melainkan klik mouse;
      * `closeEvent` menolak selama submit masih berjalan;
      * `keyPressEvent` menelan Alt+F4 / Esc / tombol Menu, apa pun
        tier-nya (modifier Alt tidak dicek: F4 yang sudah sampai di
        aplikasi terlambat untuk dicek di lapisan lain).

    `WA_DeleteOnClose` tetap dipakai: begitu alur selesai, jendela dibuang
    sendiri oleh Qt.
    """

    # Tombol yang menutup jendela / membuka menu sistem. Ditelan di SEMUA
    # tier, karena tier low justru yang paling tidak terlindungi: di sana
    # tidak ada keyboard hook sama sekali.
    _BLOCKED_KEYS = (
        Qt.Key_F4,     # Alt+F4 (tutup), F4 polos (menu window)
        Qt.Key_Escape,  # Esc
        Qt.Key_Menu,    # tombol Menu / Shift+F10 (menu konteks sistem)
    )

    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        # False selama submit berjalan. Di-set True HANYA oleh jalur internal
        # (`ExamViewerWindow._close_progress_window`), dan hanya setelah
        # langkah berikutnya menghasilkan window yang terlihat — kalau tidak,
        # `closeEvent` menolak sampai-saat penutupannya sendiri, dan layar
        # pengumpulan menutupi layar berikutnya selamanya.
        self.allow_close = False

    def closeEvent(self, event: QCloseEvent) -> None:
        if not self.allow_close:
            log.warning(
                "siswa mencoba menutup layar pengumpulan jawaban — "
                "ditolak (submit masih berjalan)",
            )
            event.ignore()
            return
        super().closeEvent(event)

    def keyPressEvent(self, event: QKeyEvent) -> None:
        if event.key() in self._BLOCKED_KEYS:
            log.warning(
                "tombol penutup (key=%d) ditelan di layar pengumpulan — "
                "submit masih berjalan",
                int(event.key()),
            )
            event.ignore()
            return
        super().keyPressEvent(event)


class ExamViewerWindow(QMainWindow):
    """Main exam window."""

    closed = pyqtSignal()
    # C1: ditembak SETELAH alur selesai total — manual (halaman selamat
    # ditutup siswa) maupun auto-submit background (sukses → halaman
    # ditutup; gagal → langsung). `closed` tidak dipakai untuk ini karena
    # ia menembak saat window TERTUTUP, sementara halaman selamat butuh
    # window HIDUP-tersembunyi di belakangnya.
    all_done = pyqtSignal()

    # Thread-safe signals for background -> UI updates
    _sig_status = pyqtSignal(str)
    _sig_pdf_ready = pyqtSignal()
    _sig_pdf_error = pyqtSignal(str)
    _sig_submit_result = pyqtSignal(bool, str)
    # C1: hasil submit background (auto-submit) → slot GUI.
    _sig_auto_submit_done = pyqtSignal(bool, str)

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
        # Payload jawaban yang SEDANG dikirim viewer ini (C2). Disimpan di
        # viewer — bukan dibaca ulang dari disk — supaya jelas-jelas
        # membandingkan "yang saya kirim" dengan "yang ada di disk", termasuk
        # di jalur submit manual yang clear_answers-nya berjalan di thread
        # GUI. `None` = belum ada yang dikirim.
        self._submitted_payload = None

        self._security: Optional[SecurityEnforcer] = None
        self._pdf_path: Optional[str] = None
        # Re-entrancy guard for _enforce_fullscreen, which is driven by a
        # window-state change that showFullScreen() itself produces.
        self._fullscreen_reasserting = False
        # Satu perbaikan fullscreen yang sudah dijadwalkan (dari event
        # geometri) — menggabungkan move/resize beruntun jadi satu perbaikan.
        self._fullscreen_repair_scheduled = False
        # Peringatan konfigurasi ujian yang muncul di layar (mis. nomor soal
        # bentrok) — supaya siswa bisa melaporkannya, bukan diam-diam
        # kehilangan jawaban.
        self._exam_warnings: List[str] = []
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
        self._sig_auto_submit_done.connect(self._on_auto_submit_done)
        # C1: True di antara window ditutup auto-submit dan hasil
        # background tiba — `closed` yang menembak di rentang itu BUKAN
        # akhir alur (halaman selamat belum tampil), jadi __main__
        # menahannya.
        self._auto_submit_pending = False
        # Latch "alur auto-submit sudah diselesaikan" (H6). Tanpa ini,
        # hasil yang telat (atau watchdog yang sudah menyelesaikan alur)
        # bisa memproses langkah akhir untuk kedua kalinya — memunculkan
        # halaman selamat di atas layar siswa berikutnya setelah alur
        # sebenarnya sudah selesai.
        self._auto_submit_finished = False
        self._autosubmit_watchdog = None

        self._setup_ui()
        # Persist start_time sejak awal (mirror Android loadExamContent) —
        # dipakai recovery re-entry saat mengirim ulang jawaban dari disk.
        config.save_start_time(self._exam.id, self._timer_widget.get_start_time_iso())
        self._init_security()  # Activate security BEFORE PDF loads

        # L-2 RACE: tiga assignment di bawah HARUS di atas _load_pdf_async().
        # `_load_pdf_thread` membaca `self._device_label` (dan mengecek
        # `_abandoned`): thread bisa jalan SEBELUM __init__ sampai ke baris
        # assignment bila urutannya dibalik → AttributeError dari worker.
        self._std_identity = map_identity_to_standard(identity_data)
        # Dihitung SEKALI di sini, lalu dipakai ulang untuk download PDF,
        # submit, dan presence. Kalau dihitung ulang di tiap call site,
        # keempatnya bisa berbeda dan gate PDF tidak match baris approval.
        # Di-scope dengan token + identitas siswa: label berarti "kursi yang
        # sedang dipakai siswa ini", bukan "mesin ini". See
        # utils.build_attempt_key untuk alasannya.
        # Exam baru dimulai: unduhan sebelumnya (kalau ada) tidak lagi
        # relevan, dan mengosongkan flag ini membuat `_load_pdf_thread`
        # menyimpan PDF yang baru diunduh.
        self._abandoned = False
        self._device_label = get_device_label(
            build_attempt_key(self._token, identity_data)
        )
        # Kunci percobaan — DIHITUNG SEKALI di sini, dipakai restore jawaban,
        # sidecar pemilik, dan submit. Tanpa satu sumber ini, `_save_answers`
        # sempat menghitung ulang sendiri dan bisa berbeda dari yang dipakai
        # submit — jawaban A lalu tercatat milik B (lihat H1 di
        # `_save_answers`).
        self._student_key = build_student_key(identity_data, self._token)
        self._presence_active = True
        self._load_pdf_async()

        # Presence (login/heartbeat/logout) — mirror Android WebSocketManager.
        # Heartbeat HTTP berjalan selama sesi (interval 60 dtk) supaya siswa
        # tampil ONLINE di dashboard monitoring pengawas; login dikirim sekali
        # di awal, logout/complete saat ujian selesai.
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
        #
        # Answers yang ditemukan di disk BARU dipulihkan setelah lembar
        # jawaban dibangun (`_build_answer_sheet`), bukan lewat timer yang
        # mulai dihitung dari konstruktor. Alasannya PRUNING: separuh
        # `restore_answers` hanya bisa berjalan kalau `_answer_widgets` sudah
        # terisi — ia membuang nilai single/multi/matching yang tidak ada
        # lagi di pilihan SEKARANG. Dengan timer 500 ms dan PDF yang baru
        # selesai setelah network, restore berjalan saat widget masih nol
        # (terukur: widget=0 saat restore), jadi jawaban basi ikut terkirim
        # dan dinilai SALAH tanpa pesan. Timer tetap dipasang sebagai
        # jaring pengaman (lihat `_restore_saved_answers`).
        #
        # Jangan pulihkan jawaban milik percobaan lain (PC lab dipakai
        # bergantian): owner yang berbeda berarti jawaban ini bukan milik
        # siswa sekarang — lihat config.resolve_submit_answers.
        # `self._student_key` dipakai langsung (bukan dihitung ulang): satu
        # sumber kebenaran untuk gerbang restore, sidecar, dan submit.
        _restore: Dict[str, Any] = {}
        _owner = config.load_answers_owner(exam.id)
        _mine = self._student_key
        saved = config.load_answers(exam.id)
        if saved and _owner is not None and str(
            _owner.get("student_key", "") or ""
        ) and str(_owner.get("student_key", "") or "") != _mine:
            log.warning(
                "restore jawaban exam %s dilewati: owner != percobaan ini",
                exam.id,
            )
        elif saved:
            _restore = dict(saved)
        self._pending_restore = _restore or None
        self._restore_applied = False
        QTimer.singleShot(500, self._restore_saved_answers)

    def _restore_saved_answers(self) -> None:
        """Pulihkan jawaban dari disk — HANYA setelah lembar ada.

        Dipanggil dua kali: dari `_build_answer_sheet` (jalur utama — saat
        itu widget sudah ada, jadi pruning benar-benar berjalan) dan dari
        timer 500 ms sebagai jaring pengaman (kalau somehow lembar sudah
        dibangun lebih dulu, mis. oleh rebuild karena PDF gagal lalu
        diulang).
        """
        pending = self.__dict__.get("_pending_restore")
        if not pending or self.__dict__.get("_restore_applied"):
            return
        if not self._answer_sheet.is_built():
            # Lembar belum ada: pruning akan mengiterasi nol widget, jadi
            # MENUNGGU lebih benar daripada berlari sia-sia.
            return
        self._restore_applied = True
        self._pending_restore = None
        self._answer_sheet.restore_answers(pending)

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
        #
        # H4: `exam.name` datang dari server dan TIDAK divalidasi (hanya
        # `panel_color` yang diregex-kan di `models.Exam`). Default
        # `QLabel.textFormat()` adalah `Qt.AutoText`, jadi tanpa dua baris
        # di bawah ini server bisa menyuntik rich text ke layar siswa —
        # termasuk `<img src="file:///...">`, yang membuat `QTextDocument`
        # membuka berkas lokal sinkron di thread GUI. Perlakuan yang sama
        # sudah dipakai `congratulations` untuk pesan guru; polanya tinggal
        # diterapkan di mana-mana.
        self._lbl_title = QLabel(_sanitize_server_text(self._exam.name))
        self._lbl_title.setTextFormat(Qt.PlainText)
        self._lbl_title.setStyleSheet("font-size: 16px; font-weight: bold;")
        top_layout.addWidget(self._lbl_title, 1)

        # Security banner. display_level is the canonical tier, so this can
        # never raise (the raw server string used to be `.upper()`d here,
        # which crashed the whole window on `"security_level": null`) and
        # never renders a locked-down exam in the low-tier colour.
        mode = self._exam.display_level
        # Index langsung: tier tak dikenal harus KeyError yang keras, bukan
        # diam-diam tampil sebagai low.
        bg, fg = SECURITY_COLORS[mode]
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
        # Mengetik terus-menerus tidak pernah menulis apa pun bila hanya
        # debounce trailing-edge: flush berkala 2 detik menulis jawaban
        # kotor walau debounce belum sempat menembak.
        self._answers_dirty = False
        self._flush_timer = QTimer(self)
        self._flush_timer.setInterval(2000)
        self._flush_timer.timeout.connect(self._flush_answers)
        self._flush_timer.start()
        self._answer_sheet.answer_changed.connect(self._on_answer_changed_autosave)

    def _on_answer_changed_autosave(self, *args) -> None:
        self._answers_dirty = True
        self._save_timer.start()

    def _flush_answers(self) -> None:
        """Tulis jawaban yang masih "kotor" ke disk (flush paksa).

        Dipanggil timer 2 detik DAN sebagai flush terakhir di kedua jalur
        keluar tanpa submit (low-tier + admin exit) — di sana autosave
        langsung dimatikan, jadi tanpa flush ini ketikan terakhir siswa
        (yang masih di dalam debounce 500 ms) tidak pernah menyentuh disk.

        Baca state lewat `__dict__` supaya aman untuk instance yang dibuat
        tanpa `__init__` (double di test): pada objek PyQt yang belum
        diinisialisasi, akses atribut yang tidak ada melempar RuntimeError.
        """
        if self.__dict__.get("_answers_dirty") and not self.__dict__.get(
            "_submitted", False
        ):
            self._save_answers()

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
            # Destinasi TAHU SEBELUM download dimulai.
            #
            # Sebelumnya `self._pdf_path` baru di-assign dari nilai balik
            # `download_pdf`, jadi `_discard_pdf()` yang dipanggil saat
            # siswa keluar tengah-download melihat `None` dan langsung
            # return. Download pun selesai menulis naskah ujian utuh ke
            # %TEMP% -- dan proses masih hidup (config dialog muncul lagi),
            # jadi berkas itu menunggu siswa berikutnya dengan lockdown
            # sudah dilepas.
            dest = os.path.join(
                tempfile.gettempdir(), f"examvan_exam_{self._exam.id}.pdf"
            )
            self._pdf_path = dest
            if getattr(self, "_abandoned", False):
                # Sudah keluar sebelum download sempat mulai.
                return
            downloaded = api.download_pdf(
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
                #
                # WAJIB self._device_label, bukan get_device_label() langsung:
                # yang terakhir tanpa attempt_key menghasilkan label MESIN,
                # sedangkan approval memakai label KURSI. Keduanya tidak akan
                # match, dan PDF tidak pernah terunduh.
                device_id=self._device_label,
            )
            if getattr(self, "_abandoned", False):
                # Race yang sesungguhnya: siswa keluar SAAT download
                # berjalan, dan download baru selesai sekarang. Berkasnya
                # harus dihapus seketika, bukan ditinggalkan untuk PC lab.
                log.info("PDF finished after exit; discarding %s", downloaded)
                self._discard_pdf()
                return
            self._pdf_path = downloaded
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

    def _build_answer_sheet(self) -> None:
        """Bangun lembar jawaban dari config soal.

        Sengaja TIDAK bergantung pada PDF. Lembar jawaban dibangun dari
        `exam.questions`; PDF cuma tampilan. Dulu `build_from_questions()` hanya
        dipanggil di cabang sukses `_on_pdf_ready`, jadi satu hiccup jaringan
        saat download — atau satu file PDF korup — membuat lembar jawaban
        tetap KOSONG: 0 widget, `get_answered_count()` = (0, 0), dialog
        konfirmasi mengarang "0 dari 0 soal", dan tombol submit tetap aktif
        sehingga siswa bisa mengirim jawaban kosong. Untuk siswa itu akhir
        ujiannya: tidak ada yang bisa diklik dan tidak ada tombol retry.
        """
        self._answer_sheet.build_from_questions(self._exam.questions)
        # Restore jawaban SEKARANG juga: widget sudah ada, jadi pruning di
        # `restore_answers` benar-benar mengiterasi pilihan soal dan
        # membuang nilai yang sudah tidak valid.
        self._restore_saved_answers()
        duplicates = self._answer_sheet.duplicate_question_numbers()
        if duplicates:
            # Konfigurasi ujian rusak: nomor soal bentrok. Server hanya
            # memvalidasi kunci jawaban (`validateQuestionKeys`), jadi ini
            # bisa saja tersimpan tanpa guru tahu. Efeknya ke siswa: dua
            # blok soal bernomor sama berbagi satu slot jawaban, jadi satu
            # blok yang dijawab tidak bisa ikut terkirim.
            #
            # Nomor yang bentrok sengaja TIDAK diubah di sisi siswa —
            # memakaikan ulang payload akan memakai kunci yang tidak ada
            # di server. Yang bisa dilakukan di sini adalah MELAPORKAN
            # supaya guru memperbaiki konfigurasi ujiannya.
            log.error(
                "Nomor soal bentrok pada exam %s: %s — satu blok jawaban "
                "per nomor tidak bisa dikirim",
                self._exam.id, ", ".join(duplicates),
            )
            self._exam_warnings.append(
                f"Perhatian: soal nomor {', '.join(duplicates)} bernomor "
                f"ganda. Satu nomor hanya bisa menyimpan satu jawaban — "
                f"hubungi pengawas."
            )
        broken = self._answer_sheet.broken_question_numbers()
        if broken:
            # Struktur soal tidak sah (bukan duplikat): tidak ada widget,
            # tidak dihitung — siswa harus tahu itu masalah konfigurasi.
            log.error(
                "Soal rusak pada exam %s: %s — tidak bisa dijawab, "
                "hubungi pengawas",
                self._exam.id, ", ".join(broken),
            )
            self._exam_warnings.append(
                f"Perhatian: soal nomor {', '.join(broken)} rusak dan "
                f"tidak bisa dijawab — hubungi pengawas."
            )

    @pyqtSlot()
    def _on_pdf_ready(self) -> None:
        # Download yang selesai setelah keluar tidak boleh membuka/membocorkan
        # naskah: buang dan kembali.
        if getattr(self, "_abandoned", False):
            self._discard_pdf()
            return
        # Lembar jawaban dibangun lebih dulu dan selalu: ajar tidak
        # bergantung pada PDF sama sekali.
        self._build_answer_sheet()
        if self._pdf_path and self._pdf_viewer.load_pdf(self._pdf_path):
            self._lbl_status.setText("PDF siap")
        else:
            # PDF gagal TIDAK berarti ujian tidak bisa dikerjakan. Status
            # mengatakannya, dan siswa tetap bisa menjawab semua soal.
            self._lbl_status.setText(
                "Gagal memuat PDF — soal tetap bisa dijawab, "
                "hubungi pengawas bila PDF tidak muncul."
            )
            log.warning("PDF gagal dimuat, ujian tetap dilanjutkan")

    @pyqtSlot(str)
    def _on_pdf_error(self, error: str) -> None:
        # Download yang GAGAL setelah siswa keluar diperlakukan sama dengan
        # yang BERHASIL: jendela sudah ditinggalkan (`_abandoned`), jadi
        # jangan membangun ulang lembar jawaban dan jangan menulis status ke
        # UI yang sudah tidak terlihat — dan buang berkas separuh yang
        # `_load_pdf_thread` sudah taruh di `_pdf_path` sebelum unduhan
        # mulai. Tanpa ini, satu klik "Keluar Ujian" bisa membangun ulang
        # seluruh lembar jawaban di jendela tertutup.
        if getattr(self, "_abandoned", False):
            log.info(
                "PDF gagal setelah keluar, lembar jawaban tidak dibangun: %s",
                error,
            )
            self._discard_pdf()
            return
        # Download gagal: soal tetap bisa dijawab. Hanya PDF yang hilang.
        self._build_answer_sheet()
        self._lbl_status.setText(
            f"Gagal mengunduh PDF: {error} — soal tetap bisa dijawab."
        )

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
            # Lembar jawaban dibuka kembali: submit gagal, siswa berhak
            # memperbaiki jawaban sebelum mencoba lagi. Lembar dikunci saat
            # submit dimulai -- lihat _do_submit untuk alasannya.
            self._answer_sheet.setEnabled(True)
            self._btn_submit.setText(" Kumpulkan Jawaban")
            # Guard WAJIB: dialog ini muncul di SETIAP submit yang gagal,
            # dan `medium` adalah level default. Tanpa guard, countdown
            # 3 detik berjalan di belakangnya dan jendela menutup --
            # persis jalan pemulihan yang sedang ditawarkan ke siswa.
            with self._modal_dialog_guard():
                _box = QMessageBox(
                    QMessageBox.Warning,
                    "Gagal",
                    f"Gagal mengumpulkan jawaban:\n{message}\n\nSilakan coba lagi.",
                    QMessageBox.Ok,
                    self,
                )
                # Pesan bisa memuat HTML server/proxy: render sebagai teks
                # polos supaya tidak diinterpretasi sebagai rich text.
                _box.setTextFormat(Qt.PlainText)
                _box.exec_()

    def _show_congratulations(self, message: str) -> "CongratulationsWindow":
        """Bangun + tampilkan halaman selamat (helper bersama C1).

        Dipakai `_cleanup_after_submit` (submit manual) DAN
        `_on_auto_submit_done` (submit background): satu tempat membangun
        halaman supaya keduanya tidak meleset. Identitas diambil dari
        pemetaan kanonik (H2), bukan lookup mentah "nama"/"nomor_ujian".
        """
        std = getattr(self, "_std_identity", None)
        if not std:
            std = map_identity_to_standard(self._identity_data)
        congrats = CongratulationsWindow(
            server_url=self._server_url,
            exam_token=self._token,
            exam_name=getattr(self._exam, "name", ""),
            student_name=str(std.get("student_name", "")),
            student_number=str(std.get("exam_number", "")),
            student_class=str(std.get("student_class", "")),
            congrats_message=message,
            public_results=bool(getattr(self._exam, "public_results", True)),
        )
        congrats.setAttribute(Qt.WA_DeleteOnClose)
        # `parent=None` dan referensinya ditahan di `self._congrats_ref`:
        # halaman ini harus HIDUP setelah viewer ditutup, bukan ikut ter-GC
        # bersama jendela ujian.
        congrats.show_fullscreen()
        self._congrats_ref = congrats
        # C3: layar ini menampilkan token ujian — di mode static-token itu
        # kredensial hasil seluruh kelas — sementara `deactivate()` sudah
        # melepas WDA_MONITOR DAN keyboard hook SEBELUM halaman dibuat.
        # Tanpa ini, PrintScreen di halaman ini menyimpan token + identitas
        # + pesan guru ke disk tanpa diblokir, di semua level. Hanya proteksi
        # capture yang dipasang (bukan strict penuh) supaya tombol "Selesai"
        # tetap bisa diklik dan jendela tidak jadi always-on-top.
        if self._security is not None:
            try:
                self._security.protect_window(congrats)
            except Exception:
                log.warning("could not protect congratulations window",
                            exc_info=True)
        return congrats

    def _disk_answers_replaced(self) -> bool:
        """True kalau isi disk sudah bukan milik payload yang dikirim ini.

        Dipakai dua tempat di viewer, dan keduanya mengarah ke arah yang
        SAMA: kalau isinya bukan milik percobaan ini, maka (1) jangan
        menghapus apa pun dari disk, dan (2) jangan tampilkan halaman yang
        akan menutupi layar percobaan berikutnya.

        Beda dari `answers_match_disk` (yang membandingkan PERSIS): di jalur
        auto-submit, thread background sudah menghapus jawabannya sendiri
        sebelum hasil sampai ke `_on_auto_submit_done`, jadi disk yang
        kosong tetap berarti "milik kita, sudah beres" — bukan "percobaan
        lain". Isi yang BERBEDA dan tidak kosong itulah yang menandakan
        attempt lain sudah menimpanya.

        Payload `None` berarti "belum ada yang terkirim" — dalam produksi itu
        tidak mungkin terjadi di jalur submit (semua jalur submit mengisi
        `_submitted_payload` sebelum menjalankan thread), jadi perlakukan
        sebagai "tidak ada bukti attempt lain" dan jangan mengubah
        perilakunya. `{}` (jawaban kosong yang sah) BUKAN `None`: itu
        payload sungguhan, dan disk yang isinya berbeda berarti attempt lain
        yang menimpanya.

        `__dict__.get` (bukan `getattr`) karena test membuat viewer lewat
        `ExamViewerWindow.__new__` tanpa `__init__` — pada objek PyQt yang
        belum di-inisialisasi, `getattr` untuk atribut yang tidak ada
        melempar RuntimeError ("super-class __init__ was never called"),
        bukan mengembalikan default.
        """
        payload = self.__dict__.get("_submitted_payload")
        if payload is None:
            return False
        try:
            current = config.load_answers(self._exam.id)
        except Exception:
            log.warning(
                "tidak bisa memastikan isi disk untuk exam %s — jawaban "
                "dibiarkan (tidak dihapus) dan halaman hasil tidak "
                "ditampilkan", self._exam.id, exc_info=True,
            )
            return True
        if not current:
            return False       # sudah dikosongkan oleh jalur sukses kita
        return current != payload

    def _cleanup_after_submit(self, message: str) -> None:
        """Clean up after successful submit."""
        # C2: call site KETIGA dari `clear_answers` setelah submit sukses.
        # Dua call site sebelumnya (`_background_submit_thread` dan
        # `server_config._recovery_submit_thread`) sudah memakai penjagaan
        # yang sama; yang ini terlewat, dan akibatnya submit
        # yang sukses — bisa saja yang TERLAMBAT, setelah siswa menutup
        # jendela lalu siswa berikutnya masuk dan autosave — menghapus
        # jawaban percobaan yang lebih baru dan memunculkan halaman selamat
        # basi di atas dialog konfigurasi yang sedang dipakai orang lain.
        replaced = self._disk_answers_replaced()
        if replaced:
            log.info(
                "skip clear_answers: disk berisi jawaban percobaan lain untuk "
                "exam %s (payload yang dikirim sudah durable)", self._exam.id,
            )
        else:
            # H9: helper yang sama dengan jalur submit background, bukan
            # `clear_answers` telanjang. `_disk_answers_replaced()` di atas
            # hanya MEMBACA disk, jadi tanpa kunci yang memagari
            # baca-hapus, autosave percobaan berikutnya bisa masuk tepat di
            # antara keduanya dan ikut terhapus.
            cleared = clear_answers_if_unchanged(
                # `__dict__.get`, bukan atribut langsung: viewer yang dibuat
                # lewat `__new__` tanpa `__init__` (test) melempar
                # RuntimeError kalau atribut belum ada.
                self._exam.id, self.__dict__.get("_submitted_payload"),
            )
            if not cleared and self.__dict__.get("_submitted_payload"):
                # `clear_answers_if_unchanged` menolak HANYA kalau isinya
                # sudah bukan payload kita — itu bukti percobaan lain menulis
                # di tengahnya, jadi halaman selamat memang tidak boleh
                # menimpa layar percobaan berikutnya.
                replaced = True
                log.info(
                    "halaman hasil dilewati: isi disk berubah saat clear "
                    "exam %s (percobaan lain menulis di tengahnya)",
                    self._exam.id,
                )
            # `elif not cleared`: tanpa payload yang dikirim
            # (`_submitted_payload` belum terisi) helper-nya sengaja
            # gagal-terbuka — TIDAK menghapus, karena tidak ada bukti isi
            # disk milik kita. Tapi itu TIDAK berarti attempt lain mengambil
            # alih, jadi halaman tetap ditampilkan. Menyamakan keduanya
            # (seperti versi sebelumnya) membuat siswa kehilangan konfirmasi
            # "berhasil dikumpulkan" tanpa sebab yang nyata, hanya karena
            # urutan pemanggilan atribut.

        # Sticky marker (F2, mirror Android): ujian ini sudah SELESAI di
        # perangkat ini. Re-entry berikutnya MewANAKKAN pilihan eksplisit
        # ("Kerjakan Ulang" / "Kembali") lewat
        # `ServerConfigDialog._offer_resubmit_choice` — bukan diblokir
        # paksa, karena memblokir akan mengunci seluruh kelas di PC yang
        # sama bila satu siswa salah klik.
        config.mark_submitted(
            self._exam.id,
            build_student_key(self._identity_data, self._token),
            student_label(self._identity_data, self._token),
        )
        # Presence: hapus heartbeat Redis (siswa tampil OFFLINE segera).
        self._stop_presence(completed=True)
        if self._security:
            self._security.deactivate()

        self._timer_widget.stop()
        self._stop_autosave()
        self._btn_submit.setEnabled(False)
        self._btn_submit.setText("✅ Terkumpul")

        # Wipe PDF temp file
        self._cleanup_pdf_viewer()
        self._discard_pdf()

        # Halaman selamat, bukan QMessageBox.
        #
        # Android sudah punya `CongratulationsActivity` (pesan guru, badge
        # nama ujian, identitas siswa, tombol copy link hasil) dan server
        # sudah menyediakan `GET /hasil/<token>` + short-link `/<token>`.
        # Desktop dulu cuma menampilkan dialog modal kecil di atas jendela
        # ujian -- siswa tidak pernah melihat identitasnya maupun cara
        # membuka hasil. Sekarang: halaman selamat tampil fullscreen
        # sebagai halaman tersendiri (parity Android
        # `CongratulationsActivity`). `SecurityEnforcer` sudah
        # `deactivate()` di atas, jadi clipboard TIDAK lagi disapu — itulah
        # sebabnya link hasil di layar ini dibersihkan otomatis oleh
        # halamannya sendiri.
        #
        # C1: viewer TIDAK `close()` di sini — `close()` menembakkan `closed`
        # secara sinkron dan `__main__` langsung menampilkan dialog
        # konfigurasi siswa berikutnya DI ATAS halaman yang belum selesai
        # dibaca. Sebagai gantinya viewer hanya `hide()` (tetap direferensi
        # `windows` + `_congrats_ref` supaya tidak ter-GC), dan menutup
        # dirinya sendiri saat halaman ditutup siswa (`page_closed`).
        #
        # C2: halaman selamat HANYA untuk jawaban yang benar-benar milik
        # attempt ini. Kalau isinya sudah diganti attempt lain, halaman ini
        # akan muncul fullscreen di atas dialog konfigurasi / jendela ujian
        # yang sedang dipakai siswa berikutnya — jadi yang dilakukan hanya
        # menyelesaikan alur (`all_done`), tanpa menumpuk halaman baru.
        if replaced:
            self.hide()
            self.all_done.emit()
            return
        congrats = self._show_congratulations(message)
        congrats.page_closed.connect(self.close)
        self.hide()

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

    def _write_answers_owner(self, where: str) -> None:
        """Catat sidecar pemilik jawaban tersimpan (audit 2 Okt 2026, H14).

        Satu helper untuk semua penulis `answers_<id>.dat` supaya tidak
        ada jalur yang "lupa sidecar" — dan supaya log menyebut jalurnya.
        """
        try:
            config.save_answers_owner(
                self._exam.id,
                self._student_key,
                student_label(self._identity_data, self._token),
            )
        except Exception:
            log.warning("save_answers_owner gagal (%s)", where, exc_info=True)

    def _save_answers(self) -> None:
        if self._submitted:
            return
        answers = self._answer_sheet.get_answers()
        # H1: sidecar pemilik WAJIB ditulis di sini, bukan hanya di jalur
        # submit. Semua penulis jawaban lewat satu fungsi ini — debounce
        # 500 ms, flush 2 detik, dan (kalau dipanggil) flush submit —
        # sedangkan `save_answers_owner` dulu hanya dipanggil di dua jalur
        # submit. File jawaban hasil autosave jadi TIDAK PUNYA pemilik,
        # dan KEDUA gerbang konsumennya gagal-TERBUKA saat sidecar hilang:
        # restore di `__init__` (owner None = "milik siapa pun") dan
        # `_offer_pending_recovery` (owner None = recovery sah).
        #
        # Di PC lab itu berarti: siswa A menjawab, proses mati sebelum ada
        # submit, siswa B masuk → B melihat jawaban A di layar dan
        # ditawari "Kirim Lagi" atas nama B. Jawaban A tercatat atas nama B
        # dan nilai A hilang. Sidecar menutup keduanya.
        #
        # H10: sidecar ditulis SEBELUM jawaban, bukan sesudahnya. Urutan
        # lama membiarkan jendela di mana jawaban sudah ada tapi sidecar
        # belum — dan kedua gerbang justru gagal-TERBUKA tepat di jendela
        # itu. Autosave menembak tiap 0,5–2 detik sepanjang satu naskah,
        # jadi jendela itu dilewati ribuan kali, dan satu Task Manager di
        # dalamnya meninggalkan jawaban tanpa pemilik. Arah yang aman:
        # kedua gerbang selalu mengecek jawaban DULU (`load_answers`), jadi
        # sidecar tanpa jawaban itu INERT, sedangkan jawaban tanpa sidecar
        # itu kebocoran. Urutan yang sama sudah dipakai
        # `config.clear_answers` (owner dihapus TERAKHIR, hanya kalau
        # jawabannya benar-benar hilang).
        self._write_answers_owner("_save_answers")
        config.save_answers(self._exam.id, answers)
        self._answers_dirty = False

    def _stop_autosave(self) -> None:
        """Hentikan debounce + flush autosave (submit/tutup)."""
        try:
            self._save_timer.stop()
        except Exception:
            pass
        try:
            self._flush_timer.stop()
        except Exception:
            pass

    # -------------------------------------------------------------------
    # Submit
    # -------------------------------------------------------------------

    def _cleanup_pdf_viewer(self) -> None:
        """Tutup dokumen PDF, TIDAK PERNAH melempar.

        `PdfWidget.cleanup()` memanggil `self._doc.close()` tanpa penjaga
        (`pdf_viewer.py:373-376`), jadi satu handle yang sudah rusak
        menjadi exception yang merambat ke mana saja. Tiga akibat nyata
        di jalur ini:

          * `_cleanup_after_submit`: jawaban sudah durable dan
            `clear_answers` sudah jalan, tapi exception escaping berarti
            halaman selamat tidak pernah dibuat dan `all_done` tidak
            pernah ditembakkan — tombol sudah tulis "✅ Terkumpul" tapi
            mati, dan tidak ada layar lagi;
          * `closeEvent` cabang `already_done`: `cleanup()` dipanggil
            SEBELUM `closed.emit()`, jadi kegagalan terus-menerus membuat
            SETIUP close di-`ignore()`kan — jendela tidak bisa ditutup,
            satu-satunya jalan keluar Task Manager;
          * jalur keluar low-tier / admin exit: siswa menekan "Ya" lalu
            tidak terjadi apa-apa.

        `_discard_pdf()` (yang menghapus berkas naskah di `%TEMP%`) tetap
        dipanggil terpisah dan tidak bergantung pada ini.
        """
        try:
            self._pdf_viewer.cleanup()
        except Exception:
            log.warning(
                "could not close the PDF document — continuing", exc_info=True,
            )

    def _discard_pdf(self) -> None:
        # Tandai dulu: download yang sedang berjalan harus tahu bahwa
        # berkasnya sudah tidak dibutuhkan. `_load_pdf_thread` mengeceknya
        # tepat setelah `download_pdf` selesai.
        self._abandoned = True
        """Hapus berkas PDF yang diunduh untuk ujian ini.

        Dipanggil di SETIAP jalur keluar, bukan hanya submit. PDF berisi
        naskah ujian utuh; meninggalkannya di `%TEMP%` berarti isinya
        masih terbuka untuk pengguna berikutnya mesin lab -- dan setelah
        lockdown dilepas tidak ada lagi proses yang membersihkannya.
        """
        path = getattr(self, "_pdf_path", None)
        if not path:
            return
        try:
            if os.path.exists(path):
                os.remove(path)
        except OSError as exc:
            log.warning("could not remove exam PDF %s: %s", path, exc)

    @contextmanager
    def _modal_dialog_guard(self) -> Iterator[None]:
        """Tangguhkan semua timer yang bisahootingkan pelajar dari dialog.

        Dua hal harus ikut, bukan hanya satu:

        * COUNTDOWN (`_timer_widget._timer`). Kalau deadline tercapai
          selama dialog terbuka, `time_up` menembak -> `_auto_submit` ->
          `_auto_submit_and_exit` menutup jendela dan mengirim jawaban.

        * FOCUS GUARD (`_security.pause_focus_guard()`). `QMessageBox`
          membuat window ini `isActiveWindow() == False` selama
          nested event loop-nya berjalan, dan `_poll_focus()` menanyakan
          persis itu setiap 500 ms dari level `medium` ke atas -- yang
          adalah DEFAULT. Tanpa penangguhan ini, dialog konfirmasi yang
          terbuka >3 detik cukup waktu untuk auto-submit dengan sendirinya,
          sementara pelajar masih membaca pertanyaannya. Lihat
          `SecurityEnforcer.pause_focus_guard()`.

        Countdown yang sedang berjalan diresume setelah dialog ditutup,
        jadi auto-submit karena waktu habis tetap terjadi -- dengan atau
        tanpa konfirmasi. Yang berubah hanya menundanya sampaiuntar
       eming dialog.
        """
        timer = getattr(self._timer_widget, "_timer", None)
        security = self._security
        focus_guard = None
        if security is not None:
            focus_guard = security.pause_focus_guard()
            focus_guard.__enter__()
        if timer is not None:
            timer.stop()
        try:
            yield
        finally:
            if focus_guard is not None:
                focus_guard.__exit__(None, None, None)
            # Jangan menghidupkan countdown pada jendela yang sudah
            # tertutup atau sudah mengumpulkan jawaban: timer 1 Hz di
            # objek yang squeeze it'll keep firing after the fact.
            if timer is not None and self.isVisible() and not self._submitted:
                timer.start()
                self._timer_widget._fired_time_up = False
                self._timer_widget.refresh_deadline()

    def _on_submit(self) -> None:
        # Quick pre-check (volatile read — race window handled by _do_submit lock)
        if self._submitted or self._submitting:
            return

        answered, total = self._answer_sheet.get_answered_count()
        warning = ""
        if self._exam_warnings:
            warning = "\n\n" + "\n".join(self._exam_warnings)
# Countdown DAN focus guard sama-sama ditangguhkan selama dialog.
        # Lihat _modal_dialog_guard() -- focus guard adalah bagian yang
        # sebelumnya hilang: tanpa itu, dialog konfirmasi yang terbuka
        # lebih dari 3 detik cukup waktu untuk auto-submit dengan sendirinya.
        with self._modal_dialog_guard():
            _box = QMessageBox(
                QMessageBox.Question,
                "Konfirmasi Pengumpulan",
                f"Anda telah menjawab {answered} dari {total} soal.\n\n"
                f"Apakah yakin ingin mengumpulkan jawaban?" + warning,
                QMessageBox.Yes | QMessageBox.No,
                self,
            )
            # Default-nya No, persis seperti `QMessageBox.question()` lama —
            # Enter atau Space yang tidak sengaja tidak boleh mengirim jawaban.
            _box.setDefaultButton(QMessageBox.No)
            # Teks bisa memuat HTML dari server: `warning` menyisipkan nomor
            # soal yang diambil VERBATIM dari `exam.questions`. Tanpa
            # setTextFormat, `Qt.AutoText` mendeteksi markup dan
            # merendernya (huruf hilang, `<b>` jadi tebal, `<a href>` jadi
            # tautan yang bisa diklik) — persis di dialog yang menentukan
            # apakah jawaban siswa dikirim atau tidak.
            _box.setTextFormat(Qt.PlainText)
            reply = _box.exec_()

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

    def _build_progress_window(self) -> "_ProgressWindow":
        """Layar "Mengumpulkan jawaban…" — topologi yang MENJAGA event loop.

        Kenapa jendela kecil ini harus ada (C1, ronde 5):

        Dulu auto-submit menutup viewer (`self.close()`). Viewer adalah
        top-level TERAKHIR yang terlihat (dialog konfigurasi sudah
        `hide()`-kan di `__main__.on_exam_selected`), dan
        `setQuitOnLastWindowClosed` tidak pernah di-override — default True.
        Akibatnya `close()` → `lastWindowClosed` → `app.quit()` →
        `app.exec_()` kembali → thread submit yang baru saja dimulai mati
        di tengah jalan. Jawaban tidak pernah sampai server, tidak ada
        halaman selamat, tidak ada notifikasi: siswa menganggur di depan
        layar mati dan menyimpulkan aplikasinya sudah tertutup.

        Satu jendela yang tetap terlihat mengembalikan invarian "selalu ada
        satu top-level yang terlihat" tanpa menahan siswa di jendela
        terkunci (alur lama) dan tanpa membiarkan `lastWindowClosed`
        menutup proses (alur C1 yang rusak).

        Sifatnya:
          * `_ProgressWindow`, bukan `QWidget` polos — jendela ini adalah
            top-level TERAKHIR yang terlihat, jadi Alt+F4 / klik X / Esc
            dari siswa akan menutup SELURUH aplikasi di tengah submit.
            Lihat kelasnya untuk ketiga lapis penahannya (C1, ronde 7);
          * `parent=None` — kalau jadi anak viewer, `hide()`/tutup viewer
            akan ikut menyeretnya;
          * `Qt.ApplicationModal` — tidak ada window lain yang boleh
            menerima klik selama jawaban dikirim;
          * `WA_DeleteOnClose` — dibuang sendiri saat ditutup, dan
            `_progress_ref` dilepas supaya tidak menggantung;
          * bar tak tertutup (0..0) — tidak ada persentase yang bohong,
            yang terjadi di belakang memang "tunggu konfirmasi server".

        CATATAN URUTAN: proteksi capture TIDAK dipasang di sini, tapi
        SETELAH `apply_fullscreen()` di `_auto_submit_and_exit`.
        Afinitas display disimpan per-HWND, jadi memasang proteksi sebelum
        state akhir terbentuk berarti proteksi hilang tanpa jejak — urutan
        yang sama dengan peringatan di `_enforce_fullscreen`.
        """
        win = _ProgressWindow(None)
        win.setWindowTitle("EXAMVAN")
        win.setWindowFlags(
            win.windowFlags()
            | Qt.WindowStaysOnTopHint
            | Qt.FramelessWindowHint
        )
        win.setAttribute(Qt.WA_DeleteOnClose)
        win.setWindowModality(Qt.ApplicationModal)

        layout = QVBoxLayout(win)
        layout.setContentsMargins(48, 48, 48, 48)
        layout.setSpacing(24)

        label = QLabel("Mengumpulkan jawaban…")
        label.setAlignment(Qt.AlignCenter)
        label.setStyleSheet("font-size: 22px; font-weight: bold;")
        layout.addWidget(label)

        sub = QLabel(
            "Jangan tutup jendela ini. Jawaban sedang dikirim ke server; "
            "tunggu sampai halaman selesai tampil."
        )
        sub.setAlignment(Qt.AlignCenter)
        sub.setWordWrap(True)
        layout.addWidget(sub)

        bar = QProgressBar()
        bar.setRange(0, 0)          # indeterminate
        bar.setTextVisible(False)
        layout.addWidget(bar)
        return win

    def _protect_progress_window(self) -> None:
        """Pasang proteksi capture layar pengumpulan (best-effort).

        Halaman "selamat" menaruh identitas + token di layar, dan selama
        menunggu hasil itulah satu-satunya layar yang tampil — jadi proteksi
        capture yang sama wajib dipakai di sini juga (lihat
        `security.enforcer.protect_window_capture`: helper modul-level yang
        justru dibuat untuk halaman yang hidup tanpa enforcer aktif).

        Dipanggil SESUDAH `apply_fullscreen()`: `SetWindowDisplayAffinity`
        disimpan per-HWND, jadi pasang sebelum state akhir terbentuk
        berarti proteksi hilang tanpa satu baris log pun.
        """
        if not protect_window_capture(getattr(self, "_progress_ref", None)):
            log.warning(
                "proteksi capture layar pengumpulan gagal", exc_info=True,
            )

    def _close_progress_window(self) -> None:
        """Tutup + lepas layar pengumpulan (WA_DeleteOnClose ikut menghapus).

        WAJIB dipanggil SETELAH langkah berikutnya menghasilkan window
        terlihat (halaman selamat atau dialog konfigurasi): `close()` pada
        top-level yang sedang terlihat menembakkan `lastWindowClosed`, dan
        kalau tidak ada window lain yang terlihat, Qt menutup aplikasi —
        persis bug C1 yang sedang diperbaiki, hanya sekarang di ujung alur.

        Jalur INTERNAL: `_ProgressWindow.closeEvent` menolak selama submit
        berjalan (C1), dan yang menutupnya di sini adalah alur yang sudah
        selesai — jadi flag yang mengatur penolakan harus dibuka lebih dulu.
        Kalau tidak, layar pengumpulan menutupi layar berikutnya selamanya
        (zombie yang justru tidak bisa ditutup siswa).

        Referensi dilepas SESUDAH `close()`: kalau `close()` melempar,
        drop-dulu membuat jendela tetap terlihat tanpa siapa pun yang
        masih memegangnya — layar mati yang tidak bisa ditutup.
        """
        win = getattr(self, "_progress_ref", None)
        if win is None:
            return
        try:
            win.allow_close = True
        except Exception:
            log.warning("could not allow progress window close", exc_info=True)
        try:
            win.close()
        except Exception:
            log.warning("could not close progress window", exc_info=True)
            # Jendela sudah tidak bisa ditutup normal (backend window sudah
            # hancur, exception dari closeEvent, dst). Setidaknya jangan
            # tinggalkan layar pengumpulan menutupi semua yang lain.
            try:
                win.hide()
            except Exception:
                log.warning("could not hide progress window", exc_info=True)
        self._progress_ref = None

    def _auto_submit_and_exit(self) -> None:
        """Mirror Android autoSubmitAndExit: layar pengumpulan, submit di background.

        Alur lama menahan window tetap terbuka (dan di strict mode tetap
        terkunci) sampai hasil jaringan tiba — jaringan mati → siswa terjebak
        di layar terkunci tanpa jalan keluar. Alur baru (parity Android):
          1. gate submit tunggal (lock);
          2. marker sticky "sudah selesai" + flush jawaban TERBARU ke disk
             (proses mati / gagal jaringan → recovery re-entry);
          3. lepas kunci + tampilkan layar pengumpulan, viewer `hide()`
             (bukan `close()` — lihat `_build_progress_window`);
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
        config.mark_submitted(
            self._exam.id,
            build_student_key(self._identity_data, self._token),
            student_label(self._identity_data, self._token),
        )
        memory = (
            self._answer_sheet.get_answers()
            if hasattr(self, '_answer_sheet') else {}
        )
        answers = config.resolve_submit_answers(
            memory, self._exam.id,
            build_student_key(self._identity_data, self._token),
        )
        # H10: sidecar DULU, baru jawaban — lihat catatan panjang di
        # `_save_answers`. Sidecar tanpa jawaban inert; jawaban tanpa
        # sidecar membocorkan jawaban ini ke siswa berikutnya.
        self._write_answers_owner("_auto_submit")
        config.save_answers(self._exam.id, answers)
        # C2: simpan payload yang SEDANG dikirim. `_cleanup_after_submit`
        # membandingkannya dengan isi disk sebelum menghapus apa pun dan
        # sebelum menampilkan halaman selamat — hanya viewer yang tahu
        # payload ini, disk tidak.
        self._submitted_payload = answers

        # 2. Presence: logout segera (TTL Redis); `complete` saat submit sukses.
        self._stop_presence(completed=False)

        # 3. Lepas kunci, lalu tukar layar ujian dengan layar pengumpulan.
        self._timer_widget.stop()
        self._stop_autosave()
        self._btn_submit.setEnabled(False)
        self._btn_submit.setText("Mengumpulkan...")
        if hasattr(self, "_answer_sheet"):
            # Sama seperti jalur manual: setelah submit dimulai, edit lebih
            # lanjut tidak pernah ikut terkirim -- lebih baik terkunci jelas.
            self._answer_sheet.setEnabled(False)
        self._cleanup_pdf_viewer()
        self._discard_pdf()
        # M1: `deactivate()` TIDAK dipanggil di sini. Melepasnya sekarang
        # membebaskan keyboard hook, WDA_MONITOR, dan ClipCursor untuk
        # SELURUH budget submit (retry sampai ~7 dtk + polling 202 sampai
        # ~77 dtk) — hampir dua menit tanpa pengawasan hanya karena antivirus
        # atau proxy lab sedang lambat. Pelepasan lockdown dipindah ke
        # `_on_auto_submit_done` (hasil sudah tiba), dan layar pengumpulan
        # yang tampil selama menunggu memakai proteksi capture.
        #
        # C1: `closed` yang ditembak close() BUKAN akhir alur (hasil
        # background belum tiba) — __main__ menahannya sampai `all_done`.
        # Karena itu viewer di-`hide()`, bukan `close()`: dengan `close()`
        # tidak ada satu pun top-level yang terlihat → `lastWindowClosed` →
        # `app.quit()` → thread submit di bawah mati sebelum jalan.
        self._auto_submit_pending = True
        # Jendela ujian disembunyikan di bawah → berhenti menaiakkannya
        # (dan mengunci pointer ke kotak yang tidak kelihatan) sampai hasil
        # tiba. Lihat `SecurityEnforcer.set_waiting_for_submit_result`.
        self._mark_waiting_for_submit(True)
        # H6: watchdog — kalau hasil background tidak pernah datang
        # (thread mati diam-diam, signal hilang), alur diselesaikan sendiri
        # alih-alih meninggalkan layar pengumpulan menggantung selamanya.
        self._start_autosubmit_watchdog()
        self._progress_ref = self._build_progress_window()
        # Helper fullscreen yang sama dengan jendela ujian dan halaman
        # selamat: `show()` → `showFullScreen()` → `setGeometry()`, supaya
        # layar pengumpulan benar-benar menutup taskbar.
        apply_fullscreen(self._progress_ref)
        # Proteksi capture SESUDAH fullscreen (bukan di `_build_progress_window`):
        # `SetWindowDisplayAffinity` disimpan per-HWND, jadi memasang sebelum
        # state akhir terbentuk hilang tanpa jejak.
        self._protect_progress_window()
        self.hide()

        # 4. Submit di background — thread MURNI: setelah viewer disembunyikan
        #    semua nilai di-capture sebagai argumen biasa, thread TIDAK
        #    menyentuh Qt (viewer bisa di-GC oleh _on_viewer_closed).
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
                self._device_label,
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

        Hasil akhir dilaporkan lewat `_sig_auto_submit_done` (sukses →
        halaman selamat; gagal → langsung `all_done`) — KECUALI bila
        dipanggil tanpa instance (test memanggil unbound dengan self=None),
        di mana tidak ada sinyal yang bisa di-emit.
        """
        try:
            resp = api.submit_with_retry(
                base_url, exam_id, name, number, sclass,
                answers, start_time, mac, identity,
                token=token,
            )
            if resp.status == "queued":
                if resp.job_id:
                    resp = api.poll_queued_result(
                        base_url, exam_id, token, mac, resp.job_id, identity,
                    )
                else:
                    # M7: antre TANPA job_id = tidak ada konfirmasi yang
                    # bisa di-poll — perlakukan sebagai GAGAL, jangan pernah
                    # tampilkan halaman hijau atas dasar 202 mentah.
                    log.warning(
                        "submit queued tanpa job_id untuk exam %s — "
                        "dianggap gagal", exam_id,
                    )
                    resp = SubmitResponse(
                        success=False,
                        status="queued",
                        message="Server mengantre jawaban tetapi tidak "
                        "memberikan konfirmasi (job_id kosong). Jawaban "
                        "tetap tersimpan — coba kumpulkan lagi.",
                    )
            if resp.success:
                # JANGAN menghapus jawaban yang bukan milik pengiriman ini.
                # Penjagaannya satu helper bersama supaya jalur "Kirim Lagi"
                # (`server_config._recovery_submit_thread`) memakai
                # perbandingan yang PERSIS sama — lihat `answers_match_disk`.
                #
                # Invariant: clear HANYA bila isi disk masih persis payload
                # yang baru saja dikonfirmasi server. Isi yang berbeda
                # berarti milik percobaan lain -- bukan urusan thread ini.
                #
                # `clear_answers_if_unchanged`, bukan `answers_match_disk`
                # + `clear_answers`: pasangan baca-hapus yang terpisah bisa
                # disela autosave percobaan baru DI ANTARA keduanya (H9).
                cleared = clear_answers_if_unchanged(exam_id, answers)
                if not cleared:
                    log.info(
                        "skip clear_answers: disk berisi jawaban percobaan "
                        "lain untuk exam %s (payload lama sudah durable)",
                        exam_id,
                    )
                try:
                    api.complete_exam(base_url, exam_id, token, mac)
                except Exception:
                    pass
                msg = resp.congrats_message or resp.message or "Jawaban berhasil dikumpulkan."
                notify.send_notification("EXAMVAN — Ujian Terkumpul", msg)
                ExamViewerWindow._emit_auto_submit_done(self, True, msg)
            else:
                notify.send_notification(
                    "EXAMVAN — Pengumpulan Gagal",
                    "Jawaban belum terkirim: " + (resp.message or "terjadi kesalahan") +
                    "\nBuka aplikasi dan pilih ujian ini untuk mengirim ulang.",
                    urgency="critical",
                )
                ExamViewerWindow._emit_auto_submit_done(
                    self, False, resp.message or "terjadi kesalahan")
        except Exception as e:
            notify.send_notification(
                "EXAMVAN — Pengumpulan Gagal",
                "Jawaban belum terkirim: " + str(e) +
                "\nBuka aplikasi dan pilih ujian ini untuk mengirim ulang.",
                urgency="critical",
            )
            ExamViewerWindow._emit_auto_submit_done(self, False, str(e))

    def _emit_auto_submit_done(self, ok: bool, msg: str) -> None:
        """Emit `_sig_auto_submit_done` bila ada instance Qt yang hidup.

        SELALU dipanggil unbound (`ExamViewerWindow._emit_auto_submit_done(
        self, ...)`), bukan `self._...`: test memanggil
        `_background_submit_thread` unbound dengan self=None (tanpa event
        loop/sinyal), dan `None._emit...` akan AttributeError SEBELUM
        guard di bawah sempat jalan. Di situ tidak ada yang bisa di-emit
        dan itu BUKAN error: jawaban/notifikasi sudah ditangani di atas.
        """
        try:
            sig = getattr(self, "_sig_auto_submit_done", None)
        except Exception:
            return
        if sig is None:
            return
        try:
            sig.emit(ok, msg)
        except Exception:
            log.debug("emit auto_submit_done gagal", exc_info=True)

    @pyqtSlot(bool, str)
    def _on_auto_submit_done(self, ok: bool, msg: str) -> None:
        """Slot GUI untuk hasil submit background (C1/H6).

        `closed` yang menembak saat viewer disembunyikan auto-submit DITAHAN
        __main__ selama `_auto_submit_pending`; flag dilepas di sini dan
        alur diselesaikan: sukses → halaman selamat (penutupannya
        menembak `all_done`); gagal → `all_done` langsung (recovery
        re-entry menawarkan "Kirim Lagi").

        URUTAN WAJIB: langkah berikutnya (halaman selamat / `all_done`)
        dijalankan TERLEBIH DAHULU, layar pengumpulan ditutup
        BELAKANGAN. `close()` pada top-level yang sedang terlihat menembak
        `lastWindowClosed`, dan kalau tidak ada window lain yang terlihat
        (Qt) menutup aplikasinya — bug C1 yang sama, dipindah ke ujung alur.

        H6: SELURUH badan dibungkus try/except + `finally`. Dulu
        `_close_progress_window()` adalah statement terakhir tanpa
        penjaga sama sekali, jadi satu exception dari langkah berikutnya
        (halaman selamat gagal dibangun, `all_done` → dialog gagal tampil)
        meninggalkan "Mengumpulkan jawaban…" di layar SELAMANYA: PyQt5
        melanjutkan setelah traceback, jadi itu zombie, bukan crash —
        `_auto_submit_pending` sudah False, tidak ada dialog, tidak ada
        tombol, satu-satunya jalan keluar Task Manager.

        Kalau langkah berikutnya ITSELF yang gagal, `all_done` (yang
        menampilkan dialog konfigurasi) tetap ditembakkan di handlers
        `except`: pilihan "tidak ada satu pun window terlihat" lebih buruk
        lagi, karena itu berarti aplikasi menutup dirinya sendiri tepat
        setelah jawaban sudah terkirim.
        """
        if self.__dict__.get("_auto_submit_finished"):
            log.warning(
                "hasil submit kedua diabaikan untuk exam %s — alur sudah "
                "selesai", getattr(self._exam, "id", "?"),
            )
            return
        self._auto_submit_finished = True
        self._auto_submit_pending = False
        self._stop_autosubmit_watchdog()
        # Layar pengumpulan sudah tidak jadi satu-satunya layar (dan
        # jendela ujian tetap tersembunyi sampai halaman selesai tampil),
        # jadi polling focus boleh kembali bekerja seperti biasa.
        self._mark_waiting_for_submit(False)
        # M1: lockdown baru dilepas di sini. Selama menunggu hasil, hook
        # keyboard / WDA_MONITOR / ClipCursor tetap aktif dan layar
        # pengumpulan terlindungi dari capture.
        if self._security is not None:
            try:
                self._security.deactivate()
            except Exception:
                log.warning("could not deactivate security (auto-submit)",
                            exc_info=True)
        handed_off = False
        try:
            # `deactivate()` di strict ME-SHOW window ujian lagi (HWND harus
            # dikembalikan ke siswa), dan viewer sudah diset `_submitted` —
            # jadi sembunyikan lagi supaya naskah ujian tidak muncul di atas
            # layar yang sedang dibaca.
            self.hide()
            # C2: sukses yang TERLAMBAT tidak boleh memunculkan halaman
            # selamat di atas layar percobaan berikutnya — jawaban di disk
            # sudah bukan milik payload yang dikirim. Yang dilakukan hanya
            # menyelesaikan alur supaya tidak ada window yang tertinggal.
            if ok and not self._disk_answers_replaced():
                congrats = self._show_congratulations(msg)
                congrats.page_closed.connect(lambda: self.all_done.emit())
            else:
                if ok:
                    log.info(
                        "halaman hasil dilewati: disk berisi jawaban "
                        "percobaan lain untuk exam %s", self._exam.id,
                    )
                self.all_done.emit()
            handed_off = True
        except Exception:
            log.exception(
                "langkah setelah submit auto-submit gagal untuk exam %s — "
                "dialihkan ke dialog konfigurasi", self._exam.id,
            )
            if not handed_off:
                try:
                    self.all_done.emit()
                except Exception:
                    # Tidak ada layar lain yang bisa ditampilkan: dialog
                    # konfigurasi pun gagal. Yang bisa dilakukan cuma
                    # memastikan tidak ada layar yang menggantung.
                    log.exception(
                        "all_done juga gagal — tidak ada layar lain untuk "
                        "ditampilkan",
                    )
        finally:
            # Halaman selesai sudah tampil, atau dialog konfigurasi sudah
            # ditampilkan oleh `all_done` → menutup layar pengumpulan
            # sekarang TIDAK bisa menutup aplikasi. Penolakan `closeEvent`
            # dari siswa dibuka di dalam `_close_progress_window`.
            self._close_progress_window()

    def _mark_waiting_for_submit(self, waiting: bool) -> None:
        """Beri tahu enforcer bahwa kita sedang/berhenti menunggu hasil.

        Selama menunggu, jendela ujian disembunyikan dan layar pengumpulan
        yang tampil — jadi `_poll_focus` tidak boleh lagi menaikkan HWND
        yang tidak terlihat (lihat
        `SecurityEnforcer.set_waiting_for_submit_result`).
        """
        if self._security is None:
            return
        mark = getattr(self._security, "set_waiting_for_submit_result", None)
        if not callable(mark):
            return
        try:
            mark(bool(waiting))
        except Exception:
            log.warning("tidak bisa menandai menunggu hasil submit",
                        exc_info=True)

    def _start_autosubmit_watchdog(self) -> None:
        """Pasang batas keras menunggu hasil submit background (H6).

        Kalau `_on_auto_submit_done` tidak pernah datang (thread mati
        diam-diam, signal hilang, proses disuspensi), layar pengumpulan
        akan menggantung selamanya. Batasnya `_AUTOSUBMIT_WATCHDOG_MS`
        (110 detik) — di atas anggaran retry + polling + notifikasi.
        """
        self._stop_autosubmit_watchdog()
        timer = QTimer(self)
        timer.setSingleShot(True)
        timer.timeout.connect(self._on_autosubmit_watchdog)
        timer.start(_AUTOSUBMIT_WATCHDOG_MS)
        self._autosubmit_watchdog = timer

    def _stop_autosubmit_watchdog(self) -> None:
        timer = self.__dict__.get("_autosubmit_watchdog")
        if timer is None:
            return
        self._autosubmit_watchdog = None
        try:
            timer.stop()
        except Exception:
            log.warning("could not stop auto-submit watchdog", exc_info=True)

    def _on_autosubmit_watchdog(self) -> None:
        """Batas keras habis: selesaikan alur seolah submit-nya gagal.

        Perlakuannya sama dengan hasil "gagal": jawaban sudah tersimpan di
        disk, jadi recovery re-entry tetap bisa mengirimnya lewat "Kirim
        Lagi" — sedangkan layar yang menggantung tidak menyimpan apa pun.
        """
        if not self._auto_submit_pending:
            return
        log.warning(
            "hasil submit background tidak pernah datang dalam %d ms — "
            "alur diselesaikan paksa (jawaban tetap ada di disk untuk "
            "'Kirim Lagi')", _AUTOSUBMIT_WATCHDOG_MS,
        )
        self._on_auto_submit_done(
            False,
            "Konfirmasi server tidak diterima dalam batas waktu. Jawaban "
            "tetap tersimpan di perangkat ini.",
        )

    def _do_submit(self) -> None:
        """Thread-safe submit gate. Only one submit runs at a time."""
        with self._submit_lock:
            if self._submitted or self._submitting:
                return
            self._submitting = True

        self._btn_submit.setEnabled(False)
        self._btn_submit.setText("Mengumpulkan...")

        # F1 fallback (mirror fix Android): saat deadline sudah lewat pada
        # re-entry (proses mati), timer time_up menembak SEBELUM restore
        # jawaban dari disk (QTimer.singleShot 500ms) → memori kosong → tanpa
        # fallback ini submit KOSONG menimpa jawaban asli dalam window grace
        # server (end_time + 60 dtk) dan clear_answers menghapusnya. Memori
        # tidak kosong → dipakai apa adanya (siswa mungkin baru mengubah
        # jawaban setelah auto-save terakhir).
        answers = config.resolve_submit_answers(
            self._answer_sheet.get_answers(), self._exam.id,
            build_student_key(self._identity_data, self._token),
        )

        # Flush payload yang PERSIS dikirim ke disk, sebelum thread jalan.
        # Dulu submit manual hanya mengandalkan autosave (debounce 500 ms):
        # proses mati di tengah polling terkonfirmasi (sampai ~77 dtk)
        # meninggalkan copy disk yang SEDIKIT LEBIH LAMA, dan recovery
        # "Kirim Lagi" lalu menimpa jawaban yang sudah durable dengan
        # payload basi itu. Jalur auto-submit sudah melakukan flush yang
        # sama (AutoSubmitF1FlushTest).
        # H10: sidecar pemilik (jawaban di disk milik percobaan ini, supaya
        # recovery re-entry tidak mengirimnya atas nama siswa lain) ditulis
        # SEBELUM jawaban — lihat catatan panjang di `_save_answers`.
        self._write_answers_owner("_do_submit")
        config.save_answers(self._exam.id, answers)
        # C2: sama seperti jalur auto-submit — payload yang dikirim dicatat
        # supaya `_cleanup_after_submit` bisa memastikan isinya masih milik
        # percobaan ini sebelum menghapus dan sebelum menampilkan halaman.
        self._submitted_payload = answers

        # Kunci lembar jawaban SELAMA submit berjalan.
        #
        # Tanpa ini, siswa bisa terus mengetik sampai 77+ detik (retry 7 dtk
        # + polling 202) padahal payload sudah snapshot: edit-nya tidak pernah
        # terkirim, dan saat sukses `clear_answers` menghapusnya dari disk
        # sementara `_save_answers` no-op karena `_submitted` -- hilang dari
        # DUA tempat sekaligus, padahal layar masih menampilkannya. Lembar
        # terbuka lagi di jalur gagal (lihat _on_submit_result).
        self._answer_sheet.setEnabled(False)

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
                self._device_label,
                self._identity_data,
                self._token,
            ),
            daemon=True,
        ).start()

    def _submit_thread(
        self, base_url, exam_id, name, number, sclass, answers, start_time, mac, identity,
        token,
    ) -> None:
        # SELALU emit _sig_submit_result, apa pun yang terjadi.
        #
        # Tanpa try/except, satu exception dari api (UnicodeDecodeError dari
        # body non-UTF-8, TimeoutError, ConnectionError) mematikan thread ini
        # tanpa sinyal apa pun. `_submitting` hanya di-reset di
        # `_on_submit_result`, jadi ia mengunci True SELAMANYA: tombol submit
        # mati, `closeEvent` menolak menutup (dijaga
        # `_submitted or _submitting`), dan deadline auto-submit ditolak
        # penjaganya. Satu-satunya jalan keluar: Task Manager -- dengan
        # jawaban belum terkirim.
        try:
            resp = api.submit_with_retry(
                base_url, exam_id, name, number, sclass, answers, start_time,
                mac, identity,
                # The server rejects a submit without X-Exam-Token outright
                # (exams.go:1016-1023). Without this every submit failed with
                # "Token tidak disertakan". Token diteruskan sebagai argumen
                # thread eksplisit — membaca self._token dari worker tidak
                # aman (window bisa di-GC setelah ditutup).
                token=token,
                on_retry=lambda attempt, total: self._sig_status.emit(
                    f"Submit gagal, percobaan {attempt}/{total}..."
                ),
            )

            # Async path (202): server hanya MENGANTRI jawaban — belum
            # durable. Poll /result sampai worker mengonfirmasi ("done")
            # sebelum menganggap sukses; copy lokal TIDAK boleh di-clear pada
            # 202 mentah (mirror Android: clearSavedAnswers hanya setelah
            # konfirmasi durable). Jika worker gagal -> resp.success=False ->
            # _on_submit_result menampilkan error dan TIDAK menghapus jawaban
            # (re-entry memulihkan dari disk).
            if resp.status == "queued":
                if resp.job_id:
                    self._sig_status.emit(
                        "Jawaban diterima server, menunggu konfirmasi...")
                    # `congrats_message` hanya ada di respons 202 ini;
                    # `/result` tidak pernah mengirimkannya. Tanpa
                    # meneruskannya, halaman selamat menampilkan teks bawaan
                    # dan pesan guru yang sengaja ditulis guru tidak pernah
                    # sampai ke siswa.
                    queued_congrats = resp.congrats_message or ""
                    resp = api.poll_queued_result(
                        base_url,
                        exam_id,
                        token,
                        mac,
                        resp.job_id,
                        identity,
                        initial_congrats=queued_congrats,
                    )
                else:
                    # M7: antre TANPA job_id = tidak ada konfirmasi yang
                    # bisa di-poll — perlakukan sebagai GAGAL, jangan pernah
                    # tampilkan halaman hijau atas dasar 202 mentah.
                    log.warning(
                        "submit queued tanpa job_id untuk exam %s — "
                        "dianggap gagal", exam_id,
                    )
                    resp = SubmitResponse(
                        success=False,
                        status="queued",
                        message="Server mengantre jawaban tetapi tidak "
                        "memberikan konfirmasi (job_id kosong). Jawaban "
                        "tetap tersimpan — coba kumpulkan lagi.",
                    )

            # Fix #2 (parity Android): tampilkan `congrats_message` custom
            # guru saat sukses (bukan hanya resp.message bawaan server).
            if resp.success:
                message = (resp.congrats_message or resp.message
                           or "Jawaban berhasil dikumpulkan.")
            else:
                message = resp.message
            self._sig_submit_result.emit(resp.success, message)
        except Exception as exc:  # noqa: BLE001
            log.warning("submit thread crashed", exc_info=True)
            self._sig_submit_result.emit(
                False,
                f"Gagal mengirim jawaban: {exc}\n\n"
                "Jawaban TIDAK hilang -- masih tersimpan di perangkat ini. "
                "Tekan 'Kumpulkan Jawaban' lagi, atau hubungi pengawas.",
            )

    # -------------------------------------------------------------------
    # Window events
    # -------------------------------------------------------------------
    @property
    def is_strict(self) -> bool:
        """True when this exam runs with the strictest lockdown.

        Read by tests and by anything that needs to know the tier. It is NOT
        what decides fullscreen: the exam window covers the whole screen in
        every level, so a medium or low exam is not something a student can
        shrink to desktop.
        """
        return self._exam.is_strict

    def _enforce_fullscreen(self) -> None:
        """Re-assert that this window still covers the whole screen.

        Two ways the exam can stop covering it, both seen in the field:

        * a later `showMaximized()` / `restore()` / `resize()` — the caller
          that does it does not know the exam is running;
        * a `setGeometry()` while the fullscreen state is still set. The state
          flag survives that, so `isFullScreen()` keeps saying True while the
          window actually sits in the work area with the taskbar showing.

        That second case is why this asks `covers_fullscreen()` (geometry)
        rather than `isFullScreen()` (state): with the state-only check the
        broken layout looked healthy and nothing ever repaired it.

        Applies at every security level — the report was "semua mode
        bermasalah", and a maximized exam window with a visible taskbar is not
        what any level is supposed to look like.
        """
        # Sesudah submit viewer ditutup dan halaman selamat tampil: jangan
        # fullscreen-kan viewer di atasnya lagi.
        if self._submitted:
            return
        if self._fullscreen_reasserting:
            return
        screen = self.screen() or QApplication.primaryScreen()
        if screen is None:
            return
        if self.isFullScreen() and covers_fullscreen(self, screen):
            return
        self._fullscreen_reasserting = True
        try:
            apply_fullscreen(self)
            # M-2: pasang ulang proteksi capture setelah perbaikan
            # fullscreen. Penyebab sebenarnya BUKAN `showFullScreen()` —
            # showFullScreen() tidak membuat ulang HWND; komentar lama di
            # sini salah dan memberi alasan meyakinkan untuk sesuatu yang
            # tidak terjadi. Pelaku sebenarnya adalah `setWindowFlags()` di
            # `SecurityEnforcer._activate_strict`: mengubah window flags
            # membuat Qt membuang HWND lama dan membuat yang baru, dan
            # afinitas display (WDA_MONITOR) disimpan per-HWND.
            #
            # Re-assert di sini tetap dipertahankan: belt-and-braces yang
            # murah (satu panggilan Win32 per perbaikan fullscreen) dan
            # sekarang juga menutupi kaden 500 ms
            # `reassert_capture_protection()` yang dipanggil `_poll_focus`
            # untuk medium DAN strict — dua tempat yang tidak harus saling
            # bergantung.
            if self._security is not None:
                self._security.reassert_capture_protection()
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

    def _schedule_fullscreen_repair(self) -> None:
        """Jadwalkan `_enforce_fullscreen` dari perubahan GEOMETRI.

        `changeEvent` hanya melihat perubahan STATE. Pindah dan resize polos
        tidak mengubah state apa pun, jadi keduanya pernah lolos tanpa satu
        perbaikan pun (terukur: `_enforce_fullscreen` dipanggil 0× sesudah
        `move()`, 0× sesudah `resize()`, 3× sesudah minimize/restore).

        Di medium/low Alt+Space juga tersedia — `keyPressEvent` hanya
        menelan `Qt.Key_Space` + Alt di strict dan tidak ada keyboard hook di
        bawah strict — jadi ini bukan concerns strict saja.

        Dijadwalkan lewat `QTimer.singleShot`, bukan dipanggil langsung:
        memperbaiki geometri dari dalam `moveEvent`/`resizeEvent` akan
        bertarung dengan WM yang sedang memindahkan/memperkecil jendela
        (di Windows drag yang sedang berjalan bisa dibatalkan). Jeda
        `_FULLSCREEN_REPAIR_DELAY_MS` juga menggabungkan banyak event
        geometri beruntun jadi satu perbaikan, dan membiarkan urutan
        `show()` yang pertama selesai apa adanya.
        """
        try:
            if self._submitted:
                return
            if self._fullscreen_reasserting:
                # Sedang memperbaiki — `apply_fullscreen` sendiri memicu
                # move/resize, jadi memperbaiki lagi hanya berputar.
                return
            if self._fullscreen_repair_scheduled:
                return
            self._fullscreen_repair_scheduled = True
            QTimer.singleShot(
                _FULLSCREEN_REPAIR_DELAY_MS,
                self._run_scheduled_fullscreen_repair,
            )
        except RuntimeError:
            # Objek PyQt yang belum diinisialisasi (`__new__` tanpa
            # `__init__`, seperti double di test) melempar RuntimeError
            # untuk SETIAP akses atribut. Lewati penjadwalan — bukan
            # alasanproteksi gagal. Pola yang sama dipakai `_flush_answers`.
            pass

    def _run_scheduled_fullscreen_repair(self) -> None:
        self._fullscreen_repair_scheduled = False
        try:
            self._enforce_fullscreen()
        except RuntimeError:
            # Jendela sudah dihancurkan sebelum timer-nya jatuh tempo.
            pass
        except Exception:
            log.warning("perbaikan fullscreen terjadwal gagal", exc_info=True)

    def moveEvent(self, event) -> None:
        super().moveEvent(event)
        self._schedule_fullscreen_repair()

    def resizeEvent(self, event) -> None:
        super().resizeEvent(event)
        self._schedule_fullscreen_repair()

    def closeEvent(self, event: QCloseEvent) -> None:
        # Already submitted — always allow close, bypass all guards
        with self._submit_lock:
            already_done = self._submitted
        if already_done:
            if self._security:
                self._security.deactivate()
            # `_cleanup_pdf_viewer` tidak pernah melempar (lihat helpernya),
            # tapi `_discard_pdf()` dan `closed.emit()` TIDAK boleh
            # bergantung pada cleanup itu sama sekali: kalau terlewat,
            # naskah ujian tertinggal di %TEMP% untuk pupil berikutnya dan
            # `__main__` tidak pernah tahu jendela ini sudah ditutup.
            self._cleanup_pdf_viewer()
            self._discard_pdf()
            self.closed.emit()
            event.accept()
            return

        # Medium and strict: a close attempt is an auto-submit, never an
        # exit. blocks_free_exit reads the canonical level, so the server's
        # "high" is covered here — previously this compared the raw string
        # against ("medium",) and a "Tinggi" exam fell through to the low
        # branch below, where a "Yes" answer closed the exam unsubmitted.
        #
        # M3: cabang ini WAJIB di SEBELUM latch. Latch ada untuk menahan
        # re-entrancy dialog, bukan untuk mem-veto auto-submit: begitu ada
        # satu exception yang lolos (lihat `finally` di bawah), latch yang
        # menggantung akan mengembalikan `event.ignore()` di sini — yaitu
        # menutup gate close→auto-submit pada medium/strict. Dengan
        # `end_time=None` tidak ada auto-submit berbasis timer, jadi satu-
        # nya jalan keluar tinggal Task Manager.
        #
        # Re-entranyi di cabang ini tetap aman TANPA latch: `_auto_submit`
        # → `_auto_submit_and_exit` memakai gate submit tunggal (lock +
        # `_submitted`), jadi closeEvent kedua tidak mengirim apa pun.
        if self._exam.blocks_free_exit:
            # Auto-submit on close attempt
            # _do_submit() handles its own _submitting guard under lock,
            # so we don't set it here.
            if self._submitted or self._submitting:
                event.ignore()
                return
            event.ignore()
            self._auto_submit()
            return

        # Guard against repeated closeEvent spam (re-entrant calls from
        # QMessageBox or self.close() during cleanup). Hanya jalur low-level
        # yang memakai latch: satu-satunya tempat di sini yang menjalankan
        # nested event loop (`QMessageBox.question`).
        if self._close_in_progress:
            event.ignore()
            return
        self._close_in_progress = True

        # Low mode: confirm close
        #
        # Guard sama seperti dialog submit. Tanpa ini dialog ini punya dua
        # countdown yang berjalan di belakangnya: timer ujian, dan focus
        # guard -- yang di low level belum aktif, tapi timer ujian saja
        # sudah cukup untuk menutup jendela ini dari dalam `closeEvent`.
        #
        # M3: SELURUH sisa badan dibungkus try/finally. Latch yang
        # menggantung adalah kondisi yang benar-benar mematikan: setiap
        # `closeEvent` berikutnya jadi `event.ignore()`, dan di medium/
        # strict itu menutup gate close→auto-submit — satu-satunya jalan
        # keluar yang tersisa cuma Task Manager. Selain itu exception yang
        # lolos dari sini pada PyQt5 5.15.10 TIDAK abort: ukurannya hanya
        # traceback ke stderr lalu alur dilanjutkan (diverifikasi ronde 8).
        # Dan pada build PyInstaller `--windowed` stderr dibuang, jadi
        # kegagalan diam-diam yang sebenarnya terlihat SISWA adalah dialog
        # keluar yang tidak pernah tampil — tombol "Keluar" tidak melakukan
        # apa pun tanpa penjelasan. Itu sebabnya path "kegagalan dialog"
        # harus ditangani di sini, bukan diteruskan.
        try:
            with self._modal_dialog_guard():
                _box = QMessageBox(
                    QMessageBox.Question,
                    "Keluar Ujian",
                    "Apakah yakin ingin keluar? Jawaban belum dikumpulkan.",
                    QMessageBox.Yes | QMessageBox.No,
                    self,
                )
                _box.setDefaultButton(QMessageBox.No)
                # Sama seperti dialog konfirmasi pengumpulan: teks dialog
                # yang dipakai siswa SELALU teks polos, supaya penambahan
                # pesan di masa depan tidak bisa membuka markup ke layarnya.
                _box.setTextFormat(Qt.PlainText)
                reply = _box.exec_()
            if reply != QMessageBox.Yes:
                event.ignore()
                return
            # Presence: siswa keluar tanpa submit — kirim logout (presence
            # habis via TTL Redis; tidak ada submit yang menghapusnya).
            self._stop_presence(completed=False)
            # M4: countdown ikut berhenti, bukan hanya autosave.
            # `_modal_dialog_guard` me-restart timer 1 Hz saat dialog ditutup
            # selama jendela masih terlihat — tanpa `stop()` di sini, jam
            # mundur terus berjalan pada jendela yang sudah tidak terlihat
            # dan `time_up` bisa menembak `_auto_submit` setelah siswa
            # keluar (jawaban terkirim pada batas waktu yang bukan lagi
            # batas waktu sebenarnya).
            self._timer_widget.stop()
            # Flush jawaban TERAKHIR sebelum autosave dimatikan.
            # Autosave punya debounce 500 ms dan flush 2 detik, jadi
            # ketikan di dalam jendela itu akan hilang tanpa pernah
            # menyentuh disk — di low tier tidak ada jalur submit yang
            # bisa menyelamatkannya, dan di PC lab jawaban itu hilang
            # permanen begitu proses ditutup.
            self._flush_answers()
            self._stop_autosave()
            if self._security:
                self._security.deactivate()
            self._cleanup_pdf_viewer()
            self._discard_pdf()
            self.closed.emit()
            with self._submit_lock:
                self._submitted = True
            event.accept()
        except Exception:
            # Kegagalan di tengah jalur keluar: jangan tutup ujian
            # diam-diam (siswa masih boleh mencoba lagi), dan jangan
            # swallow tanpa jejak di log.
            log.warning("jalur keluar low-level gagal", exc_info=True)
            event.ignore()
        finally:
            self._close_in_progress = False

    def keyPressEvent(self, event: QKeyEvent) -> None:
        # Jendela yang sudah submit tidak boleh menegakkan apa pun lagi:
        # Escape/PrintScreen akan tetap ditelan di atas halaman selamat.
        if self._submitted:
            super().keyPressEvent(event)
            return
        if self._exam.is_strict and self._security:
            # Admin exit backdoor: Ctrl+Shift+Alt+Q (tekan 3x, lalu password).
            #
            # Ketiga modifier itu harus HADIR (subset), bukan SAMA PERSIS.
            # `==` yang lama mematikan pintu darurat supervisor pada
            # modifier tambahan apa pun yang dilaporkan Qt (CapsLock/
            # NumLock sebagai group-switch di sebagian layout, IME aktif,
            # `KeypadModifier`) — dan keyboard hook Windows yang dipasang
            # strict memang menelan keydown Alt, jadi susunan modifier yang
            # sampai ke Qt bisa saja tidak lengkap.
            # Sifat keamanannya tidak berubah: tanpa KETIGANYA backdoor
            # tetap tidak terjangkau (lihat tests/test_r6_admin_exit_modifiers.py).
            _mods = event.modifiers()
            if (
                (_mods & _ADMIN_EXIT_MODIFIERS) == _ADMIN_EXIT_MODIFIERS
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

        # Tanpa guard, prompt password ini auto-submit dalam 3 detik --
        # artinya supervisor tidak pernah sempat mengetik, dan pintu
        # emergency justru mengirim jawaban pelajar.
        with self._modal_dialog_guard():
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
                # Tanpa guard, supervisor melihat "tidak dikonfigurasi"
                # dan 3 detik kemudian ujian SISWA justru terkumpul --
                # membalikkan tujuan admin exit.
                with self._modal_dialog_guard():
                    QMessageBox.warning(
                        self, "Tidak Diizinkan",
                        "Admin exit tidak dikonfigurasi.\n\n"
                        "Cara mengaktifkan:\n"
                        "• Isi kolom \"Password supervisor\" di halaman Password\n"
                        "  Admin Exit saat instalasi (halamannya selalu tampil),\n"
                        "  atau\n"
                        "• Tulis ulang file di\n"
                        "  %LOCALAPPDATA%\\EXAMVAN\\admin_password.txt,\n"
                        "  atau\n"
                        "• Set environment EXAMVAN_ADMIN_PASSWORD sebelum\n"
                        "  aplikasi jalan.\n\n"
                        "Tidak ada checkbox untuk ini — halaman passwordnya\n"
                        "selalu tampil, jadi cukup isi kolomnya.",
                    )
                return
            if not _password_matches(_ADMIN_PASSWORD, password):
                # Sama seperti di atas: supervisor salah ketik SEKALI dan
                # seluruh jawaban siswa terkirim.
                with self._modal_dialog_guard():
                    QMessageBox.warning(
                        self, "Akses Ditolak", "Password salah."
                    )
                return

            # Presence DIHENTIKAN di sini juga.
            #
            # Semua jalur keluar lain memanggil `_stop_presence`; yang ini
            # tidak, jadi `_heartbeat_timer` (60 s) tetap POST
            # `access-log heartbeat` dan dashboard pengawas menampilkan
            # siswa masih ONLINE setelah pengawas resmi mengeluarkannya.
            # WebSocket juga tidak pernah `disconnect()`, jadi ia
            # auto-reconnect lagi.
            self._stop_presence(completed=False)
            # M4: jalur ini dulunya tidak menyentuh SATU pun timer viewer.
            # Countdown 1 Hz tetap berjalan dan bisa menembak
            # `_auto_submit` → jawaban siswa terkirim beberapa detik setelah
            # pengawas menutup ujian; flush autosave 2 detik juga tetap
            # menulis ke disk SETELAH lockdown dilepas, dan bisa menimpa
            # jawaban percobaan berikutnya di PC lab yang sama.
            self._timer_widget.stop()
            # Sama seperti jalur keluar low-tier: flush jawaban terakhir
            # DULU sebelum autosave dimatikan. Supervisor menekan
            # Ctrl+Shift+Alt+Q, mengetik password, dan keluar — jawaban
            # yang diketik 300 ms sebelum itu tidak akan pernah tersimpan.
            self._flush_answers()
            self._stop_autosave()
            if self._security:
                self._security.deactivate()
            self._cleanup_pdf_viewer()
            self._discard_pdf()
            with self._submit_lock:
                self._submitted = True
            # closeEvent handles self.closed.emit() when _submitted
            self.close()
