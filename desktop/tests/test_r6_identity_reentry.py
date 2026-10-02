"""Ronde 6 (item 5) — marker "sudah terkumpul" harus benar-benar DIKONSUMSI.

Bug
---
`config.is_submitted` / `config.submitted_labels` tidak punya satu pun
production caller, tapi `ui/exam_viewer.py:781` tetap memberi tahu pembaca
bahwa "`ServerConfigDialog.is_submitted`" memblokir re-entry. Perintah itu
tidak pernah terjadi: yang ada hanyalah PENULISnya
(`config.mark_submitted` di `_cleanup_after_submit`). Gate yang sebenarnya
adalah `repeat_required` di server.

Dua jalur perbaikan, dan hanya satu yang tidak merusak apa pun:

* HAPUS `is_submitted` + test-nya — ditolak. Ia punya 10+ asersi hidup di
  LIMA file test (`test_config.py`, `test_auto_submit.py`,
  `test_hardening_round4.py` — termasuk merge-ke-raw C-E yang menyatukan
  label ter-obfuscated, `test_server_config.py`,
  `test_config_untrusted_input.py`). Menghapusnya menghapus coverage
  hardening yang masih relevan, bukan sekadar kode mati.
* KONSUMSI marker-nya — dipilih. `server_config.py` tidak lagi meng-join
  siswa secara diam-diam: begitu marker untuk identitas + ujian ini ada, siswa
  diberi pilihan eksplisit ("Jawaban sudah terkumpul — kerjakan ulang /
  kembali"), bukan langsung di-join tanpa diberi tahu.

Yang TIDAK berubah (dan diuji di sini): ini BUKAN pemblokiran. Gate lama
yang memblokir dihapus dengan sengaja — satu PC lab dipakai banyak siswa,
dan memblokir berdasarkan "sudah pernah" menutup jalan bagi siswa yang
memang harus mengulang. Yang ditambahkan adalah INFORMASI: siswa tahu
jawabannya sudah tercatat, dan memilih sendiri. `_offer_pending_recovery`
tetap `return True` tanpa syarat.
"""

from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtWidgets import QApplication, QDialog, QMessageBox

from examvan import config
from examvan.models import Exam
from examvan.ui.server_config import ServerConfigDialog

APP = QApplication.instance() or QApplication([])

BUDI = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
SITI = {"nama": "Siti", "nomor_ujian": "N02", "kelas": "9A"}
BUDI_KEY = "n01"
SITI_KEY = "n02"
TOKEN = "TOKLAB01"


def _exam(exam_id=7):
    return Exam(id=exam_id, name="Ujian", status="active", security_level="low")


class _MarkerTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-r6-marker-")
        for attr, value in (
            ("_CONFIG_DIR", Path(self._tmp)),
            ("_CONFIG_FILE", Path(self._tmp) / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self.addCleanup(shutil.rmtree, self._tmp, True)

    def _dlg(self):
        dlg = ServerConfigDialog()
        dlg._exam = _exam()
        dlg.input_token.setText(TOKEN)
        return dlg

    def _run_identity(self, dlg, identity, redo=QMessageBox.Yes):
        """Jalankan `_show_identity_dialog` dengan QMessageBox di-stub.

        `QMessageBox.exec_` yang di-stub mengembalikan StandardButton yang
        diklik (dokumentasi Qt untuk QMessageBox), jadi test bisa memilih
        jawaban siswa tanpa pernah memblokir di headless.
        """
        emitted = []
        dlg.exam_selected.connect(lambda *a: emitted.append(a))
        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(identity))
        boxes = []
        reply = QMessageBox.Yes if redo else QMessageBox.No

        def fake_exec(self):
            boxes.append(self)
            return reply

        with mock.patch(
            "examvan.ui.identity_dialog.IdentityDialog",
            return_value=identity_dialog,
        ), mock.patch.object(
            QMessageBox, "exec_", fake_exec
        ), mock.patch.object(QMessageBox, "question", return_value=reply):
            dlg._show_identity_dialog()
        return emitted, boxes


class MarkerIsActuallyConsultedTest(_MarkerTestCase):
    def test_same_student_gets_an_explicit_choice(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        _emitted, boxes = self._run_identity(self._dlg(), BUDI)
        self.assertEqual(len(boxes), 1,
                         "marker 'sudah terkumpul' untuk identitas ini tidak "
                         "dikonsumsi sama sekali — dialog masih meng-join "
                         "siswa tanpa memberi tahu jawaban yang lalu sudah "
                         "tercatat")

    def test_choice_message_names_the_already_submitted_state(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        _emitted, boxes = self._run_identity(self._dlg(), BUDI)
        shown = (boxes[0].windowTitle() + " " + boxes[0].text()).lower()
        self.assertIn("terkumpul", shown)
        self.assertIn("tercatat", shown)

    def test_choice_message_offers_both_options(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        _emitted, boxes = self._run_identity(self._dlg(), BUDI)
        labels = [b.text() for b in boxes[0].buttons()]
        self.assertEqual(len(labels), 2, labels)
        self.assertRegex(" | ".join(labels).lower(), r"ulang")
        self.assertRegex(" | ".join(labels).lower(), r"kembali|batal|tidak")

    def test_default_button_is_the_safe_one(self):
        """Enter yang tidak sengaja tidak boleh memulai percobaan baru."""
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        _emitted, boxes = self._run_identity(self._dlg(), BUDI)
        default = boxes[0].defaultButton()
        self.assertIsNotNone(default)
        self.assertNotEqual(boxes[0].buttonRole(default), QMessageBox.YesRole)

    def test_different_student_is_not_prompted(self):
        """Satu PC lab dipakai bersama — marker siswa lain bukan urusannya."""
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        emitted, boxes = self._run_identity(self._dlg(), SITI)
        self.assertEqual(boxes, [])
        self.assertEqual(len(emitted), 1, "siswa berbeda tetap boleh masuk")

    def test_other_exam_marker_is_not_prompted(self):
        config.mark_submitted(8, BUDI_KEY, label="exam_number=n01")
        emitted, boxes = self._run_identity(self._dlg(), BUDI)
        self.assertEqual(boxes, [])
        self.assertEqual(len(emitted), 1)

    def test_no_marker_means_no_prompt(self):
        emitted, boxes = self._run_identity(self._dlg(), BUDI)
        self.assertEqual(boxes, [])
        self.assertEqual(len(emitted), 1)


class MarkerChoiceIsNotABlockTest(_MarkerTestCase):
    """Memilih "kerjakan ulang" tetap boleh masuk — ini bukan gate blokir."""

    def test_choosing_to_redo_still_joins(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        emitted, _boxes = self._run_identity(self._dlg(), BUDI, redo=True)
        self.assertEqual(len(emitted), 1, emitted)
        self.assertEqual(emitted[0][2], BUDI)

    def test_choosing_go_back_does_not_join(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        emitted, _boxes = self._run_identity(self._dlg(), BUDI, redo=False)
        self.assertEqual(emitted, [], "siswa memilih kembali — jangan join")

    def test_going_back_restores_the_connect_ui(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        dlg = self._dlg()
        self._run_identity(dlg, BUDI, redo=False)
        self.assertTrue(dlg.btn_connect.isEnabled())
        self.assertTrue(dlg.input_token.isEnabled())

    def test_going_back_leaves_no_identity_behind(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        self._run_identity(self._dlg(), BUDI, redo=False)
        self.assertEqual(config.get_identity_data(), {})

    def test_offer_pending_recovery_still_never_blocks(self):
        """Kontrak lama dipertahankan: recovery BUKAN gerbang masuk."""
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        dlg = self._dlg()
        self.assertTrue(dlg._offer_pending_recovery(BUDI))
        self.assertTrue(dlg._offer_pending_recovery(SITI))


class MarkerNeverBreaksTheJoinTest(_MarkerTestCase):
    def test_config_failure_falls_back_to_joining(self):
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        with mock.patch.object(
            config, "is_submitted", side_effect=OSError("disk gone")
        ), mock.patch.object(QMessageBox, "question", return_value=QMessageBox.Yes):
            emitted = []
            dlg = self._dlg()
            dlg.exam_selected.connect(lambda *a: emitted.append(a))
            identity_dialog = mock.Mock()
            identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
            identity_dialog.get_identity_data = mock.Mock(return_value=dict(BUDI))
            with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                            return_value=identity_dialog):
                dlg._show_identity_dialog()
        self.assertEqual(len(emitted), 1,
                         "kegagalan membaca marker tidak boleh menutup jalan "
                         "siswa — client bukan tempat aturan yang tidak "
                         "bisa diaudit")

    def test_pending_recovery_takes_precedence_over_the_marker_prompt(self):
        """Jawaban yang BELUM terkirim lebih mendesak dari marker."""
        config.mark_submitted(7, BUDI_KEY, label="exam_number=n01")
        config.save_answers(7, {"1": "A"})
        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(return_value=dict(BUDI))
        dlg = self._dlg()
        recovery_titles = []
        marker_boxes = []

        def fake_question(parent, title, *_a, **_k):
            recovery_titles.append(title)
            return QMessageBox.No

        def fake_exec(self):
            marker_boxes.append(self)
            return QMessageBox.No

        with mock.patch("examvan.ui.identity_dialog.IdentityDialog",
                        return_value=identity_dialog), \
             mock.patch.object(QMessageBox, "question", side_effect=fake_question), \
             mock.patch.object(QMessageBox, "exec_", fake_exec):
            dlg._show_identity_dialog()
        self.assertEqual(recovery_titles, ["Jawaban Belum Terkirim"])
        self.assertEqual(marker_boxes, [],
                         "dua prompt sekaligus membingungkan; jawaban yang "
                         "belum terkirim harus satu-satunya yang "
                         "ditawarkan")


if __name__ == "__main__":  # pragma: no cover
    unittest.main()