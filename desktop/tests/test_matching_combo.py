"""Dropdown Menjodohkan harus bisa dipilih dengan mouse wheel.

Gejala lapangan (Windows, 30 September 2026): "tipe jawabannya matching, yang
ada drop downnya sangat susah untuk dipilih, karena ketika diklik dan discroll
langsung hilang".

Penyebabnya struktural, bukan satu baris yang salah:

  * Jawaban "matching" memakai QComboBox (answer_sheet.py:200).
  * Semua soal diletakkan di dalam QScrollArea (answer_sheet.py:58-69).

Popup QComboBox adalah top-level window terpisah, jadi bukan anggota
hierarki QScrollArea yang memegang lembar jawaban. WheelEvent yang mendarat di
combo juga tidak dimakan oleh combo, sehingga terus berjalan naik di hierarki
dan menggulir LEMBAR JAWABAN — bukan daftar jawaban. Combo ikut bergeser,
popup tidak (popup ada di luar hierarki scroll), dan hasilnya: daftar pilihan
tampak lepas dari combo dan hilang persis di saat siswa sedang memilih.

Kontrak yang dikunci di sini — guard installed pada combo:
  * popup TERBUKA  -> filter menelan event roda (return True), sehingga Qt berhenti
    di combo dan QScrollArea tidak pernah bergerak; roda dialihkan ke scrollbar
    daftar popup supaya opsi yang jauh tetap bisa dijangkau;
  * popup TERTUTUP -> filter meneruskan (return False), sehingga roda tetap
    menggulir lembar jawaban seperti sebelumnya;
  * event selain roda (klik, ketik) selalu diteruskan — guard yang terlalu
    rakus akan membuat combo tidak bisa dipakai sama sekali.
"""

from __future__ import annotations

import os
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, QPoint, QPointF, Qt
from PyQt5.QtGui import QMouseEvent, QWheelEvent
from PyQt5.QtWidgets import QApplication

from examvan.ui.answer_sheet import AnswerSheetWidget

APP = QApplication.instance() or QApplication([])

# Cukup panjang agar daftar popup punya scrollbar, dan cukup banyak baris
# agar lembar jawaban sendiri bisa digulir.
RIGHT_ITEMS = [f"opsi-{i:02d}" for i in range(40)]
LEFT_ITEMS = [f"item-{i:02d}" for i in range(12)]


# ABOUT_WHEEL_DELIVERY
# ---------------------
# Sebagian test di bawah memanggil `guard.eventFilter(combo, wheel)` secara
# langsung, bukan mengirim lewat QApplication.sendEvent. Alasannya bukan
# singkatnya: ketika popup QComboBox terbuka, QApplication
# mengarahkan event sintetis ke JENDELA POPUP (popup mengambil mouse), sehingga
# sendEvent(combo, wheel) tidak pernah menyentuh combo — spy event filter
# terpasang di combo tidak terpanggil sama sekali, dan test apa pun yang
# bergantung pada jalur itu akan lulus karena alasan yang salah.
#
# Yang diuji lewat panggilan langsung adalah KONTRAK filter itu sendiri:
# return True saat popup terbuka (Qt berhenti di combo -> QScrollArea tidak
# bergerak), return False saat tertutup, dan penerusan ke scrollbar popup.
#_JALUR_ yang mengalahkannya di dunia nyata adalah "roda mendarat di combo
# sementara popup terbuka", dan itu persis yang dicegah oleh return True.


def _wheel(delta_y: int) -> QWheelEvent:
    """Satu putaran roda. delta_y > 0 = menggulir ke atas (nilai membesar)."""
    return QWheelEvent(
        QPointF(4.0, 4.0),
        QPointF(4.0, 4.0),
        QPoint(0, 0),
        QPoint(0, delta_y),
        Qt.NoButton,
        Qt.NoModifier,
        Qt.NoScrollPhase,
        False,
    )


class MatchingComboWheelTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.sheet = AnswerSheetWidget()
        cls.sheet.build_from_questions([
            {
                "number": 1,
                "type": "matching",
                "left_items": list(LEFT_ITEMS),
                "right_items": list(RIGHT_ITEMS),
            }
        ])
        cls.sheet.resize(340, 150)
        cls.sheet.show()
        APP.processEvents()
        _, combos = cls.sheet._answer_widgets["1"]
        # Kunci combo adalah TEKS ITEM KIRI, bukan indeks baris.
        cls.combo = combos[LEFT_ITEMS[0]]

    @classmethod
    def tearDownClass(cls):
        cls.sheet.close()

    # -- helpers ---------------------------------------------------------

    def _popup_open(self) -> bool:
        view = self.combo.view()
        return bool(view) and view.window().isVisible()

    def _sheet_bar(self):
        return self.sheet._scroll.verticalScrollBar()

    def _popup_bar(self):
        return self.combo.view().verticalScrollBar()

    def _guard(self):
        """The installed filter object (not QObject.eventFilter, which every
        QObject has and which would make these assertions vacuous)."""
        return self.combo.wheel_guard

    def setUp(self):
        if self._popup_open():
            self.combo.hidePopup()
            APP.processEvents()
        # Parkir di tengah supaya kedua arah scroll selalu mungkin.
        bar = self._sheet_bar()
        bar.setValue(bar.maximum() // 2)
        APP.processEvents()

    # -- preconditions: tanpa ini test bisa lulus karena tidak ada apa pun
    #    untuk digulir ----------------------------------------------------

    def test_combo_lives_inside_a_scroll_area(self):
        # Kalau ini tidak lagi benar, gejala ini tidak mungkin terjadi lagi —
        # dan seluruh test di bawah kehilangan makna.
        self.assertIs(self.sheet._scroll.widget(), self.sheet._container)

    def test_the_sheet_really_scrolls_on_a_wheel_event(self):
        # Membuktikan QScrollArea benar-benar bergeser saat WheelEvent masuk,
        # jadi test "tidak boleh bergeser" di bawah bukan sekadar tidak
        # melakukan apa-apa.
        self.assertGreater(self._sheet_bar().maximum(), 0)
        before = self._sheet_bar().value()
        QApplication.sendEvent(self.sheet._scroll.viewport(), _wheel(-120))
        APP.processEvents()
        self.assertNotEqual(self._sheet_bar().value(), before)

    def test_long_option_list_really_scrolls(self):
        # 40 opsi dalam combo setinggi ~100px: tanpa ini tidak ada yang bisa
        # diuji, dan fix bisa "lulus" karena tidak ada apa pun untuk digulir.
        self.combo.showPopup()
        APP.processEvents()
        self.assertTrue(self._popup_open())
        self.assertGreater(self._popup_bar().maximum(), self._popup_bar().minimum())

    # -- the contract ---------------------------------------------------

    def test_guard_is_installed_on_matching_combos(self):
        # `eventFilter` selalu ada di QObject, jadi yang dicek adalah
        # filter yang benar-benar di-install, bukan metodenya.
        self.assertIsNotNone(getattr(self.combo, "wheel_guard", None))

    def test_wheel_is_consumed_while_the_popup_is_open(self):
        # Inilah mekanismenya: filter mengembalikan True -> Qt berhenti di combo
        # -> QScrollArea di atasnya tidak pernah menerima roda.
        self.combo.showPopup()
        APP.processEvents()
        self.assertTrue(self._popup_open())
        self.assertTrue(self._guard().eventFilter(self.combo, _wheel(-120)))

    def test_wheel_is_forwarded_while_the_popup_is_open(self):
        # Opsi yang jauh dari pilihan saat ini harus tetap bisa dijangkau.
        # Dipanggil langsung ke filter, bukan lewat sendEvent:-see catatan
        # ABOUT_WHEEL_DELIVERY di bawah.
        self.combo.showPopup()
        APP.processEvents()
        bar = self._popup_bar()
        bar.setValue(0)
        self._guard().eventFilter(self.combo, _wheel(-120))
        self.assertGreater(bar.value(), 0)

    def test_wheel_back_scrolls_the_popup_the_other_way(self):
        self.combo.showPopup()
        APP.processEvents()
        bar = self._popup_bar()
        self._guard().eventFilter(self.combo, _wheel(-120))
        moved = bar.value()
        self.assertGreater(moved, bar.minimum())
        self._guard().eventFilter(self.combo, _wheel(120))
        self.assertLess(bar.value(), moved)

    def test_wheel_reaching_the_popup_view_is_intercepted_too(self):
        # Saat popup mengambil mouse, roda mendarat di view popup, bukan di combo.
        # Guard di dua tempat menutup kedua jalur.
        self.combo.showPopup()
        APP.processEvents()
        view = self.combo.view()
        bar = self._popup_bar()
        bar.setValue(0)
        QApplication.sendEvent(view, _wheel(-120))
        self.assertGreater(bar.value(), 0)

    def test_sheet_never_moves_while_the_popup_is_open(self):
        self.combo.showPopup()
        APP.processEvents()
        self.assertTrue(self._popup_open())
        before = self._sheet_bar().value()
        for _ in range(6):
            QApplication.sendEvent(self.combo, _wheel(-120))
            QApplication.sendEvent(self.combo, _wheel(120))
        APP.processEvents()
        self.assertEqual(
            self._sheet_bar().value(), before,
            "popup terbuka tapi lembar jawaban tetap bergeser",
        )

    def test_wheel_passes_through_once_the_popup_closes(self):
        # Guard harus berhenti berlaku begitu popup tutup, kalau tidak lembar
        # jawaban jadi tidak bisa digulir lagi sama sekali.
        self.combo.showPopup()
        APP.processEvents()
        self.combo.hidePopup()
        APP.processEvents()
        self.assertFalse(self._popup_open())
        self.assertFalse(self._guard().eventFilter(self.combo, _wheel(-120)))

    def test_non_wheel_events_are_never_swallowed(self):
        # Guard yang terlalu rakus mematikan klik dan ketikan.
        self.combo.showPopup()
        APP.processEvents()
        press = QMouseEvent(
            QEvent.MouseButtonPress, QPointF(4.0, 4.0), Qt.LeftButton,
            Qt.LeftButton, Qt.NoModifier,
        )
        self.assertFalse(self._guard().eventFilter(self.combo, press))
        self.combo.hidePopup()
        APP.processEvents()
        self.assertFalse(self._guard().eventFilter(self.combo, press))

    def test_picking_an_option_still_records_the_answer(self):
        # Guard roda tidak boleh merusak jalur pilih normal.
        self.combo.showPopup()
        APP.processEvents()
        self.combo.setCurrentIndex(5)
        APP.processEvents()
        self.assertEqual(self.sheet.get_answers()["1"][LEFT_ITEMS[0]], RIGHT_ITEMS[4])
        self.assertEqual(self.sheet.get_answered_count(), (1, 1))

    def test_every_matching_combo_is_guarded(self):
        # Kalau hanya baris pertama yang dijaga, baris kedua ke bawah tetap
        # memakai dropdown yang hilang saat digulir.
        _, combos = self.sheet._answer_widgets["1"]
        self.assertEqual(len(combos), len(LEFT_ITEMS))
        for key, combo in combos.items():
            with self.subTest(row=key):
                self.assertIsNotNone(getattr(combo, "wheel_guard", None))

    def test_other_question_types_are_unaffected(self):
        # Guard hanya untuk matching; tipe lain tidak boleh ikut berubah.
        w = AnswerSheetWidget()
        w.build_from_questions([
            {"number": 1, "type": "single_choice", "choices": ["A", "B"]},
            {"number": 2, "type": "short_answer"},
            {"number": 3, "type": "true_false"},
        ])
        _, buttons = w._answer_widgets["1"]
        for btn in buttons.buttons():
            self.assertIsNone(getattr(btn, "wheel_guard", None))
        _, line = w._answer_widgets["2"]
        self.assertIsNone(getattr(line, "wheel_guard", None))
        _, tf = w._answer_widgets["3"]
        for btn in tf.buttons():
            self.assertIsNone(getattr(btn, "wheel_guard", None))
        w.close()


if __name__ == "__main__":
    unittest.main()
