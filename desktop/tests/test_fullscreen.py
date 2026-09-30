"""Layar penuh yang sebenarnya menutupi taskbar Windows.

Gejala lapangan (Windows, 30 September 2026): "belum fullscreen, taskbar di
bawah masih keliatan".

Rantainya, dan kenapa test biasa tidak menangkapnya:

  1. ExamViewerWindow.__init__ -> _init_security() -> enforcer._activate_strict()
     memanggil showFullScreen() pada window yang BELUM tampil. Qt lalu
     menampilkannya dan	state fullscreen langsung terpasang.

  2. __main__.main() memanggil _maximize_window(viewer, fullscreen=True), yang
     melakukan setGeometry(screen.availableGeometry()) DULUAN.

     availableGeometry() adalah AREA KERJA: seluruh layar dikurangi taskbar
     (1920x1080 -> 1920x1040). Pada window yang sudah fullscreen, setGeometry()
     MEMINDAHKAN HWND ke sana — dan karena state-nya sudah WindowFullScreen,
     showFullScreen() sesudahnya tidak mengubah apa pun (tidak ada PERUBAHAN
     state, jadi tidak ada re-layout).

  3. Akibatnya: isFullScreen() mengembalikan True, tetapi jendela hanya setinggi
     area kerja. Samanya: taskbar terlihat.

  4. _enforce_fullscreen() tidak pernah menyelamatkannya karena ia hanya
     menanyakan isFullScreen() — yang selalu True pada kondisi rusak di atas.

Jadi dua hal harus dikunci di sini:
  * geometri yang dipakai untuk fullscreen adalah screen.geometry() (SELURUH
    LAYAR, taskbar ikut tertutup), bukan availableGeometry();
  * "sudah fullscreen?" dijawab dari GEOMETRI NYATA, bukan dari flag state.
"""

from __future__ import annotations

import os
import sys
import contextlib
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QRect
from PyQt5.QtWidgets import QApplication, QMainWindow, QWidget

from examvan import __main__ as main_mod
from examvan.models import Exam
from examvan.ui import exam_viewer
from examvan.ui import fullscreen as fs

APP = QApplication.instance() or QApplication([])


# ---------------------------------------------------------------------------
# 1. Geometri target
# ---------------------------------------------------------------------------


class _FakeScreen:
    """Screen yang meniru Windows: taskbar memakan tinggi di bawah."""

    def __init__(self, full=(0, 0, 1920, 1080), available=(0, 0, 1920, 1040)):
        self._full = QRect(*full)
        self._available = QRect(*available)

    def geometry(self):
        return self._full

    def availableGeometry(self):
        return self._available


class TargetGeometryTest(unittest.TestCase):
    def test_fullscreen_target_is_the_whole_screen_not_the_work_area(self):
        screen = _FakeScreen()
        # availableGeometry() = area kerja. Memakainya untuk fullscreen
        # itulah yang membiarkan taskbar terlihat.
        self.assertNotEqual(
            fs.fullscreen_geometry(screen), screen.availableGeometry()
        )
        self.assertEqual(
            fs.fullscreen_geometry(screen), QRect(0, 0, 1920, 1080)
        )


# ---------------------------------------------------------------------------
# 2. Deteksi "sudah benar-benar fullscreen?"
# ---------------------------------------------------------------------------


class _FakeWidget:
    def __init__(self, frame, fullscreen_state=True):
        self._frame = QRect(*frame)
        self._fullscreen_state = fullscreen_state
        self.calls = []

    def frameGeometry(self):
        return self._frame

    def isFullScreen(self):
        return self._fullscreen_state


class CoversFullscreenTest(unittest.TestCase):
    def test_window_filling_the_screen_counts_as_covered(self):
        screen = _FakeScreen()
        self.assertTrue(fs.covers_fullscreen(_FakeWidget((0, 0, 1920, 1080)), screen))

    def test_window_short_by_the_taskbar_height_is_not_covered(self):
        # Persis kondisi rusak di lapangan: state fullscreen, tinggi 1040.
        screen = _FakeScreen()
        w = _FakeWidget((0, 0, 1920, 1040))
        self.assertFalse(fs.covers_fullscreen(w, screen))

    def test_state_flag_alone_is_never_enough(self):
        # isFullScreen() == True di kondisi rusak, jadi kalau predicate ini
        # memakai flag itu, _enforce_fullscreen tidak akan pernah memperbaiki.
        screen = _FakeScreen()
        w = _FakeWidget((0, 0, 1920, 1040), fullscreen_state=True)
        self.assertTrue(w.isFullScreen())
        self.assertFalse(fs.covers_fullscreen(w, screen))

    def test_narrower_window_is_not_covered(self):
        screen = _FakeScreen()
        self.assertFalse(fs.covers_fullscreen(_FakeWidget((0, 0, 1024, 1080)), screen))


# ---------------------------------------------------------------------------
# 3. _maximize_window memakai geometri layar penuh untuk mode fullscreen
# ---------------------------------------------------------------------------


class _RecordingWidget:
    def __init__(self, screen):
        self._screen = screen
        self.calls = []
        self.geo = None
        self.shown = False
        self.maximized = False
        self.fullscreen = False

    def setGeometry(self, geo):
        self.calls.append("setGeometry")
        self.geo = geo

    def show(self):
        self.calls.append("show")
        self.shown = True

    def showMaximized(self):
        self.calls.append("showMaximized")
        self.maximized = True
        self.fullscreen = False

    def showFullScreen(self):
        self.calls.append("showFullScreen")
        self.fullscreen = True
        self.maximized = False

    def isFullScreen(self):
        return self.fullscreen

    def screen(self):
        return self._screen

    def frameGeometry(self):
        return self.geo if self.geo is not None else QRect(0, 0, 0, 0)


class MaximizeWindowGeometryTest(unittest.TestCase):
    def _run(self, screen, **kwargs):
        w = _RecordingWidget(screen)
        with mock.patch(
            "PyQt5.QtWidgets.QApplication.primaryScreen", return_value=screen
        ):
            main_mod._maximize_window(w, **kwargs)
        return w

    def test_fullscreen_sets_the_whole_screen_geometry(self):
        w = self._run(_FakeScreen(), fullscreen=True)
        self.assertEqual(w.geo, QRect(0, 0, 1920, 1080))

    def test_fullscreen_never_uses_the_work_area(self):
        w = self._run(_FakeScreen(), fullscreen=True)
        self.assertNotEqual(w.geo, QRect(0, 0, 1920, 1040))

    def test_state_is_set_before_the_geometry_is_forced(self):
        # showFullScreen() pada window yang SUDAH fullscreen tidak mengulang
        # layout, jadi setGeometry sesudahnya itulah yang benar-benar menutup
        # taskbar. Urutan terbalik -&gt; taskbar kembali terlihat.
        w = self._run(_FakeScreen(), fullscreen=True)
        self.assertLess(
            w.calls.index("showFullScreen"), w.calls.index("setGeometry")
        )

    def test_non_fullscreen_still_uses_the_work_area(self):
        # Dialog konfigurasi & approval tetap maximized, bukan fullscreen.
        w = self._run(_FakeScreen())
        self.assertTrue(w.maximized)
        self.assertEqual(w.geo, QRect(0, 0, 1920, 1040))


# ---------------------------------------------------------------------------
# 4. apply_fullscreen memperbaiki jendela yang terlanjur diparkir
# ---------------------------------------------------------------------------


class ApplyFullscreenTest(unittest.TestCase):
    """apply_fullscreen harus memaksa HWND benar-benar menutupi layar.

    Apa yang bisa dan tidak bisa diuji di sini: platform `offscreen` (dipakai
    CI) melaporkan availableGeometry() == geometry(), jadi celah taskbar tidak
    ada secara fisik di lingkungan test. Keadaan rusak itu diuji secara
    deterministik di atas lewat _FakeScreen. Test di bawah memakai jendela
    sungguhan untuk memastikan urutan show -> showFullScreen -> setGeometry
    benar-benar menghasilkan rect layar penuh pada QWidget nyata.
    """

    def test_grows_a_window_that_does_not_fill_the_screen(self):
        screen = APP.primaryScreen()
        w = QMainWindow()
        w.setGeometry(0, 0, 100, 100)
        self.assertFalse(fs.covers_fullscreen(w, screen))

        self.assertTrue(fs.apply_fullscreen(w))
        APP.processEvents()
        self.assertTrue(fs.covers_fullscreen(w, screen))
        self.assertTrue(w.isFullScreen())
        w.close()

    def test_restores_the_rect_after_a_later_geometry_change(self):
        # Steps 1-3 of apply_fullscreen: state dulu, baru rect. Kalau hanya
        # showFullScreen() yang dipanggil pada window yang sudah fullscreen,
        # rect tidak ikut dihitung ulang.
        screen = APP.primaryScreen()
        w = QMainWindow()
        w.show()
        w.showFullScreen()
        APP.processEvents()
        w.setGeometry(screen.geometry().adjusted(0, 0, -200, -200))
        APP.processEvents()
        self.assertFalse(fs.covers_fullscreen(w, screen))

        self.assertTrue(fs.apply_fullscreen(w))
        APP.processEvents()
        self.assertEqual(w.geometry(), screen.geometry())
        w.close()

    def test_returns_false_without_a_screen(self):
        # Cabang tanpa screen: jangan crash, jangan klaim berhasil.
        w = mock.Mock()
        w.screen.return_value = None
        with mock.patch(
            "PyQt5.QtWidgets.QApplication.primaryScreen", return_value=None
        ):
            self.assertFalse(fs.apply_fullscreen(w))
        w.show.assert_called_once()
        w.showFullScreen.assert_called_once()


# ---------------------------------------------------------------------------
# 5. Jendela ujian menutupi layar di SEMUA level keamanan
# ---------------------------------------------------------------------------


class _NoopSecurity:
    """Pengganti SecurityEnforcer yang antarmukanya harus IKUT yang asli.

    `pause_focus_guard()` sengaja ada di sini: `_modal_dialog_guard()` di
    ExamViewerWindow memanggilnya di setiap dialog modal, dan kalau test
    double tidak memilikinya, test gagal dengan AttributeError yang
    menyesatkan -- seolah ada bug di produksi. Double yang tidak
   kurang sesuai antarmuka yang sama dengan yang dipakai Viewer adalah
    double yang tidak dipercaya.
    """

    def __init__(self, *args, **kwargs):
        self.auto_submit = mock.Mock()
        self._focus_guard_paused = False

    def activate(self):
        pass

    def deactivate(self):
        pass

    @contextlib.contextmanager
    def pause_focus_guard(self):
        self._focus_guard_paused = True
        try:
            yield
        finally:
            self._focus_guard_paused = False


class ExamViewerFullscreenTest(unittest.TestCase):
    """_enforce_fullscreen tidak boleh bergantung pada flag state saja.

    Yang diuji di sini adalah SAMBUNGANNYA: kapan _enforce_fullscreen memanggil
    apply_fullscreen. Aritmetika geotrinya sudah terkunci di atas lewat
    _FakeScreen, yang meniru taskbar Windows secara deterministik.

    Kenapa bukan geometri nyata: di platform offscreen (dan di mesin dengan
    taskbar auto-hide) availableGeometry() == geometry(), jadi celah taskbar
    tidak ada secara fisik; dan layar offscreen 800x600 lebih kecil dari
    setMinimumSize(1024, 700) ExamViewerWindow, sehingga QWidget yang
    "rusak" justru lebih besar dari layar. Pola yang sama dipakai
    tests/test_exam_mode_enforcement.py.
    """

    def setUp(self):
        # closeEvent() pada level low memunculkan dialog konfirmasi yang
        # mem-block. Menjawab "No" membuat test gagal sebagai assertion, bukan
        # hang. Pola yang sama dipakai tests/test_exam_mode_enforcement.py.
        patcher = mock.patch.object(
            exam_viewer.QMessageBox, "question",
            return_value=exam_viewer.QMessageBox.No,
        )
        patcher.start()
        self.addCleanup(patcher.stop)

    def _make_window(self, exam):
        with mock.patch("examvan.ui.exam_viewer.SecurityEnforcer", _NoopSecurity), \
             mock.patch("examvan.ui.exam_viewer.ExamWebSocket"), \
             mock.patch(
                 "examvan.ui.exam_viewer.api.download_pdf", side_effect=OSError("x")
             ):
            win = exam_viewer.ExamViewerWindow(
                exam=exam,
                server_url="https://exam.example",
                token="T",
                identity_data={},
            )
        self.addCleanup(win.deleteLater)
        # Jangan jalankan alur submit sungguhan saat jendela ditutup.
        win._auto_submit = mock.Mock()
        return win

    def _levels(self):
        for level in ("low", "medium", "high"):
            yield level, Exam.from_json({"id": 1, "security_level": level})

    def test_reasserts_for_every_security_level(self):
        # Laporan: "semua mode bermasalah". Level tidak boleh jadi alasan
        # untuk membiarkan jendela tidak menutupi layar.
        for level, exam in self._levels():
            with self.subTest(level=level):
                win = self._make_window(exam)
                with mock.patch.object(
                    type(win), "isFullScreen", return_value=False
                ), mock.patch.object(exam_viewer, "apply_fullscreen") as af:
                    win._enforce_fullscreen()
                af.assert_called_once_with(win)
                win.close()

    def test_state_flag_alone_does_not_count_as_covered(self):
        # Kondisi lapangan: isFullScreen() True, HWND di area kerja. Kalau
        # predicate hanya menanyakan flag state, apply_fullscreen tidak pernah
        # dipanggil dan taskbar tetap terlihat.
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        win = self._make_window(exam)
        app_screen = APP.primaryScreen()
        work_area = app_screen.availableGeometry()
        work_area.setHeight(work_area.height() - 40)
        with mock.patch.object(
            type(win), "isFullScreen", return_value=True
        ), mock.patch.object(
            type(win), "frameGeometry", return_value=work_area
        ), mock.patch.object(exam_viewer, "apply_fullscreen") as af:
            win._enforce_fullscreen()
        af.assert_called_once_with(win)
        win.close()

    def test_a_window_already_covering_the_screen_is_left_alone(self):
        # Re-entrancy: showFullScreen() -> WindowStateChange ->
        # _enforce_fullscreen -> showFullScreen() harus berhenti, kalau tidak
        # setiap perbaikan memicu perbaikan lagi tanpa henti.
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        win = self._make_window(exam)
        win.show()
        APP.processEvents()
        win._enforce_fullscreen()   # really put it on the screen
        APP.processEvents()
        self.assertTrue(fs.covers_fullscreen(win, APP.primaryScreen()))

        with mock.patch.object(
            type(win), "isFullScreen", return_value=True
        ), mock.patch.object(exam_viewer, "apply_fullscreen") as af:
            win._enforce_fullscreen()
        af.assert_not_called()
        win.close()

    def test_state_change_caused_by_the_repair_does_not_loop(self):
        # apply_fullscreen() mengubah state, dan perubahan itu memicu
        # changeEvent() -> _enforce_fullscreen() lagi. Setelah perbaikan pertama
        # jendela sudah menutupi layar, jadi panggilan kedua harus berhenti.
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        win = self._make_window(exam)
        win.show()
        APP.processEvents()
        calls = []
        real = exam_viewer.apply_fullscreen

        def _counting(widget):
            calls.append(widget)
            return real(widget)

        with mock.patch.object(exam_viewer, "apply_fullscreen", _counting):
            win._enforce_fullscreen()
            APP.processEvents()
        self.assertEqual(len(calls), 1)
        win.close()

    def test_synchronous_reentry_is_blocked(self):
        # Di beberapa platform state change bisa sampai SINKRON dari dalam
        # showFullScreen(). Guard re-entrancy harus menghentikan itu; tanpa
        # guard, perbaikan memicu perbaikan tanpa henti.
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        win = self._make_window(exam)
        win.show()
        calls = []

        def _reentrant(widget):
            calls.append(widget)
            widget._enforce_fullscreen()
            return True

        with mock.patch.object(
            type(win), "isFullScreen", return_value=False
        ), mock.patch.object(exam_viewer, "apply_fullscreen", _reentrant):
            win._enforce_fullscreen()
        self.assertEqual(len(calls), 1)
        win.close()


# ---------------------------------------------------------------------------
# 6. __main__.main() benar-benar menyajikan jendela ujian fullscreen
# ---------------------------------------------------------------------------


class _FakeSignal:
    def __init__(self):
        self.slots = []

    def connect(self, slot):
        self.slots.append(slot)


class _FakeDialog(QWidget):
    """QWidget sungguhan, bukan Mock: WaitingApprovalDialog menerima parent=,
    dan QDialog menolak QWidget palsu."""

    def __init__(self, *args, **kwargs):
        super().__init__()
        self.exam_selected = _FakeSignal()
        self.input_token = mock.Mock()
        self.input_token.text.return_value = "abcd1234"


class MainPresentsExamWindowTest(unittest.TestCase):
    """Jendela ujian harus fullscreen di __main__.main().

    Ini satu-satunya tempat yang memutuskan `fullscreen=` untuk jendela ujian,
    dan tidak punya test coverage sama sekali sebelumnya. Dulu nilainya
    `fullscreen=viewer.is_strict`, jadi ujian medium dan low hanya maximized
    dengan taskbar tetap terlihat.
    """

    def _run(self, level):
        from PyQt5.QtWidgets import QDialog

        viewer = mock.Mock()
        viewer.is_strict = level == "high"
        exam = Exam.from_json({"id": 1, "security_level": level})

        dialog = _FakeDialog()
        dialog._exam = exam
        waiting = mock.Mock()
        waiting.exec_.return_value = QDialog.Accepted

        with mock.patch.object(main_mod, "_setup_logging"), \
             mock.patch.object(main_mod, "_recover_gnome_settings"), \
             mock.patch.object(main_mod, "_recover_windows_settings"), \
             mock.patch.object(main_mod, "atexit"), \
             mock.patch.object(main_mod, "signal"), \
             mock.patch("PyQt5.QtWidgets.QApplication"), \
             mock.patch("examvan.ui.styles.apply_theme"), \
             mock.patch("examvan.ui.styles.is_system_dark", return_value=False), \
             mock.patch("examvan.ui.server_config.ServerConfigDialog",
                        return_value=dialog), \
             mock.patch("examvan.ui.exam_viewer.ExamViewerWindow",
                        return_value=viewer), \
             mock.patch("examvan.ui.waiting_approval.WaitingApprovalDialog",
                        return_value=waiting), \
             mock.patch.object(sys, "exit"), \
             mock.patch.object(sys, "argv", ["examvan"]), \
             mock.patch.object(main_mod, "_maximize_window") as mw:
            main_mod.main()
            # main() menyambungkan presenter ke dialog.exam_selected; jalankan
            # DI DALAM blok patch — di luar blok WaitingApprovalDialog yang
            # sungguhan akan dibangun dan exec_() akan memblokir selamanya.
            self.assertEqual(len(dialog.exam_selected.slots), 1)
            dialog.exam_selected.slots[0](exam, "https://exam.example", {})

        return mw, viewer

    def test_exam_window_is_presented_fullscreen_in_every_level(self):
        for level in ("low", "medium", "high"):
            with self.subTest(level=level):
                mw, viewer = self._run(level)
                viewer_calls = [
                    c for c in mw.call_args_list if c.args and c.args[0] is viewer
                ]
                self.assertEqual(len(viewer_calls), 1)
                self.assertIs(viewer_calls[0].kwargs.get("fullscreen"), True)

    def test_dialogs_stay_maximized(self):
        # Dialog konfigurasi & approval bukan ujian: harus tetap maximized.
        #_fullscreen_ pada panggilan mereka harus False atau None.
        mw, viewer = self._run("high")
        non_viewer_calls = [
            c for c in mw.call_args_list
            if not (c.args and c.args[0] is viewer)
        ]
        self.assertTrue(non_viewer_calls)
        for call in non_viewer_calls:
            self.assertIs(call.kwargs.get("fullscreen", False), False)


if __name__ == "__main__":
    unittest.main()
