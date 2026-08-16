"""Answer sheet widget — all 5 question types."""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from PyQt5.QtCore import Qt, pyqtSignal
from PyQt5.QtWidgets import (
    QButtonGroup,
    QCheckBox,
    QComboBox,
    QGroupBox,
    QLabel,
    QLineEdit,
    QPushButton,
    QRadioButton,
    QScrollArea,
    QVBoxLayout,
    QWidget,
)


class AnswerSheetWidget(QWidget):
    """Dynamic answer sheet built from question config."""

    answer_changed = pyqtSignal(str, object)  # (question_number_str, value)

    def __init__(self, panel_color: str = "#6366f1", parent=None):
        super().__init__(parent)
        self._panel_color = panel_color
        self._questions: List[Dict[str, Any]] = []
        self._answer_widgets: Dict[str, Any] = {}
        self._answers: Dict[str, Any] = {}
        self._setup_ui()

    def _setup_ui(self) -> None:
        self._layout = QVBoxLayout(self)
        self._layout.setContentsMargins(8, 8, 8, 8)
        self._layout.setSpacing(4)

        # Header
        self._header = QLabel("Lembar Jawaban")
        self._header.setStyleSheet(
            f"font-size: 16px; font-weight: bold; color: white;"
            f"background-color: {self._panel_color};"
            f"padding: 10px; border-radius: 6px;"
        )
        self._header.setAlignment(Qt.AlignCenter)
        self._layout.addWidget(self._header)

        # Answered count
        self._lbl_count = QLabel("0 / 0 terjawab")
        self._lbl_count.setStyleSheet("font-size: 12px; padding: 4px;")
        self._lbl_count.setAlignment(Qt.AlignCenter)
        self._layout.addWidget(self._lbl_count)

        # Scroll area for questions
        self._scroll = QScrollArea()
        self._scroll.setWidgetResizable(True)
        self._scroll.setHorizontalScrollBarPolicy(Qt.ScrollBarAlwaysOff)

        self._container = QWidget()
        self._container_layout = QVBoxLayout(self._container)
        self._container_layout.setContentsMargins(0, 0, 0, 200)
        self._container_layout.setSpacing(8)
        self._container_layout.addStretch()

        self._scroll.setWidget(self._container)
        self._layout.addWidget(self._scroll, 1)

    def build_from_questions(self, questions: List[Dict[str, Any]]) -> None:
        """Build answer sheet from question config list."""
        self._questions = questions
        self._answer_widgets.clear()

        # Clear existing widgets
        while self._container_layout.count() > 1:  # Keep the stretch
            item = self._container_layout.takeAt(0)
            if item.widget():
                item.widget().deleteLater()

        # JANGAN fabrikasi soal default saat questions kosong (sebelumnya:
        # 40 soal single-choice A-E palsu). Ujian PDF-only / tanpa konfigurasi
        # soal TIDAK punya lembar jawaban — siswa tidak boleh bisa menjawab
        # soal yang tidak ada (mirror Android: questions kosong → totalQuestions
        # = 0 + overlay disembunyikan). Lembar tetap kosong, count 0/0.
        for q in questions:
            num = str(int(q.get("number", 0)))
            qtype = q.get("type", "single_choice")
            widget = self._build_question_widget(num, qtype, q)
            self._container_layout.insertWidget(self._container_layout.count() - 1, widget)

        self._update_count()

    def _build_question_widget(
        self, num: str, qtype: str, q: Dict[str, Any]
    ) -> QGroupBox:
        """Build a single question widget based on type."""
        titles = {
            "single_choice": f"Soal {num} — Pilihan Ganda",
            "multiple_choice": f"Soal {num} — Pilihan Ganda Kompleks",
            "true_false": f"Soal {num} — Benar/Salah",
            "matching": f"Soal {num} — Menjodohkan",
            "short_answer": f"Soal {num} — Jawaban Singkat",
        }
        group = QGroupBox(titles.get(qtype, f"Soal {num}"))
        layout = QVBoxLayout(group)
        layout.setContentsMargins(12, 20, 12, 12)
        layout.setSpacing(6)

        if qtype == "single_choice":
            self._build_single_choice(num, q, layout)
        elif qtype == "multiple_choice":
            self._build_multiple_choice(num, q, layout)
        elif qtype == "true_false":
            self._build_true_false(num, q, layout)
        elif qtype == "matching":
            self._build_matching(num, q, layout)
        elif qtype == "short_answer":
            self._build_short_answer(num, q, layout)
        else:
            # Fallback: single choice A-E
            self._build_single_choice(
                num, {"choices": ["A", "B", "C", "D", "E"]}, layout
            )

        return group

    def _build_single_choice(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        choices = q.get("choices", ["A", "B", "C", "D", "E"])
        btn_group = QButtonGroup(self)
        btn_group.setExclusive(True)
        for choice in choices:
            rb = QRadioButton(str(choice))
            rb.setStyleSheet("font-size: 13px; padding: 4px;")
            btn_group.addButton(rb)
            layout.addWidget(rb)
            if self._answers.get(num) == str(choice):
                rb.setChecked(True)
        btn_group.buttonClicked.connect(
            lambda btn, n=num: self._on_answer_changed(n, btn.text())
        )
        self._answer_widgets[num] = ("single", btn_group)

    def _build_multiple_choice(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        choices = q.get("choices", ["A", "B", "C", "D", "E"])
        checkboxes = []
        saved = self._answers.get(num, [])
        if isinstance(saved, str):
            try:
                import json
                saved = json.loads(saved)
            except Exception:
                saved = []
        for choice in choices:
            cb = QCheckBox(str(choice))
            cb.setStyleSheet("font-size: 13px; padding: 4px;")
            if str(choice) in saved:
                cb.setChecked(True)
            cb.stateChanged.connect(lambda state, n=num: self._on_multi_changed(n))
            checkboxes.append(cb)
            layout.addWidget(cb)
        self._answer_widgets[num] = ("multi", checkboxes)

    def _build_true_false(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        btn_group = QButtonGroup(self)
        btn_group.setExclusive(True)
        for val in ["TRUE", "FALSE"]:
            rb = QRadioButton(val)
            rb.setStyleSheet("font-size: 13px; padding: 4px;")
            btn_group.addButton(rb)
            layout.addWidget(rb)
            if self._answers.get(num) == val:
                rb.setChecked(True)
        btn_group.buttonClicked.connect(
            lambda btn, n=num: self._on_answer_changed(n, btn.text())
        )
        self._answer_widgets[num] = ("single", btn_group)

    def _build_matching(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        left_items = q.get("left_items", [])
        right_items = q.get("right_items", [])
        saved = self._answers.get(num, {})
        if isinstance(saved, str):
            try:
                import json
                saved = json.loads(saved)
            except Exception:
                saved = {}

        combos = {}
        for i, left in enumerate(left_items):
            row_layout = QVBoxLayout()
            row_layout.setSpacing(6)
            row_layout.setContentsMargins(0, 6, 0, 6)
            lbl = QLabel(str(left))
            lbl.setStyleSheet("font-size: 13px; font-weight: bold;")
            row_layout.addWidget(lbl)

            combo = QComboBox()
            combo.addItem("-- Pilih --")
            for ri, right in enumerate(right_items):
                combo.addItem(str(right))
            # Restore saved
            saved_val = saved.get(str(i + 1), "")
            if saved_val:
                idx = right_items.index(saved_val) + 1 if saved_val in right_items else 0
                combo.setCurrentIndex(idx)
            combo.currentIndexChanged.connect(
                lambda idx, n=num, li=str(i + 1), ri_items=right_items: (
                    self._on_match_changed(n, li, idx, ri_items)
                )
            )
            combos[str(i + 1)] = combo
            row_layout.addWidget(combo)
            layout.addLayout(row_layout)

        self._answer_widgets[num] = ("matching", combos)

    def _build_short_answer(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        line = QLineEdit()
        line.setPlaceholderText("Ketik jawaban...")
        line.setMaxLength(500)
        line.setText(str(self._answers.get(num, "")))
        line.textChanged.connect(lambda text, n=num: self._on_answer_changed(n, text))
        layout.addWidget(line)
        self._answer_widgets[num] = ("short", line)

    def _on_answer_changed(self, num: str, value: Any) -> None:
        self._answers[num] = value
        self._update_count()
        self.answer_changed.emit(num, value)

    def _on_multi_changed(self, num: str) -> None:
        _, checkboxes = self._answer_widgets.get(num, ("multi", []))
        selected = [cb.text() for cb in checkboxes if cb.isChecked()]
        self._answers[num] = selected
        self._update_count()
        self.answer_changed.emit(num, selected)

    def _on_match_changed(
        self, num: str, left_key: str, combo_idx: int, right_items: list
    ) -> None:
        if num not in self._answers or not isinstance(self._answers[num], dict):
            self._answers[num] = {}
        if combo_idx == 0:
            self._answers[num].pop(left_key, None)
        else:
            self._answers[num][left_key] = right_items[combo_idx - 1]
        self._update_count()
        self.answer_changed.emit(num, self._answers[num])

    def _update_count(self) -> None:
        total = len(self._questions)
        answered = 0
        for q in self._questions:
            num = str(int(q.get("number", 0)))
            val = self._answers.get(num)
            if val is not None and val != "" and val != [] and val != {}:
                answered += 1
        self._lbl_count.setText(f"{answered} / {total} terjawab")

    def get_answers(self) -> Dict[str, Any]:
        """Return all answers as {question_num_str: value}."""
        result = {}
        for num, val in self._answers.items():
            if isinstance(val, list):
                result[num] = val
            elif isinstance(val, dict):
                result[num] = val
            else:
                result[num] = str(val) if val else ""
        return result

    def restore_answers(self, saved: Dict[str, Any]) -> None:
        """Restore answers from saved dict and update UI."""
        self._answers = {}
        for num, val in saved.items():
            self._answers[str(num)] = val

        # Update UI widgets
        for num, (wtype, widget) in self._answer_widgets.items():
            val = self._answers.get(num)
            if val is None:
                continue
            if wtype == "single":
                for btn in widget.buttons():
                    if btn.text() == str(val):
                        btn.setChecked(True)
                        break
            elif wtype == "multi":
                for cb in widget:
                    cb.setChecked(cb.text() in (val if isinstance(val, list) else []))
            elif wtype == "short":
                widget.setText(str(val))
            elif wtype == "matching":
                for key, combo in widget.items():
                    saved_val = val.get(key, "") if isinstance(val, dict) else ""
                    if saved_val:
                        for i in range(combo.count()):
                            if combo.itemText(i).startswith(str(saved_val)):
                                combo.setCurrentIndex(i)
                                break
        self._update_count()

    def get_answered_count(self) -> tuple:
        """Return (answered, total)."""
        total = len(self._questions)
        answered = 0
        for q in self._questions:
            num = str(int(q.get("number", 0)))
            val = self._answers.get(num)
            if val is not None and val != "" and val != [] and val != {}:
                answered += 1
        return answered, total
