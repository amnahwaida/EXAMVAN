"""Dropdown soal menjodohkan susah diklik: guard fokus vs popup aplikasi.

Laporan lapangan (Windows, Oktober 2026)
----------------------------------------
"Jawaban yang ada dropdownnya susah untuk di klik."

Popup daftar pilihan QComboBox adalah window top-level TERPISAH dari window
ujian. `_poll_focus()` dijalankan tiap 500 ms dan tidak tahu soal popup:

  * di strict, ia memanggil `raise_()` + `activateWindow()` pada window
    ujian. Aktivasi kembali ke window induk membuat popup kehilangan fokus
    dan QComboBox menutup daftarnya sendiri -- siswa klik dropdown, popup
    berkedip lalu hilang sebelum sempat memilih;
  * di medium, aktivasi yang berpindah ke popup bisa terbaca sebagai
    ApplicationInactive -> countdown 3 detik -> auto-submit menembak saat
    siswa masih memilih opsi.

Ronde 30 Sep sudah memperbaiki setengah masalah dropdown (`_PopupWheelGuard`
menjaga lembar jawaban tidak menggulir saat popup terbuka); sisa setengahnya
-- guard fokus yang mengalahkan popup -- diperbaiki di
`SecurityEnforcer._app_popup_open()`: selama `QApplication.activePopupWidget()`
ada, guard buta. Popup hanya bisa dibuka dari dalam window ujian, jadi tidak
ada jalan keluar yang terbuka; begitu popup tertutup, polling berikutnya
menilai fokus seperti biasa.

Yang diuji di sini adalah KONTRAK yang dilihat siswa:
  1. popup terbuka + strict -> window ujian TIDAK di-raise/di-activate;
  2. popup terbuka + app "inactive" -> countdown TIDAK dimulai;
  3. popup terbuka saat countdown kedaluwarsa -> auto-submit DITUNDA;
  4. tanpa popup -> perilaku lama tetap utuh (raise jalan, countdown jalan).
Test 4 mencegah perbaikan ini membutakan guard sepenuhnya -- kalau
`_app_popup_open()` selalu True, empat test di atas lulus tapi ujian tidak
terlindungi lagi sama sekali.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import Qt
from PyQt5.QtWidgets import QApplication, QWidget

from examvan.security.enforcer import SecurityEnforcer

APP = QApplication.instance() or QApplication([])


class _FakePopup:
    """Pengganti QApplication.activePopupWidget yang dikendalikan test.

    PyQt5 class tidak bisa di-monkeypatch (sip), jadi module-level name
    `QApplication` di `examvan.security.enforcer` yang diganti -- semua
    akses `QApplication.activePopupWidget()` di enforcer lewat nama itu.
    """

    def __init__(self, test: unittest.TestCase, popup_widget):
        self._patcher = mock.patch.object(
            __import__("examvan.security.enforcer", fromlist=["QApplication"]),
            "QApplication",
        )
        self._app = self._patcher.start()
        test.addCleanup(self._patcher.stop)
        self._app.instance.return_value = self._app
        self._app.activePopupWidget.return_value = popup_widget

    def open(self):
        self._app.activePopupWidget.return_value = QWidget()

    def close(self):
        self._app.activePopupWidget.return_value = None


class FocusGuardIsBlindWhilePopupOpenTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.app = APP

    def _enforcer(self, level: str = "strict", strict: bool = True):
        win = QWidget()
        win.raise_ = mock.Mock()
        win.activateWindow = mock.Mock()
        win.isActiveWindow = mock.Mock(return_value=True)
        enforcer = SecurityEnforcer(
            security_level=level, strict_mode=strict, window=win
        )
        # Tidak menjalankan activate() penuh: backend platform (hook
        # keyboard, grab X11) bukan yang diuji di sini. Yang diuji adalah
        # mesin guard fokusnya, yang kondisinya digerakkan langsung.
        enforcer._active = True
        return win, enforcer

    # -- 1. strict tidak boleh menutup popup yang sedang dipakai --------

    def test_strict_poll_does_not_raise_window_while_popup_is_open(self):
        win, enforcer = self._enforcer("strict", strict=True)
        popup = _FakePopup(self, None)
        popup.open()

        enforcer._poll_focus()

        win.raise_.assert_not_called()
        win.activateWindow.assert_not_called()

    def test_strict_poll_still_raises_when_no_popup(self):
        win, enforcer = self._enforcer("strict", strict=True)
        popup = _FakePopup(self, None)
        popup.close()

        enforcer._poll_focus()

        win.raise_.assert_called()
        win.activateWindow.assert_called()

    def test_medium_poll_never_raises_and_popup_does_not_change_that(self):
        # Negative control untuk medium: `raise_()` bukan jalannya, jadi
        # popup tidak mengubah apa pun -- yang diuji di medium adalah
        # countdown, bukan raise.
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.open()

        enforcer._poll_focus()

        win.raise_.assert_not_called()

    # -- 2. buka popup bukan "siswa keluar dari ujian" ------------------

    def test_inactive_app_state_while_popup_open_starts_no_countdown(self):
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.open()

        enforcer._on_app_state_changed(Qt.ApplicationInactive)

        self.assertFalse(
            enforcer._focus_timer.isActive(),
            "buka dropdown memulai countdown auto-submit 3 detik",
        )

    def test_inactive_app_state_without_popup_still_starts_countdown(self):
        # Negative control: tanpa popup, jalur "siswa keluar" tetap hidup.
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.close()

        enforcer._on_app_state_changed(Qt.ApplicationInactive)

        self.assertTrue(enforcer._focus_timer.isActive())

    def test_window_active_changed_signal_is_ignored_while_popup_open(self):
        # Ini jalur kedua yang sama cepatnya: `windowHandle().activeChanged`
        # -> `_on_app_state_changed(ApplicationInactive)`.
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.open()
        win.isActiveWindow = mock.Mock(return_value=False)

        enforcer._on_window_active_changed()

        self.assertFalse(enforcer._focus_timer.isActive())

    def test_regaining_focus_still_cancels_a_running_countdown(self):
        # Countdown yang sudah berjalan (siswa sempat keluar) lalu dia
        # kembali dan membuka dropdown: kembalinya fokus tetap harus
        # membatalkan countdown -- popup tidak boleh membatalkan
        # pembatalan.
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.close()
        enforcer._on_app_state_changed(Qt.ApplicationInactive)
        self.assertTrue(enforcer._focus_timer.isActive())
        popup.open()

        enforcer._on_app_state_changed(Qt.ApplicationActive)

        self.assertFalse(enforcer._focus_timer.isActive())

    # -- 3. timeout tidak boleh menembak saat popup terbuka --------------

    def test_focus_timeout_while_popup_open_is_deferred_not_emitted(self):
        win, enforcer = self._enforcer("medium", strict=False)
        fired = []
        enforcer.auto_submit.connect(lambda: fired.append(True))
        popup = _FakePopup(self, None)
        popup.open()

        enforcer._on_focus_timeout()

        self.assertEqual(fired, [], "auto-submit menembak di tengah memilih opsi")
        self.assertTrue(
            enforcer._focus_timer.isActive(),
            "countdown harus di-ARM ulang, bukan dibuang",
        )

    def test_focus_timeout_without_popup_emits_as_before(self):
        win, enforcer = self._enforcer("medium", strict=False)
        fired = []
        enforcer.auto_submit.connect(lambda: fired.append(True))
        popup = _FakePopup(self, None)
        popup.close()

        enforcer._on_focus_timeout()

        self.assertEqual(fired, [True])

    # -- 4. guard tidak boleh buta total ---------------------------------

    def test_no_popup_means_poll_behaves_exactly_as_before(self):
        # Penjaga mutasi: kalau `_app_popup_open()` sengaja selalu True,
        # test 1-3 tetap hijau tapi ujian tidak terlindungi. Test ini
        # memastikan jalur lama tetap dieksekusi ketika tidak ada popup.
        win, enforcer = self._enforcer("medium", strict=False)
        popup = _FakePopup(self, None)
        popup.close()
        win.isActiveWindow = mock.Mock(return_value=False)

        enforcer._poll_focus()

        self.assertTrue(enforcer._focus_timer.isActive())

    def test_stale_popup_object_does_not_crash_the_guard(self):
        # `activePopupWidget()` berjalan di GUI thread tiap 500 ms; satu
        # exception di sana mematikan timer poll tanpa pesan. Guard harus
        # gagal-aman: bila Qt menolak, anggap TIDAK ada popup (jalur lama).
        win, enforcer = self._enforcer("strict", strict=True)
        with mock.patch.object(
            __import__("examvan.security.enforcer", fromlist=["QApplication"]),
            "QApplication",
        ) as App:
            App.instance.side_effect = RuntimeError("qt died")
            self.assertFalse(enforcer._app_popup_open())


if __name__ == "__main__":
    unittest.main()
