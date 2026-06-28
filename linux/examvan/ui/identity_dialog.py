"""Identity dialog — dynamic fields from API response."""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import (
    QDialog,
    QLabel,
    QLineEdit,
    QMessageBox,
    QPushButton,
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
        self.showMaximized()

        # Outer layout centers the form card
        outer = QVBoxLayout(self)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.addStretch(2)

        # Centered card container
        card = QWidget()
        card.setFixedWidth(440)
        card.setObjectName("identityCard")
        from .styles import is_system_dark
        if is_system_dark():
            card.setStyleSheet("QWidget#identityCard { background-color: #313244; border: 1px solid #45475a; border-radius: 12px; }")
        else:
            card.setStyleSheet("QWidget#identityCard { background-color: #ffffff; border: 1px solid #ccd0da; border-radius: 12px; }")
        card_layout = QVBoxLayout(card)
        card_layout.setSpacing(10)
        card_layout.setContentsMargins(40, 40, 40, 40)

        # Exam name
        exam_label = QLabel(self._exam.name)
        exam_label.setStyleSheet("font-size: 18px; font-weight: bold;")
        exam_label.setAlignment(Qt.AlignCenter)
        exam_label.setWordWrap(True)
        card_layout.addWidget(exam_label)

        card_layout.addSpacing(16)

        # Dynamic fields
        for field in self._fields:
            lbl = QLabel(field.label + (" *" if field.required else ""))
            lbl.setStyleSheet("font-weight: bold;" if field.required else "")
            card_layout.addWidget(lbl)

            inp = QLineEdit()
            inp.setPlaceholderText(f"Masukkan {field.label.lower()}")
            # Pre-fill from saved data
            saved_val = self._saved.get(field.key, "")
            if saved_val:
                inp.setText(str(saved_val))
            card_layout.addWidget(inp)
            self._inputs[field.key] = inp

        card_layout.addSpacing(12)

        # Submit button
        btn = QPushButton("  Masuk Ujian  ")
        btn.clicked.connect(self._on_submit)
        card_layout.addWidget(btn, alignment=Qt.AlignCenter)

        # Enter key on last field triggers submit
        if self._inputs:
            last_input = list(self._inputs.values())[-1]
            last_input.returnPressed.connect(self._on_submit)

        outer.addWidget(card, alignment=Qt.AlignHCenter)
        outer.addStretch(3)

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
