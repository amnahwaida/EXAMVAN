"""MEDIUM — tiga cacat `identity_dialog` yang membuat siswa terjebak.

1. Error field kosong TIDAK di-scroll ke layar
---------------------------------------------
`_on_submit` memberi fokus ke field pertama yang kosong, tapi tidak pernah
menyrolled `QScrollArea`-nya. Pada 1024x600 dengan 8 field, field #0 yang
kosong ada di y=-145 (di luar viewport) dan scrollbar tetap di 327: siswa
menekan Enter dan TIDAK ADA YANG TERJADI. Dengan 30 field, y=-1705.

Yang Dibuktikan bukan "batas layout": setelah `ensureWidgetVisible` eksplisit,
scroll jadi 156 dan pesan error ada di y=62 — terlihat.

2. `_CardScrollArea` masih ada di rantai Tab
--------------------------------------------
focusPolicy 11 (`StrongFocus`) dan urutan tab hanya mengunci
field→field→tombol, jadi satu Tab lewat tombol mendarat di scroll area:
fokus hilang ke dead-zone dan tidak ada cincin fokus yang terlihat.
Urutan terukur: ['nama', '07', 'Masuk Ujian', '_CardScrollArea:', 'nama'].

3. Nilai yang hanya berisi karakter yang di-strip server
--------------------------------------------------------
`QLineEdit` tidak punya `setMaxLength`, sedangkan server memotong nilai
identitas ke 200 rune (`webui/internal/handlers/api/exams.go:205-220`) dan
membuang `unicode.IsControl`/Cf lebih dulu. Dua akibat:

  * nama 260 rune → client key 260 rune tapi baris tersimpan 200 →
    `HasSubmissionForStudentKey` (models/repeat_grant.go) tidak akan pernah
    cocok lagi → siswa terkunci `repeat_required` selamanya, dan namanya
    terpotong diam-diam di hasil publik;
  * `{"nama": "<U+200B>"}` → server membalas 400 "wajib diisi" SELAMANYA
    tanpa satu pesan pun di sisi siswa — nyata untuk nama yang ditempel dari
    dokumen yang membawa zero-width.

Semua diuji dengan event loop nyata dan geometri viewport yang diukur, bukan
diasumsikan.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QPoint, Qt, QTimer
from PyQt5.QtWidgets import QApplication, QDialog

APP = QApplication.instance() or QApplication([])


def _run_loop(ms: int) -> None:
    stopper = QTimer()
    stopper.setSingleShot(True)
    stopper.timeout.connect(APP.quit)
    stopper.setInterval(ms)
    stopper.start()
    try:
        APP.exec_()
    finally:
        stopper.stop()


def _exam(field_count: int, label: str = "Kolom"):
    from examvan.models import Exam

    return Exam.from_json({
        "id": 904, "name": "Ujian", "status": "active",
        "security_level": "low",
        "identity_fields": [
            {"key": f"f{i}", "label": f"{label} {i}", "required": True}
            for i in range(field_count)
        ],
    })


def _dialog(field_count: int, size=(1024, 600)):
    from examvan.ui.identity_dialog import IdentityDialog

    dlg = IdentityDialog(exam=_exam(field_count))
    dlg.resize(*size)
    dlg.show()
    _run_loop(150)
    return dlg


def _viewport_scroll(dlg) -> int:
    return dlg._scroll.verticalScrollBar().value()


def _y_in_viewport(dlg, widget) -> int:
    """Posisi widget relatif viewport scroll (negatif = di luar layar)."""
    viewport = dlg._scroll.viewport()
    top_left = widget.mapTo(viewport, QPoint(0, 0))
    return top_left.y()


class ScrollToTheEmptyFieldTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def test_the_first_empty_field_is_scrolled_into_view(self):
        dlg = _dialog(8)
        self.addCleanup(dlg.deleteLater)
        # Isi semua kecuali yang PERTAMA.
        keys = list(dlg._inputs)
        for key in keys[1:]:
            dlg._inputs[key].setText("isi")
        _run_loop(80)
        # Prasyarat yang benar-benar terjadi di lapangan: siswa mengisi form
        # dari atas ke bawah, jadi pandangannya berada di BAWAH. Di situ
        # field kosong pertama ada di y=-145 (terukur auditor: scroll 327,
        # `y_in_viewport` -145) dan pesan error-nya juga di luar layar.
        scrollbar = dlg._scroll.verticalScrollBar()
        self.assertGreater(
            scrollbar.maximum(), 0,
            "form tidak bisa di-scroll di ukuran ini — test tidak "
            "menguji apa pun",
        )
        scrollbar.setValue(scrollbar.maximum())
        _run_loop(60)
        scroll_before = _viewport_scroll(dlg)
        self.assertLess(
            _y_in_viewport(dlg, dlg._inputs[keys[0]]), 0,
            "prasyarat gagal: field kosong ternyata terlihat — test tidak "
            "menguji apa pun",
        )

        with mock.patch("examvan.ui.identity_dialog.QMessageBox"):
            dlg._on_submit()
        _run_loop(150)

        self.assertEqual(
            dlg.result(), 0,
            "dialog tidak boleh accept — field pertama masih kosong",
        )
        offender = dlg._inputs[keys[0]]
        self.assertIs(
            APP.focusWidget(), offender,
            "fokus tidak ada di field kosong — siswa mengetik ke tempat "
            "yang tidak diketahui",
        )
        self.assertNotEqual(
            _viewport_scroll(dlg), scroll_before,
            "scrollbar tidak bergerak sama sekali: `ensureWidgetVisible` "
            "tidak pernah dipanggil, jadi field yang kosong DAN pesan "
            "errornya sama-sama di luar layar",
        )
        self.assertGreaterEqual(
            _y_in_viewport(dlg, offender), 0,
            "field kosong masih di luar viewport (y<0) — siswa menekan "
            "Enter dan tidak terjadi apa-apa",
        )
        err = dlg._error_labels[keys[0]]
        self.assertFalse(err.isHidden())
        self.assertGreaterEqual(
            _y_in_viewport(dlg, err), 0,
            "pesan error inline juga di luar layar: siswa diberi tahu "
            "ada yang salah, tapi tidak di mana",
        )

    def test_a_long_form_with_the_first_field_empty_is_also_scrolled(self):
        dlg = _dialog(30)
        self.addCleanup(dlg.deleteLater)
        keys = list(dlg._inputs)
        for key in keys[1:]:
            dlg._inputs[key].setText("isi")
        _run_loop(80)
        scrollbar = dlg._scroll.verticalScrollBar()
        scrollbar.setValue(scrollbar.maximum())
        _run_loop(60)
        self.assertLess(
            _y_in_viewport(dlg, dlg._inputs[keys[0]]), 0,
            "prasyarat gagal: field kosong terlihat — test tidak menguji apa "
            "pun",
        )
        with mock.patch("examvan.ui.identity_dialog.QMessageBox"):
            dlg._on_submit()
        _run_loop(150)
        self.assertGreaterEqual(
            _y_in_viewport(dlg, dlg._inputs[keys[0]]), 0,
            "form 30 field: field kosong pertama tetap di luar layar",
        )


class TabChainTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def test_tabbing_from_the_button_never_lands_on_the_scroll_area(self):
        dlg = _dialog(3)
        self.addCleanup(dlg.deleteLater)
        dlg.activateWindow()
        _run_loop(120)

        dlg._submit_btn.setFocus(Qt.TabFocusReason)
        _run_loop(40)
        self.assertIs(APP.focusWidget(), dlg._submit_btn,
                      "setup gagal: tombol submit tidak menerima fokus")

        visited = []
        for _ in range(3):
            dlg.focusNextChild()
            _run_loop(20)
            widget = APP.focusWidget()
            visited.append(widget)
            if widget is dlg._inputs[list(dlg._inputs)[0]]:
                break
        self.assertNotIn(
            dlg._scroll, visited,
            "satu Tab lewat tombol mendarat di `_CardScrollArea`: fokus "
            "hilang ke dead-zone (tidak ada cincin fokus yang terlihat, "
            "siswa tidak tahu sedang di mana)",
        )


class InputLimitsTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        for _ in range(3):
            for w in APP.topLevelWidgets():
                try:
                    w.hide()
                except Exception:
                    pass
            APP.processEvents()

    def test_no_input_accepts_more_than_the_server_stores(self):
        dlg = _dialog(2)
        self.addCleanup(dlg.deleteLater)
        first = dlg._inputs[list(dlg._inputs)[0]]
        first.setText("A" * 260)
        _run_loop(60)
        self.assertLessEqual(
            len(first.text()), 200,
            f"klien menerima {len(first.text())} rune sementara server "
            f"memotong ke 200: kunci yang dikirim tidak akan pernah cocok "
            f"dengan baris tersimpan, jadi `HasSubmissionForStudentKey` "
            f"selalu gagal dan siswa terkunci `repeat_required` selamanya",
        )
        self.assertEqual(
            first.maxLength(), 200,
            "batas per-karakter harus dipasang di kotak input, bukan "
            "dibiarkan server yang memotong diam-diam",
        )

    def test_a_value_made_only_of_stripped_characters_is_rejected_locally(self):
        # `stripControlAndBidi` di server membuang Cf (U+200B ZERO WIDTH
        # SPACE) sebelum TrimSpace, jadi `{"nama": "<U+200B>"}` menjadi
        # kosong -> 400 "wajib diisi" selamanya tanpa pesan di sisi siswa.
        dlg = _dialog(2)
        self.addCleanup(dlg.deleteLater)
        keys = list(dlg._inputs)
        dlg._inputs[keys[0]].setText("\u200b")
        dlg._inputs[keys[1]].setText("Andi")
        _run_loop(80)

        with mock.patch("examvan.ui.identity_dialog.QMessageBox") as box:
            dlg._on_submit()
        _run_loop(80)

        self.assertNotEqual(
            dlg.result(), QDialog.Accepted,
            "dialog menerima nilai yang PASTI ditolak server: "
            "`stripControlAndBidi` membuang U+200B lebih dulu, jadi "
            "field ternilai kosong dan server membalas 400 selamanya",
        )
        err = dlg._error_labels[keys[0]]
        self.assertFalse(
            err.isHidden(),
            "tidak ada pesan yang menjelaskan kenapa isian itu ditolak — "
            "siswa melihat kotak yang ia isi nonempty tapi tetap ditolak",
        )
        self.assertIn(
            "wajib diisi", err.text(),
            f"pesan error tidak menjelaskan masalah sebenarnya: {err.text()!r}",
        )
        box.warning.assert_not_called()


if __name__ == "__main__":
    unittest.main()