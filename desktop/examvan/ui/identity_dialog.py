"""Identity dialog — dynamic fields from API response."""

from __future__ import annotations

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
        self._fields = exam.identity_fields if exam.identity_fields else self._DEFAULT_FIELDS
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
        errors = []
        for field in self._fields:
            inp = self._inputs.get(field.key)
            if not inp:
                continue
            val = inp.text().strip()
            if field.required and not val:
                errors.append(f"{field.label} wajib diisi")

        if errors:
            QMessageBox.warning(self, "Validasi", "\n".join(errors))
            return

        self.accept()

    def get_identity_data(self) -> Dict[str, str]:
        """Return {field_key: value} for all fields."""
        return {
            field.key: self._inputs[field.key].text().strip()
            for field in self._fields
            if field.key in self._inputs
        }
