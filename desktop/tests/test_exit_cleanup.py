"""Keluar dari app tidak boleh membekukan GUI atau meninggalkan jejak (#11, #12).

Bug #11 — `_stop_polling()` membekukan GUI thread sampai 12 detik
-----------------------------------------------------------------
`_POLL_JOIN_TIMEOUT` sengaja dinaikkan ke 12 s karena satu panggilan
`api.request_approval` bisa memblokir 10 s (HTTP timeout), jadi join 2 s
selalu gagal dan poller lama tetap hidup. Tapi join itu dijalankan
LANGSUNG di thread GUI:

    reject()  -> _stop_polling()  -> thread.join(12)
    closeEvent -> _stop_polling() -> thread.join(12)
    "Minta Izin Lagi" -> _stop_polling() -> thread.join(12)

Ketika jaringan mati, seluruh Qt event loop berhenti: tidak ada repaint,
tidak ada input, dan karena `SecurityEnforcer` juga digerakkan oleh timer
yang sama, tidak ada enforcement keamanan sama sekali selama 10-12 detik.
`reject()` hanya mencapai `super()` SESUDAH join, jadi dialog benar-benar
terlihat membeku.

Join-nya justru tidak perlu. `_poll_stop.set()` sudah ada, dan
generation counter di `_poll_thread` membuat poller yang keluar terlambat
langsung `return` tanpa menyentuh widget.

Bug #12 — presence dan PDF tertinggal pada jalur keluar non-submit
------------------------------------------------------------------
Semua jalur keluar lain memanggil `self._stop_presence(...)` dan menghapus
`self._pdf_path`. Dua jalur ini tidak:

  * `_admin_exit_prompt` → password supervisor diterima (exit dari sisi
    pengawas pada ujian strict);
  * `closeEvent` low-mode → siswa menekan "Keluar Ujian → Ya".

Akibatnya `_heartbeat_timer` (60 s) tetap POST `access-log heartbeat`, jadi
dashboard pengawas menampilkan siswa masih ONLINE setelah mereka resmi
dikeluarkan, dan `ExamWebSocket.disconnect()` tidak pernah dipanggil
sehingga socket auto-reconnect lagi. Berkas PDF juga tetap di `%TEMP%`
untuk pengguna berikutnya mesin itu — sedangkan lockdown sudah dilepas,
jadi tidak ada lagi yang membersihkannya.
"""

from __future__ import annotations

import inspect
import os
import tempfile
import types
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

from examvan import models
from examvan.models import Exam
from examvan.ui import waiting_approval as wa
from examvan.ui.exam_viewer import ExamViewerWindow


# ---------------------------------------------------------------------------
# #11 — stop polling tidak boleh memblokir GUI
# ---------------------------------------------------------------------------


class StopPollingDoesNotBlockGuiTestCase(unittest.TestCase):
    """`_stop_polling()` hanya memberi sinyal -- tidak pernah menunggu."""

    @staticmethod
    def _stub(**attrs):
        """Stub minimal: `_stop_polling` hanya menyentuh dua atribut ini.

        Dialog sungguhan tidak dipakai karena `_stop_polling` sengaja
        tidak menyentuh apa pun milik Qt -- justru itu inti perbaikannya.
        """
        stub = types.SimpleNamespace(
            _poll_stop=mock.Mock(),
            _poll_generation=7,
            _poll_thread_obj=mock.Mock(),
        )
        for key, value in attrs.items():
            setattr(stub, key, value)
        return stub

    def test_stop_polling_does_not_join_the_thread(self):
        joined = []

        class _Busy:
            def is_alive(self):
                return True

            def join(self, timeout=None):
                joined.append(timeout)

        stub = self._stub(_poll_thread_obj=_Busy())
        wa.WaitingApprovalDialog._stop_polling(stub)
        self.assertEqual(
            joined, [],
            "GUI thread di-join dengan thread yang sedang blocking di "
            "request HTTP; event loop Qt berhenti sampai panggilan selesai",
        )

    def test_stop_polling_only_signals_and_bumps_the_generation(self):
        stub = self._stub()
        wa.WaitingApprovalDialog._stop_polling(stub)
        stub._poll_stop.set.assert_called_once()
        self.assertEqual(
            stub._poll_generation, 8,
            "generasi harus naik supaya poller yang telat keluar tidak "
            "bisa menyentuh dialog yang sudah ditutup",
        )

    def test_stop_polling_takes_no_timeout_argument_anymore(self):
        # Kalau ada parameter `timeout`, pemanggil lain masih bisa
        # menyuruhnya menunggu -- dan bug beku GUI itu kembali.
        import inspect as _inspect

        params = list(
            _inspect.signature(wa.WaitingApprovalDialog._stop_polling).parameters
        )
        self.assertEqual(
            params, ["self"],
            f"_stop_polling masih menerima {params}; join bisa dipanggil "
            "lagi dan membekukan GUI",
        )

    def test_a_stale_poller_exits_without_touching_the_dialog(self):
        # Menghapus join TIDAK boleh meninggalkan poller yang punya akses
        # ke dialog yang sudah ditutup: generasinya yang jadi jaring
        # pengaman, jadi harus benar-benar bekerja.
        stub = self._stub(
            _poll_generation=3,
            _sig_status=mock.Mock(),
            _sig_approval=mock.Mock(),
        )
        with mock.patch.object(wa.api, "request_approval") as request:
            wa.WaitingApprovalDialog._poll_thread(stub, generation=2)
        request.assert_not_called()
        stub._sig_status.emit.assert_not_called()
        stub._sig_approval.emit.assert_not_called()

    def test_the_generation_check_compares_against_the_current_generation(self):
        # Jebakan yang harus dicegah: kalau pengecekan ini hilang atau
        # salah, SEMUA poller berhenti -- persis seperti kalau_
        #`_poll_stop.is_set()` dibalik.
        src = inspect.getsource(wa.WaitingApprovalDialog._poll_thread)
        self.assertIn("generation != self._poll_generation", src)
        self.assertIn("while not self._poll_stop.is_set():", src)


# ---------------------------------------------------------------------------
# #12 — setiap jalur keluar membersihkan presence dan PDF
# ---------------------------------------------------------------------------


def _viewer_stub(pdf_path=None, presence_calls=None, ws_calls=None):
    """ExamViewerWindow tanpa konstruktor, hanya state yang diuji."""
    win = ExamViewerWindow.__new__(ExamViewerWindow)
    win._exam = Exam(id=7, name="Ujian", status="active")
    win._token = "ABCD1234"
    win._identity_data = {"nama": "Budi"}
    win._submitted = False
    win._submitting = False
    win._close_in_progress = False
    win._submit_lock = __import__("threading").Lock()
    win._security = None
    win._heartbeat_timer = mock.Mock()
    win._sig_status = mock.Mock()
    win._sig_submit_result = mock.Mock()
    win._closed = False
    win._presence_calls = presence_calls if presence_calls is not None else []
    win._ws_calls = ws_calls if ws_calls is not None else []
    win._pdf_path = pdf_path
    win._pdf_viewer = mock.Mock()
    win._pdf_viewer.cleanup = mock.Mock()
    win._admin_exit_count = 0
    win._admin_exit_timer = mock.Mock()
    win.isVisible = lambda: True
    win.close = mock.Mock()
    win._closed = mock.Mock()
    # _modal_dialog_guard() menyentuh _timer_widget; stub-nya harus punya
    # _timer dan _fired_time_up.
    timer = mock.Mock()
    timer._timer = mock.Mock()
    timer._timer.isActive.return_value = True
    timer._fired_time_up = False
    win._timer_widget = timer

    def _stop_presence(completed=True):
        win._presence_calls.append(completed)

    win._stop_presence = _stop_presence
    return win


class EveryExitPathCleansUpTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.pdf = os.path.join(self.tmp.name, "exam.pdf")
        with open(self.pdf, "wb") as f:
            f.write(b"%PDF-1.4")

    def test_the_exit_paths_all_call_stop_presence(self):
        # Setiap jalan keluar dari ujian harus menghentikan presence.
        # Kalau ada yang melewatkan, siswa tetap terlihat ONLINE.
        src = inspect.getsource(ExamViewerWindow)
        for name in ("_admin_exit_prompt", "closeEvent"):
            body = self._method_body(src, name)
            self.assertIn(
                "_stop_presence", body,
                f"{name} tidak menghentikan presence: dashboard pengawas "
                "akan menampilkan siswa ONLINE setelah dikeluarkan",
            )

    def test_the_exit_paths_all_delete_the_pdf(self):
        src = inspect.getsource(ExamViewerWindow)
        for name in ("_admin_exit_prompt", "closeEvent"):
            body = self._method_body(src, name)
            self.assertTrue(
                "_discard_pdf" in body or "_pdf_path" in body,
                f"{name} tidak menghapus PDF; berkas soal utuh tertinggal "
                "di TEMP untuk pengguna berikutnya mesin",
            )

    @staticmethod
    def _method_body(src, name):
        head = f"    def {name}("
        start = src.index(head)
        rest = src[start + 1:]
        return rest[:rest.index("\n    def ") if "\n    def " in rest else len(rest)]

    def test_admin_exit_removes_the_pdf_and_stops_presence(self):
        win = _viewer_stub(pdf_path=self.pdf)
        with mock.patch("examvan.ui.exam_viewer._ADMIN_PASSWORD", "rahasia"), \
             mock.patch("PyQt5.QtWidgets.QInputDialog.getText",
                        return_value=("rahasia", True)):
            win._admin_exit_prompt()
        self.assertEqual(
            win._presence_calls, [False],
            "presence tidak dihentikan pada admin-exit",
        )
        self.assertFalse(
            os.path.exists(self.pdf),
            "PDF tidak dihapus pada admin-exit; kunci keamanan sudah "
            "dilepas jadi tidak ada yang membersihkannya nanti",
        )


if __name__ == "__main__":
    unittest.main()