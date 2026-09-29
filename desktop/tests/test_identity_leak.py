"""Identitas siswa sebelumnya TIDAK BOLEH tersimpan untuk sesi berikutnya.

Gejala lapangan (Windows, 30 September 2026): di PC lab dipakai bersama,
setiap submission setelah siswa pertama tercatat atas nama siswa SEBELUMNYA.
Ronde 3 review, temuan N1.

Rantainya
---------
`config.set("identity_data", ...)` hanya ada di satu tempat
(`ui/server_config.py:367`) dan TIDAK PERNAH dihapus — saya grep seluruh
`desktop/examvan/` untuk yakin. Yang dihapus cuma token:

    __main__.py:188   # Clear saved token so user must re-enter for next exam
    __main__.py:189   dialog.input_token.clear()

Jadi: token dibersihkan, nama/nomor/kelas dibiarkan. Konsekuensinya:

  1. `ui/server_config.py:356` membaca identitas siswa A,
  2. `ui/identity_dialog.py:84-86` MENGISI form dengan nama A,
  3. validasi `:112` hanya cek "wajib tidak kosong" -> nilai A lolos,
  4. `ui/identity_dialog.py:100` mengikat Enter -> B bisa menekan Enter
     tanpa membaca apa pun,
  5. jawaban B tercatat atas nama A. Tanpa dialog, tanpa warning, tanpa log.

Yang dikunci di sini: identitas harus dibersihkan di samping token, dan
form identitas harus KOSONG saat dialog dibuka untuk sesi baru.
"""

from __future__ import annotations

import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import __main__ as main_mod
from examvan import config
from examvan.models import Exam

APP = QApplication.instance() or QApplication([])


# ---------------------------------------------------------------------------
# config: ada operasi untuk membersihkan identitas
# ---------------------------------------------------------------------------


class _ConfigTestCase(unittest.TestCase):
    """Arahkan penyimpanan config ke direktori temp (pola test_config.py)."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-identity-test-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp)),
            mock.patch.object(config, "_CONFIG_FILE", Path(self._tmp) / "config.json"),
        ]
        for p in self._patches:
            p.start()
        config._cache = None

    def tearDown(self):
        for p in self._patches:
            p.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)


class ClearIdentityTest(_ConfigTestCase):
    def test_clear_identity_empties_the_stored_value(self):
        config.set("identity_data", {"nama": "Ahmad", "kelas": "9A"})
        self.assertEqual(
            config.get("identity_data"), {"nama": "Ahmad", "kelas": "9A"}
        )
        config.clear_identity()
        self.assertEqual(config.get("identity_data"), {})

    def test_clear_identity_is_safe_when_nothing_was_stored(self):
        config.clear_identity()          # tidak boleh melempar
        config.clear_identity()
        self.assertEqual(config.get("identity_data"), {})

    def test_clear_identity_does_not_touch_other_keys(self):
        config.set("exam_token", "ABCD1234")
        config.set("server_url", "https://examvan.my.id")
        config.set("identity_data", {"nama": "Ahmad"})
        config.clear_identity()
        self.assertEqual(config.get("exam_token"), "ABCD1234")
        self.assertEqual(config.get("server_url"), "https://examvan.my.id")

    def test_clear_identity_is_persisted_to_disk(self):
        # Kalau hanya cache di memori yang dibersihkan, restart aplikasi akan
        # memunculkan identitas lama lagi.
        config.set("identity_data", {"nama": "Ahmad"})
        config.clear_identity()
        config._cache = None
        self.assertEqual(config.get("identity_data"), {})


# ---------------------------------------------------------------------------
# __main__: identitas dibersihkan bersama token saat viewer ditutup
# ---------------------------------------------------------------------------


class _FakeSignal:
    def __init__(self):
        self.slots = []

    def connect(self, slot):
        self.slots.append(slot)


class _CloseFlowTest(unittest.TestCase):
    """_on_viewer_closed harus membersihkan token DAN identitas."""

    def _run_main_and_close(self):
        from PyQt5.QtWidgets import QDialog, QWidget

        # Dialog harus QWidget sungguhan: WaitingApprovalDialog menerima
        # parent= dan QDialog menolak objek palsu.

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
             mock.patch("examvan.ui.styles.is_system_dark", return_value=False), \
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
            # viewer.closed adalah Mock, jadi `.emit()` tidak menjalankan
            # apa pun. Ambil slot yang di-connect dan panggil langsung —
            # itu persis yang dilakukan Qt saat jendela ditutup.
            on_closed = viewer.closed.connect.call_args[0][0]
            on_closed()
        return dialog, clear_id, cfg_set

    def test_token_is_cleared(self):
        dialog, _, _ = self._run_main_and_close()
        dialog.input_token.clear.assert_called()

    def test_identity_is_cleared_too(self):
        # Inilah N1. Tanpa ini, siswa berikutnya mendapat form berisi nama
        # siswa sebelumnya.
        _, clear_id, _ = self._run_main_and_close()
        clear_id.assert_called()



# ---------------------------------------------------------------------------
# IdentityDialog: form harus kosong untuk sesi baru
# ---------------------------------------------------------------------------


class IdentityDialogPrefillTest(unittest.TestCase):
    def test_saved_identity_prefills_when_explicitly_passed(self):
        # Prefill tetap harus ADA kalau pemanggil benar-benar mengirimnya —
        # mis. untuk pre-fill ulang di sesi yang sama.
        from examvan.ui.identity_dialog import IdentityDialog

        exam = Exam.from_json({"id": 1, "name": "Ujian"})
        dlg = IdentityDialog(
            exam, saved_data={"student_name": "Ahmad", "exam_number": "N01",
                              "student_class": "9A"}
        )
        self.assertEqual(dlg.get_identity_data()["student_name"], "Ahmad")
        dlg.close()

    def test_prefill_never_happens_without_explicit_data(self):
        from examvan.ui.identity_dialog import IdentityDialog

        exam = Exam.from_json({"id": 1, "name": "Ujian"})
        dlg = IdentityDialog(exam, saved_data={})
        data = dlg.get_identity_data()
        self.assertEqual([v for v in data.values() if v], [])
        dlg.close()


if __name__ == "__main__":
    unittest.main()
