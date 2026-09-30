"""Lembar jawaban harus jujur soal kunci dan tahan payload rusak (#4, #14).

Bug #4 -- kunci matching adalah indeks baris, bukan teks item kiri
---------------------------------------------------------------
`_build_matching()` merender label dengan teks aslinya:

    lbl = QLabel(str(left))          # "Ibu Kota", "Bandung", ...

tapi menyimpan jawabannya dengan:

    combos[str(i + 1)] = combo                       # "1", "2", "3"
    self._answers[num][left_key] = ...               # left_key = "1"

Server menilainya dengan `evaluateMatching(studentMap, correctMap, ...)`
dan `correctMap` disusun guru dengan kunci berupa TEKS ITEM KIRI. Klien
Android juga begitu (`answers[leftItem] = ...`). Jadi begitu guru menulis
item kiri sungguhan -- "Ibu Kota" -- kunci "1" tidak pernah ada di
`correctMap`, dan soalnya dinilai **selalu salah** untuk semua siswa yang
menggunakan desktop. Android untuk ujian yang sama dinilai benar.

`restore_answers()` konsisten dengan indeks, jadi bug-nya tak terlihat
saat pengujian lokal: jawabannya bisa dibulihkan dengan benar dan tetap
salah saat dinilai.

Bug #14 -- `int(None)` membangun lembar jawaban setengah jadi
-------------------------------------------------------------
`num = str(int(q.get("number", 0)))`. `dict.get(k, default)` hanya
memakai default kalau kuncinya TIDAK ADA, jadi `{"number": None}` -- yang
dihasilkan `parseInt("")` -> `NaN` -> JSON `null` di editor admin saat
kolom Nomor dikosongkan -- memanggil `int(None)` dan melempar
`TypeError`.

`build_from_questions()` tidak punya guard, dan dipanggil dari
`_on_pdf_ready()` / `_on_pdf_error()` TANPA try/except, yaitu dari dalam
Qt slot. Exception-nya keluar dari slot, jadi perulangan berhenti di
tengah: soal-soal setelahnya tidak punya widget dan tidak bisa dijawab,
`self._duplicates` tidak pernah diisi, dan label status tidak pernah
diperbarui. Sebagian PyQt buildesia membawa ini ke `qFatal()`.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

from examvan.ui.answer_sheet import AnswerSheetWidget


def _matching_question(num=1, left=("Ibu Kota", "Gunung", "Sungai"),
                       right=("Bandung", "Semarang", "Malang")):
    """Payload soal matching SEBENARNYA dari server.

    Bentuknya `left_items` + `right_items`, bukan `items: [{left, right}]`
    -- kalau test memakai bentuk karangan, ia tidak pernah menguji apa yang
    production baca.
    """
    return {
        "number": num,
        "type": "matching",
        "left_items": list(left),
        "right_items": list(right),
    }


class MatchingKeyFollowsTheServerTestCase(unittest.TestCase):
    """#4 - kunci harus teks item kiri, sama seperti server dan Android."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.sheet = AnswerSheetWidget()
        self.addCleanup(self.sheet.deleteLater)

    def _combos(self, num="1"):
        """Widget combo untuk soal `num`, seperti yang dirender ke layar."""
        return self.sheet._answer_widgets[num][1]

    def _choose(self, num, key, right_value):
        """Sudahi combo seperti clicksiswa, bukan langsung menulis _answers.

        `_on_match_changed` adalah satu-satunya penulis, jadi menulis
        `_answers` langsung tidak akan menguji apa yang kita klaim menguji.
        """
        combos = self._combos(num)
        combo = combos[key]
        items = [combo.itemText(i) for i in range(combo.count())]
        combo.setCurrentIndex(items.index(str(right_value)))

    def test_answers_are_keyed_by_the_left_item_text(self):
        self.sheet.build_from_questions([_matching_question()])
        self._choose("1", "Ibu Kota", "Bandung")
        self._choose("1", "Gunung", "Semarang")

        answers = self.sheet.get_answers()
        self.assertIn("1", answers)
        self.assertEqual(
            answers["1"], {"Ibu Kota": "Bandung", "Gunung": "Semarang"},
            "kunci payload bukan teks item kiri. Server mencocokkan "
            "studentMap dengan correctMap yang kuncinya TEKS ITEM KIRI, "
            "jadi payload ini membuat soalnya selalu dinilai salah.",
        )

    def test_numeric_left_items_still_work(self):
        # Item kiri boleh berupa angka; server tetap memakai teksnya.
        self.sheet.build_from_questions(
            [_matching_question(left=("1", "2", "3"), right=("a", "b", "c"))]
        )
        self._choose("1", "2", "b")
        self.assertEqual(self.sheet.get_answers()["1"], {"2": "b"})

    def test_the_key_does_not_shift_with_row_position(self):
        # Regresi yang harus dicegah: kunci ikut berubah kalau urutan baris
        # berubah -- itu persis geometries index-vs-label.
        self.sheet.build_from_questions([_matching_question()])
        self._choose("1", "Ibu Kota", "Bandung")
        first = dict(self.sheet.get_answers()["1"])

        self.sheet.build_from_questions(
            [_matching_question(left=("Sungai", "Ibu Kota", "Gunung"))]
        )
        self._choose("1", "Ibu Kota", "Bandung")
        after = dict(self.sheet.get_answers()["1"])
        self.assertEqual(
            set(first) & set(after), set(first),
            "kunci berubah saat urutan baris berubah -- itu kembali ke indeks",
        )

    def test_duplicate_left_items_do_not_lose_an_entry(self):
        # Item kiri kembar tidak bisa dipetakan 1:1 ke server, tapi tidak
        # boleh sampai menimpa dirinya sendiri dan hilang begitu saja.
        self.sheet.build_from_questions(
            [_matching_question(left=("A", "A"), right=("x", "y"))]
        )
        combos = self._combos("1")
        self.assertEqual(len(combos), 2, "satu baris hilang")
        self.assertEqual(len(set(combos)), 2, "dua baris berbagi satu kunci")

    def test_saved_answers_round_trip_under_the_text_key(self):
        saved = {"1": {"Ibu Kota": "Bandung", "Gunung": "Semarang"}}
        self.sheet.build_from_questions([_matching_question()])
        self.sheet.restore_answers(saved)
        self.assertEqual(
            self.sheet.get_answers()["1"],
            {"Ibu Kota": "Bandung", "Gunung": "Semarang"},
        )
        combos = self._combos("1")
        self.assertEqual(combos["Ibu Kota"].currentIndex(),
                         [combos["Ibu Kota"].itemText(i)
                          for i in range(combos["Ibu Kota"].count())].index("Bandung"))


class BuildSurvivesBrokenQuestionNumbersTestCase(unittest.TestCase):
    """#14 — payload rusak tidak boleh menghentikan build di tengah."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.sheet = AnswerSheetWidget()
        self.addCleanup(self.sheet.deleteLater)

    def _build_all(self, questions):
        """Build dan laporkan apa pun exception-nya sebagai kegagalan."""
        self.sheet.build_from_questions(questions)

    def test_a_null_question_number_does_not_abort_the_build(self):
        # parseInt("") -> NaN -> JSON null di editor admin.
        questions = [
            {"number": 1, "type": "short_answer"},
            {"number": None, "type": "short_answer"},   # kolom Nomor kosong
            {"number": 3, "type": "short_answer"},
        ]
        self._build_all(questions)
        built = set(self.sheet._answer_widgets)
        self.assertIn("1", built)
        self.assertIn("3", built)

    def test_a_non_numeric_question_number_does_not_abort_the_build(self):
        questions = [
            {"number": "1a", "type": "short_answer"},
            {"number": 2, "type": "short_answer"},
        ]
        self._build_all(questions)
        self.assertIn("2", set(self.sheet._answer_widgets))

    def test_a_missing_question_number_does_not_abort_the_build(self):
        self._build_all([
            {"type": "short_answer"},
            {"number": 2, "type": "short_answer"},
        ])
        self.assertIn("2", set(self.sheet._answer_widgets))

    def test_questions_after_the_bad_one_are_still_answerable(self):
        questions = [
            {"number": None, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 2, "type": "single_choice", "choices": ["A", "B"]},
        ]
        self._build_all(questions)
        # Widget-nya harus ada dan bisa DIJAWAB -- bukan hanya tidak crash.
        self.assertIn("2", self.sheet._answer_widgets)
        group = self.sheet._answer_widgets["2"][1]
        button = next(b for b in group.buttons() if b.text() == "B")
        button.click()
        self.assertEqual(
            self.sheet.get_answers().get("2"), "B",
            "soal setelah yang rusak tidak bisa dijawab -- separuh lembar "
            "jawaban hilang begitu saja",
        )

    def test_one_builder_that_raises_does_not_kill_the_rest(self):
        # `_question_number` sudah menahan nomor rusak, jadi untuk menguji
        # jaring pengaman di level loop perlu builder yang benar-benar
        # gagal -- misalnya `choices` berisi object yang tidak bisa
        # dirender, atau regression di salah satu `_build_*`.
        questions = [
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 2, "type": "explodes"},
            {"number": 3, "type": "single_choice", "choices": ["A", "B"]},
        ]

        real = AnswerSheetWidget._build_question_widget

        def flaky(self_, num, qtype, q):
            if qtype == "explodes":
                raise RuntimeError("renderer gagal")
            return real(self_, num, qtype, q)

        with mock.patch.object(AnswerSheetWidget, "_build_question_widget", flaky):
            self.sheet.build_from_questions(questions)

        built = set(self.sheet._answer_widgets)
        self.assertIn("1", built)
        self.assertIn("3", built, "soal setelah builder yang gagal ikut hilang")
        self.assertEqual(self.sheet._duplicates, [])

    def test_duplicates_flag_is_always_assigned_even_when_broken(self):
        # `_duplicates` hanya diisi setelah loop selesai. Kalau loop
        # terputus, atributnya tetap milik build sebelumnya.
        self._build_all([
            {"number": None, "type": "short_answer"},
            {"number": 5, "type": "short_answer"},
            {"number": 5, "type": "short_answer"},
        ])
        self.assertEqual(self.sheet._duplicates, ["5"])


if __name__ == "__main__":
    unittest.main()