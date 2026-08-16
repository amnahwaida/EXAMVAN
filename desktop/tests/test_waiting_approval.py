"""Unit tests for examvan.ui.waiting_approval — device identity consistency.

Covers the desktop↔Android consistency fix (Agustus 2026): request-approval
must use the SAME device identity (DESKTOP:<hash>) as PDF download and
submit. Previously it sent the raw MAC while submit sent DESKTOP:<hash>, so
the approval row (the PDF gate key) never matched the submission row.
"""

from __future__ import annotations

import os
import unittest
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan.models import Exam
from examvan.ui.waiting_approval import WaitingApprovalDialog


def _app() -> QApplication:
    app = QApplication.instance()
    if app is None:
        app = QApplication([])
    return app


class WaitingApprovalIdentityTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.app = _app()

    def test_mac_address_is_device_label(self):
        exam = Exam(id=1, name="Ujian Test", status="active")
        with mock.patch(
            "examvan.ui.waiting_approval.get_device_label",
            return_value="DESKTOP:hash123456",
        ), mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            return_value=mock.Mock(success=True, status="pending"),
        ):
            dlg = WaitingApprovalDialog(
                exam, "https://x", {"nama": "Budi"}, token="ABCD1234"
            )
            try:
                self.assertEqual(dlg.mac_address, "DESKTOP:hash123456")
            finally:
                dlg.is_waiting = False
                dlg.reject()

    def test_poll_sends_device_label_as_mac(self):
        exam = Exam(id=1, name="Ujian Test", status="active")
        with mock.patch(
            "examvan.ui.waiting_approval.get_device_label",
            return_value="DESKTOP:hash123456",
        ), mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            return_value=mock.Mock(success=True, status="approved"),
        ) as ra:
            dlg = WaitingApprovalDialog(
                exam, "https://x", {"nama": "Budi"}, token="ABCD1234"
            )
            try:
                # Give the poll thread a moment to fire once.
                import time
                deadline = time.time() + 3
                while time.time() < deadline and ra.call_count == 0:
                    self.app.processEvents()
                    time.sleep(0.05)
                self.assertGreaterEqual(ra.call_count, 1)
                # request_approval(base_url, exam_id, name, number, sclass,
                #                  identity_data, mac_address, reset=..., token=...)
                # — mac_address adalah arg posisional ke-7.
                args = ra.call_args.args
                self.assertEqual(args[6], "DESKTOP:hash123456")
                # Token diteruskan sebagai keyword (anti-spam server).
                self.assertEqual(ra.call_args.kwargs.get("token"), "ABCD1234")
            finally:
                dlg.is_waiting = False
                dlg.reject()


if __name__ == "__main__":
    unittest.main()
