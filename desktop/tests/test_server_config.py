"""Headless tests for ServerConfigDialog re-entry recovery gate.

Covers the desktop↔Android consistency fix (Agustus 2026):
- after a durable submit, re-entry is BLOCKED (F2 sticky marker) — the
  watchdog must not be able to send an empty submit that overwrites real
  answers in the server's grace window;
- BUT if the auto-submit background failed (answers still on disk), re-entry
  must offer "Kirim Lagi" instead of a dead end (mirror Android
  showPendingSubmitRecoveryScreen + resubmitPendingAnswers);
- a successful resubmit clears the local copy; a failed one keeps it.
"""

from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import api, config
from examvan.models import Exam, HealthResponse, SubmitResponse, TokenExamResponse
from examvan.ui.server_config import ServerConfigDialog

APP = QApplication.instance() or QApplication([])


def _exam(exam_id=7):
    return Exam(id=exam_id, name="Ujian", status="active", security_level="low")


class RecoveryGateTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-serverconfig-test-")
        self._dir_patch = mock.patch.object(config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json"
        )
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None

        self._health_patch = mock.patch.object(
            api, "check_health", return_value=HealthResponse(success=True)
        )
        self._health_patch.start()
        self._token_patch = mock.patch.object(
            api, "get_exam_by_token",
            return_value=TokenExamResponse(success=True, exam=_exam()),
        )
        self._token_patch.start()

    def tearDown(self):
        self._token_patch.stop()
        self._health_patch.stop()
        self._file_patch.stop()
        self._dir_patch.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)

    def _connect(self, dlg):
        """Jalankan _connect_thread — recovery di-_show_recovery via signal."""
        dlg._connect_thread("https://exam.example", "ABCD1234")


def _detach_identity_dialog(dlg) -> None:
    """Lepas slot `_show_identity_dialog` supaya IdentityDialog tidak memblokir.

    `IdentityDialog.exec_()` bersifat modal: kalau flow mencapai dialog itu
    tanpa ada yang menggantinya, test menggantung sampai runner dibunuh.
    Melepasnya membuat regresi "gate tidak lagi memblokir" muncul sebagai
    assertion failure yang bisa dibaca, bukan sebagai timeout.
    """
    try:
        dlg._sig_show_identity.disconnect()
    except TypeError:
        pass


class SubmittedGateTest(RecoveryGateTestCase):
    def test_submitted_no_pending_blocks(self):
        # Token WAJIB sama dengan yang dipakai gate, karena marker
        # sekarang di-scope per percobaan (exam + token).
        config.mark_submitted(7, "ABCD1234")
        dlg = ServerConfigDialog()
        # Slot dialog identitas dilepas: kalau gate suatu saat berhenti
        # memblokir, test ini harus GAGAL dengan assertion yang jelas,
        # bukan menggantung di IdentityDialog.exec_() sampai CI timeout.
        _detach_identity_dialog(dlg)
        statuses = []
        recovered = []
        dlg._sig_status.connect(lambda msg, err: statuses.append((msg, err)))
        dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
        self._connect(dlg)
        # Blokir: ada error status, dan TIDAK ada tawaran recovery.
        self.assertTrue(any(err for _, err in statuses))
        self.assertEqual(recovered, [])

    def test_submitted_with_pending_offers_recovery(self):
        config.mark_submitted(7, "ABCD1234")
        # Token harus sudah tersimpan saat jawaban di-encode (kunci XOR).
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A"})  # submit background sebelumnya gagal
        dlg = ServerConfigDialog()
        _detach_identity_dialog(dlg)
        recovered = []
        # Mock dialog konfirmasi → jawab "No" (tidak lanjut ke thread submit).
        with mock.patch.object(
            QMessageBox, "question", return_value=QMessageBox.No
        ):
            dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
            self._connect(dlg)
        self.assertEqual(len(recovered), 1)
        self.assertEqual(recovered[0].id, 7)

    def test_not_submitted_proceeds_past_gate(self):
        dlg = ServerConfigDialog()
        recovered = []
        identity_called = []
        # Ganti slot _show_identity_dialog (IdentityDialog exec_ memblokir).
        try:
            dlg._sig_show_identity.disconnect()
        except TypeError:
            pass
        dlg._sig_show_identity.connect(lambda: identity_called.append(True))
        dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
        self._connect(dlg)
        # Tanpa marker submitted → lanjut ke dialog identitas (bukan recovery).
        self.assertEqual(identity_called, [True])
        self.assertEqual(recovered, [])


class RecoverySubmitTest(RecoveryGateTestCase):
    def test_success_clears_answers_and_emits_done(self):
        config.mark_submitted(7)
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A", "2": "B"})
        config.save_start_time(7, "2026-08-16T07:00:00Z")
        config.set("identity_data", {"nama": "Budi", "nomor_ujian": "N01"})
        dlg = ServerConfigDialog()
        dlg.input_token.setText("ABCD1234")
        dlg._server_url = "https://exam.example"
        done = []
        dlg._sig_recovery_done.connect(lambda msg: done.append(msg))
        resp = SubmitResponse(
            success=True, status="done", message="ok",
            congrats_message="Selamat, Budi!",
        )
        with mock.patch.object(api, "submit_with_retry", return_value=resp) as sub, \
                mock.patch.object(api, "complete_exam", return_value=True) as cm, \
                mock.patch.object(QMessageBox, "information"):
            dlg._recovery_submit_thread(_exam())
        # Jawaban lokal dihapus HANYA setelah durable + congrats dikirim.
        self.assertEqual(done, ["Selamat, Budi!"])
        self.assertIsNone(config.load_answers(7))
        # complete_exam dipanggil (presence OFFLINE) — di-mock, bukan network
        # call nyata ke server uji.
        cm.assert_called_once()
        # Submit memakai jawaban disk + identitas konsisten + start_time asli.
        args = sub.call_args.args
        self.assertEqual(args[5], {"1": "A", "2": "B"})
        self.assertEqual(args[6], "2026-08-16T07:00:00Z")
        # Marker sticky tetap (ujian selesai).
        self.assertTrue(config.is_submitted(7))

    def test_failure_keeps_answers(self):
        config.mark_submitted(7)
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A"})
        config.set("identity_data", {"nama": "Budi"})
        dlg = ServerConfigDialog()
        dlg.input_token.setText("ABCD1234")
        dlg._server_url = "https://exam.example"
        done = []
        dlg._sig_recovery_done.connect(lambda msg: done.append(msg))
        resp = SubmitResponse(success=False, message="server unreachable")
        with mock.patch.object(api, "submit_with_retry", return_value=resp):
            dlg._recovery_submit_thread(_exam())
        self.assertEqual(done, [])
        # Jawaban TETAP di disk untuk percobaan berikutnya.
        self.assertEqual(config.load_answers(7), {"1": "A"})
        self.assertTrue(config.is_submitted(7))


if __name__ == "__main__":
    unittest.main()


class LabSharedPcGateTest(RecoveryGateTestCase):
    """Satu PC lab, beberapa siswa, ujian yang sama.

    Ini keluhan yang prompted perbaikannya: "tidak bisa mengerjakan ujian
    yang sama untuk kedua kalinya". Penyebabnya marker `submitted_<exam_id>`
    yang hanya berdasar UJIAN, padahal disimpan per MESIN di
    `~/.config/examvan/config.json`. Begitu siswa pertama selesai, PC itu
    memblokir ujian yang sama untuk semua siswa berikutnya -- dan blocker itu
    berjalan SEBELUM dialog identitas, jadi aplikasi tidak pernah tahu itu
    siswa yang berbeda.

    Marker sekarang di-scope per token. Token adalah satu-satunya identitas
    percobaan yang sudah ada di titik gate ini, dan token berbeda berarti
    percobaan berbeda.
    """

    def test_a_second_student_with_a_new_token_is_not_blocked(self):
        # Siswa pertama selesai dengan tokennya.
        config.mark_submitted(7, "TOKEN-SISWA-01")
        # Siswa kedua, PC sama, token berbeda → harus BOLEH masuk.
        self.assertFalse(config.is_submitted(7, "TOKEN-SISWA-02"))

    def test_the_same_token_is_still_blocked(self):
        # Overwrite protection TIDAK boleh hilang: token sama = percobaan
        # sama, dan re-entry di dalam window grace akan mengirim submit
        # kosong yang menimpa jawaban asli.
        config.mark_submitted(7, "TOKEN-SISWA-01")
        self.assertTrue(config.is_submitted(7, "TOKEN-SISWA-01"))

    def test_the_legacy_machine_wide_marker_is_ignored(self):
        # PC yang pernah terkirim dengan skema lama (kunci
        # `submitted_<exam_id>` tanpa token) harus bisa dipakai lagi.
        config.set("submitted_7", True)
        self.assertFalse(config.is_submitted(7, "TOKEN-SISWA-02"))

    def test_marker_does_not_leak_the_token_into_the_config_file(self):
        config.mark_submitted(7, "RAHASIA-TOKEN-123")
        keys = [k for k in config.get_all() if k.startswith("submitted_7")]
        self.assertTrue(keys)
        for k in keys:
            self.assertNotIn("RAHASIA", k)

    def test_marker_key_is_stable_and_different_per_token(self):
        from examvan.config import _submitted_key

        self.assertEqual(
            _submitted_key(7, "ABCD1234"), _submitted_key(7, "ABCD1234")
        )
        self.assertNotEqual(
            _submitted_key(7, "ABCD1234"), _submitted_key(7, "WXYZ9999")
        )
        # Berbeda ujian juga harus berbeda kunci.
        self.assertNotEqual(
            _submitted_key(7, "ABCD1234"), _submitted_key(8, "ABCD1234")
        )

    def test_token_whitespace_does_not_create_a_second_marker(self):
        # Gate memakai token dari input yang sudah .strip(), dan marker
        # juga. Kalau tidak, "ABCD1234" dan " ABCD1234 " jadi dua percobaan.
        from examvan.config import _submitted_key

        self.assertEqual(
            _submitted_key(7, "ABCD1234"), _submitted_key(7, "  ABCD1234  ")
        )

    def test_gate_passes_a_different_token_through(self):
        config.mark_submitted(7, "TOKEN-SISWA-01")
        dlg = ServerConfigDialog()
        identity_called = []
        # IdentityDialog.exec_() memblokir, jadi slot aslinya dilepas dulu.
        try:
            dlg._sig_show_identity.disconnect()
        except TypeError:
            pass
        dlg._sig_show_identity.connect(lambda: identity_called.append(1))
        dlg._connect_thread("https://exam.example", "TOKEN-SISWA-02")
        self.assertEqual(len(identity_called), 1)
