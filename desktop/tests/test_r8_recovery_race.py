"""Ronde 8 (medium) — hasil recovery tidak boleh mendarat di atas alur
persetujuan yang masih berjalan, dan menolak harus punya penjelasan.

Balapan yang ditutup file ini
-----------------------------
`_show_recovery` menjalankan worker `_recovery_submit_thread` lalu KEMBALI
dalam ~1,7 ms, jadi `_offer_pending_recovery` langsung mengembalikan False
dan `_show_identity_dialog` melanjutkan — membersihkan identitas dan
menutup jalan masuk ujian. Worker-nya masih hidup. Kalau ia selesai
beberapa detik kemudian, `_recovery_done_slot` membangun halaman hasil
FULLSCREEN di atas dialog yang sedang dipakai, dan
`_on_result_page_closed` menulis marker submit + `clear_identity()` untuk
ujian yang alirnya masih berjalan. Di antara keduanya, siswa bisa
mengetik identitas dan token berikutnya untuk ujian yang BERBEDA.

Yang tidak boleh hilang: siswa yang jawabannya belum terkirim tetap harus
mendapat jalan mengirim ulang. Karena itu tunggunya dibuat di dalam nested
event loop — UI tetap menggambar dan tetap bisa ditutup, dan seekor
watchdog memastikan worker yang tidak pernah melapor tidak membekukan
dialog.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import config
from examvan.models import Exam, IdentityField, SubmitResponse
from examvan.ui import server_config
from examvan.ui.server_config import ServerConfigDialog

APP = QApplication.instance() or QApplication([])

EXAM_ID = 7
TOKEN = "ABCD1234"
IDENTITY = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
ANSWERS = {"1": "A", "2": "B"}

FIELDS = [
    IdentityField(key="nama", label="Nama", required=True),
    IdentityField(key="nomor_ujian", label="Nomor Ujian", required=True),
    IdentityField(key="kelas", label="Kelas", required=True),
]


class _RecordingBackend:
    def protect_window(self, window):
        return True

    def reassert_capture_protection(self):
        pass

    def set_capture_protection(self, window):
        return True

    def has_multiple_monitors(self):
        return False


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp(prefix="examvan-r8-race-")
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

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        # Dialog identitas tidak boleh ikut muncul di test ini: yang diuji
        # `_offer_pending_recovery` + `_show_recovery`.
        try:
            dlg._sig_show_identity.disconnect(dlg._show_identity_dialog)
        except (TypeError, RuntimeError):
            pass
        self.dlg = dlg
        self.dlg._exam = Exam(
            id=EXAM_ID, name="Ujian", status="active",
            security_level="low", identity_fields=FIELDS,
        )
        self.dlg._server_url = "https://exam.example"
        self.dlg._validated_token = TOKEN
        config.set("exam_token", TOKEN)
        config.set_identity_session(dict(IDENTITY), {
            "exam_id": EXAM_ID, "token": TOKEN,
        })
        config.save_answers(EXAM_ID, dict(ANSWERS))
        config.save_answers_owner(EXAM_ID, "n01", label="exam_number=N01")

        # Halaman hasil tidak boleh benar-benar fullscreen di runner
        # headless, dan proteksi capture cukup backend yang meng-catat.
        backend = mock.patch(
            "examvan.security.enforcer.get_backend",
            lambda: _RecordingBackend(),
        )
        backend.start()
        self.addCleanup(backend.stop)
        shown = mock.patch(
            "examvan.ui.congratulations.CongratulationsWindow.show_fullscreen",
        )
        shown.start()
        self.addCleanup(shown.stop)

    def _submit_after(self, delay, response):
        """`api.submit_with_retry` yang baru menjawab setelah `delay`."""
        state = {"finished": None}

        def submit(*_a, **_kw):
            time.sleep(delay)
            state["finished"] = time.monotonic()
            return response

        return state, submit


class RecoveryWorkerRaceTestCase(_Sandbox):
    def test_the_offer_does_not_return_while_the_worker_is_still_running(self):
        # Worker lambat (retry + polling bisa 84 detik di produksi). Dulu
        # `_offer_pending_recovery` sudah kembali dalam ~1,7 ms dan
        # `_show_identity_dialog` melanjutkan alurnya sementara worker
        # masih hidup.
        state, submit = self._submit_after(
            0.4,
            SubmitResponse(success=True, status="ok",
                           congrats_message="Kerja bagus!"),
        )
        built = []
        original = server_config._present_result_page

        def spy(**kwargs):
            built.append(time.monotonic())
            return original(**kwargs)

        with mock.patch("examvan.ui.server_config.api.submit_with_retry",
                        side_effect=submit), \
                mock.patch("examvan.ui.server_config.api.complete_exam"), \
                mock.patch.object(QMessageBox, "question",
                                  return_value=QMessageBox.Yes), \
                mock.patch("examvan.ui.server_config._present_result_page",
                           side_effect=spy):
            self.dlg._offer_pending_recovery(dict(IDENTITY))
            returned = time.monotonic()

        self.assertIsNotNone(
            state["finished"],
            "worker tidak pernah selesai — test ini tidak menguji apa pun",
        )
        self.assertEqual(
            len(built), 1,
            "halaman hasil recovery tidak dibangun — alur jadi buntu",
        )
        self.assertLessEqual(
            state["finished"], returned,
            "`_offer_pending_recovery` kembali sebelum worker selesai — "
            "hasilnya bisa mendarat di atas alur yang sudah lanjut",
        )
        self.assertLessEqual(
            built[0], returned,
            "halaman hasil dibangun SETELAH tawaran kembali: balasan "
            "terlambat mendarat di atas alur yang sudah berjalan",
        )

    def test_nothing_lands_after_the_offer_returned(self):
        state, submit = self._submit_after(
            0.4,
            SubmitResponse(success=True, status="ok",
                           congrats_message="Kerja bagus!"),
        )
        built = []
        original = server_config._present_result_page

        def spy(**kwargs):
            built.append(time.monotonic())
            return original(**kwargs)

        with mock.patch("examvan.ui.server_config.api.submit_with_retry",
                        side_effect=submit), \
                mock.patch("examvan.ui.server_config.api.complete_exam"), \
                mock.patch.object(QMessageBox, "question",
                                  return_value=QMessageBox.Yes), \
                mock.patch("examvan.ui.server_config._present_result_page",
                           side_effect=spy):
            self.dlg._offer_pending_recovery(dict(IDENTITY))
            count_at_return = len(built)
            # Beri waktu jauh lebih lama daripada durasi worker.
            deadline = time.monotonic() + 0.8
            while time.monotonic() < deadline:
                APP.processEvents()
                time.sleep(0.01)

        self.assertEqual(
            len(built), count_at_return,
            "halaman hasil dibangun SETELAH `_offer_pending_recovery` "
            "kembali — itu persis balapan yang ditutup file ini",
        )
        self.assertIsNotNone(state["finished"])

    def test_a_worker_that_never_reports_back_cannot_freeze_the_dialog(self):
        # Worker yang hilang (proxy tidak menjawab, proses dibunuh) tidak
        # boleh menahan dialog selamanya.
        def submit(*_a, **_kw):
            time.sleep(30)
            return SubmitResponse(success=True, status="ok")

        with mock.patch.object(server_config, "RECOVERY_WAIT_SECONDS", 0.2), \
                mock.patch("examvan.ui.server_config.api.submit_with_retry",
                           side_effect=submit), \
                mock.patch("examvan.ui.server_config.api.complete_exam"), \
                mock.patch.object(QMessageBox, "question",
                                  return_value=QMessageBox.Yes):
            started = time.monotonic()
            with self.assertLogs("examvan.ui.server_config", level="WARNING"):
                allowed = self.dlg._offer_pending_recovery(dict(IDENTITY))
            elapsed = time.monotonic() - started

        self.assertLess(elapsed, 5.0, "dialog membeku menunggu worker")
        self.assertFalse(
            allowed,
            "worker yang belum selesai berarti belum tahu apakah jawaban "
            "sudah terkirim — ujian tidak boleh dimulai di atasnya",
        )

    def test_a_failed_resend_keeps_the_answers_and_says_so(self):
        with mock.patch("examvan.ui.server_config.api.submit_with_retry",
                        return_value=SubmitResponse(
                            success=False, status="error",
                            message="403 forbidden")), \
                mock.patch.object(QMessageBox, "question",
                                  return_value=QMessageBox.Yes):
            allowed = self.dlg._offer_pending_recovery(dict(IDENTITY))

        self.assertFalse(allowed)
        self.assertEqual(
            config.load_answers(EXAM_ID), ANSWERS,
            "kirim ulang gagal tapi jawaban hilang dari disk",
        )
        self.assertTrue(
            self.dlg.lbl_status.text().strip(),
            "kegagalan tanpa penjelasan di layar",
        )
        self.assertTrue(self.dlg.btn_connect.isEnabled())


class DecliningRecoveryExplainsTestCase(_Sandbox):
    def test_declining_tells_the_student_what_happened(self):
        with mock.patch.object(QMessageBox, "question",
                               return_value=QMessageBox.No):
            allowed = self.dlg._offer_pending_recovery(dict(IDENTITY))

        self.assertFalse(allowed)
        status = self.dlg.lbl_status.text()
        self.assertTrue(
            status.strip(),
            "menolak 'Kirim Lagi' tanpa penjelasan: siswa menekan tombol "
            "dan tidak terjadi apa-apa, tidak tahu jawabannya masih di disk",
        )
        lowered = status.lower()
        self.assertTrue(
            "tertesimpan" in lowered or "masih" in lowered,
            f"penjelasan harus menyebut jawaban masih tersimpan: {status!r}",
        )
        self.assertTrue(
            "hubungkan" in lowered or "pengawas" in lowered,
            f"penjelasan harus memberi jalan keluar: {status!r}",
        )
        self.assertTrue(self.dlg.btn_connect.isEnabled())
        self.assertEqual(
            config.load_answers(EXAM_ID), ANSWERS,
            "jawaban boleh tetap di disk, tapi TIDAK boleh hilang",
        )


if __name__ == "__main__":
    unittest.main()
