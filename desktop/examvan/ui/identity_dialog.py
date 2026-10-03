"""Identity dialog — dynamic fields from API response."""

from __future__ import annotations

import logging
from typing import Any, Dict, List, Optional

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import (
    QDialog,
    QFrame,
    QLabel,
    QLineEdit,
    QMessageBox,
    QPushButton,
    QScrollArea,
    QSizePolicy,
    QVBoxLayout,
    QWidget,
)

from ..models import Exam, IdentityField
from ..utils import identity_data_with_canonical

log = logging.getLogger(__name__)


def _plausible_key(field: IdentityField) -> bool:
    """True bila key kolom ini bisa dibaca server apa adanya.

    Ronde 6 (item 2) menebak bentuk key: "memuat setidaknya satu
    huruf". Server tidak punya aturan seperti itu —

        // admin/exams.go:validateIdentityFields
        key, ok := raw.(string)
        if !ok || strings.TrimSpace(key) == "" { tolak }

        // api/exams.go:identityFieldValue
        if v, ok := data[k].(string); ok { ... }

    — yang dibutuhkan hanya "key berupa string yang tidak kosong", dan
    key itu dibaca PERSIS seperti yang tersimpan. Admin UI menurunkan
    label lalu mengganti semua non-alphanumeric dengan `_`
    (`static/js/admin.js:1012`), jadi label "2024/2025" tersimpan dengan
    key `2024_2025`: tanpa satu huruf pun. Heuristic lama menolak field
    itu — sebagai field WAJIB ia menolak join SELURUH kelas yang memakai
    ujian tersebut, dan sebagai field OPSIONAL ia dibuang diam-diam
    sehingga siswa tidak pernah ditanyakan tahun ajaran. `9` (label "9")
    punya masalah yang sama.

    Satu-satunya key yang benar-benar tidak terbaca adalah yang nilainya
    bukan string di JSON (`{"key": 123}` gagal decode ke `Key string`,
    error-nya dibuang, field tersimpan dengan `Key:""`). Fakta itu
    dibawa `IdentityField.key_is_text` dari `models.Exam.from_json`,
    bukan ditebak ulang di sini — tebakan itulah yang salah di tempat
    pertama.

    Kunci sintetis `field_<index>` tetap lolos (index selalu angka,
    labelnya mengandung huruf, dan `identityFieldValue` membacanya
    POSISIONAL). Field tanpa key punya aturannya sendiri
    (`_empty_origin_keys`).
    """
    return bool(getattr(field, "key_is_text", True))


# Lebar kartu mengikuti jendela, dengan batas atas supaya form tidak
# melebar jadi spanduk di layar 4K dan batas bawah supaya tidak jadi
# kolom kurus di jendela kecil. Rasio 0.45 dipakai, bukan lebar tetap:
# dialog ini SELALU dibuka maximized (`server_config._show_identity_dialog`),
# jadi lebar viewport bisa 1280 (laptop) sampai 3840 (ruang kelas).
_CARD_WIDTH_RATIO = 0.45
_CARD_MIN_WIDTH = 340
_CARD_MAX_WIDTH = 560
# Ruang kosong minimal antara kartu dan tepi jendela, agar kartu tidak
# menempel ke sisi layar saat jendela sedang dikecilkan.
_CARD_GUTTER = 40


class _CardScrollArea(QScrollArea):
    """QScrollArea yang menjaga lebar kartu tetap sebanding viewport.

    `setWidgetResizable(True)` membuat QScrollArea meregangkan widget
    sampai SELURUH viewport, sehingga `setFixedWidth(440)` yang lama
    menghasilkan kolom 440 x 910 di tengah jendela 1920px: 740px ruang
    kosong di kiri, 740px di kanan, dan 478px ruang kosong di dalam
    kartu sendiri.

    Lebar kartu di sini = clamp(45% dari lebar viewport) antara 340 dan
    560px. Makin lebar jendela, kartu ikut melebar sampai batas atas;
    makin sempit, menyusut sampai batas bawah -- dan tidak pernah lebih
    lebar dari viewport dikurangi gutter, supaya tidak ada isian yang
    terpotong.

    Sumbu vertikal sengaja TIDAK diatur di sini: kartu dibuat melekat
    pada `sizeHint`-nya lewat `QSizePolicy.Fixed`, supaya tidak ikut
    menjulang. Kalau isinya lebih tinggi daripada viewport, QScrollArea
    yang cuidar -- itulah gunanya area ini.
    """

    def resizeEvent(self, event):
        super().resizeEvent(event)
        self._fit_card_width()

    def _fit_card_width(self) -> None:
        card = self.widget()
        if card is None:
            return
        viewport_width = self.viewport().width()
        if viewport_width <= 0:
            # Belum ada viewport -- `_fit_card_width` dipanggil juga
            # dari `showEvent`, sebelum layout pertama selesai.
            # `resizeEvent` berikutnya akan menyinkronkan sendiri.
            return
        usable = viewport_width - 2 * _CARD_GUTTER
        target = int(viewport_width * _CARD_WIDTH_RATIO)
        width = max(_CARD_MIN_WIDTH, min(_CARD_MAX_WIDTH, target, usable))
        if card.width() != width:
            card.setFixedWidth(width)


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
                # Fakta "key ini bukan string" harus ikut ke salinan; kalau
                # tidak, `_plausible_key` melihat default `True` dan field
                # yang tidak bisa dibaca server lolos ke form — persis
                # 400 selamanya yang ronde ini menutupnya.
                key_is_text=bool(getattr(f, "key_is_text", True)),
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
            if _plausible_key(fld) or fld.key in self._empty_origin_keys
        ]
        for fld in list(self._fields):
            if _plausible_key(fld) or fld.key in self._empty_origin_keys:
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
        # Label error inline, satu per field (lihat _setup_ui). Tersembunyi
        # sampai field itu benar-benar kosong saat submit.
        self._error_labels: Dict[str, QLabel] = {}
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
        self._scroll = _CardScrollArea()
        self._scroll.setWidgetResizable(True)
        self._scroll.setFrameShape(QFrame.NoFrame)
        self._scroll.setHorizontalScrollBarPolicy(Qt.ScrollBarAlwaysOff)

        card = QWidget()
        # Lebar TIDAK lagi dikunci 440px: `_CardScrollArea`
        # menyinkronkannya ke lebar jendela setiap kali jendela berubah.
        # `Fixed` pada sumbu vertikal yang membuat kartu melekat pada
        # `sizeHint`-nya -- tanpa itu kartu ikut menjulang setinggi viewport
        # dan jadi kolom kosong yang 3/4 isinya hampa.
        policy = card.sizePolicy()
        policy.setVerticalPolicy(QSizePolicy.Fixed)
        card.setSizePolicy(policy)
        card.setObjectName("identityCard")
        # Warna kartu mengikuti `styles.app_theme_dark()`, bukan
        # tema sistem: kartu gelap di dalam jendela terang (atau
        # sebaliknya) adalah regresi butir 1 di
        # `tests/test_styles_dark_regression.py`.
        card.setStyleSheet("QWidget#identityCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(18)
        card_layout.setContentsMargins(40, 40, 40, 40)
        # SLACK tinggi viewport HARUS diserap stretch, bukan label.
        #
        # QScrollArea dengan `widgetResizable(True)` meregangkan kartu
        # sampai SETINGGI viewport: di layar 1080p maximized kartu 440px
        # lebarnya ikut menjulang ~910px, sementara isinya cuma ~432px.
        # Selisih ~478px itu, tanpa stretch di card_layout, dibagikan
        # QBoxLayout ke widget yang boleh melar — dan widget itu adalah
        # QLabel yang `setWordWrap(True)`: tinggi tiap label jadi 102px
        # padahal teksnya sendiri 18px. Teksnya tetap menempel di ATAS
        # kotak label itu, jadi jarak dari teks ke kotak isinya membesar
        # dari ~22px jadi ~106px -- dan jarak antar field dari 80px jadi
        # 164px. Semua test "form muat tanpa scroll" tetap hijau,
        # karena isinya memang muat: yang melar cuma ruang kosongnya.
        #
        # Jadi efek `group.setSpacing(4)` + `card_layout.setSpacing(18)`
        # di bawah — yang sengaja dibuat untuk "label menempel ke
        # inputnya" — praktis tidak pernah terlihat. Stretch di awal dan
        # akhir membuat sisa ruang tinggal di situ: label kembali 18px,
        # field rapat, dan blok form tetap ter-center vertikal.
        card_layout.addStretch(1)

        # Exam name
        exam_label = QLabel(self._exam.name)
        exam_label.setStyleSheet("font-size: 18px; font-weight: bold;")
        exam_label.setAlignment(Qt.AlignCenter)
        exam_label.setWordWrap(True)
        card_layout.addWidget(exam_label)

        card_layout.addSpacing(16)

        # Keterangan tanda bintang. Tanpa ini, `*` di samping label
        # field adalah simbol yang tidak dijelaskan: siswa baru tahu
        # artinya setelah menekan tombol lalu melihat pesan validasi.
        # Murah, dan sekarang pesan invalidasi juga inline -- jadi
        # asterisnya punya arti SEBELUM ada yang salah.
        legend = QLabel("Kolom bertanda * wajib diisi")
        legend.setObjectName("identityLegend")
        legend.setAlignment(Qt.AlignCenter)
        legend.setWordWrap(True)
        card_layout.addWidget(legend)

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

            # Pesan error INLINE di bawah field-nya sendiri, bukan
            # QMessageBox. Alasannya: QMessageBox menutupi form dan
            # memaksa siswa membaca daftar kolom di tempat lain, lalu
            # mencari lagi field mana yang salah di balik dialog itu.
            # Label di sini muncul tepat di bawah kotak yang kosong.
            err = QLabel("")
            err.setObjectName("identityFieldError")
            err.setWordWrap(True)
            err.hide()
            group.addWidget(err)
            self._error_labels[field.key] = err

            # Mengetik menghapus pesan field itu saja: siswa tidak perlu
            # menekan ulang tombol untuk tahu arah perbaikannya. Field
            # lain yang masih kosong tidak ikut hilang — pesannya masih
            # benar.
            inp.textChanged.connect(
                lambda _text, k=field.key: self._clear_field_error(k))

            card_layout.addLayout(group)

        card_layout.addSpacing(12)

        # Submit button
        # Tanpa dua spasi di sekeliling teks. Lebar tombol dulu
        # dipalsukan dengan spasi: (1) label terlihat tidak center
        # begitu font berubah, dan (2) spasi itu ikut terender serta
        # ikut terklik bersama tombol.
        self._submit_btn = QPushButton("Masuk Ujian")
        # `padding: 10px 24px` di QSS sudah memberi ruang napas;
        # lebar minimum ini hanya menjaga agar tombol tidak terlihat
        # kecil di tema dengan font kecil.
        self._submit_btn.setMinimumWidth(200)
        self._submit_btn.clicked.connect(self._on_submit)
        card_layout.addWidget(self._submit_btn, alignment=Qt.AlignCenter)

        # Pasangan stretch penutup: lihat catatan di awal _setup_ui. Tanpa
        # ini sisa ruang kartu kembali dibagi ke label.
        card_layout.addStretch(1)

        # Urutan tab eksplisit: field -> field -> ... -> tombol.
        #
        # Tanpa ini fokus awal jatuh ke QScrollArea (satu-satunya widget
        # yang punya focusPolicy di antara keduanya), jadi begitu form
        # terbuka tidak ada kotak yang punya cincin fokus dan mengetik
        # tidak masuk ke mana pun — siswa harus klik dulu, atau Tab
        # beberapa kali sambil tidak tahu sedang berada di mana.
        # `self._inputs` berurutan sesuai `self._fields`, yaitu urutan
        # tampil, jadi zip ini memang urutan yang dilihat siswa.
        ordered_inputs = list(self._inputs.values())
        for previous, following in zip(ordered_inputs, ordered_inputs[1:]):
            QWidget.setTabOrder(previous, following)
        if ordered_inputs:
            QWidget.setTabOrder(ordered_inputs[-1], self._submit_btn)

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
        # Validasi WAJIB: pesan INLINE, satu di bawah field yang kosong.
        #
        # Sebelumnya semua pesan dikumpulkan ke QMessageBox. Dialog itu
        # menutupi form, dan daftarnya malah diringkas jadi "lima
        # pertama + sisa dihitung": pada form panjang siswa diberi tahu
        # ADA field yang salah tanpa diberi tahu YANG MANA, lalu harus
        # menutup dialog, lalu membaca form untuk mencari kotak kosong.
        # Sekarang setiap field yang kosong menandai dirinya sendiri, jadi
        # yang tampil persis sama dengan yang terlihat di layar, dan
        # tidak ada lagi batas "lima pertama" yang hanya urutan internal.
        #
        # Dua QMessageBox di atas TETAP modal: itu error KONFIGURASI
        # (key tidak terbaca server, atau field tanpa key), bukan
        # kesalahan isi siswa, dan tidak ada field di layar yang bisa
        # menunjukkannya.
        self._clear_all_errors()
        first_bad_key: Optional[str] = None
        for field in self._fields:
            inp = self._inputs.get(field.key)
            if not inp:
                continue
            if field.required and not inp.text().strip():
                err = self._error_labels.get(field.key)
                if err is not None:
                    err.setText(f"{field.label} wajib diisi")
                    err.show()
                if first_bad_key is None:
                    first_bad_key = field.key

        if first_bad_key is not None:
            # Fokus ke field pertama yang kosong, teksnya disorot supaya
            # mengetik langsung menimpa. Pesan field lain tetap terlihat:
            # siswa perlu tahu berapa yang masih harus diperbaiki.
            offender = self._inputs.get(first_bad_key)
            if offender is not None:
                offender.setFocus(Qt.OtherFocusReason)
                offender.selectAll()
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

    def _clear_field_error(self, key: str) -> None:
        """Sembunyikan pesan inline satu field (dipakai textChanged)."""
        err = self._error_labels.get(key)
        # `isHidden()`, bukan `isVisible()`: `isVisible()` ikut false
        # kalau dialog-nya belum tampil (mis. validasi dipanggil tanpa
        # `show()`), sehingga pesan yang SUDAH tampil tidak pernah
        # dibersihkan -- teks lamanya tinggal di label dan muncul lagi
        # begitu dialog dibuka.
        if err is not None and not err.isHidden():
            err.clear()
            err.hide()

    def _clear_all_errors(self) -> None:
        """Sembunyikan semua pesan inline sebelum validasi ulang."""
        for err in self._error_labels.values():
            err.clear()
            err.hide()

    def showEvent(self, event) -> None:
        """Fokus ke field pertama begitu form tampil.

        `server_config` membuka form ini dengan `showMaximized()` lalu
        `exec_()`, dan tidak ada satu pun panggilan `setFocus()` di
        sana. Sebelum handler ini, fokus mendarat di QScrollArea --
        satu-satunya widget di antara keduanya yang punya focusPolicy --
        sehingga begitu form terbuka tidak ada kotak dengan cincin fokus,
        ketikan pertama siswa hilang, dan satu-satunya penanda posisi
        kursor adalah placeholder yang lenyap begitu karakter pertama
        masuk.

        Field default punya placeholder "Masukkan <label>", jadi isinya
        tidak pernah kosong, sehingga tidak terlihat jelas bahwa teks masuk
        mana pun. Fokus di field pertama menutup semua itu sekaligus.
        """
        super().showEvent(event)
        # Lebar viewport baru diketahui di sini pada maximized: sebelum
        # dialog tampil, `_CardScrollArea` masih berlebar 0 sehingga
        # kartu belum pernah diukur.
        self._scroll._fit_card_width()
        first = next(iter(self._inputs.values()), None)
        if first is not None:
            first.setFocus(Qt.OtherFocusReason)
            # Kursor di akhir teks, bukan selectAll: isian yang
            # tersimpan harus dibiarkan utuh, dan ketikan siswa
            # menyunting di ujungnya, bukan menggantinya.
            first.setCursorPosition(len(first.text()))

    def get_identity_data(self) -> Dict[str, str]:
        """Return {field_key: value} for all fields — plus the canonical keys.

        Kunci sintetis `field_<index>` ikut terkirim apa adanya. Bila
        TEPAT SATU field tanpa key, nilainya JUGA dikirim di bawah kunci
        warisan `""`: baris server lama menyimpan Key:"" dan mencari
        `body.IdentityData[""]`.

        Ketiga kunci kanonik (`student_name`, `exam_number`,
        `student_class`) disisipkan di sini — SATU-SATUNYA tempat di mana
        identitas siswa jadi payload. Ini yang sebelumnya dijanjikan
        sebagai kode mati: `map_identity_to_standard` sengaja tidak
        menebak kunci yang tidak dikenal, jadi konfigurasi tanpa kata slot
        (`alamat`, `kode_pos`, `field_<n>`) akan submit dengan HTTP 200
        sementara kolom DB kosong dan kunci siswa jatuh ke token ujian.
        Server membaca `identity_data` lebih dulu sebelum kolom top-level
        (`api/exams.go:1006-1033`), jadi sisipan ini membuat kolom DB
        otoritatif untuk setiap konfigurasi tanpa perubahan server.

        Key asli tidak pernah hilang (validasi server memakai key yang
        TERSIMPAN) dan nilai kanonik yang sudah ada tidak ditimpa —
        lihat `utils.identity_data_with_canonical`.
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
        return identity_data_with_canonical(data)
