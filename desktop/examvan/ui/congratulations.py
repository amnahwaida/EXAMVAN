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
import time
import unicodedata
from typing import Optional

from PyQt5.QtCore import Qt, QTimer, pyqtSignal
from PyQt5.QtGui import QPixmap
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

# Panjang maksimal teks server yang dirender (pesan guru). Tanpa batas,
# `congrats_message` raksasa dari server rusak/meledak membuat kartu
# membesar tak terkendali di layar lab kecil.
_SERVER_TEXT_LIMIT = 2000

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
        self._result_url = build_result_link(server_url, exam_token)
        self._public_results = bool(public_results)
        self._copied_at: Optional[float] = None
        self._cleared = False
        self._closed = False

        self.setWindowTitle("EXAMVAN — Selesai")
        # Minimum kecil (bukan 560x480): apply_fullscreen memaksa geometri
        # sendiri, jadi minimum yang lebih kecil selalu aman dan tidak
        # memotong kartu di layar lab kecil.
        self.setMinimumSize(360, 280)

        central = QWidget()
        outer = QVBoxLayout(central)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.setSpacing(0)
        outer.addStretch(2)

        card = QWidget()
        card.setObjectName("congratsCard")
        card.setMaximumWidth(560)
        card.setMinimumWidth(420)
        from .styles import is_system_dark

        if is_system_dark():
            card.setStyleSheet(
                "QWidget#congratsCard { background-color: #313244; "
                "border: 1px solid #45475a; border-radius: 12px; }"
            )
        else:
            card.setStyleSheet(
                "QWidget#congratsCard { background-color: #ffffff; "
                "border: 1px solid #ccd0da; border-radius: 12px; }"
            )
        card_layout = QVBoxLayout(card)
        card_layout.setContentsMargins(40, 36, 40, 28)
        card_layout.setSpacing(12)

        # Ikon sukses DIGAMBAR style Qt (SP_DialogApplyButton), bukan glyph
        # emoji: glyph tergantung font sistem -- di lingkungan tanpa font
        # emoji ia menjadi kotak kosong (tofu), dan momen paling penting
        # bagi siswa tidak boleh bergantung pada keberuntungan font.
        # Fallback ke teks hanya kalau style tidak menyediakan pixmap.
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
        title.setStyleSheet("font-size: 22px; font-weight: 700; color: #16a34a;")
        card_layout.addWidget(title)

        self._exam_badge = QLabel(
            _sanitize_server_text(exam_name.strip() or "Ujian"))
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

        message = _sanitize_server_text(
            (congrats_message or "").strip() or _DEFAULT_CONGRATS)
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
            val = QLabel(_sanitize_server_text(str(value).strip()))
            val.setTextFormat(Qt.PlainText)
            # Nilai panjang (nama ganda, kelas gabungan) membungkus, bukan
            # mendorong kartu melebar: word-wrap + melebar mengisi baris.
            val.setWordWrap(True)
            val.setSizePolicy(QSizePolicy.Expanding, QSizePolicy.Preferred)
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
        # Teks link gabungan tetap disanitasi (bukan URL-nya yang diubah:
        # _sanitize hanya membuang kontrol tak terlihat, dan link hasil
        # selalu percent-encoded sehingga semantik URL tidak tersentuh).
        self._link_label = QLabel(
            _sanitize_server_text(
                f"Link hasil: {self._result_url}" if self._result_url
                else "Link hasil tidak tersedia (token ujian kosong)."
            )
        )
        self._link_label.setTextFormat(Qt.PlainText)
        self._link_label.setWordWrap(True)
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
        # Jam dinding (wall-clock), bukan monotonic: hitung mundur ini
        # dibandingkan dengan waktu yang dilihat siswa, dan suspend yang
        # membekukan monotonic tidak boleh memperpanjang masa tinggal
        # token di clipboard.
        self._copied_at = time.time()
        self._cleared = False
        self._start_timer()
        self._tick()

    def _tick(self) -> None:
        """Perbarui hitung mundur; bersihkan bila sudah lewat."""
        if self._copied_at is None or self._cleared:
            return
        # Perbandingan float langsung: `int()` dulu memotong 29,9 detik
        # menjadi 29 sehingga tombol menulis "1s" padahal sisa 0,1 detik —
        # dan sebaliknya 30,0 tepat baru bersih. Tanpa pemotongan,
        # "habis" berarti benar-benar habis.
        elapsed = time.time() - self._copied_at
        if elapsed < CLIPBOARD_CLEAR_SECONDS:
            self._copy_btn.setText(f"Copy Link ({int(CLIPBOARD_CLEAR_SECONDS - elapsed)}s)")
            return
        self._clear_clipboard_if_ours()

    def _clear_clipboard_if_ours(self) -> None:
        """Kosongkan clipboard -- HANYA kalau isinya masih link kita.

        Kalau siswa menyalin sesuatu yang lain setelah menekan tombol,
        isi clipboard itu miliknya; menghapusnya adalah kehilangan data
        yang tidak disengaja.

        Batas yang harus jujur (M11): `clipboard.clear()` hanya
        mengosongkan clipboard AKTIF. Riwayat clipboard OS (mis. Win+V di
        Windows, manajer clipboard desktop Linux) bisa tetap menyimpan
        salinan link di luar jangkauan Qt — scrubber ini menutup pintu
        paste biasa, bukan forensik riwayat. Token memang kredensial
        seluruh kelas pada mode static; yang menutup lubang itu
        sepenuhnya adalah guru menonaktifkan publikasi nilai, bukan
        tombol ini.
        """
        if not self._result_url:
            return
        self._cleared = True
        self._copied_at = None
        try:
            clipboard = QApplication.clipboard()
            if clipboard.text().strip() == self._result_url:
                clipboard.clear()
            # Reset tombol di DALAM try: kalau clipboard gagal diakses,
            # biarkan hitung mundur apa adanya daripada menampilkan
            # "Copy Link" seolah link sudah bersih.
            self._copy_btn.setText("Copy Link")
            self._copy_btn.setEnabled(
                bool(self._result_url) and self._public_results)
        except Exception:
            log.debug("clipboard clear failed", exc_info=True)

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
        # salinan yang dijaga. Tanpa ini, menyembunyikan lalu
        # menampilkan halaman membekukan countdown selamanya.
        if self._copied_at is not None and not self._cleared:
            self._start_timer()
        super().showEvent(event)

    def hideEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Halaman disembunyikan tanpa ditutup (mis. di-hide pemanggil):
        # token tidak boleh tertinggal di clipboard hanya karena halaman
        # tidak terlihat — bersihkan dulu, lalu hentikan timer supaya
        # tidak membuang tick pada widget gaib.
        self._clear_clipboard_if_ours()
        self._stop_timer()
        super().hideEvent(event)

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Menutup layar tidak boleh meninggalkan token di clipboard --
        # kalau siswa menutup sebelum hitung mundur habis, timer ikut
        # mati dan tidak ada yang membersihkan.
        first_close = not self._closed
        self._clear_clipboard_if_ours()
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
