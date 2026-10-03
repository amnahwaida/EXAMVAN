"""Startup harus gagal dengan pesan yang BISA DIBACA, bukan traceback.

Bug / asimetri
--------------
`examvan/ws.py` melakukan hard-import di level modul:

    from PyQt5.QtWebSockets import QWebSocket

dan `examvan/ui/exam_viewer.py` meng-import `ws` di level modul juga.
`__main__.main()` meng-import `exam_viewer` DI TENGAH main:

    from .ui.styles import app_theme_dark, apply_theme
    apply_theme(dark=app_theme_dark())

    from .ui.server_config import ServerConfigDialog
    from .ui.exam_viewer import ExamViewerWindow      <-- import ws di sini

Jadi di PC yang PyQt5-nya tidak punya QtWebSockets (paket distro yang
memecah PyQt5, `python3-pyqt5.qtwebsockets` terpisah), urutannya:

    1. proses mulai
    2. tema dipasang
    3. ModuleNotFoundError
    4. tidak ada jendela, tidak ada QMessageBox

Siswa melihat shortcut yang "tidak terjadi apa-apa", dan teknisi PC
lab menebak Windows-nya rusak -- bukan modul yang hilang.

Yang diuji di sini adalah kontrak pesan itu: guard harus ada, dan
harus menyebut modul yang hilang beserta cara memperbaikinya.
"""

from __future__ import annotations

import pathlib
import unittest
from unittest import mock

from examvan import __main__ as main_mod


class MissingQtModuleIsReportedTest(unittest.TestCase):
    def test_no_missing_modules_on_a_healthy_environment(self):
        # Kalau suite ini gagal di sini, lingkungan testnya yang salah
        # -- persis kondisi yang harus ditangani guard.
        self.assertEqual(
            main_mod._missing_qt_modules(), (),
            "lingkungan test tidak punya QtWebSockets; guard-nya tidak "
            "bisa diuji di sini",
        )

    def test_qtwebsockets_is_on_the_required_list(self):
        # Kalau nama modul dihapus dari daftar, PC tanpa QtWebSockets
        # kembali ke traceback -- dan tidak ada test lain yang menangkap.
        names = [name for name, _package in main_mod._REQUIRED_QT_MODULES]
        self.assertIn("PyQt5.QtWebSockets", names)
        # `examvan/ws.py` benar-benar meng-hard-import yang itu:
        ws_source = (
            pathlib.Path(__file__).resolve().parents[1]
            / "examvan" / "ws.py"
        ).read_text(encoding="utf-8")
        self.assertIn(
            "from PyQt5.QtWebSockets import QWebSocket",
            ws_source,
            "ws.py tidak lagi meng-hard-import QtWebSockets -- daftar "
            "required di __main__ harus ikut diperbarui",
        )

    def test_missing_module_is_detected(self):
        with mock.patch("importlib.util.find_spec", return_value=None):
            self.assertEqual(
                main_mod._missing_qt_modules(),
                ("PyQt5.QtCore", "PyQt5.QtWidgets", "PyQt5.QtWebSockets"),
            )

    def test_only_the_missing_one_is_reported(self):
        real = __import__("importlib.util", fromlist=["util"]).find_spec

        def fake(name):
            if name == "PyQt5.QtWebSockets":
                return None
            return real(name)

        with mock.patch("importlib.util.find_spec", side_effect=fake):
            self.assertEqual(
                main_mod._missing_qt_modules(), ("PyQt5.QtWebSockets",)
            )

    def test_broken_parent_package_counts_as_missing(self):
        # `find_spec` melempar ValueError kalau induknya bukan package.
        # Itu sama saja tidak bisa dipakai -- tidak boleh lolos sebagai
        # "ada".
        with mock.patch("importlib.util.find_spec",
                        side_effect=ValueError("bukan package")):
            self.assertIn("PyQt5.QtWidgets", main_mod._missing_qt_modules())


class AbortOnMissingQtTest(unittest.TestCase):
    def test_guard_shows_a_dialog_and_exits_nonzero(self):
        from PyQt5.QtWidgets import QApplication, QMessageBox

        app = QApplication.instance() or QApplication([])
        with mock.patch.object(main_mod, "_missing_qt_modules",
                               return_value=("PyQt5.QtWebSockets",)), \
             mock.patch.object(QMessageBox, "critical") as critical:
            with self.assertRaises(SystemExit) as ctx:
                main_mod._abort_on_missing_qt(app)

        self.assertEqual(
            ctx.exception.code, 2,
            "keluar dengan kode 2 (environment salah), bukan 0 -- kode 0 "
            "disalin installer/log sebagai 'berhasil'",
        )
        critical.assert_called_once()
        args = critical.call_args.args
        self.assertIn("tidak bisa dijalankan", args[1])
        body = args[2]
        self.assertIn("PyQt5.QtWebSockets", body)
        # Harus menyebut cara memperbaiki, bukan cuma nama modulnya.
        self.assertIn("pip install", body)
        self.assertIn("requirements.txt", body)
        self.assertIn("python3-pyqt5.qtwebsockets", body)

    def test_guard_is_a_noop_when_everything_is_present(self):
        from PyQt5.QtWidgets import QApplication, QMessageBox

        app = QApplication.instance() or QApplication([])
        with mock.patch.object(QMessageBox, "critical") as critical:
            main_mod._abort_on_missing_qt(app)
        critical.assert_not_called()

    def test_main_calls_the_guard_before_importing_exam_viewer(self):
        # Guard harus dipanggil SEBELUM `from .ui.exam_viewer import`.
        # Kalau urutannya dibalik, import gagal duluan dan guard tidak
        # pernah jalan -- persis bug aslinya.
        import inspect

        source = inspect.getsource(main_mod.main)
        guard_at = source.index("_abort_on_missing_qt(app)")
        viewer_at = source.index("from .ui.exam_viewer import ExamViewerWindow")
        self.assertLess(
            guard_at, viewer_at,
            "guard dipanggil SETELAH import exam_viewer -- import yang "
            "gagal duluan, guard tidak pernah dipakai",
        )


class HelpTextTest(unittest.TestCase):
    def test_help_text_names_the_missing_modules(self):
        body = main_mod._MISSING_QT_HELP.format(modules="PyQt5.QtWebSockets")
        self.assertIn("PyQt5.QtWebSockets", body)

    def test_help_text_is_for_a_student_and_a_technician(self):
        # Dua audiens membaca layar ini: siswa (butuh tahu harus
        # berhenti dan melapor) dan teknisi lab (butuh tahu apa yang
        # harus dipasang).
        body = main_mod._MISSING_QT_HELP.format(modules="X")
        self.assertIn("hubungi pengawas", body.lower())
        self.assertIn("pip install", body)


if __name__ == "__main__":
    unittest.main()
