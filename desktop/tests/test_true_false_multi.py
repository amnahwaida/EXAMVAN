"""True/False satu nomor dengan beberapa pernyataan.

Kontrak baru: `statements` berisi daftar pernyataan dan `key` berisi daftar
TRUE/FALSE dengan urutan yang sama. Klien mengirim jawaban sebagai list
sejajar dengan statements, sehingga server bisa menilai per posisi.
"""

from __future__ import annotations

import os
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.ui.answer_sheet import AnswerSheetWidget

APP = QApplication.instance() or QApplication([])


class TrueFalseMultiTest(unittest.TestCase):
    def _sheet(self):
        sheet = AnswerSheetWidget()
        self.addCleanup(sheet.deleteLater)
        sheet.build_from_questions([
            {
                "number": 1,
                "type": "true_false",
                "statements": ["Ibu kota Indonesia adalah Jakarta.", "2 + 2 = 5"],
                "key": ["TRUE", "FALSE"],
            }
        ])
        return sheet

    def test_builds_one_radio_group_per_statement(self):
        sheet = self._sheet()
        wtype, groups = sheet._answer_widgets["1"]
        self.assertEqual(wtype, "tf_multi")
        self.assertEqual(len(groups), 2)
        for group in groups:
            self.assertEqual(
                [btn.text() for btn in group.buttons()], ["Benar", "Salah"]
            )
            self.assertEqual(
                [sheet._radio_value(btn) for btn in group.buttons()],
                ["TRUE", "FALSE"],
            )

    def test_answers_are_emitted_as_index_aligned_list(self):
        sheet = self._sheet()
        _, groups = sheet._answer_widgets["1"]
        groups[0].buttons()[0].click()   # TRUE
        groups[1].buttons()[1].click()   # FALSE
        APP.processEvents()
        self.assertEqual(sheet.get_answers(), {"1": ["TRUE", "FALSE"]})
        self.assertEqual(sheet.get_answered_count(), (1, 1))

    def test_restore_places_saved_values_on_the_right_statements(self):
        sheet = self._sheet()
        sheet.restore_answers({"1": ["FALSE", "TRUE"]})
        _, groups = sheet._answer_widgets["1"]
        self.assertEqual(sheet._radio_value(groups[0].checkedButton()), "FALSE")
        self.assertEqual(sheet._radio_value(groups[1].checkedButton()), "TRUE")
        self.assertEqual(sheet.get_answers(), {"1": ["FALSE", "TRUE"]})

    def test_legacy_single_true_false_still_uses_single_group(self):
        sheet = AnswerSheetWidget()
        self.addCleanup(sheet.deleteLater)
        sheet.build_from_questions([{"number": 1, "type": "true_false"}])
        wtype, widget = sheet._answer_widgets["1"]
        self.assertEqual(wtype, "single")
        widget.buttons()[0].click()
        self.assertEqual(sheet.get_answers(), {"1": "TRUE"})


if __name__ == "__main__":
    unittest.main()
