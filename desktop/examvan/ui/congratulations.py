"""Halaman selamat setelah submit — parity Android `CongratulationsActivity`.

Bukan lagi `QDialog` pop-up di atas jendela ujian: kini ini HALAMAN penuh
(`QMainWindow`) yang menggantikan jendela ujian di layar, ditampilkan
fullscreen supaya siswa jelas-jelas melihat bahwa sesi ujian sudah selesai
-- bukan melihat sebuah kotak dialog kecil yang seolah-olah masih bagian
dari ujian.

Kenapa clipboard dibersihkan otomatis
-------------------------------------
PC lab dipakai bersama, dan `exam_token` adalah kredensial seluruh kelas
pada mode static. Commit sebelumnya menutup kebocorannya dari tiga sisi
(storage origin tidak menerima header token, `config.json` 0600, kunci
jawaban tidak lagi mengikuti token). Menyalin URL yang memuat token ke
clipboard dan meninggalkannya mengembalikan kebocoran itu lewat pintu
paling mudah.

`SecurityEnforcer` memang menyapu clipboard tiap 10 detik, tapi hanya
selama ujian berjalan — dan `_cleanup_after_submit` sudah memanggil
`deactivate()` sebelum layar ini tampil. Jadi tanpa pembersih eksplisit,
link itu bertahan di clipboard mesin sampai ada yang menimpanya.

Karena itu: hitung mundur 30 detik, dan clipboard hanya dikosongkan kalau
isinya MASIH link yang kita buat — supaya tidak menghapus sesuatu yang
siswa sendiri salin setelahnya.

Kenapa modalness DIHAPUS
------------------------
Dialog modal bersarang di atas `ExamViewerWindow` membuat halaman ini
menjadi "pop-up": terasa seperti bagian ujian yang masih berjalan,
countdown di bar atas tetap terlihat di belakangnya, dan tombol X-nya
mengambang di atas bottom bar "Kumpulkan Jawaban" yang seharusnya sudah
tidak relevan. Sebagai halaman non-modal terpisah, jendela ujian ditutup
DAHULU sebelum halaman ini tampil, sehingga yang dilihat siswa adalah
SATU layar bersih: ucapan guru, identitas, dan link hasilnya.

Halaman ini sengaja TIDAK fullscreen-terkunci: siswa sudah selesai
mengerjakan, tidak ada lagi yang perlu dijaga. Ia tampil fullscreen
secara default agar terasa seperti "halaman akhir", tetapi tetap bisa
di-alt-tab kalau siswa ingin menyalin link ke browser pilihannya.
"""

from __future__ import annotations

import logging
import re
import time
import unicodedata
from typing import Optional

from PyQt5.QtCore import Qt, QTimer, pyqtSignal
from PyQt5.QtGui import QFont, QFontMetrics, QPixmap
from PyQt5.QtWidgets import (
    QApplication,
    QFrame,
    QHBoxLayout,
    QLabel,
    QMainWindow,
    QPushButton,
    QScrollArea,
    QSizePolicy,
    QStyle,
    QVBoxLayout,
    QWidget,
)

from .. import APP_VERSION
from ..utils import build_result_link

log = logging.getLogger(__name__)

# Berapa lama link hasil boleh tinggal di clipboard setelah disalin.
CLIPBOARD_CLEAR_SECONDS = 30

# Berapa kali clipboard dicoba baca/kosongkan lagi setelah satu kegagalan
# sebelum saruan (countdown) dilepas. Clipboard yang MATI permanen —
# display manager belum start, Wayland tidak mengizinkan aplikasi
# membaca selection milik proses lain — tidak boleh membuat halaman ini
# mencoba tanpa henti; setelah batas, pembersih terakhir yang tersisa
# adalah `closeEvent`/`hideEvent`, yang memakai anggaran baru.
_CLIPBOARD_READ_RETRIES = 5

# Teks tombol saat clipboard tidak berhasil dibaca/dikosongkan. WAJIB
# berbeda dari "Copy Link": kalau teksnya sama, siswa mengira token sudah
# hilang padahal masih ada di clipboard PC lab.
_CLIPBOARD_UNCLEARED_TEXT = "Copy Link (gagal dibersihkan)"

# Batas PANJANG teks server (pesan guru, nama ujian). Tanpa batas,
# teks dari server bisa memaksa Qt menghitung layout untuk karakter
# yang tidak akan pernah dibaca siswa di layar lab. Batas panjang ini
# sendiri tidak membatasi TINGGI apa pun -- tinggi dijepit oleh
# `_SERVER_TEXT_MAX_LINES` di bawah.
_SERVER_TEXT_LIMIT = 2000

# Batas TINGGI yang dirender, dalam baris. Batas panjang di atas tidak
# accomplish apa pun terhadap tinggi yang dirender: 9 baris pesan guru
# saja sudah membuat tombol "Selesai" keluar dari layar 1024x600, dan 2000
# karakter satu paragraf membungkus jadi puluhan baris. Yang diukur di
# sini adalah tinggi setelah word-wrap pada lebar TERBURUK (kartu di lebar
# minimumnya).
_SERVER_TEXT_MAX_LINES = 6

# Lebar minimum kartu. Dipakai sekali untuk minimum kartu DAN minimum
# jendela: dua angka yang terpisah pasti akan berselisih lagi.
#
# 320, bukan 420: minimum jendela lama (360) sudah dikunci test
# (`tests/test_congratulations.py::test_minimum_size_is_small`), dan
# angka itu sendiri bisa dipertahankan hanya kalau kartu boleh lebih
# sempit darinya. Yang penting keduanya hidup berdampingan: pada
# minimum yang dideklarasikan, kartu muat di viewport dan TIDAK ada
# scrollbar horizontal — dan di layar 1024x600 kartu tetap selebar
# maksimumnya (560), jadi tampilan tidak berubah sama sekali.
CARD_MIN_WIDTH = 320

# Tinggi minimum jendela. SENGJA longgar: isi kartu dibungkus scroll
# area justru untuk layar kecil, jadi memaksa tinggi penuh hanya
# membuat jendela lebih besar dari layar. Yang dijaga hanya satu: tinggi
# minimum yang DIUMUMKAN harus sama dengan yang dipasang
# (`test_minimum_size_is_small` mengunci 280).
_WINDOW_MIN_HEIGHT = 280

# Margin kiri/kanan kartu (lihat `card_layout.setContentsMargins`).
_CARD_H_MARGIN = 40


# Override/isolate dua arah (bidi): bisa membalik urutan tampil atau
# menyembunyikan bagian label (mis. menyamarkan teks di sebelah tombol).
_BIDI_CHARS = frozenset(
    "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"
)

# Kontrol whitespace yang sah di QLabel (baris baru, tab) — dipertahankan.
_WHITESPACE_CONTROLS = frozenset("\t\n\r")


def _sanitize_server_text(text: object, limit: int = _SERVER_TEXT_LIMIT) -> str:
    """Bersihkan teks dari server sebelum dirender di QLabel.

    Membuang karakter kontrol tak terlihat (kategori Unicode Cc, Cf, Co,
    Cs — kecuali whitespace sah) dan karakter bidi override/isolate, lalu
    memotong sampai `limit`. Bersama `setTextFormat(Qt.PlainText)` di
    label, ini memastikan teks guru tidak bisa menyuntik HTML/Qt rich-text
    atau memanipulasi arah tampil label.
    """
    cleaned = []
    for ch in str(text or ""):
        if ch in _WHITESPACE_CONTROLS:
            cleaned.append(ch)
            continue
        cat = unicodedata.category(ch)
        if cat in ("Cc", "Cf", "Co", "Cs"):
            continue
        if ch in _BIDI_CHARS:
            continue
        cleaned.append(ch)
    return "".join(cleaned)[:limit]


def _clean_server_base(server_url: object) -> str:
    """Bersihkan BASE server SEBELUM link hasil dibangun.

    Sama seperti `_sanitize_server_text`, tapi tanpa batas panjang dan
    tanpa mempertahankan whitespace: memotong base URL menghasilkan URL
    yang tidak bisa dibuka, dan baris baru di dalam URL tidak pernah sah.

    Yang menentukan di sini adalah KAPAN pembersihan terjadi. Dulu
    sanitasi diterapkan ke teks gabungan `f"Link hasil: {url}"`, sedangkan
    `_result_url` — yang justru disalin ke clipboard — tidak pernah
    disentuh. Akibatnya apa pun yang dibuang sanitizer hilang dari LAYAR
    saja: ZWSP di `server_url` berarti label menampilkan
    `https://examvan.my.id/ABCD1234` sementara clipboard berisi
    `https://exam\u200bvan.my.id/ABCD1234`, dan URL lebih dari 2000
    karakter membuat token terpotong dari label. Membersihkan base lebih
    dulu membuat clipboard dan layar tidak mungkin berbeda.
    """
    kept = [
        ch for ch in str(server_url or "")
        if ch not in _WHITESPACE_CONTROLS
        and unicodedata.category(ch) not in ("Cc", "Cf", "Co", "Cs")
        and ch not in _BIDI_CHARS
    ]
    return "".join(kept).strip()


# Dua baris kosong (atau lebih) beruntun, boleh dikelilingi whitespace.
_BLANK_LINE_RUN = re.compile(r"[ \t]*\r?\n[ \t]*(?:\r?\n[ \t]*)+")

# Batas iterasi pemotongan: pemotongan selalu menghapus karakter, jadi
# ini hanya jaring pengaman, bukan syarat yang diharapkan terjadi.
_MAX_CLAMP_PASSES = 400


def _clamp_server_message(
    text: str,
    max_lines: int = _SERVER_TEXT_MAX_LINES,
    wrap_width: int = CARD_MIN_WIDTH - 2 * _CARD_H_MARGIN,
) -> str:
    """Ratakan baris kosong, lalu potong sampai tinggi yang dirender muat.

    Dua sebab tinggi yang berbeda, jadi dua tahap:

    * baris kosong beruntun (guru mengetik Enter berulang di
      `<textarea maxlength="500">`, server hanya `TrimSpace`) menambah
      tinggi tanpa menambah informasi;
    * satu paragraf panjang membungkus jadi banyak baris, jadi batas
      karakter saja tidak membatasi apa pun.

    Penghitungan tinggi memakai lebar TERBURUK: kartu pada lebar
    minimumnya, dikurangi margin kartu. Dengan begitu tombol "Selesai"
    tetap terlihat di layar 1024x600 bahkan kalau kartu melebar atau
    font sistem lebih besar dari asumsi.

    Teks yang terpotong diberi tanda "…" supaya tidak terlihat utuh
    padahal tidak — dan supaya guru/pengawas tahu pesannya perlu
    diperpendek.
    """
    collapsed = _BLANK_LINE_RUN.sub("\n\n", text).strip()
    lines = collapsed.split("\n")
    if len(lines) > max_lines:
        collapsed = "\n".join(lines[:max_lines]).rstrip()

    passes = 0
    while collapsed and passes < _MAX_CLAMP_PASSES:
        passes += 1
        if _rendered_line_count(collapsed, wrap_width) <= max_lines:
            break
        cut = max(1, int(len(collapsed) * 0.9))
        head = collapsed[:cut]
        space = head.rfind(" ")
        if space > 0:
            head = head[:space]
        collapsed = head.rstrip()

    if collapsed != text.strip():
        collapsed = (collapsed.rstrip() + "…") if collapsed else "…"
    return collapsed


def _rendered_line_count(text: str, width: int, pixel_size: int = 15) -> int:
    """Berapa baris yang benar-benar dirender QLabel untuk `text`.

    Perkiraan konservatif: font default aplikasi pada ukuran yang sama
    dengan `font-size` label pesan, dan lebar terburuk yang mungkin
    dipakai. Exactness tidak diperlukan — yang dibutuhkan adalah angka
    yang tidak pernah terlalu kecil, supaya penjepitan tidak melepaskan
    teks yang ternyata tidak muat.
    """
    font = QFont()
    font.setPixelSize(pixel_size)
    fm = QFontMetrics(font)
    height = fm.boundingRect(
        0, 0, max(1, width), 100 * fm.height(), Qt.TextWordWrap, text,
    ).height()
    return max(1, -(-height // max(1, fm.lineSpacing())))


_DEFAULT_CONGRATS = (
    "Jawabanmu sudah berhasil dikumpulkan. Terima kasih telah mengerjakan "
    "ujian ini dengan jujur."
)


class CongratulationsWindow(QMainWindow):
    """Layar selesai: pesan guru, nama ujian, identitas, copy link hasil.

    Halaman penuh yang berdiri sendiri (bukan dialog modal di atas jendela
    ujian). Layout memakai `styles.SECURITY_COLORS` dan kartu identitas yang
    sama dengan jendela ujian supaya konsisten.
    """

    # Ditutupnya halaman ini adalah pemicu langkah BERIKUTNYA di alur
    # submit: ExamViewer menutup dirinya saat sinyal ini menembak, bukan
    # sebelumnya -- kalau viewer ditutup lebih dulu, `__main__` langsung
    # menampilkan ServerConfigDialog untuk siswa berikutnya DI ATAS halaman
    # selamat yang belum selesai dibaca siswa sekarang.
    page_closed = pyqtSignal()

    def __init__(
        self,
        *,
        server_url: str,
        exam_token: str,
        exam_name: str = "",
        student_name: str = "",
        student_number: str = "",
        student_class: str = "",
        congrats_message: Optional[str] = None,
        # H6: False bila guru mematikan publikasi nilai (exam.public_results).
        # Default True = perilaku lama bila kunci absen di respons.
        public_results: bool = True,
        parent: Optional[QWidget] = None,
    ) -> None:
        super().__init__(parent)
        self._public_results = bool(public_results)
        # H6: guru mematikan publikasi nilai → TIDAK ADA link sama sekali,
        # bukan link yang tombol salinnya disembunyikan. Menyembunyikan
        # tombol saja tidak menutup apa pun: token tetap tercetak di
        # label ini (dan di layar apa pun yang memfoto halaman), padahal
        # di mode static-token itu kredensial hasil SELURUH KELAS.
        # `_result_url` yang kosong membuat `_copy_btn` mati, label jatuh
        # ke pesan jujur, dan tidak ada satu pun QString di halaman yang
        # memuat token.
        self._result_url = (
            build_result_link(_clean_server_base(server_url), exam_token)
            if self._public_results
            else ""
        )
        self._copied_at: Optional[float] = None
        # Jangkar jam dinding saat menyalin (lihat `_tick`). Dipisah dari
        # `_copied_at` karena yang mengukur jendela waktu harus jam yang
        # tidak bisa dimundurkan, sedangkan jam dinding hanya dipakai
        # untuk menangkap lompatan waktu (suspend / koreksi NTP).
        self._copied_wall_at: Optional[float] = None
        self._cleared = False
        self._closed = False
        # Berapa kali scrubbing clipboard gagal berturut-turut. Disimpan
        # di widget, bukan di variabel lokal, karena keputusannya harus
        # bertahan di antara `_tick` — dan harus di-reset setiap kali
        # siswa menyalin ulang (percobaan baru).
        self._clear_failures = 0

        self.setWindowTitle("EXAMVAN — Selesai")

        central = QWidget()
        outer = QVBoxLayout(central)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.setSpacing(0)
        outer.addStretch(2)

        card = QWidget()
        card.setObjectName("congratsCard")
        card.setMaximumWidth(560)
        card.setMinimumWidth(CARD_MIN_WIDTH)
        # Warna kartu mengikuti `styles.app_theme_dark()`, bukan
        # tema sistem: kartu gelap di dalam jendela terang (atau
        # sebaliknya) adalah regresi butir 1 di
        # `tests/test_styles_dark_regression.py`.
        card.setStyleSheet(
            "QWidget#congratsCard { background-color: #313244; "
            "border: 1px solid #45475a; border-radius: 12px; }"
        )
        card_layout = QVBoxLayout(card)
        card_layout.setContentsMargins(40, 36, 40, 28)
        card_layout.setSpacing(12)

        # Ikon sukses DIGAMBAR style Qt (SP_DialogApplyButton), bukan glyph
        # emoji: glyph tergantung font sistem -- di lingkungan tanpa font
        # emoji ia menjadi kotak kosong (tofu), dan momen paling penting
        # bagi siswa tidak boleh bergantung pada keberuntungan font.
        # Tidak ada fallback apa pun kalau style tidak menyediakan
        # pixmap; lihat catatan di bawah label.
        icon_label = QLabel()
        icon = self.style().standardIcon(QStyle.SP_DialogApplyButton)
        pm = icon.pixmap(48, 48)
        icon_label.setAlignment(Qt.AlignCenter)
        if not pm.isNull():
            icon_label.setPixmap(pm)
        # Tanpa pixmap sistem, label sengaja dikosongkan: fallback glyph
        # emoji ("\u2705") menjadi kotak kosong (tofu) di mesin tanpa font
        # emoji — momen paling penting bagi siswa tidak boleh bergantung
        # pada keberuntungan font.
        card_layout.addWidget(icon_label)

        title = QLabel("Jawaban Berhasil Dikumpulkan")
        title.setAlignment(Qt.AlignCenter)
        title.setWordWrap(True)
        # Minimum eksplisit 1 dengan alasan yang sama seperti label link
        # di bawah: tanpa ini `minimumSizeHint` QLabel memakai lebar kata
        # terpanjang, dan judulah yang menentukan minimum layout kartu
        # (356 px pada font 22 px + margin 80) — jadi minimum jendela
        # bergantung pada font sistem, bukan pada angka minimum kartu.
        title.setMinimumWidth(1)

        title.setStyleSheet("font-size: 22px; font-weight: 700; color: #16a34a;")
        card_layout.addWidget(title)

        self._exam_badge = QLabel(_clamp_server_message(
            _sanitize_server_text(exam_name.strip() or "Ujian")))
        self._exam_badge.setTextFormat(Qt.PlainText)
        self._exam_badge.setAlignment(Qt.AlignCenter)
        self._exam_badge.setWordWrap(True)
        # Warna teks badge DIWARISI dari tema (bukan abu-abu hardcode):
        # abu #64748b di kartu gelap kontrasnya ~2.8:1 -- di bawah ambang
        # WCAG untuk teks 13px. Di tema terang maupun gelap, warna label
        # bawaan QSS selalu dirancang terbaca di atas kartu.
        self._exam_badge.setStyleSheet(
            "font-size: 13px;"
            "background: rgba(148, 163, 184, 0.15);"
            "border-radius: 10px; padding: 5px 12px;"
        )
        card_layout.addWidget(self._exam_badge)

        # `congrats_message` adalah `<textarea maxlength="500">` yang
        # hanya di-`TrimSpace` server, jadi 15 baris kosong di tengah
        # pesan adalah masukan yang sah — dan 9 baris saja sudah
        # mendorong tombol "Selesai" keluar dari layar 1024x600. Batas
        # 2000 karakter tidak membatasi tinggi apa pun; yang membatasi
        # adalah `_clamp_server_message` (baris kosong diratakan, tinggi
        # dijepit).
        message = _clamp_server_message(
            _sanitize_server_text(
                (congrats_message or "").strip() or _DEFAULT_CONGRATS))
        self._congrats = QLabel(message)
        self._congrats.setTextFormat(Qt.PlainText)
        self._congrats.setWordWrap(True)
        self._congrats.setAlignment(Qt.AlignCenter)
        self._congrats.setStyleSheet("font-size: 15px;")
        card_layout.addWidget(self._congrats)

        card_layout.addSpacing(6)

        self._identity_box = QFrame()
        self._identity_box.setObjectName("congratsIdentity")
        self._identity_box.setStyleSheet(
            "QWidget#congratsIdentity { background: rgba(148, 163, 184, 0.08);"
            "border: 1px solid rgba(148, 163, 184, 0.25); border-radius: 10px; }"
        )
        identity_layout = QVBoxLayout(self._identity_box)
        identity_layout.setContentsMargins(16, 12, 16, 12)
        identity_layout.setSpacing(6)
        for label, value in (
            ("Nama", student_name),
            ("Nomor Ujian", student_number),
            ("Kelas", student_class),
        ):
            # Field kosong DILEWATI, bukan ditampilkan dengan label kosong:
            # itu terlihat seperti data yang gagal dimuat.
            if not str(value or "").strip():
                continue
            row = QHBoxLayout()
            key = QLabel(label)
            key.setTextFormat(Qt.PlainText)
            # Kunci identitas mewarisi warna tema -- alasan sama dengan
            # badge di atas: abu-abu hardcode tidak cukup kontras di kartu
            # gelap, dan tema sudah memberi warna yang benar.
            key.setStyleSheet("font-size: 13px;")
            # Diklem dengan tinggi yang sama seperti pesan guru: nilai
            # ini juga label multi-baris, dan nama yang ditempel tanpa
            # spasi tidak boleh membuat minimum layout kartu meledak
            # (lihat catatan minimum pada label link di bawah).
            val = QLabel(_clamp_server_message(
                _sanitize_server_text(str(value).strip())))
            val.setTextFormat(Qt.PlainText)
            # Nilai panjang (nama ganda, kelas gabungan) membungkus, bukan
            # mendorong kartu melebar: word-wrap + melebar mengisi baris.
            val.setWordWrap(True)
            val.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Preferred)
            # Minimum eksplisit 1 dengan alasan yang sama seperti label
            # link: nama yang ditempel tanpa spasi tidak boleh membuat
            # minimum layout kartu meledak.
            val.setMinimumWidth(1)
            val.setStyleSheet("font-size: 13px; font-weight: 600;")
            row.addWidget(key)
            row.addStretch(1)
            row.addWidget(val)
            identity_layout.addLayout(row)
        # Kartu identitas tanpa field sama sekali tidak dirender: frame
        # kosong di layar "selesai" terlihat seperti widget yang gagal.
        if identity_layout.count() > 0:
            card_layout.addWidget(self._identity_box)

        card_layout.addSpacing(6)

        link_row = QHBoxLayout()
        # Label memakai `_result_url` APA ADANYA: yang tampil di layar dan
        # yang masuk clipboard harus string yang sama, tanpa syarat.
        # Sanitasi diterapkan ke BASE-nya sebelum link dibangun (lihat
        # `_clean_server_base`), bukan ke teks gabungan ini — menerapkannya
        # di sini dulu berarti apa pun yang dibuang hilang dari layar saja
        # (ZWSP, atau pemotongan 2000 karakter yang menghapus token dari
        # label), dan siswa menyalin URL yang tidak bisa dia buka).
        self._link_label = QLabel(
            f"Link hasil: {self._result_url}" if self._result_url
            else "Link hasil tidak tersedia (token ujian kosong)."
        )
        self._link_label.setTextFormat(Qt.PlainText)
        self._link_label.setWordWrap(True)
        # Minimum eksplisit 1 (bukan 0 — 0 berarti "tidak disetel",
        # jadi layout memakai `minimumSizeHint` yang diukur dari kata
        # terpanjang): untuk link hasil "kata" itu bisa berupa URL 2000
        # karakter tanpa spasi, dan minimum layout kartu meledak ke 14000
        # px sehingga scrollbar horizontal muncul di halaman yang tidak
        # perlu bergulir. Word-wrap membuat QLabel mampu memecah kata
        # panjang, jadi minimum kecil aman dan link tetap terbaca penuh.
        self._link_label.setMinimumWidth(1)
        # TANPA TextSelectableByMouse: menyeleksi link dengan mouse
        # menyalin token ke clipboard TANPA hitung mundur pembersih —
        # membuka lagi kebocoran yang tombol Copy (satu-satunya jalur
        # salin) sudah tutup. Tombol Copy tetap satu-satunya jalan.
        self._link_label.setTextInteractionFlags(Qt.NoTextInteraction)
        self._link_label.setStyleSheet("font-size: 12px;")
        link_row.addWidget(self._link_label, 1)

        self._copy_btn = QPushButton("Copy Link")
        self._copy_btn.setEnabled(
            bool(self._result_url) and self._public_results)
        self._copy_btn.setToolTip(
            f"Link ini menampilkan hasil seluruh kelas, dan akan dihapus "
            f"otomatis dari clipboard dalam "
            f"{CLIPBOARD_CLEAR_SECONDS} detik supaya token ujian tidak "
            "tertinggal di komputer bersama."
        )
        self._copy_btn.clicked.connect(self.copy_link)
        link_row.addWidget(self._copy_btn, 0)
        card_layout.addLayout(link_row)

        if self._public_results:
            note_text = (
                "Buka link tersebut di browser untuk melihat hasil ujianmu. "
                "Link ini menampilkan hasil seluruh kelas."
            )
        else:
            # H6: guru mematikan publikasi nilai — tidak ada link yang bisa
            # dibuka siswa; tombol salin disembunyikan dan catatannya jujur.
            note_text = (
                "Nilai tidak dipublikasikan guru — hubungi pengawas."
            )
            self._copy_btn.hide()
            self._copy_btn.setEnabled(False)
        note = QLabel(note_text)
        note.setWordWrap(True)
        note.setAlignment(Qt.AlignCenter)
        # Warna diwarisi dari tema: abu hardcode #94a3b8 kontrasnya di
        # bawah ambang WCAG di atas kartu gelap.
        note.setStyleSheet("font-size: 11px;")
        card_layout.addWidget(note)

        card_layout.addSpacing(10)

        # Tombol selesai: satu-satunya jalan keluar yang tampil.
        # Dulu dialog modal ini hanya punya X kecil di title bar; siswa
        # (dan pengawas) tidak punya tombol yang jelas untuk menutup
        # layar ini dan kembali ke dialog konfigurasi.
        self._finish_btn = QPushButton("Selesai")
        self._finish_btn.setStyleSheet(
            "QPushButton { padding: 10px 32px; font-weight: bold; }"
        )
        self._finish_btn.clicked.connect(self.close)
        card_layout.addWidget(self._finish_btn, alignment=Qt.AlignCenter)

        version = QLabel(f"EXAMVAN {APP_VERSION}")
        version.setAlignment(Qt.AlignCenter)
        version.setStyleSheet("font-size: 11px;")
        card_layout.addWidget(version)

        outer.addWidget(card, alignment=Qt.AlignHCenter)
        outer.addStretch(2)

        # Konten kartu dibungkus scroll area: layar kecil (1024x600, laptop
        # lama lab) tidak boleh memotong tombol "Selesai" di luar jangkauan
        # -- pelajaran yang sama dengan IdentityDialog (review ronde 3, N10).
        scroll = QScrollArea()
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.NoFrame)
        scroll.setWidget(central)
        self.setCentralWidget(scroll)

        # Minimum jendela dihitung SETELAH scroll area terpasang, dan
        # lebarnya harus memuat kartu PLUS scrollbar vertikal: scrollbar
        # memakan lebar viewport, jadi minimum 420 dengan viewport 405
        # memaksa scrollbar horizontal muncul hanya karena aritmetika itu.
        # 360px yang pernah dipakai di sini lebih buruk — kartunya 420,
        # jadi mengalah bukan hanya soal scrollbar.
        self.setMinimumSize(
            CARD_MIN_WIDTH + scroll.verticalScrollBar().sizeHint().width(),
            _WINDOW_MIN_HEIGHT,
        )

    # ------------------------------------------------------------------
    # Accessors (dipakai test, bukan internal produksi)
    # ------------------------------------------------------------------

    def congrats_text(self) -> str:
        return self._congrats.text()

    def exam_name_text(self) -> str:
        return self._exam_badge.text()

    def identity_text(self) -> str:
        labels = self._identity_box.findChildren(QLabel)
        return " ".join(
            lab.text() for lab in labels if lab.parent() is self._identity_box
        )

    def result_url(self) -> str:
        return self._result_url

    def displayed_link_text(self) -> str:
        """Teks label link SEBENAARNYA yang dilihat siswa.

        Dipakai test untuk mengunci invarian "yang tampil = yang
        disalin" tanpa menyentuh atribut private.
        """
        return self._link_label.text()

    def copy_button(self) -> QPushButton:
        return self._copy_btn

    def finish_button(self) -> QPushButton:
        return self._finish_btn

    # ------------------------------------------------------------------
    # Copy + auto-clear
    # ------------------------------------------------------------------

    def copy_link(self) -> None:
        if not self._result_url:
            return
        try:
            QApplication.clipboard().setText(self._result_url)
        except Exception:
            log.warning("could not write result link to clipboard", exc_info=True)
            return
        # Dua jangkar, dua jam (lihat `_tick`). `monotonic` yang mengukur
        # jendela: tidak bisa dimundurkan administrator maupun NTP.
        # `time()` disimpan terpisah hanya untuk menangkap lompatan waktu —
        # mesin tidur membekukan `monotonic`, jadi tanpa jam dinding token
        # yang disalin sebelum tidur akan menetap sampai proses selesai.
        self._copied_at = time.monotonic()
        self._copied_wall_at = time.time()
        self._cleared = False
        # Salinan baru = percobaan baru: anggaran retry scrubbing
        # di-reset, supaya clipboard yang sempat mati tidak ikut
        # mengurangi jatah percobaan untuk link yang baru disalin.
        self._clear_failures = 0
        self._start_timer()
        self._tick()

    def _tick(self) -> None:
        """Perbarui hitung mundur; bersihkan bila sudah lewat.

        Jendela dihitung dari jam yang TIDAK bisa dimundurkan
        (`time.monotonic()`): dengan jam dinding saja, koreksi NTP atau
        pengaturan tanggal manual satu jam ke belakang membuat `elapsed`
        negatif, syarat "belum lewat" selalu benar, dan token kelas
        tertinggal di clipboard PC lab sampai jam dinding menyusul — tombol
        ikut menampilkan "Copy Link (3629s)" selama itu.

        Jam dinding tetap dipakai, sebagai pemutus kedua: suspend
        membekukan `monotonic` sementara waktu nyata tetap berjalan, jadi
        lompatan maju pada jam dinding harus tetap mengakhiri jendela.
        Jawabannya: jendela berakhir pada jam mana pun yang lebih dulu
        lewat (`max`), dan sisa yang ditampilkan dijepit ke
        [0, CLIPBOARD_CLEAR_SECONDS] supaya tidak pernah menjanjikan
        jendela yang lebih panjang dari yang ada.

        Perbandingan float langsung: `int()` dulu memotong 29,9 detik
        menjadi 29 sehingga tombol menulis "1s" padahal sisa 0,1 detik —
        dan sebaliknya 30,0 tepat baru bersih. Tanpa pemotongan,
        "habis" berarti benar-benar habis.
        """
        if self._copied_at is None or self._cleared:
            return
        elapsed = time.monotonic() - self._copied_at
        if self._copied_wall_at is not None:
            elapsed = max(elapsed, time.time() - self._copied_wall_at)
        if elapsed >= CLIPBOARD_CLEAR_SECONDS:
            self._clear_clipboard_if_ours()
            return
        remaining = min(
            float(CLIPBOARD_CLEAR_SECONDS),
            max(0.0, CLIPBOARD_CLEAR_SECONDS - elapsed),
        )
        self._copy_btn.setText(f"Copy Link ({int(remaining)}s)")

    def _clear_clipboard_if_ours(self, *, fresh_attempt: bool = False) -> None:
        """Kosongkan clipboard -- HANYA kalau isinya masih link kita.

        Kalau siswa menyalin sesuatu yang lain setelah menekan tombol,
        isi clipboard itu miliknya; menghapusnya adalah kehilangan data
        yang tidak disengaja. Dua kasus yang sengaja diperlakukan
        BERBEDA:

        * baca mengembalikan TEKS LAIN -> milik siswa, jangan sentuh;
        * baca mengembalikan `""` -> itu BUKAN izin mengosongkan apa pun.
          Di Wayland, atau saat pemilik selection X11 sudah mati,
          pembacaan kosong terjadi karena tidak ada yang memegang
          selection, bukan karena isinya memang kosong. Menghapus di titik
          ini bisa menghapus salinan yang BARU DIMILIKI proses lain
          (balapan antara pembacaan dan salinan siswa). Jadi `clear()`
          tidak dipanggil, tapi keputusannya dianggap sudah diambil:
          mengulanginya selamanya tidak menambah informasi apa pun dan
          hanya membakar CPU tiap detik.

        Batas yang harus jujur (M11): `clipboard.clear()` hanya
        mengosongkan clipboard AKTIF. Riwayat clipboard OS (mis. Win+V di
        Windows, manajer clipboard desktop Linux) bisa tetap menyimpan
        salinan link di luar jangkauan Qt — scrubber ini menutup pintu
        paste biasa, bukan forensik riwayat. Token memang kredensial
        seluruh kelas pada mode static; yang menutup lubang itu
        sepenuhnya adalah guru menonaktifkan publikasi nilai, bukan
        tombol ini.

        `fresh_attempt=True` dipakai oleh `closeEvent`/`hideEvent`:
        mereka adalah kesempatan terakhir, jadi anggaran percobaan
        di-reset — clipboard yang sedang tidak terbaca saat satu tick
        tidak boleh membekukan pembersih terakhir.
        """
        if not self._result_url:
            return
        if not self._scrub_clipboard(fresh_attempt=fresh_attempt):
            # Tidak ada keputusan (baca/clear gagal): countdown TETAP
            # hidup dan `_tick` akan mencoba lagi pada detik berikutnya.
            # Melepas saruan di sini adalah bug H6: satu pembacaan yang
            # gagal membuat token tinggal di clipboard PC lab selamanya
            # dengan tidak ada satu pun QString yang menyatakannya.
            return
        # Baru SEKARANG saruan dilepas: pembacaan berhasil dan keputusan
        # sudah diambil.
        self._copied_at = None
        self._copied_wall_at = None
        self._cleared = True
        self._copy_btn.setText("Copy Link")
        self._copy_btn.setEnabled(
            bool(self._result_url) and self._public_results)

    def _scrub_clipboard(self, *, fresh_attempt: bool = False) -> bool:
        """Satu percobaan pembersihan clipboard. True bila SUDAH diputuskan.

        False berarti "belum ada keputusan": clipboard tidak bisa dibaca
        atau isinya milik kita tapi tidak bisa dikosongkan, jadi pemanggil
        WAJIB menyisakan countdown untuk dicoba lagi.
        """
        if not self._result_url:
            # Tidak ada yang bisa disapu: jangan sentuh clipboard siswa
            # hanya karena halaman ini sedang disembunyikan.
            return True
        if fresh_attempt:
            self._clear_failures = 0
        try:
            clipboard = QApplication.clipboard()
            current = clipboard.text()
        except Exception:
            self._note_clipboard_failure("dibaca")
            return False
        self._clear_failures = 0
        if current.strip() == self._result_url:
            try:
                clipboard.clear()
            except Exception:
                self._note_clipboard_failure("dikosongkan")
                return False
        elif not current.strip():
            log.info(
                "clipboard terbaca kosong: tidak ada pemilik selection "
                "atau selection sudah mati — isinya tidak disentuh"
            )
        return True

    def _note_clipboard_failure(self, what: str) -> None:
        """Catat kegagalan scrubbing: tombol jujur, saruan belum lepas.

        Percobaan dibatasi supaya halaman tidak mencoba membaca clipboard
        yang sudah mati selamanya. Setelah batas, countdown dilepas (agar
        `_tick` tidak memanggil clipboard tiap detik) tapi TIDAK dengan
        kasar: tombolnya tetap menampilkan kegagalan, dan menekan ulang
        tombol Copy akan menyalakan countdown baru.
        """

        self._clear_failures += 1
        if self._clear_failures < _CLIPBOARD_READ_RETRIES:
            log.debug(
                "clipboard gagal %s (percobaan %d/%d) — countdown tetap "
                "dijaga untuk mencoba lagi",
                what, self._clear_failures, _CLIPBOARD_READ_RETRIES,
            )
            self._copy_btn.setText(_CLIPBOARD_UNCLEARED_TEXT)
            return
        log.warning(
            "clipboard gagal %s %d kali berturut-turut — link hasil dibiarkan "
            "apa adanya; hanya menutup halaman ini atau menekan ulang "
            "tombol Copy yang bisa mencoba lagi",
            what, self._clear_failures,
        )
        self._copied_at = None
        self._copied_wall_at = None
        self._cleared = True
        self._copy_btn.setText(_CLIPBOARD_UNCLEARED_TEXT)
        self._copy_btn.setEnabled(
            bool(self._result_url) and self._public_results)

    # ------------------------------------------------------------------
    # Timer
    # ------------------------------------------------------------------

    def _start_timer(self) -> None:
        timer = getattr(self, "_timer", None)
        if timer is not None and timer.isActive():
            return
        if timer is None:
            timer = self._timer = QTimer(self)
            timer.setInterval(1000)
            timer.timeout.connect(self._tick)
        timer.start()

    def _stop_timer(self) -> None:
        timer = getattr(self, "_timer", None)
        if timer is not None:
            timer.stop()

    def showEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Halaman tampil lagi setelah disalin (hide lalu show): hitung
        # mundur harus lanjut, jadi timer di-restart bila masih ada
        # salinan yang dijaga.
        #
        # Cabang ini bukan syarat fiktif. `hideEvent` menyapu
        # clipboard sekarang, tapi TIDAK membuang deadline-nya (lihat
        # catatan di sana), jadi setelah hide/show yang jujur syarat di
        # benar-benar terpenuhi. Sebelumnya `hideEvent` ikut melepas
        # saruan, cabang ini mati, dan satu-satunya test yang
        # mengejarnya memanggil `_stop_timer()` langsung dengan alasan
        # yang salah: QTimer tidak dimatikan suspend, dia resume lalu
        # berbunyi.
        if self._copied_at is not None and not self._cleared:
            self._start_timer()
        super().showEvent(event)

    def hideEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Halaman disembunyikan tanpa ditutup (mis. di-hide pemanggil):
        # token tidak boleh tertinggal di clipboard hanya karena halaman
        # tidak terlihat — jadi clipboard DISAPU SEKARANG (fail-safe,
        # tidak melemah sedikit pun dari perilaku lama).
        #
        # Yang TIDAK dilakukan: membuang deadline countdown. Hide/show
        # yang sementara (minimisasi, Alt-Tab, dialog di atasnya) dulu
        # membuang satu-satunya naganya, sehingga tidak ada lagi yang
        # menjaga clipboard setelah halaman kembali tampil — dan
        # clipboard manager yang mengembalikan salinan lama (X11
        # selection restore saat fokus) tidak pernah disapu. Jadi yang
        # dipanggil di sini adalah `_scrub_clipboard` (keputusan + isi
        # clipboard), bukan `_clear_clipboard_if_ours` yang sekalian
        # melepas saruan. Timer tetap dihentikan supaya tick tidak
        # terbuang untuk widget yang tidak terlihat; `showEvent` yang
        # menyalakannya lagi.
        self._scrub_clipboard(fresh_attempt=True)
        self._stop_timer()
        super().hideEvent(event)

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Menutup layar tidak boleh meninggalkan token di clipboard --
        # kalau siswa menutup sebelum hitung mundur habis, timer ikut
        # mati dan tidak ada yang membersihkan. Ini kesempatan TERAKHIR,
        # jadi anggaran percobaan scrubbing di-reset (`fresh_attempt`).
        first_close = not self._closed
        self._clear_clipboard_if_ours(fresh_attempt=True)
        self._closed = True
        self._stop_timer()
        super().closeEvent(event)
        # Setelah super(): penerima sinyal (ExamViewer) menutup dirinya
        # sebagai respons; widget ini sendiri sedang dalam proses tutup,
        # jadi urutannya aman. HANYA pada penutupan pertama -- close()
        # kedua pada widget yang sudah tersembunyi tetap menjalankan
        # closeEvent, dan sinyal ganda membuat alur berikutnya (viewer
        # menutup diri, dialog konfigurasi tampil) dijalankan dua kali.
        if first_close:
            self.page_closed.emit()

    # ------------------------------------------------------------------
    # Navigasi
    # ------------------------------------------------------------------

    def show_fullscreen(self) -> None:
        """Tampilkan sebagai halaman penuh di seluruh layar.

        Sengaja memakai helper `apply_fullscreen()` yang sama dengan jendela
        ujian -- satu definisi "fullscreen yang benar" (state + geometri,
        taskbar tertutup) dipakai bersama, bukan dua implementasi yang bisa
        saling meleset seperti bug 30 Sep 2026.

        Catatan: penelepon boleh menjalankan ini saat jendela masih hidden;
        `apply_fullscreen()` memanggil `show()` sendiri. Setelah itu jendela
        TIDAK dipaksa tetap fullscreen -- siswa sudah selesai ujian, dan
        resize/alt-tab oleh siswa tidak di-reassert ulang.
        """
        from .fullscreen import apply_fullscreen

        try:
            apply_fullscreen(self)
        except Exception:
            log.warning("could not show congratulations fullscreen", exc_info=True)
            self.showMaximized()

    def is_closed(self) -> bool:
        return self._closed
