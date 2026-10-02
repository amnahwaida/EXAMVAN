"""Pemulihan jawaban harus persis, dan nomor fallback tidak boleh menabrak (#3, #4).

#3 — `restore_answers` mencocokkan PREFIX, build mencocokkan PERSIS
----------------------------------------------------------------
`_build_matching` memulihkan dengan persamaan persis:

    idx = right_items.index(saved_val) + 1 if saved_val in right_items else 0

`restore_answers` memulihkan dengan awalan:

    if combo.itemText(i).startswith(str(saved_val)):

Konsekuensi yang sudah dijalankan kode sungguhan:

    sebelum restore : {'7': {'Ibu Kota': 'A'}}
    setelah restore : {'7': {'Ibu Kota': 'Andi'}}

`"Andi".startswith("A")` benar, jadi combo diisi "Andi". Itu memicu
`currentIndexChanged` -> `_on_match_changed` -> jawaban siswa tertimpa,
lalu `answer_changed` emit -> `_save_timer.start()` -> map yang sudah
rusak ditulis ke disk 500 ms kemudian.

Dan `restore_answers` adalah jalur UTAMA di produksi: jadwalnya 500 ms
setelah window dibuat (`exam_viewer`), sedangkan `_build_matching` baru
jalan setelah PDF selesai diunduh. Jadi pemulihan persis di
`_build_matching` biasanya melihat `_answers` kosong.

Pemicunya sesederhana `right_items = ["Andi", "Budi", "A"]` -- atau
`["B", "Budi"]`, `["1", "10"]`.

#4 — fallback nomor bentrok dengan nomor soal yang asli
------------------------------------------------------
`_question_number()` mengembalikan `str(position + 1)` kalau nomornya
rusak. `position + 1` berada di RUANG YANG SAMA dengan nomor asli.

    soal #1: {"number": null} -> "1"   (fallback posisi 0+1)
    soal #2: {"number": 1}     -> "1"   (nomor asli)

Di `build_from_questions` blok kedua dapat kunci internal `"1#1"` --
kedua widget memang ter-gambar. Tapi handler-nya memakai `num`, bukan
kunci internal: `_on_answer_changed(n=num, ...)`, `_on_multi_changed`,
`_on_match_changed`. Jadi keduanya menulis `self._answers["1"]` dan
**yang dijawab terakhir menimpa yang lain**; satu blok lenyap dari payload
tanpa jejak.

Verifikasi:

    internal keys : ['1', '1#1', '2']
    payload keys  : []

Semua jawaban dari blok bentrok hilang. Dan siswa melihat peringatan
"soal nomor 1 bernomor ganda" yang secara faktual salah: tidak ada
duplikasi, ada nomor yang hilang.
"""

from __future__ import annotations

import unittest

from PyQt5.QtWidgets import QApplication

from examvan.ui.answer_sheet import AnswerSheetWidget


def _matching(num=7, left=("Ibu Kota",), right=("Andi", "Budi", "A")):
    return {
        "number": num,
        "type": "matching",
        "left_items": list(left),
        "right_items": list(right),
    }


class RestoreAnswersIsExactTestCase(unittest.TestCase):
    """#3 — pemulihan harus persis, bukan awalan."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.sheet = AnswerSheetWidget()
        self.addCleanup(self.sheet.deleteLater)
        self.sheet.build_from_questions([_matching()])

    def test_an_exact_answer_is_not_overwritten_by_a_prefix_match(self):
        # "A" adalah jawaban yang benar; "Andi" hanya berawalan "A".
        self.sheet.restore_answers({"7": {"Ibu Kota": "A"}})
        self.assertEqual(
            self.sheet.get_answers().get("7"), {"Ibu Kota": "A"},
            "jawaban 'A' berubah karena 'Andi'.startswith('A') benar -- "
            "pemulihan jawaban harus persis, bukan awalan",
        )

    def test_a_full_answer_survives_a_restart(self):
        for value in ("Andi", "Budi", "A"):
            with self.subTest(value=value):
                # Lembar baru per nilai: restore_answers memakai setdefault
                # (tidak menimpa jawaban yang sudah ada), jadi satu lembar
                # untuk tiga nilai akan menahan nilai pertama selamanya.
                sheet = AnswerSheetWidget()
                self.addCleanup(sheet.deleteLater)
                sheet.build_from_questions([_matching()])
                sheet.restore_answers({"7": {"Ibu Kota": value}})
                self.assertEqual(
                    sheet.get_answers().get("7"), {"Ibu Kota": value}
                )

    def test_numeric_prefix_collisions_are_also_safe(self):
        # "1" dan "10": "10".startswith("1") juga benar.
        self.sheet.build_from_questions(
            [_matching(right=("1", "10", "2"))]
        )
        self.sheet.restore_answers({"7": {"Ibu Kota": "1"}})
        self.assertEqual(self.sheet.get_answers().get("7"), {"Ibu Kota": "1"})

    def test_build_time_restore_is_exact_too(self):
        # Jalur build harus menerapkan aturan yang SAMA, kalau tidak
        # perilaku berbeda tergantung apakah PDF selesai sebelum atau
        # sesudah 500 ms.
        sheet = AnswerSheetWidget()
        self.addCleanup(sheet.deleteLater)
        sheet._answers = {"7": {"Ibu Kota": "A"}}
        sheet.build_from_questions([_matching()])
        self.assertEqual(
            sheet.get_answers().get("7"), {"Ibu Kota": "A"},
            "pemulihan saat build memakai aturan berbeda dari restore_answers",
        )

    def test_an_unknown_saved_value_leaves_the_combo_unset(self):
        # Nilai yang tidak ada di `right_items` harus TIDAK mengisi combo
        # dengan teks executor -- memaksa index 0 akan menimpa jawaban.
        self.sheet.restore_answers({"7": {"Ibu Kota": "Tidak Ada"}})
        answers = self.sheet.get_answers().get("7") or {}
        self.assertNotIn(
            "Tidak Ada", str(answers),
            "nilai yang tidak dikenal justru ditulis ke jawaban",
        )


class FallbackNumberNeverCollidesTestCase(unittest.TestCase):
    """#4 — nomor cadangan harus berada di ruang sendiri."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.sheet = AnswerSheetWidget()
        self.addCleanup(self.sheet.deleteLater)

    def test_a_missing_first_number_does_not_shadow_the_real_one(self):
        # Guru mengosongkan kolom Nomor pada soal PERTAMA.
        self.sheet.build_from_questions([
            {"number": None, "type": "short_answer"},
            {"number": 1, "type": "short_answer"},
            {"number": 2, "type": "short_answer"},
        ])
        keys = set(self.sheet._answer_widgets)
        self.assertIn("1", keys, "soal bernomor asli hilang")
        # Blok tanpa nomor harus punya key-nya sendiri, bukan berbagi
        # dengan soal bernomor 1.
        broken_key = next(k for k in keys if k != "1" and k != "2")
        _, real = self.sheet._answer_widgets["1"]
        _, broken = self.sheet._answer_widgets[broken_key]

        # Jawaban soal bernomor 1 YANG BENAR dulu -- ini yang tidak boleh
        # hilang. Urutan menjawab menentukan siapa yang ditimpa, jadi
        # diuji dari arah yang merugikan.
        real.setText("JAWABAN ASLI SOAL 1")
        broken.setText("tidak sengaja menyinggung blok lain")

        self.assertEqual(
            self.sheet.get_answers().get("1"), "JAWABAN ASLI SOAL 1",
            "nomor cadangan menabrak nomor asli; menyinggung blok lain "
            "menimpa jawaban soal 1 tanpa jejak",
        )

    def test_the_colliding_block_still_reaches_the_payload(self):
        # Blok bermasalah harus tetap bisa dijawab dan tersimpan -- tidak
        # dikorbankan demi menyelesaikan ini.
        self.sheet.build_from_questions([
            {"number": None, "type": "short_answer"},
            {"number": 1, "type": "short_answer"},
        ])
        _, group_a = self.sheet._answer_widgets["1"]
        group_a.setText("isi blok bermasalah")

        payload = str(self.sheet.get_answers())
        self.assertIn(
            "isi blok bermasalah", payload,
            "soal dengan nomor rusak menjadi tidak bisa dijawab -- "
            "half the sheet hilang karena satu kolom kosong di form guru",
        )

    def test_every_question_stays_independent(self):
        # Fixture yang lebih luas: beberapa nomor rusak di antara nomor asli.
        self.sheet.build_from_questions([
            {"number": 1, "type": "short_answer"},
            {"number": None, "type": "short_answer"},
            {"number": 2, "type": "short_answer"},
            {"number": "x", "type": "short_answer"},
            {"number": 3, "type": "short_answer"},
        ])
        for num, text in (("1", "satu"), ("2", "dua"), ("3", "tiga")):
            wtype, widget = self.sheet._answer_widgets[num]
            widget.setText(text)
        answers = self.sheet.get_answers()
        for num, text in (("1", "satu"), ("2", "dua"), ("3", "tiga")):
            self.assertEqual(
                answers.get(num), text,
                f"soal {num} kehilangan jawabannya",
            )

    def test_the_duplicate_warning_is_not_shown_for_a_missing_number(self):
        # "Nomor ganda" adalah fakta yang SALAH: tidak ada duplikasi,
        # ada nomor yang hilang. Memberi tahu siswa hal itu tidak benar
        # hanya membingungkan.
        self.sheet.build_from_questions([
            {"number": None, "type": "short_answer"},
            {"number": 1, "type": "short_answer"},
        ])
        self.assertEqual(
            self.sheet._duplicates, [],
            "nomor kosong dilaporkan sebagai nomor ganda; tidak ada "
            "duplikasi, ada nomor yang hilang",
        )


if __name__ == "__main__":
    unittest.main()