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

from PyQt5.QtWidgets import QApplication, QDialog, QMessageBox

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


# Kunci marker "sudah dikumpulkan" = identitas siswa (nomor ujian),
# bukan token. Di lab satu token dipakai bersama seluruh kelas, jadi token
# sebagai kunci akan membuat siswa-siswa saling memblokir.
BUDI = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
SITI = {"nama": "Siti", "nomor_ujian": "N02", "kelas": "9A"}
STUDENT_KEY = "n01"

REPO_PATH = Path(__file__).resolve().parents[2]


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
        config.mark_submitted(7, STUDENT_KEY)
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("ABCD1234")
        statuses = []
        dlg._sig_status.connect(lambda msg, err: statuses.append((msg, err)))
        dlg._sig_recovery_available.connect(
            lambda exam: self.fail("recovery tidak boleh ditawarkan")
        )
        self.assertFalse(dlg._check_already_submitted(BUDI))
        # Blokir: ada error status yang menyebut perangkat, dan TIDAK ada
        # tawaran recovery.
        self.assertTrue(any(err for _, err in statuses), statuses)

    def test_submitted_with_pending_offers_recovery(self):
        config.mark_submitted(7, STUDENT_KEY)
        # Token harus sudah tersimpan saat jawaban di-encode (kunci XOR).
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A"})  # submit background sebelumnya gagal
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("ABCD1234")
        recovered = []
        dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
        # _show_recovery memakai QMessageBox.question yang modal — tanpa
        # stub di sini test menggantung, bukan gagal.
        with mock.patch.object(
            QMessageBox, "question", return_value=QMessageBox.No
        ):
            self.assertFalse(dlg._check_already_submitted(BUDI))
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

    Keluhan yang memicu perbaikannya: "tidak bisa mengerjakan ujian yang
    sama untuk kedua kalinya". Penyebabnya marker `submitted_<exam_id>`
    yang hanya berdasar UJIAN, padahal disimpan per MESIN di
    `~/.config/examvan/config.json`. Begitu siswa pertama selesai, PC itu
    memblokir ujian yang sama untuk semua siswa berikutnya.

    Marker sekarang per (ujian, siswa). Gerbangnya dijalankan setelah
    identitas diketahui -- sebelum itu aplikasi hanya tahu token, dan di
    lab token sering dipakai BERSAMA seluruh kelas, jadi token sebagai kunci
    akan membuat siswa saling memblokir. That's the bug the previous
    commit introduced: scoping by token looked right and was wrong.
    """

    def _dlg(self):
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("TOKLAB01")
        return dlg

    def test_second_student_on_the_same_pc_is_not_blocked(self):
        # Ini inti masalahnya. Token SAMA, PC SAMA, ujian SAMA.
        config.mark_submitted(7, "n01")
        dlg = self._dlg()
        self.assertTrue(
            dlg._check_already_submitted(SITI),
            "siswa kedua dengan nomor berbeda harus BOLEH masuk",
        )

    def test_the_same_student_is_still_blocked(self):
        # Overwrite protection TIDAK boleh hilang: siswa yang sama, ujian
        # yang sama → diblokir, supaya re-entry dalam window grace tidak
        # mengirim submit kosong yang menimpa jawaban asli.
        config.mark_submitted(7, "n01")
        dlg = self._dlg()
        self.assertFalse(dlg._check_already_submitted(BUDI))

    def test_legacy_machine_wide_marker_is_ignored(self):
        # PC yang pernah terkirim dengan skema lama (kunci
        # `submitted_<exam_id>` tanpa identitas) harus bisa dipakai lagi.
        config.set("submitted_7", True)
        dlg = self._dlg()
        self.assertTrue(dlg._check_already_submitted(SITI))

    def test_identity_key_falls_back_to_name_when_no_number(self):
        # Ujian yang tidak mengumpulkan nomor ujian harus tetap bisa
        # membedakan siswa lewat nama.
        config.mark_submitted(7, "budi")
        dlg = self._dlg()
        self.assertFalse(dlg._check_already_submitted(
            {"nama": "Budi", "kelas": "9A"}
        ))
        self.assertTrue(dlg._check_already_submitted(
            {"nama": "Andi", "kelas": "9A"}
        ))

    def test_without_any_identity_falls_back_to_the_token(self):
        # Tidak ada identitas sama sekali: jatuh ke token, yaitu perilaku
        # lama. Konservatif -- memblokir lebih baik daripada melepas.
        #
        # Token di sini harus <= 8 karakter: `input_token` memakai
        # setMaxLength(8), jadi token yang lebih panjang dipotong diam-diam
        # dan testnya menguji hal yang tidak terjadi di aplikasi.
        config.mark_submitted(7, "toklab01")
        dlg = self._dlg()
        self.assertFalse(dlg._check_already_submitted({}))

    def test_marker_does_not_leak_the_student_key_into_the_config_file(self):
        # Nama siswa tidak boleh tersimpan mentah: config.json ada di disk
        # dan ikut dikirim bersama jawaban.
        config.mark_submitted(7, "budi")
        keys = [k for k in config.get_all() if k.startswith("submitted_7")]
        self.assertTrue(keys)
        for k in keys:
            self.assertNotIn("budi", k.lower())

    def test_marker_key_is_stable_and_case_insensitive(self):
        from examvan.config import _submitted_key

        self.assertEqual(_submitted_key(7, "N01"), _submitted_key(7, "N01"))
        # Nomor ujian bisa diketik "n01" atau "N01"; itu satu siswa.
        self.assertEqual(_submitted_key(7, "N01"), _submitted_key(7, " n01 "))
        self.assertNotEqual(_submitted_key(7, "N01"), _submitted_key(7, "N02"))
        self.assertNotEqual(_submitted_key(7, "N01"), _submitted_key(8, "N01"))


class GateWiringTest(RecoveryGateTestCase):
    """Gerbang harus benar-benar TERWIRING di alur, bukan cuma ada fungsi.

    Test lain memanggil `dlg._check_already_submitted(...)` langsung, jadi
    semuanya lulus walaupun pemanggilnya dihapus dari
    `_show_identity_dialog`. Itu kelas gap yang sama seperti membaca
    `.iss` sebagai teks: yang diuji bukan jalur yang benar-benar dipakai.

    Test di sini menjalankan alur aslinya dengan `IdentityDialog` di-stub,
    lalu memastikan `exam_selected` hanya terpakai bila gerbang mengizinkan.
    """

    def _run_dialog(self, dlg, identity):
        emitted = []
        dlg.exam_selected.connect(lambda *a: emitted.append(a))
        with mock.patch("examvan.ui.identity_dialog.IdentityDialog") as Dlg:
            inst = Dlg.return_value
            inst.exec_.return_value = QDialog.Accepted
            inst.get_identity_data.return_value = identity
            dlg._show_identity_dialog()
        return emitted

    def _dlg(self, token="TOKLAB01"):
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText(token)
        return dlg

    def test_a_blocked_student_never_reaches_exam_selected(self):
        config.mark_submitted(7, STUDENT_KEY)          # Budi sudah submit
        dlg = self._dlg()
        statuses = []
        dlg._sig_status.connect(lambda msg, err: statuses.append((msg, err)))
        emitted = self._run_dialog(dlg, BUDI)
        self.assertEqual(
            emitted, [],
            "exam_selected terpakai padahal siswa sudah submit -- ini akan "
            "membuka jalan ke approval, PDF, dan submit kosong",
        )
        self.assertTrue(any(err for _, err in statuses), statuses)

    def test_a_second_student_does_reach_exam_selected(self):
        config.mark_submitted(7, STUDENT_KEY)          # Budi sudah submit
        dlg = self._dlg()
        emitted = self._run_dialog(dlg, SITI)
        self.assertEqual(len(emitted), 1, emitted)
        self.assertEqual(emitted[0][2], SITI)

    def test_gate_runs_before_exam_selected_not_after(self):
        # Urutan penting: identitas disimpan dulu (supaya recovery nanti
        # tahu), tapi gate harus menolak sebelum sinyal keluar.
        config.mark_submitted(7, STUDENT_KEY)
        dlg = self._dlg()
        self.assertEqual(self._run_dialog(dlg, BUDI), [])

    def test_the_gate_is_wired_into_the_dialog_path(self):
        # Penjaga tambahan: kalau ada yang memindahkan gate keluar dari
        # _show_identity_dialog, test ini langsung terlihat.
        src = (REPO_PATH / "desktop/examvan/ui/server_config.py").read_text(
            encoding="utf-8"
        )
        body = src[src.index("def _show_identity_dialog"):]
        self.assertIn("_check_already_submitted", body)
