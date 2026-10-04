"""Ronde 10 — perbaikan bug yang ditemukan review + laporan lapangan.

Lima hal yang dikunci di sini:

1. **Skew basi saat ujian mulai.** `skew` dihitung sekali saat Hubungkan,
   padahal di antara Hubungkan dan jendela ujian ada dialog identitas +
   menunggu persetujuan. Siswa yang memundurkan jam di fase itu mendapat
   deadline awal yang lebih panjang. Perbaikannya: segarkan skew dari
   `/api/health` tepat saat persetujuan diterima (sebelum sinyal approved).

2. **`_suppress_mupdf_warnings` tidak memulihkan state.** `previous`
   mengambil atribut FUNGSI-nya, bukan nilai; restore selalu memaksa True.

3. **`remember_url` tidak menghormati janji URL.** Dengan checkbox tidak
   dicentang, `server_url` tetap ditulis ke disk.

4. **Prompt uninstall menyesatkan.** Ia menawarkan "Pertahankan KEDUA
   folder", padahal berkas kredensial selalu dihapus lewat [UninstallDelete].

5. **Tombol X / Escape pada dialog token tidak menutup jendela.**
   `reject()` memanggil `self.close()`, sementara bawaan `QDialog.closeEvent`
   memanggil `reject()`; saat window terlihat, close event lalu di-`ignore()`
   dan siswa harus membunuh proses lewat Task Manager.

6. **Kotak token disamarkan seperti password.** Token ujian bukan
   password: token itu 8 karakter yang tercetak di Amplop Lembar Jawaban,
   sering diketik ulang oleh siswa, dan ditolak server kalau ada satu
   karakter salah. Disamarkan, siswa tidak bisa memastikan O vs 0 dan
   tidak bisa membaca token dari layar ruang ujian. Keputusan pengguna:
   tampilkan apa adanya.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QLineEdit

from examvan import api, config
from examvan.models import Exam, RequestApprovalResponse
from examvan.ui.server_config import ServerConfigDialog
from examvan.ui.waiting_approval import WaitingApprovalDialog

APP = QApplication.instance() or QApplication([])


def _exam() -> Exam:
    return Exam(id=1, name="Ujian", status="active")


# ---------------------------------------------------------------------------
# 1 — skew disegarkan saat approval
# ---------------------------------------------------------------------------


class SkewRefreshedWhenExamStartsTest(unittest.TestCase):
    def setUp(self):
        api.set_server_skew_ms(0)

    def _drive_to_approved(self):
        dlg = WaitingApprovalDialog(
            _exam(), "https://sekolah.sch.id", {"nama": "Andi"}, token="ABCD1234",
        )
        order: list[str] = []
        dlg._sig_status.connect(lambda k, t, m: order.append(k))
        # Kendalikan polling: satu putaran saja, tanpa menunggu 5 detik.
        dlg._stop_polling()
        dlg._poll_stop.clear()
        dlg._poll_stop.wait = lambda _t=0.0: dlg._poll_stop.set()

        calls: list[str] = []

        def _health(url):
            calls.append(url)
            order.append("health")
            # Meniru /api/health yang mengisi ulang skew global.
            api.set_server_skew_ms(424242)
            return mock.Mock(success=True, status="ok")

        approved = RequestApprovalResponse(
            success=True, status="approved", message="",
        )
        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            return_value=approved,
        ), mock.patch(
            "examvan.ui.waiting_approval.api.check_health",
            side_effect=_health,
        ):
            dlg._poll_thread()
        return dlg, order, calls

    def test_approval_refreshes_the_server_time_skew(self):
        _, order, calls = self._drive_to_approved()
        self.assertEqual(
            calls, ["https://sekolah.sch.id"],
            "approval tidak menyegarkan skew — deadline awal tetap dihitung "
            "dari jam perangkat yang mungkin sudah digeser selama menunggu",
        )
        self.assertEqual(api.get_server_skew_ms(), 424242)

    def test_skew_is_refreshed_before_the_exam_is_announced(self):
        _, order, _ = self._drive_to_approved()
        self.assertIn("health", order)
        self.assertIn("approved", order)
        self.assertLess(
            order.index("health"), order.index("approved"),
            "skew harus segar SEBELUM siswa diberi tahu ujian dimulai",
        )

    def test_non_approved_statuses_do_not_hit_health(self):
        dlg = WaitingApprovalDialog(
            _exam(), "https://sekolah.sch.id", {"nama": "Andi"}, token="ABCD1234",
        )
        dlg._stop_polling()
        dlg._poll_stop.clear()
        dlg._poll_stop.wait = lambda _t=0.0: dlg._poll_stop.set()
        pending = RequestApprovalResponse(success=True, status="pending", message="")
        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=[pending, OSError("selesai")],
        ), mock.patch(
            "examvan.ui.waiting_approval.api.check_health",
        ) as health:
            dlg._poll_thread()
        health.assert_not_called()


# ---------------------------------------------------------------------------
# 2 — restore state MuPDF
# ---------------------------------------------------------------------------


class MupdfRestoreTest(unittest.TestCase):
    def setUp(self):
        import fitz

        self.fitz = fitz
        self._original = fitz.TOOLS.mupdf_display_errors()
        self.addCleanup(self.fitz.TOOLS.mupdf_display_errors, self._original)

    def test_previous_false_is_restored(self):
        from examvan.ui.pdf_viewer import _suppress_mupdf_warnings

        self.fitz.TOOLS.mupdf_display_errors(False)
        with _suppress_mupdf_warnings():
            self.fitz.TOOLS.mupdf_display_errors(False)
        self.assertFalse(
            self.fitz.TOOLS.mupdf_display_errors(),
            "state sebelumnya (False) tidak dipulihkan — restore selalu "
            "memaksa True karena `previous` mengambil fungsi, bukan nilainya",
        )

    def test_previous_true_is_restored(self):
        from examvan.ui.pdf_viewer import _suppress_mupdf_warnings

        self.fitz.TOOLS.mupdf_display_errors(True)
        with _suppress_mupdf_warnings():
            pass
        self.assertTrue(self.fitz.TOOLS.mupdf_display_errors())


# ---------------------------------------------------------------------------
# 3 — remember_url juga menghormati server_url
# ---------------------------------------------------------------------------


class RememberUrlGovernsStorageTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        patcher = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp.name) / "config.json"
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        self._saved_cache = config._cache
        config._cache = None
        self.addCleanup(lambda: setattr(config, "_cache", self._saved_cache))
        # Patch SEBELUM dialog dibuat: `_sig_show_identity` terhubung ke
        # bound method saat __init__, jadi mem-patch kelas sesudahnya tidak
        # mengubah slot yang sudah tersambung (dialog modal -> hang).
        identity = mock.patch.object(ServerConfigDialog, "_show_identity_dialog")
        identity.start()
        self.addCleanup(identity.stop)

    def _connect(self, remember: bool, url: str = "https://sekolah.sch.id"):
        dlg = ServerConfigDialog()
        health = mock.Mock(success=True, status="ok", certificate_fingerprint="")
        exam_resp = mock.Mock(success=True, exam=_exam(), message="", error="")
        with mock.patch(
            "examvan.ui.server_config.api.check_health", return_value=health
        ), mock.patch(
            "examvan.ui.server_config.api.get_exam_by_token",
            return_value=exam_resp,
        ):
            dlg._connect_thread(url, "ABCD1234", remember_url=remember)
        return dlg

    def test_unchecked_does_not_write_the_url(self):
        self._connect(remember=False)
        self.assertEqual(
            config.get("server_url", ""), "",
            "checkbox 'jangan simpan' tidak menghentikan penyimpanan URL",
        )

    def test_checked_still_writes_the_url(self):
        self._connect(remember=True)
        self.assertEqual(config.get("server_url", ""), "https://sekolah.sch.id")


# ---------------------------------------------------------------------------
# 5 — dialog token benar-benar menutup
# ---------------------------------------------------------------------------


class ServerConfigClosesTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        patcher = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp.name) / "config.json"
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        self._saved_cache = config._cache
        config._cache = None
        self.addCleanup(lambda: setattr(config, "_cache", self._saved_cache))

    def test_x_button_closes_the_dialog(self):
        dlg = ServerConfigDialog()
        dlg.show()
        self.assertTrue(dlg.isVisible())
        dlg.close()
        self.assertFalse(
            dlg.isVisible(),
            "tombol X tidak menutup dialog — close event di-ignore() dan "
            "siswa harus membuka Task Manager untuk keluar",
        )

    def test_escape_reject_closes_the_dialog(self):
        dlg = ServerConfigDialog()
        dlg.show()
        dlg.reject()
        self.assertFalse(
            dlg.isVisible(),
            "Escape tidak menutup dialog; reject() -> close() -> closeEvent "
            "bawaan -> reject() -> event di-ignore()",
        )

    def test_close_event_is_accepted(self):
        dlg = ServerConfigDialog()
        dlg.show()
        event = mock.Mock()
        dlg.closeEvent(event)
        event.accept.assert_called_once()
        event.ignore.assert_not_called()

    def test_closing_it_lets_the_app_reach_last_window_closed(self):
        """Bukti tombol X benar-benar mengakhiri aplikasi, bukan freeze.

        `lastWindowClosed` hanya dipancarkan dari dalam event loop — jadi
        inilah satu-satunya cara mengujinya (dan itu memang situasi
        produksi: `main()` menjalankan `app.exec_()`).
        """
        from PyQt5.QtCore import QTimer

        app = QApplication.instance()
        fired: list = []
        app.lastWindowClosed.connect(lambda: fired.append(True))
        self.addCleanup(app.lastWindowClosed.disconnect)
        dlg = ServerConfigDialog()
        dlg.show()
        QTimer.singleShot(0, dlg.close)
        QTimer.singleShot(500, app.quit)  # jaring: jangan menggantung
        app.exec_()
        self.assertTrue(
            fired,
            "menutup dialog tidak memicu lastWindowClosed; pada PC nyata "
            "aplikasi tetap berjalan tanpa jendela (Task Manager lagi)",
        )


# ---------------------------------------------------------------------------
# 6 — token ujian tampil apa adanya (bukan disamarkan seperti password)
# ---------------------------------------------------------------------------


class TokenFieldIsReadableTest(unittest.TestCase):
    """Kotak token bukan kotak password (laporan lapangan, ronde 10)."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-r10-token-")
        self._dir_patch = mock.patch.object(
            config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json")
        self._dir_patch.start()
        self._file_patch.start()
        self.addCleanup(self._dir_patch.stop)
        self.addCleanup(self._file_patch.stop)
        self.addCleanup(shutil.rmtree, self._tmp, True)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    def test_the_token_field_shows_plaintext(self):
        config.set("exam_token", "ABCD1234")
        config.set("remember_url", True)
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        self.assertEqual(
            dlg.input_token.echoMode(),
            QLineEdit.Normal,
            "token ujian disamarkan seperti password; siswa tidak bisa "
            "memastikan O vs 0 dan tidak bisa membaca token dari layar",
        )

    def test_the_plain_field_still_feeds_connect(self):
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg.input_token.setText("abcd1234")
        self.assertEqual(dlg.input_token.text().strip().upper(), "ABCD1234")


# ---------------------------------------------------------------------------
# 4 — prompt uninstall jujur soal kredensial
# ---------------------------------------------------------------------------


class UninstallPromptHonestyTest(unittest.TestCase):
    def setUp(self):
        iss = Path(__file__).resolve().parents[2] / "windows" / "installer" / "examvan.iss"
        self.text = iss.read_text(encoding="utf-8")
        self.body = self.text.split("usPostUninstall", 1)[1].split("end;", 1)[0]

    def test_prompt_states_credentials_are_always_removed(self):
        self.assertIn("admin_password.txt", self.body)
        self.assertIn("config.json", self.body)
        self.assertIn(
            "SELALU", self.body,
            "prompt menawarkan 'pertahankan' tanpa menjelaskan bahwa berkas "
            "kredensial tetap dihapus — siswa/pengawas bisa mengira data itu "
            "selamat",
        )


if __name__ == "__main__":
    unittest.main()
