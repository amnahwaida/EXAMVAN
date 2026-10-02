"""Identitas yang disimpan tapi tidak dipakai harus dibersihkan (#1).

Regresi dari commit `a58bdf5`
-----------------------------
Commit itu memindahkan `config.set("identity_data", identity)` ke ATAS
pemeriksaan recovery agar `_recovery_submit_thread` tidak membaca store
yang kosong. Itu benar dan perlu. Tapi konsekuensinya belumdipikirkan:
sekarang ada jalur yang menyimpan identitas lalu `return` tanpa pernah
memancarkan `exam_selected` --

    config.set("identity_data", identity)
    if not self._offer_pending_recovery():
        return          # exam_selected TIDAK pernah emit

`config.clear_identity()` hanya dipanggil dari `__main__._on_viewer_closed`
dan dari jalur pembatalan layar persetujuan. Keduanya tidak terjangkau
dari sini: `exam_selected` tidak pernah emit, jadi tidak ada viewer yang
pernah dibuat, jadi tidak ada `closed` signal.

Siswa menjawab "Tidak" pada prompt kirim-ulang (atau pengiriman ulang
gagal), lalu menutup app. Identitasnya TINGGAL di config.

Siswa berikutnya membuka app -> `_show_identity_dialog` membaca
`config.get("identity_data", {})` -> `IdentityDialog` mengisi seluruh
field -> `last_input.returnPressed.connect(self._on_submit)` ->
SATU TOMBOL ENTER sudah cukup menjawab ujian atas nama orang lain. Tanpa
dialog, tanpa warning, tanpa log.

Ini persis kebocoran yang commit `9f984e6` sudah tutup untuk jalur
pembatalan; jalur recovery ini membuka hole yang sama dari arah lain.

Prinsip yang ditegakkan test ini: identitas adalah data pribadi yang
hanya boleh bertahan selamaExamNYA benar-benar berjalan. Kalau
ujian tidak dimulai, identitas harus dibersihkan -- di SIAPAPUN jalurnya.
"""

from __future__ import annotations

import os
import pathlib
import tempfile
import unittest
from unittest import mock

from PyQt5.QtWidgets import QDialog, QMessageBox

from examvan import config
from examvan.models import Exam
from examvan.ui.server_config import ServerConfigDialog


_EXAM = Exam(id=7, name="Ujian", status="active")
_IDENTITY = {"nama": "SITI", "nomor_ujian": "N02", "kelas": "9B"}


class _RealSignal:
    """Signal kecil yang benar-benar connect/emit."""

    def __init__(self):
        self._slots = []

    def connect(self, slot):
        self._slots.append(slot)

    def emit(self, *args):
        for slot in self._slots:
            slot(*args)


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        for attr, value in (
            ("_CONFIG_DIR", pathlib.Path(self._tmp.name)),
            ("_CONFIG_FILE", pathlib.Path(self._tmp.name) / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class DecliningRecoveryClearsIdentityTestCase(_ConfigSandbox):
    """#1 — menjawab "Tidak" pada prompt recovery tidak boleh meninggalkan identitas."""

    def _dialog(self):
        dlg = ServerConfigDialog.__new__(ServerConfigDialog)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg.btn_connect = mock.Mock()
        dlg.lbl_status = mock.Mock()
        dlg.input_url = mock.Mock()
        dlg.input_token = mock.Mock()
        dlg._sig_recovery_available = _RealSignal()
        dlg.exam_selected = mock.Mock()
        dlg.accept = mock.Mock()
        dlg._show_recovery = lambda exam: None
        dlg._sig_recovery_available.connect(dlg._show_recovery)
        return dlg

    def _run_identity_dialog(self, dlg, recover_reply):
        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(_IDENTITY))
        with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog), \
             mock.patch("examvan.config.load_answers", return_value={"1": "A"}), \
             mock.patch.object(QMessageBox, "question",
                               return_value=recover_reply):
            dlg._show_identity_dialog()

    def test_declining_the_resend_clears_the_identity(self):
        dlg = self._dialog()
        self._run_identity_dialog(dlg, QMessageBox.No)
        self.assertEqual(
            config.get("identity_data", {}), {},
            "siswa menjawab 'Tidak' pada prompt kirim-ulang; identitasnya "
            "tinggal di config dan akan dipakai mengisi form siswa "
            "berikutnya -- yang bisa menekan Enter untuk menjawab atas "
            "namanya",
        )

    def test_a_failed_resend_leaves_no_identity_behind(self):
        # Pengiriman ulang GAGAL: `_sig_enable_btn.emit()`, dialog
        # konfigurasi tetap terbuka, tidak ada ujian yang berjalan.
        # `_show_recovery` sengaja tidak di-stub di sini supaya jalur
        # production berjalan -- tapi `exec_`-nya yang dikembalikan
        # supaya tidak memblokir.
        dlg = self._dialog()
        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(_IDENTITY))

        def fake_resend(*_a, **_k):
            # Modell kegagalan: worker tidak pernah sampai durable,
            # jadi yang terjadi adalah "aktifkan lagi tombolnya".
            dlg._sig_enable_btn = _RealSignal()
            fired = []
            dlg._sig_enable_btn.connect(lambda: fired.append(1))
            dlg._sig_enable_btn.emit()
            return fired

        with mock.patch("examvan.config.load_answers", return_value={"1": "A"}), \
             mock.patch.object(QMessageBox, "question",
                               return_value=QMessageBox.Yes), \
             mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog):
            dlg._show_recovery = fake_resend
            dlg._sig_recovery_available.connect(dlg._show_recovery)
            dlg._show_identity_dialog()

        self.assertEqual(
            config.get("identity_data", {}), {},
            "pengiriman ulang tidak berhasil tapi identitas tetap tertinggal; "
            "PC lab dipakai bergantian dan siswa berikutnya akan mendapat "
            "form yang sudah terisi",
        )

    def test_closing_the_dialog_after_declining_leaves_nothing_behind(self):
        # Persis skenario yang nyata: decline -> tutup app -> siswa lain.
        dlg = self._dialog()
        self._run_identity_dialog(dlg, QMessageBox.No)
        dlg._sig_recovery_available.emit(_EXAM)   # dialog masih terbuka

        stored = config.get("identity_data", {})
        self.assertEqual(
            stored, {},
            f"masih ada {stored!r} di config; siswa berikutnya akan "
            "melihat form yang sudah terisi",
        )


class IdentitySurvivesOnlyWhileAnExamIsRunningTestCase(_ConfigSandbox):
    """Identitas boleh bertahan HANYA di antara `_show_identity_dialog`
    dan viewer benar-benar dibuat."""

    def test_identity_is_persisted_when_the_exam_starts(self):
        # Kebalikan dari test di atas: jangan sampai perbaikannya
        # membersihkan terlalu cepat dan merusak recovery yang justru
        # membutuhkan identitas.
        dlg = ServerConfigDialog.__new__(ServerConfigDialog)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg.btn_connect = mock.Mock()
        dlg.lbl_status = mock.Mock()
        dlg.input_url = mock.Mock()
        dlg.input_token = mock.Mock()
        dlg._sig_recovery_available = _RealSignal()
        dlg.exam_selected = mock.Mock()
        dlg.accept = mock.Mock()
        dlg._show_recovery = lambda exam: None
        dlg._sig_recovery_available.connect(dlg._show_recovery)

        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(_IDENTITY))
        with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog), \
             mock.patch("examvan.config.load_answers", return_value=None):
            dlg._show_identity_dialog()

        self.assertEqual(
            config.get("identity_data", {}), _IDENTITY,
            "identitas tidak disimpan saat ujian benar-benar dimulai",
        )
        dlg.exam_selected.emit.assert_called_once()


if __name__ == "__main__":
    unittest.main()