"""Prompt resume untuk sesi desktop yang terhenti sebelum submit."""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QDialog, QMessageBox

from examvan import config
from examvan.models import Exam
from examvan.ui.server_config import ServerConfigDialog
from examvan.ui.timer import ElapsedTimerWidget

APP = QApplication.instance() or QApplication([])

EXAM_ID = 7
TOKEN = "ABCD1234"
START_TIME = "2026-08-16T07:00:00Z"
BUDI = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
SITI = {"nama": "Siti", "nomor_ujian": "N02", "kelas": "9A"}


def _exam(exam_id=EXAM_ID):
    return Exam(id=exam_id, name="Ujian", status="active", security_level="low")


class _Sandbox(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-resume-")
        self._dir_patch = mock.patch.object(
            config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json")
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None
        self.addCleanup(self._dir_patch.stop)
        self.addCleanup(self._file_patch.stop)
        self.addCleanup(shutil.rmtree, self._tmp, True)
        self.addCleanup(setattr, config, "_cache", None)

    def _dialog(self, identity_exam=None):
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = identity_exam or _exam()
        dlg.input_token.setText(TOKEN)
        dlg._server_url = "https://exam.example"
        try:
            dlg._sig_recovery_available.disconnect(dlg._show_recovery)
        except (TypeError, RuntimeError):
            pass
        return dlg

    def _run_identity(self, dlg, identity, reply):
        emitted = []
        recovered = []
        boxes = []
        dlg.exam_selected.connect(lambda *a: emitted.append(a))
        dlg._sig_recovery_available.connect(lambda exam: recovered.append(exam))
        identity_dialog = mock.Mock()
        identity_dialog.exec_ = mock.Mock(return_value=QDialog.Accepted)
        identity_dialog.get_identity_data = mock.Mock(
            return_value=dict(identity))

        def fake_exec(box):
            boxes.append(box)
            return reply(box) if callable(reply) else reply

        with mock.patch(
            "examvan.ui.identity_dialog.IdentityDialog",
            return_value=identity_dialog,
        ), mock.patch.object(QMessageBox, "exec_", fake_exec):
            dlg._show_identity_dialog()
        return emitted, recovered, boxes

    def _save_crash_state(self, identity_key="n01", label="exam_number=n01"):
        config.save_start_time(EXAM_ID, START_TIME)
        config.save_answers(EXAM_ID, {"1": "A"})
        config.save_answers_owner(EXAM_ID, identity_key, label=label)


class ActiveResumePromptTest(_Sandbox):
    def test_same_student_is_offered_resume_before_recovery(self):
        self._save_crash_state()
        dlg = self._dialog()

        emitted, recovered, boxes = self._run_identity(
            dlg, BUDI, QMessageBox.Yes)

        self.assertEqual(len(emitted), 1)
        self.assertEqual(recovered, [], "resume tidak boleh memicu Kirim Lagi")
        self.assertEqual(len(boxes), 1)
        self.assertIn("Lanjutkan", boxes[0].windowTitle())
        self.assertIn(START_TIME, boxes[0].informativeText())
        self.assertTrue(dlg.consume_resume_request())

    def test_custom_yes_button_is_not_mistaken_for_cancel(self):
        self._save_crash_state()
        dlg = self._dialog()

        def click_yes(box):
            for button in box.buttons():
                if box.buttonRole(button) == QMessageBox.YesRole:
                    button.click()
                    break
            return 0

        emitted, recovered, boxes = self._run_identity(
            dlg, BUDI, click_yes)

        self.assertEqual(len(emitted), 1)
        self.assertEqual(recovered, [])
        self.assertEqual(len(boxes), 1)
        self.assertTrue(dlg.consume_resume_request())

    def test_cancelling_resume_clears_identity_and_does_not_start(self):
        self._save_crash_state()
        dlg = self._dialog()

        emitted, recovered, boxes = self._run_identity(
            dlg, BUDI, QMessageBox.No)

        self.assertEqual(emitted, [])
        self.assertEqual(recovered, [])
        self.assertEqual(len(boxes), 1)
        self.assertFalse(dlg.consume_resume_request())
        self.assertEqual(config.get_identity_data(), {})
        self.assertTrue(dlg.btn_connect.isEnabled())

    def test_another_students_answers_are_not_resumed(self):
        self._save_crash_state(identity_key="n02", label="exam_number=n02")
        dlg = self._dialog()

        emitted, _recovered, boxes = self._run_identity(
            dlg, BUDI, QMessageBox.No)

        self.assertEqual(len(emitted), 1)
        self.assertEqual(boxes, [])
        self.assertFalse(dlg.consume_resume_request())

    def test_a_completed_attempt_is_not_resumed(self):
        self._save_crash_state()
        config.mark_submitted(EXAM_ID, "n01", label="exam_number=n01")
        dlg = self._dialog()

        _emitted, _recovered, boxes = self._run_identity(
            dlg, BUDI, QMessageBox.No)

        self.assertEqual(boxes, [])
        self.assertFalse(dlg.consume_resume_request())

    def test_no_start_time_means_no_resume(self):
        config.save_answers(EXAM_ID, {"1": "A"})
        config.save_answers_owner(EXAM_ID, "n01", label="exam_number=n01")
        dlg = self._dialog()

        _emitted, _recovered, boxes = self._run_identity(
            dlg, BUDI, QMessageBox.No)

        self.assertEqual(boxes, [])
        self.assertFalse(dlg.consume_resume_request())


class RedoClearsStaleMarkerTest(_Sandbox):
    def test_redo_removes_the_old_submit_marker(self):
        dlg = self._dialog()
        config.mark_submitted(EXAM_ID, "n01", label="exam_number=n01")
        self.assertTrue(config.is_submitted(EXAM_ID, "n01"))

        with mock.patch.object(QMessageBox, "exec_",
                               return_value=QMessageBox.Yes):
            self.assertTrue(dlg._offer_resubmit_choice(BUDI))

        self.assertFalse(
            config.is_submitted(EXAM_ID, "n01"),
            "percobaan baru masih dianggap selesai — crash berikutnya "
            "tidak akan menawarkan resume",
        )

    def test_redo_works_with_a_custom_yes_button(self):
        dlg = self._dialog()
        config.mark_submitted(EXAM_ID, "n01", label="exam_number=n01")

        def click_yes(box):
            for button in box.buttons():
                if box.buttonRole(button) == QMessageBox.YesRole:
                    button.click()
                    break
            return 0

        with mock.patch.object(QMessageBox, "exec_", click_yes):
            self.assertTrue(dlg._offer_resubmit_choice(BUDI))

        self.assertFalse(config.is_submitted(EXAM_ID, "n01"))

    def test_going_back_keeps_the_old_submit_marker(self):
        dlg = self._dialog()
        config.mark_submitted(EXAM_ID, "n01", label="exam_number=n01")

        with mock.patch.object(QMessageBox, "exec_",
                               return_value=QMessageBox.No):
            self.assertFalse(dlg._offer_resubmit_choice(BUDI))

        self.assertTrue(config.is_submitted(EXAM_ID, "n01"))


class ResumeTimerTest(unittest.TestCase):
    def test_resume_start_time_is_normalized_and_elapsed_is_preserved(self):
        started = datetime.now(timezone.utc) - timedelta(seconds=125)
        widget = ElapsedTimerWidget(
            start_time=started.isoformat().replace("+00:00", "Z"))
        try:
            widget._update()
            self.assertEqual(
                widget.get_start_time_iso(),
                started.strftime("%Y-%m-%dT%H:%M:%SZ"),
            )
            self.assertRegex(widget._label.text(), r"^00:02:0[4-6]$")
        finally:
            widget.stop()

    def test_invalid_resume_start_time_falls_back_to_fresh_start(self):
        before = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        widget = ElapsedTimerWidget(start_time="bukan-waktu")
        try:
            self.assertGreaterEqual(widget.get_start_time_iso(), before)
        finally:
            widget.stop()


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
