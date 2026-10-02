"""Identity dialog — dynamic fields from API response."""

from __future__ import annotations

import logging
from typing import Any, Dict, List, Optional

from PyQt5.QtCore import Qt, QTimer
from PyQt5.QtWidgets import (
    QDialog,
    QFrame,
    QLabel,
    QLineEdit,
    QMessageBox,
    QPushButton,
    QScrollArea,
    QVBoxLayout,
    QWidget,
)

from ..models import Exam, IdentityField

log = logging.getLogger(__name__)


def _plausible_key(key: str) -> bool:
    """True bila key ini bisa menjadi nama kolom identitas yang masuk akal.

    Ronde 6 (item 2). Key non-string dari server di-`str()`-kan di
    `models.Exam.from_json`, jadi angka JSON `123` sampai ke sini sebagai
    `"123.0"`. Go tidak bisa decode `123` ke `Key string`, jadi field itu
    tersimpan dengan `Key:""` dan `identityFieldValue` mencari `field_<idx>`
    lalu `""` — tidak pernah `"123.0"`. Akibatnya field wajib selalu kosong
    dan server menjawab 400 selamanya.

    Penentunya sengaja kasar dan hanya demi satu hal: memisahkan key yang
    SELALU ditolak server dari key yang mungkin dipakai. Syaratnya key memuat
    setidaknya satu huruf. Kunci sintetis `field_<index>` tetap lolos
    (mengandung huruf "field") dan key-less field tetap punya aturannya
    sendiri (`_empty_origin_keys`), jadi tidak ada perilaku lama yang berubah.
    """
    return any(ch.isalpha() for ch in str(key or ""))


class IdentityDialog(QDialog):
    """Dialog that collects student identity fields dynamically."""

    # Default fields if exam has none
    _DEFAULT_FIELDS = [
        IdentityField(key="student_name", label="Nama", required=True),
        IdentityField(key="exam_number", label="Nomor Ujian", required=True),
        IdentityField(key="student_class", label="Kelas", required=True),
    ]

    def __init__(
        self,
        exam: Exam,
        saved_data: Optional[Dict[str, Any]] = None,
        parent=None,
    ):
        super().__init__(parent)
        self._exam = exam
        raw_fields = exam.identity_fields if exam.identity_fields else self._DEFAULT_FIELDS
        # Audit 2 Okt 2026 (HIGH H15): field key duplikat di API response
        # membuat _inputs[key] ditimpa — siswa memasukkan dua entri dengan
        # nama sama, salah satunya diam-diam hilang dari data yang dikirim.
        # Deduplicate pertahankan field PERTAMA, tapi bila SALAH SATU
        # tabrakan `required`, hasil gabungannya required (tanda bintang
        # tampil — itulah pemberitahuan ke siswa), dan log peringatan
        # menyebut ujian + key supaya backend tahu.
        #
        # Ronde 6 (item 1): normalisasi dedup harus PERSIS seperti server.
        # `webui/internal/handlers/admin/exams.go:validateIdentityFields`
        # menolak duplikat dengan `strings.ToLower(key)` pada trimmed key,
        # sedangkan `seen` di sini tadinya ber-key raw: `Nama` + `nama`
        # terbaca sebagai dua field berbeda. Akibatnya siswa melihat DUA
        # kotak berlabel sama, mengisinya dua kali, dan hanya satu key
        # yang punya makna — sementara server menganggapnya satu key (dan
        # kini menolak konfigurasi itu saat disimpan). Sekarang `seen`
        # ber-key `casefold()`, sementara key ASLI tetap dipakai untuk
        # wire supaya nilai yang diketik mendarat di key yang dibaca server
        # (`body.IdentityData[field.Key]`).
        #
        # Key kosong/whitespace-only (C2) TIDAK lagi dibuang: dibuang
        # berarti form kosong tetap lolos validasi (Accepted) lalu server
        # membalas 400 selamanya. Key dinormalisasi strip, dan tiap field
        # tanpa key mendapat kunci sintetis `field_<index>` (index = posisi
        # di exam.identity_fields) supaya widget TETAP dibangun dan nilainya
        # TETAP terkumpul.
        #
        # Salinan IdentityField baru dipakai (bukan item asli) supaya
        # normalisasi strip + merge required tidak mengubah objek milik
        # Exam/_DEFAULT_FIELDS bersama.
        seen: Dict[str, IdentityField] = {}
        # Key peta dedup = bentuk casefolded, key yang dipakai untuk wire =
        # key ASLI. Lihat catatan di atas: server menormalisasi dengan
        # `strings.ToLower` (exams.go validateIdentityFields), jadi
        # `Nama` + `nama` adalah SATU key di sana. Dengan `seen` ber-key
        # raw, client membangun dua input berlabel sama dan hanya satu
        # key yang punya makna — siswa mengetik dua kali, satu hilang.
        self._fields: List[IdentityField] = []
        # Kunci sintetis yang berasal dari key kosong — untuk aturan
        # warisan `"": value` di get_identity_data dan penolakan
        # multi-kosong-required di _on_submit.
        self._empty_origin_keys: List[str] = []
        for idx, f in enumerate(raw_fields):
            norm_key = str(f.key or "").strip()
            empty_origin = not norm_key
            if empty_origin:
                norm_key = f"field_{idx}"
            # Bentuk yang dipakai server untuk dedup: lower dari trimmed
            # (`strings.ToLower(key)` di validateIdentityFields).
            # `casefold` lebih agresif dari `lower` untuk beberapa huruf
            # non-ASCII, dan itu aman: biaya salah gabung di sini jauh lebih
            # kecil daripada client mengirim dua key yang server anggap satu.
            dedup_key = norm_key.casefold()
            if dedup_key in seen:
                first = seen[dedup_key]
                if f.required and not first.required:
                    first.required = True
                log.warning(
                    "IdentityField key duplikat (case-insensitive) %r vs "
                    "%r pada %s — hanya %r yang dipakai%s", f.key,
                    first.key, exam.name, first.key,
                    " (gabungan required)" if f.required else "",
                )
                continue
            kept = IdentityField(
                key=norm_key,
                label=f.label,
                required=bool(f.required),
            )
            seen[dedup_key] = kept
            self._fields.append(kept)
            if empty_origin:
                self._empty_origin_keys.append(norm_key)
                log.warning(
                    "IdentityField tanpa key (label %r) pada %s — "
                    "memakai kunci sintetis %r",
                    f.label, exam.name, norm_key,
                )
        # Ronde 6 (item 2): putuskan nasib field dengan key yang tidak
        # masuk akal (lihat _plausible_key). Ketiganya berdasarkan satu
        # kesimpulan yang sama: nilai yang diketik untuk field bermasalah TIDAK
        # PERNAH sampai ke kolom yang benar di server, jadi diam-diam
        # menaruhnya hanya menghasilkan 400 tanpa diagnosis.
        #
        #   * wajib  -> JANGAN dibuang (membuangnya meregresi fix C2 dan
        #     membuat form kosong lolos). Dialog TOLAK join dengan pesan
        #     konfigurasi; satu-satunya jalan adalah pengawas memperbaikinya.
        #   * opsional + masih ada key lain yang masuk akal -> dibuang, supaya
        #     siswa tidak mengetik sesuatu yang tidak akan pernah tersimpan.
        #   * opsional + tidak ada alternatif -> widget tetap dibangun;
        #     membuangnya membuat form kosong Accepted, dan itu lebih buruk.
        self._broken_key_labels: List[str] = []
        usable = [
            fld for fld in self._fields
            if _plausible_key(fld.key) or fld.key in self._empty_origin_keys
        ]
        for fld in list(self._fields):
            if _plausible_key(fld.key) or fld.key in self._empty_origin_keys:
                continue
            label = fld.label or fld.key
            if fld.required:
                self._broken_key_labels.append(label)
                log.warning(
                    "IdentityField wajib dengan key tidak valid %r (label "
                    "%r) pada %s — server tidak akan membacanya; join "
                    "ditolak sampai konfigurasi diperbaiki",
                    fld.key, fld.label, exam.name,
                )
                continue
            if usable:
                self._fields.remove(fld)
                log.warning(
                    "IdentityField opsional dengan key tidak valid %r "
                    "(label %r) pada %s — dibuang, ada field lain yang "
                    "memiliki key yang bisa dibaca server",
                    fld.key, fld.label, exam.name,
                )
            else:
                log.warning(
                    "IdentityField opsional dengan key tidak valid %r "
                    "(label %r) pada %s — tetap ditampilkan karena tidak "
                    "ada alternatif; isinya tidak akan tersimpan di server",
                    fld.key, fld.label, exam.name,
                )
        # Ronde 6 (item 6): latch re-entran untuk `_on_submit`.
        #
        # `_submit_btn.clicked` dan `last_input.returnPressed` keduanya
        # terhubung ke `_on_submit`, dan `returnPressed` MEMANCARKAN ULANG
        # (auto-repeat) selama tombol ditahan — sumber pemicu ganda yang
        # persis sama dengan yang sudah membuat `_connect_in_flight` perlu
        # di `ServerConfigDialog` (audit HIGH H3). Tidak ada latch di sini.
        #
        # Hari ini `accept()` kedua tidak merusak apa pun karena
        # `QDialog::done()` pada dialog yang sudah ditutup itu no-op — dan
        # justru itu bahayanya: sifat "tidak merusak" itu dijamin detail
        # implementasi Qt, bukan kode di repo ini.
        #
        # Latch dipasang SESUDA validasi (lihat `_on_submit`), jadi setiap
        # jalur yang ditolak selalu meninggalkan latch bersih dan siswa
        # bisa memperbaiki isiannya lalu menekan lagi.
        self._submitting = False
        self._inputs: Dict[str, QLineEdit] = {}
        self._saved = saved_data or {}
        self._setup_ui()

    def _setup_ui(self) -> None:
        self.setWindowTitle("Identitas Siswa")
        self.setWindowFlags(self.windowFlags() & ~Qt.WindowContextHelpButtonHint)

        # Outer layout centers the form card.
        #
        # Faktor stretch di sini penting dan pernah salah. Semula:
        #
        #     outer.addStretch(2)              # atas
        #     outer.addWidget(self._scroll, 1)  # kartu
        #     outer.addStretch(2)              # bawah
        #
        # Extra space dibagi PROPORSIONAL terhadap faktor stretch, jadi
        # scroll area hanya mendapat 1 dari 5 bagian -- sisanya ruang
        # kosong. Di layar 1080p viewport-nya sekitar 200 px,
        # sementara kartunya 400-an px: form harus di-scroll padahal layar
        # masih sebagian besar kosong. Persis yang dilaporkan siswa.
        #
        # Dua stretch kecil (faktor 1) dipakai supaya kartu tetap ter-center
        # VERTIKAL saat isinya pendek, tapi scroll area sekarang jelas
        # mendominasi (faktor 20). Kalau kartunya memang lebih tinggi dari
        # layar, stretch mengempis dan scrolling tetap terjadi seperti
        # seharusnya -- itu gunanya QScrollArea di sini.
        outer = QVBoxLayout(self)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.addStretch(1)

        # The card scrolls. Ujian with many identity fields used to push the
        # "Masuk Ujian" button below the bottom of the screen with no way to
        # reach it — the student was locked out before the exam even started.
        self._scroll = QScrollArea()
        self._scroll.setWidgetResizable(True)
        self._scroll.setFrameShape(QFrame.NoFrame)
        self._scroll.setHorizontalScrollBarPolicy(Qt.ScrollBarAlwaysOff)

        card = QWidget()
        card.setFixedWidth(440)
        card.setObjectName("identityCard")
        from .styles import is_system_dark
        if is_system_dark():
            card.setStyleSheet("QWidget#identityCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
        else:
            card.setStyleSheet("QWidget#identityCard { background-color: #ffffff; border: 1px solid #ccd0da; border-radius: 12px; }")
        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(18)
        card_layout.setContentsMargins(40, 40, 40, 40)

        # Exam name
        exam_label = QLabel(self._exam.name)
        exam_label.setStyleSheet("font-size: 18px; font-weight: bold;")
        exam_label.setAlignment(Qt.AlignCenter)
        exam_label.setWordWrap(True)
        card_layout.addWidget(exam_label)

        card_layout.addSpacing(16)

        # Dynamic fields -- dikelompokkan per field, bukan lemparan label
        # dan input dengan jarak seragam. Laporan lapangan: "jarak antar
        # label terlalu lebar". Pengukurannya: label->input dan
        # input->label berikutnya sama-sama 10px, jadi label terasa
        # melayang tanpa jelas milik siapa. Perbaikannya hierarki, bukan
        # sekadar memperkecil angka: setiap field dibungkus satu sub-layout
        # dengan spacing rapat (label menempel ke inputnya, 4px), dan
        # JARAK ANTAR FIELD yang melebar (card spacing 18px). Mata membaca
        # "label + kotak" sebagai satu unit, lalu berhenti sejenak sebelum
        # unit berikutnya.
        for field in self._fields:
            # Key sudah dinormalisasi di __init__ (strip; kosong → sintetis
            # `field_<index>`), jadi setiap field PASTI punya widget.
            group = QVBoxLayout()
            group.setSpacing(4)
            lbl = QLabel(field.label + (" *" if field.required else ""))
            lbl.setStyleSheet("font-weight: bold;" if field.required else "")
            lbl.setWordWrap(True)
            group.addWidget(lbl)

            inp = QLineEdit()
            inp.setPlaceholderText(f"Masukkan {field.label.lower()}")
            # Pre-fill from saved data
            saved_val = self._saved.get(field.key, "")
            if saved_val:
                inp.setText(str(saved_val))
            group.addWidget(inp)
            self._inputs[field.key] = inp
            card_layout.addLayout(group)

        card_layout.addSpacing(12)

        # Submit button
        self._submit_btn = QPushButton("  Masuk Ujian  ")
        self._submit_btn.clicked.connect(self._on_submit)
        card_layout.addWidget(self._submit_btn, alignment=Qt.AlignCenter)

        # Enter key on last field triggers submit
        if self._inputs:
            last_input = list(self._inputs.values())[-1]
            last_input.returnPressed.connect(self._on_submit)

        self._scroll.setWidget(card)
        self._scroll.setAlignment(Qt.AlignCenter)
        # Faktor 20, bukan 1: kartu harus mendapat tinggi layar, bukan
        # fifth thereof. Lihat catatan di atas.
        outer.addWidget(self._scroll, 20)

        # Spacer bottom
        outer.addStretch(1)

    def _on_submit(self) -> None:
        # Ronde 6 (item 2): field WAJIB dengan key yang tidak pernah dibaca
        # server (lihat _plausible_key). Diperiksa PERNAH PADA AWAL, sebelum
        # validasi isian dan tanpa syarat apa pun: nilai yang diketik di kotak
        # itu tidak akan pernah mendarat di kolom yang benar, jadi `accept()`
        # hanya menghasilkan 400 "Identitas '<label>' wajib diisi" yang sama
        # sekali tidak bisa diperbaiki dari sisi siswa. Menolak di sini jelas,
        # dan satu-satunya jalan adalah pengawas memperbaiki konfigurasinya.
        if self._broken_key_labels:
            QMessageBox.warning(
                self,
                "Konfigurasi Ujian Salah",
                f"konfigurasi ujian salah: kolom "
                f"'{self._broken_key_labels[0]}' wajib diisi tetapi key-nya "
                "tidak valid — hubungi pengawas",
            )
            return
        # C2: beberapa field tanpa key DAN salah satunya required berarti
        # konfigurasi ujian rusak — server melewati yang non-required tapi
        # MENOLAK yang required tanpa key yang jelas. Tolak gabungnya di
        # sini (jangan accept): satu-satunya jalan adalah pengawas
        # memperbaiki konfigurasinya. Beberapa-tapi-semua-opsional: lanjut
        # (server melewati yang non-required).
        if len(self._empty_origin_keys) > 1:
            bad_labels = [
                fld.label or fld.key for fld in self._fields
                if fld.key in self._empty_origin_keys and fld.required
            ]
            if bad_labels:
                QMessageBox.warning(
                    self,
                    "Konfigurasi Ujian Salah",
                    f"konfigurasi ujian salah: field '{bad_labels[0]}' "
                    f"tidak punya key — hubungi pengawas",
                )
                return
        errors = []
        first_bad_key: Optional[str] = None
        for field in self._fields:
            inp = self._inputs.get(field.key)
            if not inp:
                continue
            val = inp.text().strip()
            if field.required and not val:
                errors.append(f"{field.label} wajib diisi")
                if first_bad_key is None:
                    first_bad_key = field.key

        if errors:
            # Form panjang: tampilkan ~5 pertama + sisa dihitung, lalu
            # fokus ke pelanggar pertama supaya siswa langsung tahu.
            shown = errors[:5]
            text = "\n".join(shown)
            if len(errors) > 5:
                text += f"\n…dan {len(errors) - 5} field lain"
            QMessageBox.warning(self, "Validasi", text)
            if first_bad_key is not None:
                offender = self._inputs.get(first_bad_key)
                if offender is not None:
                    offender.setFocus()
            return

        # Ronde 6 (item 6): semua jalur DI ATAS return tanpa latch — validasi
        # selalu jalan lebih dulu dan selalu meninggalkan latch bersih, jadi
        # siswa bisa memperbaiki isiannya lalu menekan lagi. Latch baru
        # menyala di titik ini: di bawah ini tidak ada `return`, hanya
        # `accept()`.
        if self._submitting:
            return
        self._submitting = True
        self.accept()

    def get_identity_data(self) -> Dict[str, str]:
        """Return {field_key: value} for all fields.

        Kunci sintetis `field_<index>` ikut terkirim apa adanya. Bila
        TEPAT SATU field tanpa key, nilainya JUGA dikirim di bawah kunci
        warisan `""`: baris server lama menyimpan Key:"" dan mencari
        `body.IdentityData[""]`.
        """
        data = {
            field.key: self._inputs[field.key].text().strip()
            for field in self._fields
            if field.key in self._inputs
        }
        if len(self._empty_origin_keys) == 1:
            only = self._empty_origin_keys[0]
            if only in data:
                data[""] = data[only]
        return data
