"""Server configuration dialog — URL + token input, health check."""

from __future__ import annotations

import logging
import threading
from typing import Dict, Optional

from PyQt5.QtCore import Qt, QEventLoop, QTimer, pyqtSignal, pyqtSlot
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
    build_student_key_source,
    get_device_label,
    map_identity_to_standard,
    student_label,
)
from .congratulations import _sanitize_server_text
from .exam_viewer import answers_match_disk  # noqa: F401  (re-export untuk pengujian)

log = logging.getLogger(__name__)

# Alamat server default untuk deployment sekolah.
#
# Satu konstanta, bukan string yang disalin ke beberapa tempat: dipakai juga
# oleh `windows/README.md` dan dialog, supaya tidak bisa melenceng.
# Bentuk KANONIK (https://, tanpa trailing slash) karena `_on_connect`
# menormalkan input dengan asumsi itu.
DEFAULT_SERVER_URL = "https://examvan.my.id"

# Slot identitas yang benar-benar milik SATU siswa, jadi satu-satunya
# yang boleh jadi dasar "jawaban di disk ini punya kamu".
#
# `exam_number` satu-satunya: nama dipakai dua siswa berbeda di kelas
# berbeda, dipakai seluruh angkatan, dan token dipakai SELURUH kelas
# pada mode static-token. `_offer_resubmit_choice` sudah memakai aturan
# yang sama untuk modal "sudah terkumpul" — dua aturan berbeda untuk
# kasus yang sama hanya menghasilkan dua perilaku berbeda.
#
# Kunci yang BUKAN per-siswa bukan hanya tidak berguna sebagai bukti:
# ia sama untuk semua orang yang memakai PC itu, jadi perbandingan
# `owner_key != current_key` selalu lulus dan recovery milik siapa pun
# bisa dikirim atas nama siapa pun.
_PER_STUDENT_KEY_SOURCE = "exam_number"

# Batas menunggu worker "Kirim Lagi" melapor (detik). Worker ini bisa
# berjalan ~84 detik di produksi (retry 7 + polling 202), jadi batasnya
# longgar; tujuannya bukan memotong pengiriman yang sah, melainkan
# memastikan dialog TIDAK PERNAH menggantung permanen karena worker yang
# hilang (proxy buntu, proses dibunuh, koneksi yang membeku).
RECOVERY_WAIT_SECONDS = 180


def _present_result_page(
    *,
    server_url: str,
    exam_token: str,
    exam_name: str,
    identity: Dict[str, str],
    message: str,
    public_results: bool,
    on_page_closed,
):
    """Bangun + perlakukan halaman hasil, lalu PASANG pembersihannya.

    Ronde 7 (H1). Halaman hasil dibangun di beberapa tempat:

    * `ui/exam_viewer.py:_show_congratulations` — dipakai
      `_cleanup_after_submit` dan `_on_auto_submit_done`;
    * `_recovery_done_slot` di file ini.

    Site kedua sebelumnya punya `page_closed` WARNA, dan itu bukan
    detail: di jalur recovery tidak pernah ada `ExamViewerWindow`, jadi
    rantai `page_closed` → viewer menutup diri → `closed` →
    `__main__._after_viewer_gone` yang biasanya menjalankan
    `config.clear_identity()` dan menghidupkan lagi UI koneksi tidak
    pernah jalan. Akibatnya identitas siswa yang baru saja selesai tetap
    di config, dipakai mengisi form siswa berikutnya, dan karena
    `last_input.returnPressed` terikat ke submit, SATU Enter sudah cukup
    menjawab atas nama orang sebelumnya. `mark_submitted` juga tidak
    pernah dipanggil di sana.

    Karena itu "bangun halaman" dan "pasang apa yang terjadi setelah
    ditutup" harus SATU tempat. Site baru yang hanya memanggil
    `CongratulationsWindow(...)` akan gagal di test
    `tests/test_r7_recovery_page_cleanup.py` (yang menghitung site
    pembuatan halaman di file ini).

    CATATAN jujur soal cakupan: site-site di `exam_viewer.py` tidak bisa
    ikut memakai helper ini ronde ini — file itu di luar daftar edit, dan
    `server_config.py` meng-import `exam_viewer` di level modul, jadi
    mengimpor balik akan menjadi import melingkar. Yang bisa dilakukan
    sekarang: semua site di file ini lewat helper, dan kedua site
    `exam_viewer` memang sudah menyambungkan `page_closed` sendiri
    (`_cleanup_after_submit`). Menggabungkan semuanya perlu satu file
    bersama (mis. `ui/result_page.py`) — pekerjaan lanjutan.

    `on_page_closed` adalah callback yang dijalankan sekali saat siswa
    menutup halaman; ia harus melakukan pembersihan sesi (identitas,
    marker submit, UI koneksi). `identity` yang dipakai untuk itu adalah
    identitas yang benar-benar diproses, bukan apa pun yang kebetulan
    ada di config.
    """
    from .congratulations import CongratulationsWindow

    std = map_identity_to_standard(identity or {})
    congrats = CongratulationsWindow(
        server_url=server_url,
        exam_token=exam_token,
        exam_name=exam_name,
        student_name=str(std.get("student_name", "")),
        student_number=str(std.get("exam_number", "")),
        student_class=str(std.get("student_class", "")),
        congrats_message=message,
        public_results=public_results,
    )
    # WA_DeleteOnClose: Qt menghapus sendiri saat siswa menekan
    # "Selesai", dan referensinya ditahan oleh pemanggil supaya tidak
    # ter-GC saat slot ini kembali.
    congrats.setAttribute(Qt.WA_DeleteOnClose)
    congrats.show_fullscreen()
    # Dipasang begitu halaman ada, SEBELUM siswa sempat menekan
    # "Selesai": `closeEvent` tetap berjalan pada halaman yang sedang
    # shown, dan `page_closed` hanya dipancarkan sekali pada penutupan
    # pertama.
    if on_page_closed is not None:
        congrats.page_closed.connect(on_page_closed)

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
            "proteksi capture halaman hasil gagal — token ujian bisa "
            "terekam lewat PrintScreen"
        )
    return congrats


def _redact_key_for_log(key: str, source: str) -> str:
    """Bentuk yang AMAN untuk dicetak ke log.

    `build_student_key` bisa TURUN ke token ujian ketika tidak ada kolom
    identitas yang bisa dipakai (lihat `utils.build_student_key_source`).
    Pada mode static-token itu kredensial hasil SELURUH KELAS, dan `app.log`
    di PC lab tidak dibersihkan antar siswa — jadi mencetaknya berarti kelas
    berikutnya punya token-nya hanya dengan mengetik
    ``%USERPROFILE%`` + ``\\.config\\examvan\\app.log`` di Notepad.

    Yang dicetak cukup untuk diagnosis: `source` sudah ditulis sebagai
    argumen terpisah, jadi untuk `source == "token"` tidak ada yang perlu
    ditambah. Kunci yang BUKAN token (nama/nomor) tetap dicetak — itu PII,
    tapi bukan kredensial, dan sangat berguna saat menelusuri jawaban salah
    orang.
    """
    if source == "token":
        return "<token: tidak dicetak>"
    return repr(key)


class ServerConfigDialog(QDialog):
    """Initial dialog: enter server URL and exam token."""

    exam_selected = pyqtSignal(object, str, object)  # (Exam, server_url, identity_data)

    # Signals for thread-safe UI updates
    _sig_status = pyqtSignal(str, bool)      # (message, is_error)
    _sig_enable_btn = pyqtSignal()
    _sig_show_identity = pyqtSignal()
    _sig_recovery_available = pyqtSignal(object)  # Exam — jawaban belum terkirim
    _sig_recovery_done = pyqtSignal(str)          # (message) — kirim ulang sukses
    # (url, fingerprint lama, fingerprint baru) — sertifikat berubah.
    # Signal terpisah karena `_check_certificate_fingerprint` dipanggil dari
    # worker: QMessageBox hanya boleh dibangun di UI thread.
    _sig_certificate_changed = pyqtSignal(str, str, str)

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
        self._sig_certificate_changed.connect(self._certificate_changed_slot)

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
        # Warna kartu mengikuti `styles.app_theme_dark()`, bukan
        # tema sistem: kartu gelap di dalam jendela terang (atau
        # sebaliknya) adalah regresi butir 1 di
        # `tests/test_styles_dark_regression.py`.
        card.setStyleSheet("QWidget#loginCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
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

        # Catatan host. H3: URL tersimpan yang bukan server sekolah tetap
        # di-prefill (lab swakelola tidak boleh setiap pagi mengetik
        # ulang), tapi tanpa catatan ia tidak bisa dibedakan dari server
        # sekolah di mata siswa maupun pengawas — dan setelah URL itu
        # tercatat, token kelas berikutnya dikirim ke sana.
        self._server_url_note = QLabel("")
        self._server_url_note.setWordWrap(True)
        self._server_url_note.setAlignment(Qt.AlignCenter)
        self._server_url_note.setStyleSheet("font-size: 11px;")
        self._server_url_note.hide()
        card_layout.addWidget(self._server_url_note)
        self.input_url.textChanged.connect(self._refresh_server_url_note)

        # Token
        lbl_token = QLabel("Token Ujian")
        self.input_token = QLineEdit()
        self.input_token.setPlaceholderText("8 karakter (contoh: ABCD1234)")
        self.input_token.setMaxLength(8)
        # TAMPILKAN APA-ADANYA, bukan disamarkan. Token ujian bukan
        # password: itu 8 karakter yang tercetak di Amplop Lembar
        # Jawaban, sering diketik ulang oleh siswa, dan server MENOLAKnya
        # kalau satu karakter salah — huruf O vs angka 0 tidak bisa
        # dipastikan dari deretan titik, dan token juga harus tetap
        # terbaca dari layar ruang ujian. Masking tidak menambah
        # keamanan apa pun di sini: `text()` selalu mengembalikan nilai
        # aslinya, jadi alur validasi token tidak pernah bergantung
        # padanya.
        self.input_token.setEchoMode(QLineEdit.Normal)
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
        # `QLabel` default `Qt.AutoText`, dan label ini membawa DUA teks yang
        # datang dari server: `health.status` (`GET /api/health`) dan
        # `resp.message`/`resp.error` (`GET /api/exams/token/<t>`). Tanpa
        # `PlainText`, satu string dari server (atau halaman blok proxy)
        # bisa menyuntik markup ke layar siswa, dan `<img src="file://...">`
        # membuat `QTextDocument` membuka berkas lokal sinkron di thread GUI.
        # Sanitasi teksnya sendiri ada di `_set_status_slot`.
        self.lbl_status.setTextFormat(Qt.PlainText)
        card_layout.addWidget(self.lbl_status)

        outer.addWidget(card, alignment=Qt.AlignHCenter)

        # Spacer bottom
        outer.addStretch(2)


    def server_url_warning_text(self) -> str:
        """Catatan host yang sedang terlihat di bawah kotak URL."""
        return self._server_url_note.text()

    def _server_is_trusted(self, url: str) -> bool:
        """Host yang boleh dipakai tanpa konfirmasi ulang.

        Dua sumber, dan hanya dua:

        * `DEFAULT_SERVER_URL` — server sekolah, yang dituju semua
          instalasi;
        * `trusted_server_url` — host yang PERNAH dikonfirmasi pengawas
          di komputer ini, jadi lab swakelola tidak terkunci di dialog
          pada setiap peluncuran.

        Host lain harus dikonfirmasi lewat `_confirm_trusted_server`.
        """
        url = str(url or "").strip().rstrip("/")
        if url == DEFAULT_SERVER_URL:
            return True
        trusted = str(
            config.get("trusted_server_url", "") or ""
        ).strip().rstrip("/")
        return bool(trusted) and url == trusted

    def _refresh_server_url_note(self, *_args) -> None:
        """Tampilkan catatan kalau host di kotak bukan host percaya.

        Dipanggil dari `textChanged`, jadi catatan ini selalu soal host
        yang SEDANG diketik — bukan host yang tersimpan. Tanpa itu, URL
        lama ter-prefill tanpa memberitahu siapa pun bahwa host itu akan
        ditanyakan lagi.
        """
        url = self.input_url.text().strip()
        if url and not self._server_is_trusted(url):
            self._server_url_note.setStyleSheet(
                "font-size: 11px; color: #b45309;")
            self._server_url_note.setText(
                f"Server ini bukan server sekolah ({DEFAULT_SERVER_URL}). "
                "Pastikan dengan pengawas — host akan ditanyakan sebelum "
                "token ujian dikirim."
            )
            self._server_url_note.show()
        else:
            self._server_url_note.setText("")
            self._server_url_note.hide()

    def _confirm_trusted_server(self, url: str) -> bool:
        """True = boleh lanjut connects. False = sudah dijelaskan, jangan
        kirim apa pun.

        Dijalankan di UI thread (`_on_connect`), jadi dialognya boleh
        modal.

        H3: `_on_connect` dulu hanya menolak cleartext. Host HTTPS apa
        pun — termasuk impostor yang menyalin sertifikat asli — langsung
        dipakai, lalu `_connect_thread` menyimpannya, jadi token kelas
        siswa berikutnya terkirim ke sana.
        """
        if self._server_is_trusted(url):
            return True
        box = QMessageBox(self)
        box.setIcon(QMessageBox.Warning)
        box.setWindowTitle("Server Bukan Server Sekolah")
        box.setText(f"EXAMVAN akan mengirim token ujian ke:\n{url}")
        box.setInformativeText(
            f"Host ini bukan server sekolah bawaan ({DEFAULT_SERVER_URL}).\n\n"
            "Token ujian pada mode static adalah kredensial hasil SELURUH "
            "KELAS: server tujuan bisa membukanya, membaca identitas siswa, "
            "dan menerima semua jawaban kelas ini. Lanjutkan hanya kalau "
            "pengawas sudah memberi tahu server ini benar."
        )
        box.addButton("Hubungkan ke Server Ini", QMessageBox.YesRole)
        batal = box.addButton("Batal", QMessageBox.NoRole)
        # Default = Batal: satu Enter yang tidak sengaja pada dialog ini
        # berarti "kirim kredensial kelas ke host yang tidak saya kenal".
        box.setDefaultButton(batal)
        # `exec_()` mengembalikan StandardButton dari tombol yang diklik
        # (documented untuk QMessageBox) — sama seperti
        # `_offer_resubmit_choice`, supaya keputusannya tidak bergantung
        # pada `clickedButton()` yang kosong saat dialog ditutup dengan
        # Escape atau tombol tutup.
        reply = box.exec_()
        if reply != QMessageBox.Yes:
            log.warning(
                "koneksi ke server yang bukan server sekolah DITOLAK: %s", url,
            )
            # `_enable_connect_ui` mengosongkan label status, jadi
            # penjelasannya ditulis SETELAH itu.
            self._enable_connect_ui()
            self.lbl_status.setStyleSheet("color: #e53935;")
            self.lbl_status.setText(
                f"Koneksi dibatalkan: {url} bukan server sekolah. Hubungi "
                "pengawas, atau pakai server sekolah."
            )
            return False
        log.warning(
            "server yang dipakai bukan server sekolah: %s (disetujui)", url,
        )
        # Hanya disimpan kalau siswa memang memilih "Simpan URL & Token".
        # Kalau tidak, host tetap ditanyakan lagi pada peluncuran berikutnya —
        # konsisten dengan janji checkbox bahwa tidak ada yang ditulis.
        if self.chk_remember.isChecked():
            config.set("trusted_server_url", url)
        return True

    def _load_saved(self) -> None:
        """Isi form dari config, atau default server bila belum pernah diisi.

        `remember_url` akhirnya DICHIBAHKAN, yang sebelumnya tidak berlaku
        sama sekali: nilainya dibaca tapi tidak pernah dipakai sebagai
        syarat, sehingga URL dan token selalu di-prefill apa pun yang
        siswa pilih. Sekarang kontraknya jelas:

          dicentang   -> isi lagi di komputer ini (dan simpan lagi)
          tidak       -> jangan sentuh; form dikosongkan, dan token TIDAK
                        ditulis ke disk sama sekali

        Koreksi alasan yang lama (diberi dua kali di file ini):
        "`_xor_obfuscate` memakainya sebagai kunci decode jawaban yang
        tersimpan" TIDAK benar sejak kunci jawaban dipisah ke
        `_OBFUSCATE_KEY` (lihat docstring fungsi itu sendiri). Token
        tidak lagi ikut campur, jadi tidak ada satu pun fitur di disk yang
        membutuhkannya.

        Yang TETAP benar dan perlu diketahui: `answers_*.dat` versi LAMA
        memang hanya bisa didecode dengan mencoba token yang pernah dipakai
        (`_legacy_token_candidates`), jadi "Kirim Lagi" untuk berkas
        sekecil itu perlu pengawas. Berkas jawaban versi sekarang tidak
        bergantung pada token sama sekali.

        URL yang tersimpan TETAP di-prefill walau tidak dipercaya (H3):
        lab swakelola yang sah tidak boleh mengetik ulang tiap pagi. Yang
        membuatnya jujur adalah catatan terlihat dari
        `_refresh_server_url_note` dan konfirmasi di `_confirm_trusted_server`
        — bukan dengan menyembunyikan URL yang sudah pernah dipakai.
        """
        remember = bool(config.get("remember_url", True))
        self.chk_remember.setChecked(remember)
        if not remember:
            self._refresh_server_url_note()
            return

        url = config.get("server_url", "") or DEFAULT_SERVER_URL
        token = config.get("exam_token", "")
        if url:
            self.input_url.setText(url)
        if token:
            self.input_token.setText(token.upper())
        self._refresh_server_url_note()

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

        # Gerbang kepercayaan host, SEBELUM ada yang dikirim. Belum ada satu
        # pun request: penolakan terjadi sebelum worker dijalankan, jadi
        # token kelas tidak pernah menyentuh host yang tidak disetujui.
        if not self._confirm_trusted_server(url):
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
                # Urutan sengaja: `enable` dulu, status belakangan.
                # `_enable_connect_ui` mengosongkan label status, jadi
                # emit terbalik membuat pesan kegagalan lenyap sebelum
                # sempat dibaca (dan `--windowed` membuang log).
                self._sig_enable_btn.emit()
                self._sig_status.emit(
                    f"Gagal terhubung ke server:\n{health.status}", True)
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
                self._sig_enable_btn.emit()
                self._sig_status.emit(msg, True)
                return

            # Config ditulis setelah token lookup berhasil — lebih dulu
            # berarti URL ada di disk untuk server yang bahkan tidak
            # mengenal token itu.
            #
            # `remember_url` sekarang BERARTI: tidak dicentang = URL dan
            # token tidak ditulis sama sekali. Alasan lama ("token harus
            # tersimpan karena `_xor_obfuscate` memakainya sebagai kunci
            # decode") sudah tidak benar sejak kunci jawaban dipisah ke
            # `_OBFUSCATE_KEY`, dan `_xor_obfuscate` sendiri menyatakan
            # itu. Yang membaca token dari config adalah jalur "Kirim
            # Lagi", dan jalur itu sekarang menerimanya dari UI thread
            # (`_show_recovery`), bukan dari disk.
            #
            # URL ikut di-gate (audit ronde 10): dulu komentar di atas
            # menjanjikan "URL tidak ditulis" tapi `server_url` selalu
            # ditulis, jadi janji privasinya tidak ditepati.
            if remember_url:
                config.set("server_url", url)
            config.set("remember_url", remember_url)
            config.set("exam_token", token if remember_url else "")

            # Tidak ada gerbang "sudah dikerjakan" di titik mana pun.
            # Lihat _offer_pending_recovery() untuk penjelasan policies-nya.

            self._exam = resp.exam
            self._server_url = url

            # Show identity dialog on UI thread via signal
            self._sig_show_identity.emit()
        except Exception as exc:  # noqa: BLE001 — jaring apapun harus sampai ke UI
            log.warning("connect thread crashed", exc_info=True)
            self._sig_enable_btn.emit()
            self._sig_status.emit(
                f"Koneksi gagal: {exc}\nCoba lagi atau hubungi pengawas.", True
            )

    def _check_certificate_fingerprint(self, url: str, fingerprint: str) -> None:
        """Bandingkan fingerprint sertifikat dengan kunjungan sebelumnya.

        Disimpan per URL (kunci `cert_fp_<url>`).

        Kunjungan PERTAMA ke host yang sudah dipercaya (server sekolah, atau
        host yang dikonfirmasi `_confirm_trusted_server`) langsung
        disimpan — tidak ada yang bisa dikonfirmasi karena belum ada
        pembanding, dan `_on_connect` sudah meminta persetujuan sebelum
        host asing bisa dipakai. Host yang TIDAK dipercaya tidak
        pernah mencapai titik ini: `_connect_thread` baru jalan setelah
        konfirmasi, jadi sidik jari impostor tidak pernah di-pin diam-diam.

        Kunjungan berikutnya yang BERBEDA = peringatan yang harus dilihat
        orang, bukan baris log: build `--windowed` membuang stderr, jadi
        "sudah dicatat di log" berarti "tidak terjadi apa pun yang dilihat
        pemantau". Dialog-nya dikirim lewat signal supaya tampil di UI
        thread (QMessageBox dari worker adalah pelanggaran threading Qt),
        dan TIDAK memblokir: rotasi sertifikat yang sah adalah hal biasa
        di sekolah, dan memblokir semuanya mematikan app tepat saat
        sekolah sedang memutar sertifikatnya. Yang berubah adalah
        penglihatannya, bukan policy-nya.
        """
        try:
            key = f"cert_fp_{url}"
            previous = str(config.get(key, "") or "")
            if previous and previous != fingerprint:
                log.warning(
                    "certificate fingerprint changed for %s: %s -> %s",
                    url, previous[:16], fingerprint[:16],
                )
                self._sig_certificate_changed.emit(
                    url, previous, fingerprint,
                )
                return
            if not previous:
                config.set(key, fingerprint)
        except Exception:
            # Pemantauan fingerprint tidak boleh menggagalkan koneksi.
            log.debug("fingerprint check failed", exc_info=True)

    @pyqtSlot(str, str, str)
    def _certificate_changed_slot(
        self, url: str, previous: str, current: str,
    ) -> None:
        """Tampilkan perubahan sertifikat di UI thread (lihat di atas)."""
        box = QMessageBox(self)
        box.setIcon(QMessageBox.Warning)
        box.setWindowTitle("Sertifikat Server Berubah")
        box.setText(f"Sertifikat {url} berbeda dari kunjungan terakhir.")
        box.setInformativeText(
            "Sertifikat lama : " + (previous[:32] or "(tidak diketahui)") + "\n"
            "Sertifikat baru : " + (current[:32] or "(tidak diketahui)") + "\n\n"
            "Bisa jadi rotasi sertifikat yang sah, bisa juga proxy yang "
            "mencurigakan di jaringan sekolah. Hubungi pengawas sebelum "
            "melanjutkan."
        )
        box.setStandardButtons(QMessageBox.Ok)
        box.exec_()

    # --- Slots (run on UI thread, connected via signals) ---

    def closeEvent(self, event) -> None:
        """X / Alt+F4: terima close event, JANGAN panggil `reject()`.

        Bawaan `QDialog.closeEvent` memanggil `reject()`. Sejak `reject()`
        di bawah memanggil `self.close()` (supaya `lastWindowClosed` menutup
        aplikasi, bukan sekadar `hide()` yang meninggalkan zombie tanpa
        jendela), keduanya saling mengunci: `close()` -> `closeEvent` ->
        `reject()` -> `close()` -> saat kembali, `isVisible()` masih True ->
        bawaan men-`ignore()` event-nya. Hasilnya tombol X seolah mati dan
        siswa membunuh proses lewat Task Manager. Menerima event di sini
        memutus lingkaran itu.
        """
        event.accept()

    def reject(self) -> None:
        # Escape: tutup jendela betulan, bukan hanya hide(). Dialog ini
        # satu-satunya window (quitOnLastWindowClosed); hide diam-diam
        # meninggalkan aplikasi zombie tanpa jendela. `closeEvent` kita
        # menerima event-nya, jadi tidak ada rekursi.
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
        # SEBELUMNYA teks mentah. Dua pemanggil terbesarnya mengirim teks
        # server apa adanya: `health.status` dari `/api/health` dan
        # `resp.message`/`resp.error` dari `/api/exams/token/<t>`. Dengan
        # `Qt.PlainText` di `lbl_status` markup sudah tidak dieksekusi, tapi
        # karakter tersembunyi (Cf) dan bidi override masih bisa
        # menyamarkan isi pesan di mata siswa. Sanitizer yang DIPAKAI
        # (`congratulations._sanitize_server_text`) sama dengan yang dipakai
        # `exam_viewer`/`identity_dialog`/`waiting_approval`/`answer_sheet`:
        # satu implementasi, bukan lima.
        color = "#e53935" if is_error else "#2e7d32"
        self.lbl_status.setStyleSheet(f"color: {color};")
        self.lbl_status.setText(_sanitize_server_text(msg))

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
            # Menolak TIDAK boleh jadi jalan buntu tanpa penjelasan. Tawaran
            # tetap ditolak (lihat `_offer_pending_recovery`), tapi siswa
            # sekarang tahu jawabannya masih ada di disk, kenapa tidak
            # dikirim, dan apa yang bisa dia lakukan: mencoba lagi lewat
            # tombol Hubungkan, atau tanya pengawas.
            self._enable_connect_ui()
            self.lbl_status.setStyleSheet("color: #b45309;")
            self.lbl_status.setText(
                "Jawaban lama masih tersimpan di komputer ini dan TIDAK "
                "dikirim. Tekan 'Hubungkan' lagi untuk mencoba kirim ulang; "
                "jawaban itu akan tertimpa kalau kamu mulai mengerjakan "
                "ujian, jadi hubungi pengawas kalau tidak bisa dikirim."
            )
            return

        self.btn_connect.setEnabled(False)
        self.lbl_status.setStyleSheet("color: #2e7d32;")
        self.lbl_status.setText("Mengirim ulang jawaban...")
        # Token di-capture di UI thread, sama seperti identitas. Alasannya
        # bukan hanya widget: dengan checkbox "Simpan" tidak dicentang,
        # token TIDAK ada di config sama sekali (lihat `_connect_thread`),
        # jadi membacanya dari sana membuat kirim ulang selalu 401.
        self._run_recovery_worker(
            exam, dict(self._recovery_identity), self._current_token()
        )

    def _run_recovery_worker(
        self, exam, identity: Dict[str, str], token: str
    ) -> None:
        """Jalankan worker kirim ulang, lalu TUNGGU sampai ia melapor.

        Kenapa menunggu (ronde 8). Dulu method ini hanya memulai thread
        dan langsung kembali dalam ~1,7 ms, jadi `_offer_pending_recovery`
        mengembalikan False dan `_show_identity_dialog` melanjutkan —
        membersihkan identitas, sementara worker masih hidup. Kalau ia
        selesai beberapa detik kemudian, `_recovery_done_slot` membangun
        halaman hasil fullscreen di atas dialog yang sedang dipakai dan
        `_on_result_page_closed` menulis marker submit + `clear_identity()`
        untuk ujian yang alirnya masih berjalan. Di antara keduanya siswa
        bisa saja sudah mengetik identitas dan token untuk ujian BERBEDA.

        Menunggu di dalam nested `QEventLoop` (bukan `thread.join()`):
        UI tetap menggambar dan tetap bisa ditutup, jadi menunggu tidak
        sama dengan membekukan dialog. `watchdog` menutup jalan keluar
        kalau worker benar-benar hilang — tanpa itu, "tunggu sampai
        melapor" berubah menjadi kunci macet untuk siswa.

        Setiap sinyal "sudah selesai" yang mungkin dipakai worker
        (`_sig_recovery_done`, `_sig_status`, `_sig_enable_btn`) ikut
        menghentikan tunggu ini, dan semua koneksi DIBUANG setelahnya:
        kalau tidak, slot yang sudah keluar tetap menyalakan `loop.quit()`
        untuk waiter berikutnya.
        """
        loop = QEventLoop(self)
        # Dua flag, bukan satu: `watchdog` juga menghentikan loop, tapi
        # "berhenti karena worker melapor" dan "berhenti karena batas
        # waktu habis" punya akibat yang BERBEDA — yang kedua tidak boleh
        # diperlakukan sebagai keberhasilan diam-diam.
        reported = {"value": False}

        def _on_reported(*_args):
            if reported["value"]:
                return
            reported["value"] = True
            loop.quit()

        def _on_timeout():
            loop.quit()

        watchdog = QTimer(self)
        watchdog.setSingleShot(True)
        watchdog.timeout.connect(_on_timeout)

        signals = (
            self._sig_recovery_done, self._sig_status, self._sig_enable_btn,
        )
        for signal in signals:
            signal.connect(_on_reported)
        watchdog.start(int(RECOVERY_WAIT_SECONDS * 1000))
        try:
            threading.Thread(
                target=self._recovery_submit_thread,
                args=(exam, identity, token),
                daemon=True,
            ).start()
            if not reported["value"]:
                loop.exec_()
        finally:
            watchdog.stop()
            for signal in signals:
                try:
                    signal.disconnect(_on_reported)
                except (TypeError, RuntimeError):
                    pass
        if not reported["value"]:
            # Worker tidak melapor sampai batas tunggu: jawabannya belum
            # diketahui sudah tersimpan di server, dan memulai percobaan
            # baru di atas pengiriman yang masih berjalan adalah
            # dual-attempt yang justru ingin dicegah.
            log.warning(
                "kirim ulang untuk exam %s tidak melapor dalam %d detik — "
                "ujian tidak dilanjutkan, jawaban tetap di disk",
                getattr(exam, "id", "?"), int(RECOVERY_WAIT_SECONDS),
            )
            self._enable_connect_ui()
            self.lbl_status.setStyleSheet("color: #e53935;")
            self.lbl_status.setText(
                "Pengiriman ulang tidak memberi jawaban dalam batas waktu. "
                "Jawaban tetap tersimpan di komputer ini — hubungi pengawas."
            )

    def _recovery_submit_thread(
        self, exam, identity: Dict[str, str], token: str = ""
    ) -> None:
        """Kirim ulang jawaban tersimpan di background — TIDAK menyentuh Qt.

        `identity` dan `token` DITERIMA SEBAGI ARGUMEN (lihat
        `_show_recovery`), bukan dibaca ulang dari config: nilainya harus
        PERSIS yang ditawarkan di dialog recovery, apa pun yang terjadi
        pada config selama proses — termasuk `remember_url` yang dikosongkan
        tokennya.

        `config.get("exam_token", ...)` tetap jadi fallback supaya call
        site lama (dan test yang memanggil worker langsung) tidak ikut
        rusak, tapi bukan sumber utama lagi.
        """
        token = str(token or config.get("exam_token", "") or "").strip().upper()

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
                # `config.clear_answers_if_unchanged`, bukan
                # `answers_match_disk()` lalu `clear_answers()`: pasangan
                # baca-then-hapus itu raced (H9) — di antara pembacaan dan
                # unlink, autosave percobaan berikutnya bisa menimpa disk
                # beserta sidecar owner-nya, lalu keduanya terhapus. Helper
                # di `config` memegang `_answers_lock` yang sama dengan
                # `save_answers`, jadi tidak ada yang bisa menimpanya di
                # tengah transaksi. Dipanggil juga oleh jalur auto-submit
                # dan cleanup manual, jadi ketiga call site satu aturan.
                if config.clear_answers_if_unchanged(exam.id, answers):
                    pass
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
                # `enable` emitted LEBIH DAHULU: `_enable_connect_ui`
                # mengosongkan label status, jadi pesan kegagalan yang
                # emitted setelahnya akan langsung dihapus dan siswa
                # tidak pernah tahu kenapa kirim ulang gagal.
                self._sig_enable_btn.emit()
                self._sig_status.emit(
                    "Pengiriman ulang gagal: "
                    + (resp.message or "terjadi kesalahan")
                    + ". Jawaban tetap tersimpan — coba lagi.",
                    True,
                )
        except Exception as e:
            self._sig_enable_btn.emit()
            self._sig_status.emit(
                "Pengiriman ulang gagal: " + str(e)
                + ". Jawaban tetap tersimpan — coba lagi.",
                True,
            )

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
        #
        # Identitas yang DIPAKAI worker recovery (audit HIGH H13) — bukan
        # apa pun yang kebetulan ada di config saat halaman ini tampil, dan
        # dipakai juga untuk penandaan submit saat halaman ditutup.
        identity = dict(
            self._recovery_identity or config.get_identity_data() or {}
        )
        # Token yang tervalidasi, BUKAN `config.get("exam_token")`: config
        # masih menyimpan token dari percobaan sebelumnya kalau siswa
        # mengetik ulang token di kotak yang sama (recovery dibaca ulang
        # pada connect berikutnya), dan link yang ditampilkan harus milik
        # ujian yang benar-benar dikerjakan — bukan milik token basi.
        exam_token = self.validated_token
        # Ronde 7 (H1): bangun + pasang pembersihannya lewat SATU helper.
        # Site ini sebelumnya tidak punya `page_closed`, dan di jalur
        # recovery tidak pernah ada ExamViewerWindow — jadi rantai
        # `page_closed` -> `closed` -> `_after_viewer_gone` yang biasanya
        # memanggil `config.clear_identity()` tidak pernah jalan.
        # Konsekuensinya berurutan: identitas siswa yang baru selesai tetap
        # tinggal di config dan dipakai mengisi form siswa berikutnya
        # (`last_input.returnPressed` terikat ke submit, jadi SATU Enter
        # cukup menjawab atas namanya), dan `mark_submitted` tidak pernah
        # dipanggil sehingga `_offer_resubmit_choice` selalu keluar lebih
        # awal tanpa memberi tahu apa pun.
        self._congrats_ref = _present_result_page(
            server_url=self._server_url,
            exam_token=exam_token,
            exam_name=getattr(self._exam, "name", ""),
            identity=identity,
            message=msg,
            public_results=bool(
                getattr(self._exam, "public_results", True)
            ),
            on_page_closed=lambda: self._on_result_page_closed(
                identity, exam_token
            ),
        )

        # M-token-leak: kotak token dikosongkan begitu ujian benar-benar
        # selesai. Pada jalur ini tidak pernah ada ExamViewerWindow yang
        # dibuat, jadi pembersihan `__main__` (`_on_viewer_closed`) tidak
        # pernah jalan — dan kotak yang sudah ter-prefill adalah satu
        # klik (atau satu Enter) dari memakai token kelas lagi di PC lab
        # yang dipakai bersama.
        #
        # `config["exam_token"]` TIDAK lagi dibiarkan begitu: sejak kunci
        # jawaban dipisah ke `_OBFUSCATE_KEY` (lihat `config._xor_obfuscate`)
        # tidak ada fitur yang membutuhkannya, jadi dengan "Simpan" tidak
        # dicentang token tidak pernah masuk disk sama sekali. Kalau
        # dicentang, `remember_url` yang mengatur apakah token boleh
        # di-prefill pada kunjungan berikutnya.
        try:
            self.input_token.clear()
        except Exception:
            log.debug("could not clear token input", exc_info=True)

    def _on_result_page_closed(
        self, identity: Dict[str, str], exam_token: str
    ) -> None:
        """Kontrak setelah halaman hasil DITUTUP — sama seperti jalur ujian.

        Ronde 7 (H1). Jalur recovery tidak pernah membuat
        `ExamViewerWindow`, jadi tidak ada rantai pembersih
        `page_closed` -> viewer menutup diri -> `closed` ->
        `__main__._after_viewer_gone` TIDAK ADA di sini. Yang di-copy adalah
        kontrak yang di sana: identitas dibersihkan, marker submit ditulis,
        dan UI koneksi dikembalikan.

        * identitas: tanpa ini `_prefill_identity_if_same_exam` mengisi
          form siswa berikutnya dengan nama/nomor/kelas siswa sebelumnya,
          dan `last_input.returnPressed` yang terikat ke `_on_submit`
          membuat SATU Enter cukup menjawab atas namanya;
        * marker submit: pasangan `build_student_key`/`student_label` yang
          SAMA dengan `_cleanup_after_submit` di `exam_viewer`, supaya
          penandaan dari kedua jalur saling mengenal. Tanpa ini
          `_offer_resubmit_choice` selalu `return True` di baris pertama
          dan siswa berikutnya tidak diberi tahu bahwa percobaan itu ada;
        * UI koneksi: tombol + kedua input + guard in-flight. Tanpa ini
          PC lab terkunci setelah satu recovery.

        Idempoten: `page_closed` hanya dipancarkan sekali, tapi
        `CongratulationsWindow` bisa `close()` dua kali, jadi tetap aman
        untuk dipanggil berulang.
        """
        try:
            config.mark_submitted(
                self._exam.id if self._exam is not None else 0,
                build_student_key(identity, exam_token),
                student_label(identity, exam_token),
            )
        except Exception:
            log.warning("gagal menulis marker submit", exc_info=True)
        # `clear_identity()` membersihkan identitas DAN konteksnya sekaligus
        # (lihat config.clear_identity), jadi tidak perlu `set
        # ("identity_context", {})` terpisah di sini.
        config.clear_identity()
        self._enable_connect_ui()

    def _current_token(self) -> str:
        """Token yang dipakai flow ini, tanpa pernah menyentuh widget.

        `validated_token` sengaja dibungkus try/except: sebagian test
        (dan beberapa jalur error) membangun dialog tanpa constructor,
        jadi membaca atribut yang belum ada tidak boleh menjatuhkan
        gerbang yang sedang menjaga jawaban siswa.
        """
        try:
            return str(self.validated_token or "").strip().upper()
        except Exception:
            return ""

    @staticmethod
    def _owner_key_is_per_student(owner: Dict[str, str]) -> bool:
        """Bisa nggak sidecar itu membuktikan pemiliknya satu siswa?

        `config.save_answers_owner` menyimpan `student_label(identity,
        token)` yang selalu berbentuk `"<slot>=<nilai>"` atau
        `"token (identitas kosong)"`. Label yang menandai slot bersama —
        atau label yang tidak bisa dibaca sama sekali — tidak bisa jadi
        bukti kepemilikan: kunci yang sama bukan bukti siapa pun.

        Perbandingan kunci di `_offer_pending_recovery` tetap jalan; yang
        ini hanya memastikan data sidecar dan kunci yang dibandingkan
        memang bicara tentang satu siswa.
        """
        label = str(owner.get("label", "") or "").strip()
        if not label or "=" not in label:
            return False
        return label.split("=", 1)[0].strip() == _PER_STUDENT_KEY_SOURCE

    def _refuse_recovery(self, reason: str) -> None:
        """Jawaban di disk TIDAK ditawarkan — dengan penjelasan jujur.

        Dua hal yang harus benar di sini: siswa diberi tahu (menekan
        tombol lalu tidak terjadi apa-apa adalah kegagalan, bukan
        kebijakan), dan identitas pemilik tidak ikut terbocorkan — itu
        bukan urusannya siapa yang menjawab tadi.
        """
        self.lbl_status.setStyleSheet("color: #e53935;")
        self.lbl_status.setText(
            "Ada jawaban lama di komputer ini, tapi identitas ujian ini "
            f"tidak punya kolom yang bisa memastikan itu jawabanmu "
            f"({reason}). File-nya tetap tersimpan dan TIDAK dikirim; "
            "hubungi pengawas kalau kamu yakin itu jawabanmu."
        )

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
          sendiri; pelanggaran hanya dilog.

        Kunci yang SAMANYA bukan bukti (ronde 8, H2)
        --------------------------------------------
        Membandingkan kunci saja tidak cukup, karena kunci bisa jatuh ke
        nilai yang bukan milik satu orang. `build_student_key_source`
        mengembalikan `(token, "token")` untuk ujian yang tidak punya kolom
        nomor/nama/kelas yang memetakan — dan itu bukan kasus langka:
        kunci field di webui dibangun dari label yang hanya huruf kecil
        dan angka, jadi label non-Latin menjadi `field_1`, `field_2`, ...
        dan `map_identity_to_standard` tidak mengenali satu pun.

        Di konfigurasi seperti itu semua siswa memakai satu token, jadi
        `owner_key != current_key` SELALU benar dan tidak pernah menahan
        apa pun. Menawarkan jawaban siswa A kepada siswa B bukan hanya
        salah soal privasi: jawaban A akan tercatat atas nama B.

        Karena itu kunci siswa yang sedang diketik WAJIB berasal dari slot
        per-siswa (`_PER_STUDENT_KEY_SOURCE`), dan sidecar pemiliknya
        harus bisa membuktikan hal yang sama. Menolak tawaran BUKAN
        menolak ujian:
        `_offer_pending_recovery` tetap mengembalikan True, siswa boleh
        masuk dan mengerjakan ulang, dan berkasnya tidak pernah dihapus.
        Yang hilang hanya "Kirim Lagi" — dan itu memang satu-satunya hal
        yang tidak bisa dilakukan dengan benar tanpa bukti pemilik.

        Fail-open yang lama ("owner tidak dikenal → tawarkan") DIHAPUS,
        hanya untuk kunci yang bukan per-siswa: tanpa sidecar tidak ada
        data apa pun tentang pemilik, jadi tidak ada yang bisa
        dibuktikan. Yang tetap fail-open adalah jalur tanpa jawaban
        tersimpan sama sekali (tidak ada apa pun yang perlu dipulihkan).
        """
        assert self._exam is not None
        if config.load_answers(self._exam.id):
            owner = config.load_answers_owner(self._exam.id)
            current_key = build_student_key(identity, self._current_token())
            _, source = build_student_key_source(
                identity, self._current_token()
            )
            if source != _PER_STUDENT_KEY_SOURCE:
                # Tidak bisa dibuktikan milik siapa pun. Tidak ditawarkan,
                # tidak dikirim, tidak dihapus — dan siapa pun pemiliknya
                # TIDAK ikut disebut ke pengetik sekarang (bukan
                # urusannya).
                log.warning(
                    "recovery untuk exam %s tidak ditawarkan: kunci "
                    "identitas sekarang turun ke %s (kunci=%s), bukan "
                    "satu siswa — semua siswa di PC ini akan mendapat "
                    "kunci yang sama",
                    self._exam.id, source,
                    _redact_key_for_log(current_key, source),
                )
                self._refuse_recovery(f"kunci identitas: {source}")
                return True
            if owner is not None:
                owner_key = str(owner.get("student_key", "") or "")
                if not self._owner_key_is_per_student(owner):
                    log.warning(
                        "recovery untuk exam %s tidak ditawarkan: label "
                        "pemilik tidak menandai satu siswa (label=%r)",
                        self._exam.id, owner.get("label", ""),
                    )
                    self._refuse_recovery("pemilik tidak dapat dipastikan")
                    return True
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
        gagal, dan pengaman kepemilikannya sudah ada di
        `_offer_pending_recovery`. Menoffer "kirim ulang" berdasarkan file
        akan membuka overwrite terhadap baris yang sudah benar.

        Ronde 7 (H3) — modal HANYA untuk kunci yang benar-benar satu siswa
        ------------------------------------------------------------------
        `build_student_key` bisa MENURUN ke nama lalu ke kelas lalu ke
        token. Config madrasah yang paling biasa — `nama`, `kelas`,
        `agama`, `jenis_kelamin`, tanpa kolom nomor ujian — menghasilkan
        kunci `'ahmad'` untuk siapa pun bernama Ahmad, jadi siswa 7 dan
        siswa 19 memakai satu kunci yang sama. Tanpa pemeriksaan sumber,
        modal ini dipertanyakan ke orang yang salah: "Jawaban untuk
        identitas ini sudah tercatat di perangkat ini" adalah tuduhan
        salah orang, dan `_cleanup_after_submit` juga menulis marker di
        bawah kunci yang sama.

        Jadi sumber kunci diperiksa lebih dulu (`build_student_key_source`):
        hanya `exam_number` yang unik per siswa, jadi hanya itu yang boleh
        jadi dasar modal. Fallback lain di-`log` dan dilewati — lebih baik
        tidak memberi tahu daripada memberi tahu orang yang salah, dan ini
        tetap BUKAN pemblokiran: siswa tetap boleh masuk.

        Kalau ujiannya memang tidak punya kolom nomor maupun nama, level log
        diturunkan ke INFO: tidak ada yang hilang, konfigurasinya memang
        begitu sejak awal. Kalau ada kolom yang SEHARUSNYA jadi nomor tapi
        tidak termapping, itu WARNING-worthy — itulah konfigurasi yang
        membuat kunci degrade di tempat yang tidak diharapkan.
        """
        assert self._exam is not None
        try:
            attempt, source = build_student_key_source(
                identity, self.validated_token
            )
            if source != "exam_number":
                collects = self._exam_collects_student_identifier()
                log.log(
                    logging.WARNING if collects else logging.INFO,
                    "kunci marker submit untuk exam %s turun ke %s "
                    "(kunci=%s) — modal 'sudah terkumpul' tidak "
                    "dipertanyakan karena kunci itu bukan satu siswa: "
                    "%s",
                    self._exam.id, source,
                    _redact_key_for_log(attempt, source),
                    "ujian punya kolom nomor/nama yang bisa dipakai"
                    if collects else
                    "ujian memang tidak punya kolom nomor/nama",
                )
                return True
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

    def _exam_collects_student_identifier(self) -> bool:
        """True bila ujian punya kolom nomor ujian ATAU nama yang terpetakan.

        Hanya untuk memilih level log di `_offer_resubmit_choice`: kalau
        config-nya memang tidak punya salah satu, kunci yang degrade bukan
        kesalahan pemetaan dan INFO sudah cukup. Kalau config-nya ADA kolom
        identitas tapi kunci tetap turun ke nama/kelas, itu WARNING-worthy.

        Dihitung dengan `map_identity_to_standard` yang sama — nilainya
        dibuat non-blank semua supaya hanya NAMA KUNCI yang berperan.
        """
        fields = getattr(self._exam, "identity_fields", None) or []
        probe = {f.key: "1" for f in fields if getattr(f, "key", "")}
        if not probe:
            return False
        std = map_identity_to_standard(probe)
        return bool(std.get("exam_number") or std.get("student_name"))

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
