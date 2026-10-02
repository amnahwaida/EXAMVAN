"""Halaman hasil pada jalur RECOVERY: proteksi capture + token benar + kebocoran.

Bug H2 — halaman hasil recovery tanpa proteksi apa pun
------------------------------------------------------
`ServerConfigDialog._recovery_done_slot` membuat `CongratulationsWindow`
sendiri. Ada DUA tempat yang membuat halaman ini di seluruh aplikasi
(`exam_viewer._show_congratulations` dan `_recovery_done_slot`), tapi hanya
satu yang memanggil proteksi (`exam_viewer`, `protect_window`). Jalur
recovery -- justru jalur yang dipakai saat auto-submit background GAGAL dan
siswa menekan "Kirim Lagi" -- menampilkan token ujian dengan nol proteksi:

  * `deactivate()` sudah melepas WDA_MONITOR, keyboard hook, ClipCursor,
    dan sweeper clipboard sebelum halaman ini dibuat;
  * `config.set("exam_token", ...)` berjalan pada setiap connect, jadi
    token TERISI dan tombol Copy aktif;
  * PrintScreen di halaman ini menyimpan token + identitas + pesan guru ke
    disk tanpa diblokir.

Bug M-token-leak — token kelas tertinggal di dialog untuk siswa berikutnya
------------------------------------------------------------------------
`_recovery_done_slot` tidak pernah memanggil `input_token.clear()`, dan pada
jalur recovery tidak pernah ada `ExamViewerWindow` yang dibuat -- jadi
pembersihan `__main__` (`_on_viewer_closed`) tidak pernah jalan. Kolom
token masih berisi `ABCD1234` setelah ujian benar-benar selesai; PC lab
dipakai bersama, dan kotak itu sudah ter-prefill, jadi siswa berikutnya
tekan Enter saja sudah ikut memakai token kelas.

Bug token salah — config basi mengalahkan token tervalidasi
------------------------------------------------------------
`_recovery_done_slot` membangun halaman dari `config.get("exam_token")`,
padahal token yang tervalidasi ada di `self.validated_token`. Kalau siswa
mengetik ulang token di kotak yang sama, config masih menyimpan nilai dari
percobaan sebelumnya -- dan halaman menampilkan link yang salah, milik
ujian yang salah.

Test di sini menjalankan jalur recovery SEBENARNYA: `_recovery_submit_thread`
dengan `api` palsu yang sukses (emit `_sig_recovery_done` → `_recovery_done_slot`
langsung, koneksi signal langsung), lalu memeriksa halaman yang benar-benar
dibuat -- bukan dengan membaca baris sumber.
"""

from __future__ import annotations

import os
import pathlib
import tempfile
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QLabel

from examvan import config
from examvan.models import Exam, SubmitResponse


_EXAM = Exam(id=7, name="Ujian Matematika", status="active")
_IDENTITY = {"nama": "SITI", "nomor_ujian": "N02", "kelas": "9B"}

# Token yang Lolos validasi di dialog (kandidat siswa mengetik ulang di
# kotak yang sama), dan token basi yang masih tersimpan di config dari
# percobaan sebelumnya.
_VALIDATED_TOKEN = "NEW12345"
_STALE_CONFIG_TOKEN = "STALE99"


class _RecordingBackend:
    """Backend yang mencatat setiap `set_capture_protection` yang diminta.

    Cuma butuh satu metode: halaman hasil hanya memanggil proteksi capture,
    bukan strict penuh (tombol "Selesai" harus tetap bisa diklik).
    """

    def __init__(self) -> None:
        self.protected = []
        self.fail = False

    def set_capture_protection(self, window) -> None:
        if self.fail:
            raise RuntimeError("backend goyang (simulasi kegagalan)")
        self.protected.append(window)

    # Metode lain tidak boleh dipakai halaman hasil; kalau muncul, test
    # harus gagal keras, bukan diam-diam lolos.
    def __getattr__(self, name):
        raise AssertionError(f"backend dipanggil untuk {name!r}, tak terduga")


class _ConfigSandbox(unittest.TestCase):
    """Arahkan config ke file sementara supaya config.json asli utuh."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        sandbox = pathlib.Path(self._tmp.name) / "examvan"
        sandbox.mkdir(parents=True, exist_ok=True)
        for attr, value in (
            ("_CONFIG_DIR", sandbox),
            ("_CONFIG_FILE", sandbox / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class RecoveryDonePageTestCase(_ConfigSandbox):
    """`_recovery_done_slot`: proteksi capture, token tervalidasi, input bersih."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _run_recovery_success(self, backend: _RecordingBackend):
        """Jalankan jalur recovery penuh dengan `api` palsu yang sukses.

        Mengembalikan (dialog, halaman) — halaman diambil dari
        `dialog._congrats_ref`, yaitu objek yang benar-benar dirender siswa.
        """
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)

        # Keadaan setelah `_connect_thread` sukses: token tervalidasi,
        # config sudah storing token BASI (percobaan sebelumnya), kotak
        # input masih menampungi token yang baru diketik.
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg._validated_token = _VALIDATED_TOKEN
        dlg._recovery_identity = dict(_IDENTITY)
        dlg.input_token.setText(_VALIDATED_TOKEN)
        config.set("exam_token", _STALE_CONFIG_TOKEN)

        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True,
            status="ok",
            message="ok",
            congrats_message="Kerja bagus!",
        )

        patcher = mock.patch("examvan.ui.server_config.api", fake_api)
        patcher.start()
        self.addCleanup(patcher.stop)

        backend_patcher = mock.patch(
            "examvan.security.enforcer.get_backend", lambda: backend
        )
        backend_patcher.start()
        self.addCleanup(backend_patcher.stop)

        # Sinyal `_sig_recovery_done` terpasang ke `_recovery_done_slot`
        # tanpa QueuedConnection → emit memanggil slot-nya langsung.
        dlg._recovery_submit_thread(_EXAM, dict(_IDENTITY))

        page = getattr(dlg, "_congrats_ref", None)
        self.assertIsNotNone(
            page, "jalur recovery sukses tidak membuat halaman hasil sama sekali")
        self.addCleanup(page.deleteLater)
        return dlg, page

    # -- H2: proteksi capture -------------------------------------------

    def test_the_recovery_page_gets_capture_protection(self):
        backend = _RecordingBackend()
        _dlg, page = self._run_recovery_success(backend)

        self.assertIn(
            page,
            backend.protected,
            "halaman hasil recovery dirender tanpa proteksi tangkapan layar: "
            "PrintScreen menyimpan token kelas + identitas ke disk, di semua "
            "level keamanan, karena deactivate() sudah melepas WDA_MONITOR",
        )

    def test_capture_protection_is_requested_after_the_page_is_shown(self):
        # Afinitas display (SetWindowDisplayAffinity) disimpan per-HWND
        # native, jadi proteksi baru bermakna setelah HWND-nya ada:
        # `show_fullscreen()` harus dipanggil SEBELUM minta proteksi.
        # (Test ini tidak gagal sebelum perbaikan — guard urutan, bukan
        # reproduksi bug; yang menangkap bug-nya test di atas.)
        backend = _RecordingBackend()
        _dlg, page = self._run_recovery_success(backend)
        self.assertTrue(
            page.isVisible(),
            "halaman hasil harus tampil fullscreen sebelum dilindungi",
        )

    def test_a_failing_backend_does_not_break_the_recovery_page(self):
        backend = _RecordingBackend()
        backend.fail = True
        with self.assertLogs("examvan.ui.server_config", level="WARNING"):
            _dlg, page = self._run_recovery_success(backend)
        self.addCleanup(page.close)
        self.assertTrue(
            page.isVisible(),
            "backend goyang tidak boleh menggagalkan halaman hasil -- "
            "siswa harus tetap melihat hasilnya",
        )

    # -- Token: yang tervalidasi, bukan config basi ---------------------

    def test_the_page_uses_the_validated_token_not_the_stale_config_one(self):
        _dlg, page = self._run_recovery_success(_RecordingBackend())
        self.assertEqual(
            page.result_url(),
            f"https://exam.example/{_VALIDATED_TOKEN}",
            "halaman hasil memakai token dari config (basi) alih-alih token "
            "tervalidasi → link yang ditampilkan milik ujian yang salah",
        )

    def test_no_label_on_the_page_shows_the_stale_token(self):
        _dlg, page = self._run_recovery_success(_RecordingBackend())
        for label in page.findChildren(QLabel):
            self.assertNotIn(
                _STALE_CONFIG_TOKEN,
                label.text(),
                "token dari config basi masih tercetak di layar",
            )


class RecoveryDoneClearsTokenInputTestCase(_ConfigSandbox):
    """M-token-leak: kotak token harus kosong setelah recovery sukses."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def test_the_token_input_is_cleared_after_the_exam_is_finished(self):
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg._validated_token = _VALIDATED_TOKEN
        dlg._recovery_identity = dict(_IDENTITY)
        dlg.input_token.setText(_VALIDATED_TOKEN)

        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", congrats_message="Selesai",
        )
        backend_patcher = mock.patch(
            "examvan.security.enforcer.get_backend",
            lambda: _RecordingBackend(),
        )
        backend_patcher.start()
        self.addCleanup(backend_patcher.stop)

        with mock.patch("examvan.ui.server_config.api", fake_api):
            dlg._recovery_submit_thread(_EXAM, dict(_IDENTITY))

        page = getattr(dlg, "_congrats_ref", None)
        if page is not None:
            self.addCleanup(page.deleteLater)

        self.assertEqual(
            dlg.input_token.text(),
            "",
            "token kelas masih tertulis di dialog setelah ujian selesai; "
            "PC lab dipakai bersama dan kotak ini sudah ter-prefill untuk "
            "siswa berikutnya (tekan Enter saja sudah cukup)",
        )

    def test_the_token_stays_readable_in_config_for_pending_answers(self):
        # Keputusan sadar: config["exam_token"] SENGAJA tidak dikosongkan.
        # Token itu adalah kunci XOR decode jawaban tersimpan
        # (config._xor_obfuscate) — mengosongkannya membuat fitur "Kirim
        # Lagi" milik siswa berikutnya tidak bisa decode jawabannya — dan
        # `remember_url` sudah mengatur apakah token itu boleh
        # di-prefill ke kotak pada kunjungan berikutnya.
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = _EXAM
        dlg._server_url = "https://exam.example"
        dlg._validated_token = _VALIDATED_TOKEN
        dlg._recovery_identity = dict(_IDENTITY)
        dlg.input_token.setText(_VALIDATED_TOKEN)
        config.set("exam_token", _VALIDATED_TOKEN)

        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", congrats_message="Selesai",
        )
        backend_patcher = mock.patch(
            "examvan.security.enforcer.get_backend",
            lambda: _RecordingBackend(),
        )
        backend_patcher.start()
        self.addCleanup(backend_patcher.stop)

        with mock.patch("examvan.ui.server_config.api", fake_api):
            dlg._recovery_submit_thread(_EXAM, dict(_IDENTITY))

        page = getattr(dlg, "_congrats_ref", None)
        if page is not None:
            self.addCleanup(page.deleteLater)

        self.assertEqual(
            config.get("exam_token"),
            _VALIDATED_TOKEN,
            "config token dikosongkan → kunci decode jawaban tersimpan "
            "hilang, recovery siswa berikutnya rusak",
        )


if __name__ == "__main__":
    unittest.main()