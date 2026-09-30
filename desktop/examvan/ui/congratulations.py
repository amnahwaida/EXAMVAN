"""Halaman selamat setelah submit — parity Android `CongratulationsActivity`.

Desktop sebelumnya hanya menampilkan `QMessageBox` lalu menutup jendela,
jadi siswa tidak pernah melihat pesan guru, nama ujian, identitasnya,
maupun link hasil — padahal server sudah menyediakan targetnya
(`GET /hasil/:token` dan short-link `/<token>` di `webui/cmd/server/main.go`).

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
"""

from __future__ import annotations

import logging
import time
from typing import Optional

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import (
    QApplication,
    QDialog,
    QFrame,
    QHBoxLayout,
    QLabel,
    QPushButton,
    QVBoxLayout,
    QWidget,
)

from .. import APP_VERSION
from ..utils import build_result_link

log = logging.getLogger(__name__)

# Berapa lama link hasil boleh tinggal di clipboard setelah disalin.
CLIPBOARD_CLEAR_SECONDS = 30

_DEFAULT_CONGRATS = (
    "Jawabanmu sudah berhasil dikumpulkan. Terima kasih telah mengerjakan "
    "ujian ini dengan jujur."
)


class CongratulationsDialog(QDialog):
    """Layar selesai: pesan guru, nama ujian, identitas, copy link hasil.

    Sengaja modal tanpa tombol keluar yang mencolok: siswa sudah selesai
    bekerja, jadi tidak ada lagi yang perlu dijaga. Layout memakai
    `styles.SECURITY_COLORS` supaya konsisten dengan jendela ujian.
    """

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
        parent: Optional[QWidget] = None,
    ) -> None:
        super().__init__(parent)
        self._result_url = build_result_link(server_url, exam_token)
        self._copied_at: Optional[float] = None
        self._cleared = False

        self.setWindowTitle("Selesai")
        self.setModal(True)
        self.setMinimumWidth(520)

        root = QVBoxLayout(self)
        root.setContentsMargins(28, 26, 28, 22)
        root.setSpacing(14)

        title = QLabel("Jawaban Berhasil Dikumpulkan")
        title.setAlignment(Qt.AlignCenter)
        title.setStyleSheet(
            "font-size: 20px; font-weight: 700; color: #16a34a;"
        )
        root.addWidget(title)

        self._exam_badge = QLabel(exam_name.strip() or "Ujian")
        self._exam_badge.setAlignment(Qt.AlignCenter)
        self._exam_badge.setStyleSheet(
            "font-size: 13px; color: #64748b;"
            "background: #f1f5f9; border-radius: 10px; padding: 5px 12px;"
        )
        root.addWidget(self._exam_badge)

        message = (congrats_message or "").strip() or _DEFAULT_CONGRATS
        self._congrats = QLabel(message)
        self._congrats.setWordWrap(True)
        self._congrats.setAlignment(Qt.AlignCenter)
        self._congrats.setStyleSheet("font-size: 15px; color: #0f172a;")
        root.addWidget(self._congrats)

        self._identity_box = QFrame()
        self._identity_box.setStyleSheet(
            "background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 10px;"
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
            key.setStyleSheet("color: #64748b; font-size: 13px;")
            val = QLabel(str(value).strip())
            val.setStyleSheet("color: #0f172a; font-size: 13px; font-weight: 600;")
            row.addWidget(key)
            row.addStretch(1)
            row.addWidget(val)
            identity_layout.addLayout(row)
        root.addWidget(self._identity_box)

        link_row = QHBoxLayout()
        self._link_label = QLabel(
            f"Link hasil: {self._result_url}" if self._result_url
            else "Link hasil tidak tersedia (token ujian kosong)."
        )
        self._link_label.setWordWrap(True)
        self._link_label.setTextInteractionFlags(Qt.TextSelectableByMouse)
        self._link_label.setStyleSheet("font-size: 12px; color: #334155;")
        link_row.addWidget(self._link_label, 1)

        self._copy_btn = QPushButton("Copy Link")
        self._copy_btn.setEnabled(bool(self._result_url))
        self._copy_btn.setToolTip(
            f"Link akan dihapus otomatis dari clipboard dalam "
            f"{CLIPBOARD_CLEAR_SECONDS} detik supaya token ujian tidak "
            "tertinggal di komputer bersama."
        )
        self._copy_btn.clicked.connect(self.copy_link)
        link_row.addWidget(self._copy_btn, 0)
        root.addLayout(link_row)

        note = QLabel(
            f"Buka link tersebut di browser untuk melihat hasil ujianmu. "
            f"(EXAMVAN {APP_VERSION})"
        )
        note.setAlignment(Qt.AlignCenter)
        note.setStyleSheet("font-size: 11px; color: #94a3b8;")
        root.addWidget(note)

    # ------------------------------------------------------------------
    # Accessors (dipakai test, bukan internal produksi)
    # ------------------------------------------------------------------

    def congrats_text(self) -> str:
        return self._congrats.text()

    def exam_name_text(self) -> str:
        return self._exam_badge.text()

    def identity_text(self) -> str:
        labels = self._identity_box.findChildren(QLabel)
        return " ".join(lab.text() for lab in labels if lab.parent() is self._identity_box)

    def result_url(self) -> str:
        return self._result_url

    def copy_button(self) -> QPushButton:
        return self._copy_btn

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
        self._copied_at = time.monotonic()
        self._cleared = False
        self._tick()

    def _tick(self) -> None:
        """Perbarui hitung mundur; bersihkan bila sudah lewat."""
        if self._copied_at is None or self._cleared:
            return
        remaining = CLIPBOARD_CLEAR_SECONDS - int(time.monotonic() - self._copied_at)
        if remaining > 0:
            self._copy_btn.setText(f"Copy Link ({remaining}s)")
            return
        self._clear_clipboard_if_ours()

    def _clear_clipboard_if_ours(self) -> None:
        """Kosongkan clipboard -- HANYA kalau isinya masih link kita.

        Kalau siswa menyalin sesuatu yang lain setelah menekan tombol,
        isi clipboard itu miliknya; menghapusnya adalah kehilangan data
        yang tidak disengaja.
        """
        self._cleared = True
        self._copied_at = None
        try:
            clipboard = QApplication.clipboard()
            if clipboard.text().strip() == self._result_url:
                clipboard.clear()
        except Exception:
            log.debug("clipboard clear failed", exc_info=True)
        self._copy_btn.setText("Copy Link")
        self._copy_btn.setEnabled(bool(self._result_url))

    # ------------------------------------------------------------------
    # Timer
    # ------------------------------------------------------------------

    def showEvent(self, event) -> None:  # noqa: N802 (Qt API)
        self._start_timer()
        super().showEvent(event)

    def _start_timer(self) -> None:
        from PyQt5.QtCore import QTimer

        if getattr(self, "_timer", None) is not None:
            return
        self._timer = QTimer(self)
        self._timer.setInterval(1000)
        self._timer.timeout.connect(self._tick)
        self._timer.start()

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt API)
        # Menutup layar tidak boleh meninggalkan token di clipboard --
        # kalau siswa menekan X sebelum hitung mundur habis, timer ikut
        # mati dan tidak ada yang membersihkan.
        self._clear_clipboard_if_ours()
        super().closeEvent(event)

    def reject(self) -> None:
        self._clear_clipboard_if_ours()
        super().reject()