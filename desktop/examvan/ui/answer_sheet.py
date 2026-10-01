"""Answer sheet widget — all 5 question types."""

from __future__ import annotations

import logging
from typing import Any, Dict, List, Optional

from PyQt5.QtCore import QEvent, QObject, Qt, pyqtSignal
log = logging.getLogger(__name__)

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


class _PopupWheelGuard(QObject):
    """Keep the scroll area still while a combo popup is open.

    A QComboBox popup is a separate top-level window, so it is NOT part of the
    QScrollArea hierarchy that holds the answer sheet. A wheel event that lands
    on the combo is not consumed by the combo either, so it keeps travelling up
    the hierarchy and scrolls the SHEET instead of the option list.

    The consequence, reported from Windows on 30 September 2026: the combo
    slid away, the popup stayed where it was (it is not a child of the scroll
    area, so it does not move with it), and the option list appeared to vanish
    at the exact moment the student was choosing. With a long right-hand column
    the options below the fold were unreachable that way too.

    While the popup is open this filter:

      * consumes the wheel event (returns True), so Qt stops at the combo and
        the scroll area never moves; and
      * applies the wheel delta to the popup's own scrollbar, so a long list is
        still navigable.

    The filter is installed on the combo AND on the popup view, because the two
    are not the same delivery path: depending on whether the popup has taken
    the mouse grab, a wheel lands on one or the other, and both must be covered.

    With the popup closed it returns False and everything behaves exactly as
    before — the wheel scrolls the answer sheet again. Non-wheel events are
    always passed through; a filter that swallowed them would make the combo
    unusable.
    """

    def __init__(self, combo: QComboBox, parent=None):
        super().__init__(parent)
        self._combo = combo

    def _popup_is_open(self) -> bool:
        view = self._combo.view()
        # The popup container is the view's top-level window; the view itself
        # stays "visible" as a child of it, so isVisible() on the view is not
        # a reliable signal.
        return bool(view) and view.window().isVisible()

    def eventFilter(self, obj, event) -> bool:
        if event.type() != QEvent.Wheel:
            return False
        if not self._popup_is_open():
            return False
        self._scroll_popup(event)
        return True

    def _scroll_popup(self, event) -> None:
        view = self._combo.view()
        bar = view.verticalScrollBar()
        # Sign matters and is the opposite of the intuitive one:
        # angleDelta().y() > 0 means the wheel was rotated AWAY from the user,
        # which scrolls the list UP, i.e. towards a SMALLER scrollbar value.
        # (Verified against a QAbstractItemView's own wheel handling.)
        delta = event.angleDelta().y()
        if not delta:
            pixel = event.pixelDelta().y()
            delta = pixel if pixel else 0
        if not delta:
            return
        bar.setValue(bar.value() - delta)


class _BrokenQuestion(Exception):
    """Konfigurasi satu soal tidak sah — tidak bisa dijawab dengan benar.

    Audit 2 Okt 2026 (HIGH H8): soal yang rusak dulu punya dua nasib yang
    sama-sama salah dan TIDAK KONSISTEN satu sama lain:

    * `choices` null/tipe salah → TypeError di tengah build → blok dilewati
      (tanpa widget) TAPI tetap masuk hitungan "N / M terjawab" — siswa
      memburu soal yang tidak bisa diajawab sampai waktu habis;
    * `type` tidak dikenal → fallback diam-diam ke pilihan ganda A–E palsu
      — siswa menjawab sesuatu yang tidak pernah dinilai sesuai tipenya.

    Sekarang soal rusak DITANDAI: tidak ada widget, tidak masuk hitungan,
    nomornya dikembalikan lewat `broken_question_numbers()` supaya
    ExamViewer menampilkannya sebagai peringatan yang bisa dilaporkan
    siswa ke pengawas (konfigurasi ujian harus diperbaiki guru).
    """


class AnswerSheetWidget(QWidget):
    """Dynamic answer sheet built from question config."""

    answer_changed = pyqtSignal(str, object)  # (question_number_str, value)

    def __init__(self, panel_color: str = "#6366f1", parent=None):
        super().__init__(parent)
        self._panel_color = panel_color
        self._questions: List[Dict[str, Any]] = []
        self._answer_widgets: Dict[str, Any] = {}
        self._answers: Dict[str, Any] = {}
        # Nomor soal yang bentrok pada build terakhir — konfigurasi ujian
        # yang rusak; lihat build_from_questions.
        self._duplicates: List[str] = []
        # Nomor soal yang GAGAL dibangun (struktur tidak sah) pada build
        # terakhir — lihat _BrokenQuestion. Tidak masuk hitungan terjawab.
        self._broken_numbers: List[str] = []
        # Nomor yang VALID untuk payload/hitungan, diisi oleh build terakhir.
        # None = belum pernah build (hitungan fallback dihitung dari config).
        self._counted_numbers: Optional[List[str]] = None
        self._pending_entry: Any = None
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

    @staticmethod
    def _canonical_number(raw: Any) -> Optional[str]:
        """Nomor soal kanonik, atau None bila tidak sah.

        Audit 2 Okt 2026 (HIGH H7): `str(int(raw))` memotong nomor
        DESIMAL — server menerima dan menilai soal "2.5" dengan kunci
        "2.5" (webui `normalizeQNum`), tapi klien mengirimnya sebagai "2".
        Jawaban siswa mendarat di slot yang tidak ada di kunci guru dan
        soal itu dinilai 0 TANPA pesan apa pun.

        Sekarang: int → "3"; float 2.5 → "2.5"; string "2.5" → "2.5";
        string "07" → "7" (normalisasi, sama dengan Android). bool dan
        bentuk lain → None (bukan 1 — `int(True) == 1` dulu meloloskan
        `"number": true` dari config yang rusak).
        """
        if isinstance(raw, bool):
            return None
        if isinstance(raw, int):
            return str(raw)
        if isinstance(raw, float):
            if raw != raw:  # NaN
                return None
            if raw.is_integer():
                return str(int(raw))
            return repr(raw)
        if isinstance(raw, str):
            text = raw.strip()
            if not text:
                return None
            try:
                return str(int(text))
            except ValueError:
                pass
            try:
                value = float(text)
            except ValueError:
                return None
            if value.is_integer():
                return str(int(value))
            return repr(value)
        return None

    @classmethod
    def _question_number(cls, raw: Any, position: int, taken: set) -> str:
        """Nomor soal sebagai string, tanpa PERNAH melempar.

        `int(q.get("number", 0))` sebelumnya melempat pada dua payload nyata
        dari editor admin:

        * kolom Nomor dikosongkan -> `parseInt("")` -> NaN -> JSON `null`,
          jadi `int(None)` -> TypeError;
        * nomor berisi teks -> ValueError.

        `dict.get(k, default)` hanya memakai default saat kuncinya tidak
        ADA, jadi `{"number": null}` tetap memanggil `int(None)`.

        Nomor sah (termasuk DESIMAL — lihat _canonical_number) dipakai apa
        adanya. Nomor CADANGAN diberi awalan "soal-" yang tidak mungkin
        dipakai nomor asli (yang selalu numerik), dan tidak perlu loop
        penghindaran tabrakan.
        """
        canonical = cls._canonical_number(raw)
        if canonical is not None:
            return canonical
        # Unik SECARA KONSTRUKSI: `position` berbeda untuk setiap soal,
        # dan prefiks "soal-" tidak mungkin sama dengan nomor soal asli
        # yang selalu numerik. Jadi tidak perlu loop penghindar
        # tabrakan -- dan jebakan loop yang tidak bisa diuji hanya
        # menambah cabang yang tidak pernah dieksekusi.
        #
        # `taken` tetap dipakai di `build_from_questions` untuk
        # memisahkan nomor cadangan dari nomor ASLI -- yang boleh
        # benar-benar kembar, dan itu yang dilaporkan ke siswa.
        candidate = f"soal-{position + 1}"
        log.warning(
            "soal #%d punya nomor tidak valid (%r); memakai nomor %s. "
            "Nilai ini akan ikut terkirim ke server, jadi perbaiki "
            "nomornya di editor.",
            position + 1, raw, candidate,
        )
        return candidate

    def build_from_questions(self, questions: List[Dict[str, Any]]) -> List[str]:
        """Build answer sheet from question config list.

        Returns the question numbers that appear MORE THAN ONCE. Those make
        the exam config broken — see the note below; the caller is expected
        to surface them, not swallow them.
        """
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
        #
        # Nomor soal DUPLIKAT
        # -----------------------
        # `_answer_widgets` di-key dengan nomor soal, jadi dua soal bernomor
        # sama menimpa satu sama lain: DUA blok tergambar di layar, hanya
        # SATU yang tercatat. Jawaban blok pertama menimpa (dan ditimpa)
        # blok kedua, dan `restore_answers` juga tidak bisa memulihkannya
        # karena entri pertamanya sudah hilang.
        #
        # Server hanya memvalidasi kunci jawaban (`validateQuestionKeys`,
        # admin/exams.go) — nomor duplikat tidak pernah dicek, jadi guru bisa
        # menyimpannya tanpa tahu. Di sisi siswa hasilnya satu blok soal
        # yang dijawab hilang tanpa pesan.
        #
        # Yang dilakukan di sini: widget disimpan di bawah kunci INTIK
        # ("1", "1#1", ...) supaya tidak ada blok yang terlantar, nomor
        # bentrok dikembalikan agar bisa dilaporkan, dan hitungan memakai
        # nomor DISTINKT agar cocok dengan payload (kontrak server:
        # `answers = {nomor: nilai}` — dua soal bernomor sama memang tidak
        # bisa diangkut dua-duanya).
        seen: Dict[str, int] = {}
        duplicates: List[str] = []
        broken: List[str] = []
        # Nomor soal yang widget-nya BERHASIL dibangun — inilah yang masuk
        # hitungan terjawab dan payload. Di-set SEBELUM loop, bukan
        # sesudahnya: kalau satu soal melempar exception, atribut-atribut
        # ini tidak pernah ter-assign dan pemanggil masih membaca sisa
        # build sebelumnya.
        self._duplicates = duplicates
        self._broken_numbers = broken
        counted: List[str] = []

        # Nomor yang sudah dipakai nomor ASLI lain (kanonik — desimal
        # "2.5" tetap "2.5", bukan terpotong). Nomor cadangan tidak
        # boleh masuk ke sini -- itulah yang membuat keduanya menabrak.
        reserved = {
            num for num in (
                self._canonical_number(q.get("number"))
                for q in questions if isinstance(q, dict)
            )
            if num is not None
        }
        for position, q in enumerate(questions):
            try:
                num = self._question_number(q.get("number"), position, reserved)
                qtype = q.get("type", "single_choice")
                group, entry = self._build_question_widget(num, qtype, q)

                key = num
                if key in self._answer_widgets:
                    if num not in duplicates:
                        duplicates.append(num)
                    key = f"{num}#{seen.get(num, 0)}"
                seen[num] = seen.get(num, 0) + 1

                self._answer_widgets[key] = entry
                self._container_layout.insertWidget(
                    self._container_layout.count() - 1, group
                )
                # Soal ini bisa dijawab → nomornya masuk hitungan
                # (distinkt; dua soal bernomor sama = satu slot).
                if num not in counted:
                    counted.append(num)
            except _BrokenQuestion as exc:
                # Struktur soal tidak sah (HIGH H8): TIDAK dibuatkan widget
                # dengan pilihan palsu, dan TIDAK dihitung — siswa tidak
                # menghabiskan waktu memburu blok yang tidak ada. Nomornya
                # dilaporkan lewat broken_question_numbers().
                num = self._question_number(q.get("number"), position, reserved)
                if num not in broken:
                    broken.append(num)
                log.warning(
                    "soal #%d (%s) tidak sah dan dilewati: %s",
                    position + 1, num, exc,
                )
                continue
            except Exception:
                # Satu soal rusak TIDAK BOLEH menghentikan build: pemanggil
                # ini adalah slot Qt tanpa try/except, jadi exception yang
                # lolos membuat semua soal setelahnya tidak punya widget dan
                # tidak bisa dijawab, `_duplicates` tak pernah terisi, dan
                # label status tidak pernah diperbarui. Soal yang gagal di
                # sini juga tidak boleh dihitung — sama seperti _BrokenQuestion.
                log.exception("soal #%d gagal dibangun; dilewati", position + 1)
                try:
                    num = self._question_number(q.get("number"), position, reserved)
                    if num not in broken:
                        broken.append(num)
                except Exception:
                    pass
                continue

        self._duplicates = duplicates
        self._broken_numbers = broken
        self._counted_numbers = counted
        self._update_count()
        return duplicates

    def broken_question_numbers(self) -> List[str]:
        """Nomor soal yang struktur tidak sah pada build terakhir.

        Dipakai ExamViewer untuk menampilkan peringatan — soal ini tidak
        bisa dijawab dan tidak dihitung, jadi siswa harus tahu bahwa itu
        masalah konfigurasi (diperbaiki guru), bukan dia lupa menjawab.
        """
        return list(self._broken_numbers)

    def duplicate_question_numbers(self) -> List[str]:
        """Nomor soal yang bentrok pada build terakhir (lihat di atas)."""
        return list(self._duplicates)

    def question_numbers(self) -> List[str]:
        """Nomor soal DISTINKT yang BISA DIJAWAB, sesuai urutan konfigurasi.

        Inilah yang dihitung di label "N / M terjawab": payload submit memakai
        nomor soal sebagai kunci, jadi dua soal bernomor sama hanya
        menyumbang satu slot. Menghitung per blok akan melaporkan
        "2 / 2 terjawab" untuk satu jawaban.

        Setelah build, hasilnya diambil dari daftar yang dihitung PADA SAAT
        build (lihat `_counted_numbers`): soal yang strukturnya rusak
        tidak punya widget dan TIDAK dihitung — konsisten dengan layar,
        bukan berhitung ulang dari config dan melaporkan soal hantu
        (audit HIGH H8).
        """
        if self._counted_numbers is not None:
            return list(self._counted_numbers)
        out: List[str] = []
        reserved = {
            num for num in (
                self._canonical_number(q.get("number"))
                for q in self._questions if isinstance(q, dict)
            )
            if num is not None
        }
        for position, q in enumerate(self._questions):
            # Memakai helper yang sama dengan build: nomor rusak tidak
            # boleh membuat `get_answered_count()` -- yang dipanggil SETIAP
            # kali siswa menjawab -- melempar dari dalam slot Qt.
            # `taken=out` menjaga hasil build dan hasil hitung ini
            # konsisten: nomor cadangan yang sama tidak boleh muncul dua
            # kali hanya karena dihitung ulang.
            num = self._question_number(q.get("number"), position, reserved)
            if num not in out:
                out.append(num)
        return out

    def _build_question_widget(
        self, num: str, qtype: str, q: Dict[str, Any]
    ) -> tuple:
        """Build a single question widget.

        Returns `(group_box, entry)` instead of registering itself in
        `_answer_widgets`: `build_from_questions` yang memutuskan kuncinya,
        karena hanya dia yang tahu soal nomor mana yang bentrok.
        """
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
            # Tipe tidak dikenal/null TIDAK lagi difabricasi jadi pilihan
            # ganda A–E (audit HIGH H8): pilihan palsu membuat siswa
            # menjawab sesuatu yang tidak pernah dinilai sesuai tipenya.
            # Lempar _BrokenQuestion → nomor dilaporkan, tidak dihitung.
            raise _BrokenQuestion(
                f"tipe soal tidak dikenal: {qtype!r}"
            )

        return group, self._pending_entry

    @staticmethod
    def _valid_string_list(value: Any) -> bool:
        """True hanya untuk list berisi minimal satu item scalar."""
        if not isinstance(value, list) or not value:
            return False
        return all(isinstance(item, (str, int, float)) for item in value)

    def _build_single_choice(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        choices = q.get("choices", ["A", "B", "C", "D", "E"])
        # HIGH H8: choices null/tipe salah/kosong → soal rusak, BUKAN
        # TypeError yang dilewati diam-diam dan tetap dihitung, dan BUKAN
        # fallback A–E palsu.
        if not self._valid_string_list(choices):
            raise _BrokenQuestion(
                f"choices tidak sah ({type(choices).__name__!s})"
            )
        choices = [str(c) for c in choices]
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
        self._pending_entry = ("single", btn_group)

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
            # Closure, bukan lookup `self._answer_widgets[num]`: dengan
            # nomor bentrok, kunci internal bukan `num` dan lookup akan
            # mengambil daftar checkbox dari blok yang salah.
            cb.stateChanged.connect(
                lambda state, boxes=checkboxes, n=num: self._on_multi_changed(n, boxes)
            )
            checkboxes.append(cb)
            layout.addWidget(cb)
        self._pending_entry = ("multi", checkboxes)

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
        self._pending_entry = ("single", btn_group)

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
            # Keep the sheet still while the popup is open — see
            # _PopupWheelGuard for why this is needed at all.
            guard = _PopupWheelGuard(combo)
            combo.installEventFilter(guard)
            combo.view().installEventFilter(guard)
            # Keep a reference: a parentless QObject would be garbage
            # collected and the filter would vanish without a word.
            combo.wheel_guard = guard
            # Kunci = TEKS ITEM KIRI, bukan indeks baris.
            #
            # Server menilai dengan `evaluateMatching(studentMap,
            # correctMap, ...)`; `correctMap` disusun guru dengan kunci
            # berupa teks item kiri, dan klien Android juga memakai
            # `answers[leftItem]`. Kunci "1"/"2"/"3" tidak pernah ada di
            # sana, jadi begitu guru menulis item kiri sungguhan ("Ibu
            # Kota") soalnya dinilai SALAH untuk semua siswa desktop,
            # sementara Android untuk ujian yang sama dinilai benar.
            left_key = str(left)
            if left_key in combos:      # item kiri kembar
                left_key = f"{left_key}#{len(combos)}"
            # Restore saved
            saved_val = saved.get(left_key, "")
            if saved_val:
                idx = right_items.index(saved_val) + 1 if saved_val in right_items else 0
                combo.setCurrentIndex(idx)
            combo.currentIndexChanged.connect(
                lambda idx, n=num, li=left_key, ri_items=right_items: (
                    self._on_match_changed(n, li, idx, ri_items)
                )
            )
            combos[left_key] = combo
            row_layout.addWidget(combo)
            layout.addLayout(row_layout)

        self._pending_entry = ("matching", combos)

    def _build_short_answer(self, num: str, q: Dict, layout: QVBoxLayout) -> None:
        line = QLineEdit()
        line.setPlaceholderText("Ketik jawaban...")
        line.setMaxLength(500)
        line.setText(str(self._answers.get(num, "")))
        line.textChanged.connect(lambda text, n=num: self._on_answer_changed(n, text))
        layout.addWidget(line)
        # lewat _pending_entry, seperti _build_single_choice / _build_multi /
        # _build_matching. Menulis langsung ke _answer_widgets membuat
        # `build_from_questions` selalu melihat num sudah ada, sehingga
        # SETIAP soal short_answer dilaporkan sebagai nomor bentrok --
        # peringatan palsu ke siswa dan log.error ke guru, sekali per submit.
        self._pending_entry = ("short", line)

    def _on_answer_changed(self, num: str, value: Any) -> None:
        self._answers[num] = value
        self._update_count()
        self.answer_changed.emit(num, value)

    def _on_multi_changed(self, num: str, checkboxes: Optional[list] = None) -> None:
        if checkboxes is None:
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
        nums = self.question_numbers()
        total = len(nums)
        answered = 0
        for num in nums:
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

        # Update UI widgets. Kunci internal bisa "1#1" kalau nomor soal
        # bentrok; payloadnya tetap nomor asli.
        for slot, (wtype, widget) in self._answer_widgets.items():
            num = slot.split("#", 1)[0]
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
                # Buang entri yang isinya TIDAK ada di(right_items).
                # `self._answers = val` di atas sudah menyalin semua
                # nilai mentah ke payload, jadi nilai yang tidak dikenal
                # akan terkirim ke server apa adanya. Itu bukan hanya
                #sia-sia: `evaluateMatching` membandingkan string,
                # jadi entri sampah apa pun == jawaban salah.
                if isinstance(val, dict):
                    known = {}
                    for key, combo in widget.items():
                        options = {
                            combo.itemText(i) for i in range(combo.count())
                        }
                        candidate = val.get(key)
                        if candidate is not None and str(candidate) in options:
                            known[key] = str(candidate)
                    if known != val:
                        self._answers[num] = known
                for key, combo in widget.items():
                    saved_val = val.get(key, "") if isinstance(val, dict) else ""
                    if not saved_val:
                        continue
                    # PERSIS, sama seperti `_build_matching` (line ~414).
                    #
                    # `itemText(i).startswith(saved_val)` dulu dipakai di
                    # sini, dan itu menimpa jawaban siswa:
                    #
                    #     sebelum restore : {'Ibu Kota': 'A'}
                    #     setelah restore : {'Ibu Kota': 'Andi'}
                    #
                    # karena "Andi".startswith("A") benar. Itu memicu
                    # `currentIndexChanged` -> `_on_match_changed` ->
                    # `answer_changed` -> autosave 500 ms, jadi jawaban
                    # rusak langsung ditulis ke disk.
                    #
                    # Dan `restore_answers` adalah jalur UTAMA di
                    # produksi: dijadwalkan 500 ms setelah window dibuat,
                    # sedangkan `_build_matching` baru jalan setelah PDF
                    # selesai -- jadi pemulihan persis di build biasanya
                    # melihat `_answers` kosong.
                    target = str(saved_val)
                    found = -1
                    for i in range(combo.count()):
                        if combo.itemText(i) == target:
                            found = i
                            break
                    # found <= 0 berarti: tidak ada, atau hanya placeholder
                    # "-- Pilih --". Jangan sentuh combo: memaksa index
                    # akan memicu _on_match_changed dan menulis nilai
                    # yang tidak dikenal itu ke _answers.
                    if found > 0:
                        combo.setCurrentIndex(found)
        self._update_count()

    def get_answered_count(self) -> tuple:
        """Return (answered, total), dihitung per nomor DISTINKT.

        Payload submit memakai nomor soal sebagai kunci, jadi dua soal
        bernomor sama hanya menyumbang satu slot. Menghitung per blok akan
        melaporkan "2 / 2 terjawab" untuk satu jawaban.
        """
        nums = self.question_numbers()
        answered = 0
        for num in nums:
            val = self._answers.get(num)
            if val is not None and val != "" and val != [] and val != {}:
                answered += 1
        return answered, len(nums)
