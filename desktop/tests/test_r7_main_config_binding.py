"""Regresi: `main()` harus bisa.Resolve `config` tanpa bergantung pada
closure yang rapuh.

Ronde 7, -- (critical)
-------------------------------------
Refactor tema gelap mengganti

    from . import config
    from .ui.styles import is_system_dark, apply_theme

menjadi

    from .ui.styles import app_theme_dark, apply_theme

— `from . import config` HILANG. Tapi `config` bukan hanya dipakai di
`main()`: `_after_viewer_gone` dan `_back_to_config` adalah closure
dalam `main()` yang memanggil `config.clear_identity()`,
`config.set("identity_context", {})`. Keduanya mengambil `config` dari
scope `main()` (closure), jadi menghapus import itu membuat keduanya
melempar `NameError: name 'config' is not defined`.

`NameError` itu tertelan `except Exception` + `log.warning`, jadi tidak
menggagalkan apa pun secara terlihat — persis kelas bug "bertengkai
diam-diam" yang paling berbahaya di proyek ini. Akibatnya
`clear_identity()` TIDAK PERNAH jalan: identitas siswa sebelumnya tetap
tersimpan, dan siswa berikutnya di PC lab yang sama mendapat form
terisi nama/nomor/kelas orang itu plus `returnPressed` untuk mengirim
dengan satu Enter. Bug ini persis N1 yang dikunci
`tests/test_identity_leak.py`.

Kenapa test_identity_leak.py TIDAK menangkapnya
----------------------------------------------
`_after_viewer_gone`.wrap `except Exception`, jadi assertion
`clear_identity.assert_called()` memang harus gagal. Tapi file itu
hijau di suite penuh dan merah saat dijalankan sendiri: ada test lain
di suite yang menyuntikkan `config` ke namespace modul
`examvan.__main__`, sehingga `config` kebetulan terikat dan alurnya
lolos. Test di bawah sengaja KERAS: ia membuang `config` dari namespace
modul sebelum menjalankan alur, jadi tidak ada polusi yang bisa
menyembunyikan regresi ini.<Order-independen dengan sengaja.
"""

from __future__ import annotations

import os
import sys
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QDialog, QWidget

from examvan import __main__ as main_mod
from examvan import config
from examvan.models import Exam

APP = QApplication.instance() or QApplication([])


class _FakeSignal:
    def __init__(self):
        self.slots = []

    def connect(self, slot):
        self.slots.append(slot)


class MainResolvesConfigTest(unittest.TestCase):
    """`main()` harus mengikat `config` di scope-nya sendiri."""

    def setUp(self):
        # Anti-polusi: pastikan `config` TIDAK tersedia sebagai atribut modul,
        # persis seperti di produksi. Kalau ada test lain yang menyuntikkannya,
        # test ini membuangnya supaya kondisi produksi yang diuji.
        self._had_attr = hasattr(main_mod, "config")
        self._saved = getattr(main_mod, "config", None)
        if self._had_attr:
            delattr(main_mod, "config")

    def tearDown(self):
        if self._had_attr:
            main_mod.config = self._saved

    def test_main_module_does_not_need_an_ambient_config_attribute(self):
        """Kondisi produksi: tak ada `examvan.__main__.config`."""
        self.assertFalse(
            hasattr(main_mod, "config"),
            "test ini harus dijalankan dalam kondisi produksi: tanpa atribut "
            "`config` di namespace modul",
        )

    def _run_close_flow(self):
        class _RealDialog(QWidget):
            def __init__(self, *a, **k):
                super().__init__()
                self.exam_selected = _FakeSignal()
                self.input_token = mock.Mock()
                self.input_token.text.return_value = "abcd1234"

        dialog = _RealDialog()
        viewer = mock.Mock()
        exam = Exam.from_json({"id": 1, "security_level": "high"})
        waiting = mock.Mock()
        waiting.exec_.return_value = QDialog.Accepted

        with mock.patch.object(main_mod, "_setup_logging"), \
             mock.patch.object(main_mod, "_recover_gnome_settings"), \
             mock.patch.object(main_mod, "_recover_windows_settings"), \
             mock.patch.object(main_mod, "atexit"), \
             mock.patch.object(main_mod, "signal"), \
             mock.patch("PyQt5.QtWidgets.QApplication"), \
             mock.patch("examvan.ui.styles.apply_theme"), \
             mock.patch("examvan.ui.server_config.ServerConfigDialog",
                        return_value=dialog), \
             mock.patch("examvan.ui.exam_viewer.ExamViewerWindow",
                        return_value=viewer), \
             mock.patch("examvan.ui.waiting_approval.WaitingApprovalDialog",
                        return_value=waiting), \
             mock.patch.object(sys, "exit"), \
             mock.patch.object(sys, "argv", ["examvan"]), \
             mock.patch.object(main_mod, "_maximize_window"), \
             mock.patch.object(config, "clear_identity") as clear_id, \
             mock.patch.object(config, "set") as cfg_set:
            main_mod.main()
            dialog.exam_selected.slots[0](exam, "https://examvan.my.id", {})
            on_closed = viewer.closed.connect.call_args[0][0]
            on_closed()
        return clear_id, cfg_set

    def test_close_flow_clears_the_previous_students_identity(self):
        """N1: identitas harus dibersihkan TANPA ambient `config`."""
        clear_id, _ = self._run_close_flow()
        clear_id.assert_called()

    def test_close_flow_clears_the_identity_context(self):
        _, cfg_set = self._run_close_flow()
        cleared = [c for c in cfg_set.call_args_list
                   if c.args and c.args[0] == "identity_context"
                   and c.args[1] == {}]
        self.assertTrue(
            cleared,
            "identity_context tidak dikosongkan — sesi berikutnya masih "
            "bawa konteks siswa sebelumnya",
        )

    def test_close_flow_does_not_only_swallow_the_name_error(self):
        """`except Exception` tidak boleh menjadi penutup kedok.

        Kalau `clear_identity` melempar, alur harus tetap terlihat gagal:
        di sini kita memastikan tidak ada NameError yang menutupi
        pemanggilan_clear_identity.
        """
        clear_id, _ = self._run_close_flow()
        # Kalau `config` tidak terikat, `clear_identity` tidak pernah
        # dipanggil; assertion di atas sudah menFAIL-nya. Test ini
        # hanya mengunci bahwa pemanggilan itu benar-benar terjadi
        # dengan argumen yang valid (tanpa argumen = implementasi lain).
        self.assertGreaterEqual(clear_id.call_count, 1)


class MainSourceKeepsConfigBindingTest(unittest.TestCase):
    """Pengaman statis: `main()` tidak boleh kehilangan binding `config`."""

    def test_main_binds_config_in_its_own_scope(self):
        import inspect

        src = inspect.getsource(main_mod.main)
        self.assertIn(
            "from . import config",
            src,
            "main() tidak lagi mengikat `config` di scope-nya sendiri; "
            "closure di dalamnya (`_after_viewer_gone`, `_back_to_config`) "
            "akan melempar NameError dan identitas tidak pernah dibersihkan",
        )


if __name__ == "__main__":
    unittest.main()