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
    """_ATURAN_ "sudah dikerjakan" dihapus dari client.

    Gate ini dulu memblokir dengan "Ujian ini sudah dikumpulkan pada
    perangkat ini" begitu ada jawaban yang pernah dikirim dari PC ini --
    bahkan dengan identitas yang berbeda. Di lab yang berbagi PC itu
    menutup jalan bagi semua siswa setelah yang pertama.

    Sekarang client tidak pernah menolak berdasarkan itu. Yang tersisa
    hanya pemulihan data: jawaban yang masih tersimpan di disk karena
    submit sebelumnya gagal, dan itu milik siswa.
    """

    def test_a_previous_submission_no_longer_blocks(self):
        # Inilah yang dikeluhkan user: setelah submit pertama, PC yang sama
        # menolak semua siswa berikutnya untuk ujian yang sama.
        config.mark_submitted(7, STUDENT_KEY, label="exam_number=n01")
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("TOKLAB01")
        statuses = []
        dlg._sig_status.connect(lambda msg, err: statuses.append((msg, err)))
        for identity in (BUDI, SITI, {"nama": "Andi", "nomor_ujian": "N09"}):
            with self.subTest(identity=identity):
                self.assertTrue(
                    dlg._offer_pending_recovery(),
                    f"{identity} seharusnya boleh masuk",
                )
        self.assertEqual(statuses, [], "tidak boleh ada pesan penolakan")

    def test_repeat_attempt_by_the_same_student_is_allowed(self):
        # Siswa yang sama mengulang juga BOLEH. Overwrite protection tidak
        # lagi jadi alasan memblokir: submissions di server adalah tabel
        # tanpa UNIQUE(exam_id, mac_address) -- submit menambah baris baru,
        # jadi percobaan lama tidak tertimpa.
        config.mark_submitted(7, STUDENT_KEY, label="exam_number=n01")
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("TOKLAB01")
        self.assertTrue(dlg._offer_pending_recovery())

    def test_pending_answers_still_offer_recovery(self):
        # Yang tersisa bukan pembatasan: jawaban yang belum terkirim harus
        # tetap ditawarkan supaya tidak hilang.
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A"})
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText("ABCD1234")
        recovered = []
        dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
        with mock.patch.object(
            QMessageBox, "question", return_value=QMessageBox.No
        ):
            self.assertFalse(dlg._offer_pending_recovery())
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


class MarkerBookkeepingTest(RecoveryGateTestCase):
    """Marker tetap dicatat, tapi tidak lagi memblokir siapa pun.

    Yang dicatat sekarang adalah dict {hash kunci: label}, bukan boolean,
    supaya config.json bisa menunjukkan identitas mana yang sudah pernah
    mengirim. Kuncinya masih di-hash: token adalah kredensial dan tidak
    boleh tersimpan mentah, sedangkan nomor/nama siswa sudah ada plaintext
    di `identity_data` pada file yang sama.
    """

    def _dlg(self, token="TOKLAB01"):
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText(token)
        return dlg

    def test_marker_is_scoped_per_student(self):
        config.mark_submitted(7, "n01", label="exam_number=n01")
        config.mark_submitted(7, "n02", label="exam_number=n02")
        self.assertTrue(config.is_submitted(7, "n01"))
        self.assertTrue(config.is_submitted(7, "n02"))
        self.assertFalse(config.is_submitted(7, "n03"))

    def test_marker_is_scoped_per_exam_too(self):
        config.mark_submitted(7, "n01", label="a")
        self.assertFalse(config.is_submitted(8, "n01"))

    def test_labels_are_kept_for_the_record(self):
        config.mark_submitted(7, "n01", label="exam_number=n01")
        config.mark_submitted(7, "n02", label="exam_number=n02")
        self.assertEqual(
            config.submitted_labels(7), ["exam_number=n01", "exam_number=n02"]
        )

    def test_token_is_never_stored_in_the_clear(self):
        config.mark_submitted(7, "abcd1234", label="token (identitas kosong)")
        raw = str(config.get_all())
        self.assertNotIn("abcd1234", raw.lower())

    def test_a_legacy_boolean_marker_does_not_block(self):
        # Nilai lama adalah boolean. Membacanya sebagai "sudah submits"
        # akan menghidupkan kembali blokir yang baru saja dihapus.
        config.set("submitted_7", True)
        self.assertFalse(config.is_submitted(7, "n01"))
        self.assertTrue(self._dlg()._offer_pending_recovery())

    def test_marker_key_is_stable_and_case_insensitive(self):
        from examvan.config import _submitted_key

        self.assertEqual(_submitted_key("N01"), _submitted_key("N01"))
        self.assertEqual(_submitted_key("N01"), _submitted_key(" n01 "))
        self.assertNotEqual(_submitted_key("N01"), _submitted_key("N02"))


class NoClientSideGateTest(RecoveryGateTestCase):
    """Tidak boleh ada gerbang "sudah dikerjakan" tersisa di client.

    Dijalankan lewat alur aslinya dengan IdentityDialog di-stub, bukan
    dengan memanggil method secara langsung: pemanggilan langsung lulus
    walaupun pemanggilnya dihapus dari alur -- kelas gap yang sama seperti
    membaca .iss sebagai teks.
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

    def test_a_second_student_on_the_same_pc_reaches_exam_selected(self):
        config.mark_submitted(7, "n01", label="exam_number=n01")
        emitted = self._run_dialog(self._dlg(), SITI)
        self.assertEqual(len(emitted), 1, emitted)
        self.assertEqual(emitted[0][2], SITI)

    def test_the_same_student_may_repeat_the_exam(self):
        config.mark_submitted(7, "n01", label="exam_number=n01")
        emitted = self._run_dialog(self._dlg(), BUDI)
        self.assertEqual(len(emitted), 1, emitted)

    def test_no_rejection_message_is_ever_shown(self):
        config.mark_submitted(7, "n01", label="exam_number=n01")
        dlg = self._dlg()
        statuses = []
        dlg._sig_status.connect(lambda msg, err: statuses.append((msg, err)))
        self._run_dialog(dlg, BUDI)
        blocked = [m for m, err in statuses if "sudah dikumpulkan" in m]
        self.assertEqual(
            blocked, [],
            "pesan 'sudah dikumpulkan' harus hilang dari client",
        )

    def test_the_blocking_call_site_is_gone(self):
        src = (REPO_PATH / "desktop/examvan/ui/server_config.py").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("is_submitted", src)
        self.assertNotIn("sudah dikumpulkan", src)
