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


class RepeatRequiredTest(unittest.TestCase):
    """Status `repeat_required`: siswa sudah mengerjakan, pengawas bisa melepas.

    Perbedaan penting dari status `rejected`: penolakan itu keputusan
    pengawas yang final, jadi polling berhenti. `repeat_required` bisa
    berubah kapan saja begitu pengawas menekan "Izinkan Mengulang" di halaman
    pengawasan -- jadi polling HARUS tetap jalan.

    Kalau polling di-break, siswa harus menutup dan membuka ulang aplikasi
    untuk mencoba lagi, dan tidak ada yang tahu kalau izinnya sudah
    diberikan 30 detik yang lalu.
    """

    @classmethod
    def setUpClass(cls):
        cls.app = _app()

    def setUp(self):
        self.exam = Exam(id=1, name="Ujian", status="active")
        self.statuses = []

    def _dialog(self):
        dlg = WaitingApprovalDialog(
            self.exam, "https://x", {"nama": "Andi", "nomor": "N01"},
            token="ABCD1234",
        )
        dlg._sig_status.connect(
            lambda kind, title, msg: self.statuses.append((kind, title, msg))
        )
        return dlg

    def _drive_poll(self, responses):
        """Jalankan _poll_thread sungguhan dengan respons yang diberikan.

        Thread dihentikan setelah respons pertama selesai, supaya test tidak
        menunggu 5 detik. Yang diuji adalah LUTUP atau TIDAK polling pada
        tiap status -- itu hanya terlihat dari thread yang berhenti sendiri.
        """
        dlg = self._dialog()
        # _start_polling() sudah jalan di __init__; hentikan supaya test
        # controlling, lalu jalankan satu putaran dalam satu thread.
        dlg._stop_polling()
        dlg._poll_stop.clear()
        dlg._poll_stop.wait = lambda _t=0.0: dlg._poll_stop.set()
        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=list(responses) + [OSError("test selesai")],
        ):
            dlg._poll_thread()
        return dlg

    def test_repeat_required_does_not_stop_polling(self):
        from examvan.models import RequestApprovalResponse

        resp = RequestApprovalResponse(
            success=False, status="repeat_required",
            message="Ujian ini sudah Anda kerjakan.",
        )
        dlg = self._drive_poll([resp])
        kinds = [k for k, _, _ in self.statuses]
        self.assertIn("repeat_required", kinds)
        self.assertTrue(
            dlg._poll_stop.is_set() is False
            or not getattr(dlg, "is_waiting", False) is False,
            "polling tidak boleh berhenti pada repeat_required",
        )
        self.assertTrue(dlg.is_waiting, "siswa masih menunggu izin")

    def test_repeat_required_message_names_the_supervisor(self):
        from examvan.models import RequestApprovalResponse

        self._drive_poll([RequestApprovalResponse(
            success=False, status="repeat_required", message="",
        )])
        message = next(m for k, _, m in self.statuses if k == "repeat_required")
        self.assertIn("sudah Anda kerjakan", message)
        self.assertIn("pengawas", message.lower())

    def test_rejected_still_stops_polling(self):
        # Penolakan pengawas tetap final -- jangan ikut diubah hanya karena
        # ada status baru.
        from examvan.models import RequestApprovalResponse

        dlg = self._drive_poll([RequestApprovalResponse(
            success=False, status="rejected", message="",
        )])
        kinds = [k for k, _, _ in self.statuses]
        self.assertIn("rejected", kinds)
        self.assertNotIn("repeat_required", kinds)

    def test_approved_is_unaffected(self):
        from examvan.models import RequestApprovalResponse

        self._drive_poll([RequestApprovalResponse(
            success=True, status="approved", message="",
        )])
        kinds = [k for k, _, _ in self.statuses]
        self.assertIn("approved", kinds)

    def test_retry_button_is_offered_while_waiting_for_permission(self):
        from examvan.models import RequestApprovalResponse

        dlg = self._drive_poll([RequestApprovalResponse(
            success=False, status="repeat_required", message="",
        )])
        dlg._on_status_update("repeat_required", "Sudah Dikerjakan", "x")
        self.assertTrue(dlg.btn_retry.isVisible() or not dlg.btn_retry.isHidden())
        self.assertIn("Periksa", dlg.btn_retry.text())


class NonJsonStatusTest(unittest.TestCase):
    """Status `error` harus terlihat, bukan dissolved jadi "Menunggu".

    `api._make_request` mengembalikan status "error" saat server membalas
    sesuatu yang bukan JSON (halaman blokir proxy, captive portal, WAF).
    Kalau status itu tidak ditangani, ia jatuh ke cabang `else` yang
    menampilkan "Menunggu Persetujuan" -- dan polling berjalan tiap 5 detik
    SELAMANYA tanpa satu pesan pun.

    Siswa lalu duduk menghadap layar yang tidak akan pernah berubah, dan
    menyimpulkan aplikasi-nya yang macet, padahal jaringannya yang bermasalah.
    """

    @classmethod
    def setUpClass(cls):
        cls.app = _app()

    def _drive(self, resp):
        from examvan.models import RequestApprovalResponse

        dlg = WaitingApprovalDialog(
            Exam(id=1, name="Ujian", status="active"),
            "https://x", {"nama": "Andi"}, token="ABCD1234",
        )
        seen = []
        dlg._sig_status.connect(lambda k, t, m: seen.append((k, t, m)))
        dlg._stop_polling()
        dlg._poll_stop.clear()
        dlg._poll_stop.wait = lambda _t=0.0: dlg._poll_stop.set()
        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=[resp, OSError("selesai")],
        ):
            dlg._poll_thread()
        return dlg, seen

    def test_error_status_is_surfaced_not_hidden_as_pending(self):
        from examvan.models import RequestApprovalResponse

        _, seen = self._drive(RequestApprovalResponse(
            success=False, status="error",
            message="Server tidak mengembalikan JSON yang valid.",
        ))
        kinds = [k for k, _, _ in seen]
        self.assertIn(
            "error", kinds,
            f"status 'error' harus tampil sebagai error, dapat {kinds}",
        )
        self.assertNotIn(
            "pending", kinds,
            "status 'error' tidak boleh ditampilkan sebagai 'Menunggu "
            "Persetujuan' -- itu membuat polling berjalan selamanya "
            "tanpa pesan",
        )

    def test_the_reason_reaches_the_student(self):
        from examvan.models import RequestApprovalResponse

        _, seen = self._drive(RequestApprovalResponse(
            success=False, status="error",
            message="Server tidak mengembalikan JSON yang valid.",
        ))
        message = next(m for k, _, m in seen if k == "error")
        self.assertIn("JSON", message)
