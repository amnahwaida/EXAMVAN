"""Unit tests for examvan.ui.waiting_approval — device identity consistency.

Covers the desktop↔Android consistency fix (Agustus 2026): request-approval
must use the SAME device identity (DESKTOP:<hash>) as PDF download and
submit. Previously it sent the raw MAC while submit sent DESKTOP:<hash>, so
the approval row (the PDF gate key) never matched the submission row.
"""

from __future__ import annotations

import os
import threading
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
        # Gate: tahan poll thread DAEMON di dalam request_approval sampai
        # assertion selesai, lalu lepaskan SETELAH is_waiting=False. Tanpa
        # gate, thread bisa berada tepat di antara check `while is_waiting`
        # dan pemanggilan API saat mock di-stop → panggilan ke request_approval
        # NYATA (network call di unit test) — pola race yang sama dengan
        # test_auto_submit (assertion selesai tapi thread belum mati).
        # time.sleep di-no-op-kan agar thread keluar cepat setelah release.
        entered = threading.Event()
        release = threading.Event()

        def _gated(*args, **kwargs):
            entered.set()
            release.wait(timeout=5)
            return mock.Mock(success=True, status="pending")

        with mock.patch(
            "examvan.ui.waiting_approval.get_device_label",
            return_value="DESKTOP:hash123456",
        ), mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=_gated,
        ), mock.patch("examvan.ui.waiting_approval.time.sleep"):
            dlg = WaitingApprovalDialog(
                exam, "https://x", {"nama": "Budi"}, token="ABCD1234"
            )
            try:
                # Poll thread masuk ke mock call → diblokir (deterministik).
                self.assertTrue(entered.wait(3))
                self.assertEqual(dlg.mac_address, "DESKTOP:hash123456")
            finally:
                dlg.is_waiting = False
                dlg.reject()
                # Thread keluar: setelah release, cek is_waiting → break.
                release.set()

    def test_poll_sends_device_label_as_mac(self):
        exam = Exam(id=1, name="Ujian Test", status="active")
        entered = threading.Event()
        release = threading.Event()

        def _gated(*args, **kwargs):
            entered.set()
            release.wait(timeout=5)
            return mock.Mock(success=True, status="approved")

        with mock.patch(
            "examvan.ui.waiting_approval.get_device_label",
            return_value="DESKTOP:hash123456",
        ), mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=_gated,
        ) as ra, mock.patch("examvan.ui.waiting_approval.time.sleep"):
            dlg = WaitingApprovalDialog(
                exam, "https://x", {"nama": "Budi"}, token="ABCD1234"
            )
            try:
                # Thread masuk ke mock call → args sudah tercatat, thread
                # diblokir → assert deterministik (tidak ada call ke-2 yang
                # bisa menimpa call_args / bocor setelah mock di-stop).
                self.assertTrue(entered.wait(3))
                self.assertEqual(ra.call_count, 1)
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
                # Approved → thread break & selesai (is_waiting sudah False).
                release.set()


if __name__ == "__main__":
    unittest.main()
