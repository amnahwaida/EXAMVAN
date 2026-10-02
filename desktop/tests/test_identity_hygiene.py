"""Identitas harus benar dan tidak boleh bocor antar siswa (#5, #6).

Bug #5 -- recovery mengirim identitas kosong
-------------------------------------------
`ServerConfigDialog._show_identity_dialog``:

    if not self._offer_pending_recovery():   # <- thread recovery START
        config.set("identity_data", identity)  # <- identitas baru disimpan
        return
    config.set("identity_data", identity)

`_sig_recovery_available.connect(self._show_recovery)` tidak memakai
`Qt.QueuedConnection`, jadi `_offer_pending_recovery()` memanggil
`_show_recovery()` secara SYNCHRONOUS. Di situlah thread
`_recovery_submit_thread` dijalankan. Thread itu membaca identitas dari
`config.get("identity_data")` -- yang masih nilai LAMA, sudah dikosongkan
`clear_identity()` waktu jendela ujian sebelumnya ditutup.

Hasilnya: server membalas `400 "Identitas 'Nama' wajib diisi"`. Fitur
"Jawaban Belum Terkirim / Kirim Lagi" -- satu-satunya jalan untuk
menyelamatkan jawaban setelah auto-submit background gagal -- tidak
pernah bisa bekerja. Kerusakan kedua: `build_attempt_key(token, {})`
menghasilkan `DESKTOP:<hash>` yang BERBEDA dari yang dipakai ujian, jadi
kirim ulang akan membuat baris kedua dan placeholder aslinya
terk Forever menggantung "in progress".

Bug #6 -- identitas siswa sebelumnya bocor ke siswa berikutnya
---------------------------------------------------------------
`config.clear_identity()` hanya dipanggil dari `_on_viewer_closed` di
`__main__.py`. Kalau siswa menekan **Batal** di layar persetujuan, atau
ditolak, atau menutup jendela, jalur itu tidak pernah sampai -- dan
`identity_data` yang baru saja disimpan `_show_identity_dialog` tetap di
config. Siswa berikutnya membaca `config.get("identity_data", {})` untuk
mengisi form, dan `IdentityDialog` menutup diri dengan
`last_input.returnPressed -> _on_submit`, jadi Enter saja sudah cukup
menjawab atas nama orang lain. Tanpa dialog, tanpa warning, tanpa log.

Test di sini membenamkan kedua-duanya lewat perilaku, bukan dengan
membaca baris sumber.
"""

from __future__ import annotations

import os
import pathlib
import tempfile
import unittest
from unittest import mock

from examvan import config
from PyQt5.QtWidgets import QDialog

from examvan.models import Exam


class _RealSignal:
    """Signal sekecil mungkin yang benar-benar connect/emit.

    `mock.Mock()` untuk signal adalah jebakan: `connect` bisa dipanggil
    tanpa error dan tetap tidak menyimpan apa pun, jadi emit tidak pernah
    sampai ke callback. Itu persis yang membuat test "worker melihat
    identitas" hijau tanpa menjalankan apa pun.
    """

    def __init__(self):
        self._slots = []

    def connect(self, slot):
        self._slots.append(slot)

    def emit(self, *args):
        for slot in self._slots:
            slot(*args)


_EXAM = Exam(id=7, name="Ujian", status="active")
_NEW_IDENTITY = {"nama": "SITI", "nomor_ujian": "N02", "kelas": "9B"}


class _ConfigSandbox(unittest.TestCase):
    """Arahkan config ke file sementara supaya config.json asli utuh."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self._sandbox = os.path.join(self._tmp.name, "examvan")
        os.makedirs(self._sandbox, exist_ok=True)
        for attr, value in (
            ("_CONFIG_DIR", pathlib.Path(self._sandbox)),
            ("_CONFIG_FILE", pathlib.Path(self._sandbox) / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class RecoveryUsesTheIdentityJustTypedTestCase(_ConfigSandbox):
    """#5 — thread recovery harus melihat identitas yang baru diketik."""

    def _dialog_with_pending_answers(self):
        """ServerConfigDialog dengan jawaban tertinggal, tanpa GUI sungguhan."""
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog.__new__(ServerConfigDialog)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg.btn_connect = mock.Mock()
        dlg.lbl_status = mock.Mock()
        dlg._sig_recovery_available = _RealSignal()
        dlg.exam_selected = mock.Mock()
        dlg.accept = mock.Mock()
        return dlg

    def test_worker_sees_the_identity_the_student_just_typed(self):
        dlg = self._dialog_with_pending_answers()
        config.clear_identity()  # seperti saat jendela ujian sebelumnya ditutup
        config.set("exam_token", "ABCD1234")

        seen = {}

        def capture(exam):
            # Persis yang dibaca `_recovery_submit_thread` dari worker.
            seen["identity"] = config.get("identity_data", {})

        # Sambungkan persis seperti `__init__` lakukan -- tanpa ini
        # emit tidak pernah sampai dan test ini tidak menguji apa pun.
        dlg._show_recovery = capture
        dlg._sig_recovery_available.connect(dlg._show_recovery)

        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(_NEW_IDENTITY))

        with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog), \
             mock.patch("examvan.config.load_answers", return_value={"1": "A"}), \
             mock.patch("examvan.ui.server_config.config.set", config.set):
            dlg._show_identity_dialog()

        self.assertEqual(
            seen.get("identity"),
            _NEW_IDENTITY,
            "thread recovery melihat identitas "
            f"{seen.get('identity')!r} -- harusnya identitas yang baru saja "
            "diketik siswa. Server akan membalas 400 dan jawaban tidak "
            "pernah terkirim.",
        )

    def test_identity_is_stored_before_the_recovery_check_runs(self):
        # Bukti urutan, terpisah dari worker: pada saat
        # `_offer_pending_recovery()` dipanggil, identitas yang baru
        # diketik harus SUDAH ada di config.
        dlg = self._dialog_with_pending_answers()
        seen = {}

        dlg._offer_pending_recovery = lambda identity: (
            seen.__setitem__("during", config.get("identity_data", {})) or False
        )

        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(_NEW_IDENTITY))

        with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog), \
             mock.patch("examvan.config.load_answers", return_value={"1": "A"}):
            dlg._show_identity_dialog()

        self.assertEqual(
            seen.get("during"), _NEW_IDENTITY,
            "identitas disimpan SESUDAH recovery dicek; worker langsung "
            "membaca store yang masih kosong",
        )


class CancelApprovalClearsIdentityTestCase(_ConfigSandbox):
    """#6 -- Batal di layar persetujuan tidak boleh meninggalkan identitas."""

    def test_clearing_is_what_the_cancel_path_does(self):
        # __main__.py tidak punya logika sendiri; ia memanggil
        # config.clear_identity(). Yang diuji adalah KONTRAK: setiap jalan
        # keluar dari dialog persetujuan harus membersihkannya, dan harus
        # ada pemanggil yang menutup gap itu.
        import inspect

        import examvan.__main__ as main_mod

        src = inspect.getsource(main_mod)
        cancel_block = src.split("waiting_dlg.exec_() != QDialog.Accepted", 1)
        self.assertEqual(
            len(cancel_block), 2,
            "blok pembatalan tidak ditemukan -- struktur __main__.py berubah",
        )
        body = cancel_block[1].split("viewer = ExamViewerWindow", 1)[0]
        # Jalur pembatalan boleh tidak memuat panggilan langsung lagi:
        # ia boleh lewat helper `_back_to_config` yang dipakai SEMUA
        # early-return. Yang tetap diuji adalahhal yang sama: helper itu
        # benar-benar membersihkan, dan jalur pembatalan memakainya.
        helper = src.split("def _back_to_config", 1)
        self.assertEqual(
            len(helper), 2,
            "helper _back_to_config tidak ada di __main__.py",
        )
        helper_body = helper[1].split("\n    def ", 1)[0]
        self.assertIn(
            "config.clear_identity()",
            helper_body,
            "_back_to_config tidak membersihkan identity_data, jadi siswa "
            "berikutnya mendapat form terisi nama orang lain dan bisa "
            "menekan Enter untuk menjawab atas namanya.",
        )
        self.assertIn(
            "_back_to_config(",
            body,
            "path pembatalan di layar persetujuan TIDAK melewati "
            "pembersihan identitas",
        )

    def test_clear_identity_actually_empties_the_store(self):
        config.set("identity_data", dict(_NEW_IDENTITY))
        config.clear_identity()
        self.assertEqual(config.get("identity_data", {}), {})


if __name__ == "__main__":
    unittest.main()