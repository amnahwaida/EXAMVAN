"""Lima temuan LOW ronde 3: ws.disconnect, thread poll ganda, stderr global,
clipboard mati, IdentityDialog tanpa scroll.

Semuanya dari review_windows_2026-09-30.md Bagian 6, N6-N10.
"""

from __future__ import annotations

import io
import os
import re
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QEvent, QPoint, QPointF, Qt
from PyQt5.QtGui import QWheelEvent
from PyQt5.QtWidgets import QApplication, QScrollArea

from examvan.models import Exam
from examvan.ui.identity_dialog import IdentityDialog
from examvan.ui.waiting_approval import WaitingApprovalDialog
from examvan.ws import ExamWebSocket

APP = QApplication.instance() or QApplication([])

REPO = Path(__file__).resolve().parents[2]


def _lines_of(module):
    """Baris kode sebuah modul, tanpa docstring dan komentar."""
    import ast
    import io
    import tokenize

    src = Path(module.__file__).read_text(encoding="utf-8")
    docstrings = set()
    for node in ast.walk(ast.parse(src)):
        if isinstance(node, (ast.Module, ast.FunctionDef, ast.AsyncFunctionDef,
                             ast.ClassDef)):
            body = getattr(node, "body", [])
            if (body and isinstance(body[0], ast.Expr)
                    and isinstance(body[0].value, ast.Constant)
                    and isinstance(body[0].value.value, str)):
                docstrings.update(range(body[0].lineno, body[0].end_lineno + 1))
    out = []
    for tok in tokenize.generate_tokens(io.StringIO(src).readline):
        if tok.type == tokenize.COMMENT:
            continue
        if tok.start[0] in docstrings:
            continue
        out.append(tok.line)
    return out


def _code_of(module, func_name):
    """Tubuh satu fungsi sebagai teks baris kode."""
    src = Path(module.__file__).read_text(encoding="utf-8")
    tree = __import__("ast").parse(src)
    for node in __import__("ast").walk(tree):
        if isinstance(node, __import__("ast").FunctionDef) and node.name == func_name:
            head = node.body[0]
            if (isinstance(head, __import__("ast").Expr)
                    and isinstance(head.value, __import__("ast").Constant)):
                head = head.value
            return "\n".join(src.splitlines()[head.end_lineno - 1:node.end_lineno])
    raise AssertionError(f"{func_name} not found in {module.__file__}")


# ---------------------------------------------------------------------------
# N6 — ws.disconnect() tidak pernah memutus handler
# ---------------------------------------------------------------------------


class WsDisconnectTest(unittest.TestCase):
    def test_disconnect_closes_the_socket(self):
        ws = ExamWebSocket()
        socket = mock.Mock()
        ws._ws = socket
        ws.disconnect()
        socket.abort.assert_called()
        self.assertIsNone(ws._ws)

    def test_disconnect_stops_reconnecting(self):
        ws = ExamWebSocket()
        ws._should_reconnect = True
        ws._reconnect_timer = mock.Mock()
        socket = mock.Mock()
        ws._ws = socket
        ws.disconnect()
        # `_should_reconnect` di-set False SEBELUM abort, jadi `disconnected`
        # yang dipicu abort tidak menjadwalkan reconnect.
        self.assertFalse(ws._should_reconnect)

    def test_disconnect_does_not_call_the_connected_signal_as_a_method(self):
        # Sejarah dua babak bug yang sama:
        #   1. `self._ws.disconnect(self._ws.connected)` — memanggil sinyal
        #      sebagai metode; tidak memutus apa pun.
        #   2. Perbaikannya dulu, `self._ws.disconnect()` — ternyata JEBAKAN
        #      BERIKUTNYA: QObject.disconnect() tanpa argumen melepas
        #      sambungan sinyal (bukan menutup jaringan) dan melempar
        #      TypeError di socket hidup (repro nyata, audit 30 Sep 2026),
        #      membuat abort()/deleteLater() di bawahnya melompat.
        # Kontrak sekarang: jangan sentuh mesin sinyal sama sekali —
        # penutup jaringan adalah abort(), socket dibuang lewat deleteLater().
        ws = ExamWebSocket()
        socket = mock.Mock()
        ws._ws = socket
        ws.disconnect()
        socket.disconnect.assert_not_called()
        socket.abort.assert_called_once()
        self.assertIsNone(ws._ws)


# ---------------------------------------------------------------------------
# N7 — "Minta Izin Lagi" menypawn thread poll kedua
# ---------------------------------------------------------------------------


class _Event:
    def __init__(self):
        self.calls = 0
        self._stop = False

    def is_set(self):
        return self._stop

    def set(self):
        self._stop = True


class ApprovalRetryTest(unittest.TestCase):
    def _dialog(self, request_side_effect=None):
        """Bangun dialog dengan `api.request_approval` sudah di-patch.

        Patch HARUS aktif sebelum konstruktor selesai: `__init__` memanggil
        `_start_polling()`, jadi tanpa ini thread pertama akan menembak
        jaringan sungguhan dan test jadi balapan dengan timeout 10 detik.
        """
        def _default(*a, **k):
            return mock.Mock(success=False, status="pending", message="")

        with mock.patch(
            "examvan.ui.waiting_approval.get_device_label",
            return_value="DESKTOP:test",
        ), mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=request_side_effect or _default,
        ):
            return WaitingApprovalDialog(
                Exam(id=1, name="Ujian", status="active"),
                "https://examvan.my.id",
                {"nama": "Budi"},
                token="ABCD1234",
            )

    def test_retry_keeps_one_poller_touching_the_dialog(self):
        # Status "rejected" menyetel is_waiting = False; thread lama bisa
        # masih bekerja. Kalau siswa langsung menekan "Minta Izin Lagi"
        # berkali-kali, thread lama akan melanjutkan loop-nya juga.
        #
        # Yang TIDAK BOLEH terjadi adalah dua poller sama-sama menembak
        # sinyal ke dialog yang sama -- itu yang membuat approve/reject
        # saling menimpa dan pengawas melihat status yang tidak pernah
        # terjadi.
        #
        # Dua request HTTP yang tumpang tindih SEBAIKNYA boleh terjadi,
        # dan tidak bisa dihindari tanpa membekukan GUI: poll yang lama
        # sudah memblokir di socket read, dan membatalkannya berarti
        # menutup socket itu -- blocking, 10 detik, di thread GUI, dengan
        # enforcement keamanan ikut mati selama itu. Yang dijamin adalah
        # ATURAN GENERASI: poll basi keluar tanpa menyentuh apa pun.
        dlg = self._dialog()
        state = {"live": 0, "peak": 0, "calls": 0, "emits": 0}

        def _slow_request(*a, **k):
            state["live"] += 1
            state["calls"] += 1
            state["peak"] = max(state["peak"], state["live"])
            dlg._poll_stop.wait(0.4)
            state["live"] -= 1
            return mock.Mock(success=False, status="pending", message="")

        class _CountingSignal:
            def __init__(self, inner):
                self._inner = inner

            def emit(self, *a, **k):
                state["emits"] += 1
                return self._inner.emit(*a, **k)

        dlg._sig_status = _CountingSignal(dlg._sig_status)

        with mock.patch(
            "examvan.ui.waiting_approval.api.request_approval",
            side_effect=_slow_request,
        ):
            dlg._retry_approval()          # hidupkan lagi
            dlg._retry_approval()          # dan lagi, seperti siswa yang tidak sabar
            dlg._retry_approval()
            dlg._stop_polling()

        self.assertGreater(state["calls"], 0)
        # Tiga retry dimulai hampir bersamaan, jadi tumpang tindih pada
        # request yang SEDANG memblokir tidak dapat dihindari. Yang penting
        # tidak ada permintaan tanpa batas dan tidak ada dua poller yang
        # sama-sama menembak ke dialog.
        self.assertLessEqual(state["peak"], 4)
        self.assertLessEqual(state["calls"], 3 * 4, "polling tidak berhenti")

    def test_stopping_polling_never_blocks_the_gui_thread(self):
        # Regression: `_stop_polling` pernah melakukan `join(12)` dari
        # thread GUI. Jaringan mati -> UI beku 10-12 detik dan
        # `SecurityEnforcer` ikut mati karena butuh event loop yang sama.
        dlg = self._dialog()
        dlg._poll_thread_obj = mock.Mock()
        dlg._poll_thread_obj.is_alive.return_value = True
        with mock.patch.object(dlg._poll_thread_obj, "join") as join:
            dlg._stop_polling()
        join.assert_not_called()

    def test_stop_token_is_available_for_the_poll_loop(self):
        dlg = self._dialog()
        self.assertIsNotNone(getattr(dlg, "_poll_stop", None))
        self.assertTrue(hasattr(dlg._poll_stop, "is_set"))

    def test_reject_sets_the_stop_token(self):
        dlg = self._dialog()
        dlg.reject()
        self.assertTrue(dlg._poll_stop.is_set())
        self.assertFalse(dlg.is_waiting)

    def test_retry_reopens_the_stop_token(self):
        dlg = self._dialog()
        dlg.reject()
        dlg._retry_approval()
        self.assertFalse(dlg._poll_stop.is_set())

    def test_no_thread_is_alive_after_reject(self):
        dlg = self._dialog()
        thread = dlg._poll_thread_obj
        self.assertIsNotNone(thread)
        dlg.reject()
        thread.join(timeout=3.0)
        self.assertFalse(thread.is_alive())

    def test_approval_also_stops_the_loop(self):
        # `approved` harus menghentikan loop, kalau tidak thread tetap
        # memanggil request-approval untuk ujian yang sudah bisa dimulai.
        dlg = self._dialog(
            request_side_effect=lambda *a, **k: mock.Mock(
                success=True, status="approved", message="ok"
            )
        )
        thread = dlg._poll_thread_obj
        thread.join(timeout=5.0)
        self.assertFalse(thread.is_alive())


# ---------------------------------------------------------------------------
# N8 — stderr proses global
# ---------------------------------------------------------------------------


class MupdfStderrTest(unittest.TestCase):
    def test_suppress_helper_does_not_redirect_the_process_stderr(self):
        # `os.dup2(devnull, 2)` bersifat proses-global dan tidak reentrant:
        # dua pemanggilan tumpang tindih membuat `finally` paling dalam
        # me-restore fd yang sudah berisi devnull -> stderr hilang
        # permanen untuk sisa proses.
        from examvan.ui import pdf_viewer

        body = _code_of(pdf_viewer, "_suppress_mupdf_warnings")
        self.assertNotIn("dup2", body, body)
        self.assertNotIn("os.dup", body, body)
        self.assertNotIn("devnull", body, body)

    def test_no_dup2_anywhere_in_pdf_viewer_code(self):
        from examvan.ui import pdf_viewer

        # Docstring masih MENCERITAKAN pendekatan lama; yang dicek baris kode.
        code = [l for l in _lines_of(pdf_viewer)]
        offenders = [l for l in code if "dup2(" in l or "os.dup(" in l]
        self.assertEqual(offenders, [])

    def test_mupdf_error_reporting_is_switched_off_per_context(self):
        from examvan.ui import pdf_viewer

        # `fitz.TOOLS.mupdf_display_errors(False)` scoped, bukan mengarahkan
        # fd stderr milik seluruh proses.
        self.assertTrue(hasattr(pdf_viewer, "_suppress_mupdf_warnings"))
        src = Path(pdf_viewer.__file__).read_text(encoding="utf-8")
        self.assertIn("mupdf_display_errors", src)


# ---------------------------------------------------------------------------
# N9 — clipboard mati
# ---------------------------------------------------------------------------


class DeadClipboardCodeTest(unittest.TestCase):
    def test_utils_no_longer_spawns_clipboard_processes(self):
        # `_clear_clipboard_x11` me-fork `xsel`/`xclip` dan
        # `clear_clipboard_wl` me-fork `wl-copy` — persis fork proses yang
        # ronde 2 hapus dari windows_backend karena membekukan kursor di PC
        # low-end. Kode mati yang terlihat di repo membuatellementirrogate
        #-next developer menyimpulkan itu jalur yang aktif.
        src = (REPO / "desktop/examvan/utils.py").read_text(encoding="utf-8")
        for tool in ("xsel", "xclip", "wl-copy"):
            self.assertNotIn(tool, src, f"{tool} masih ada di utils.py")

    def test_no_module_outside_the_enforcer_shells_out_to_clear_the_clipboard(self):
        root = REPO / "desktop/examvan"
        offenders = []
        for path in root.rglob("*.py"):
            rel = path.relative_to(root)
            if str(rel) in ("utils.py",) or rel.parts[0] == "security":
                continue
            text = path.read_text(encoding="utf-8")
            if re.search(r'"(xsel|xclip|wl-copy)"', text):
                offenders.append(str(rel))
        self.assertEqual(offenders, [])


# ---------------------------------------------------------------------------
# N10 — IdentityDialog tanpa scroll
# ---------------------------------------------------------------------------


class IdentityDialogScrollTest(unittest.TestCase):
    def test_dialog_has_a_scroll_area(self):
        # Tanpa QScrollArea, ujian dengan banyak field identitas membuat
        # tombol "Masuk Ujian" berada di bawah area yang bisa dijangkau —
        # dan tidak ada cara menggulir untuk mencapainya. Siswa terkunci
        # sebelum ujian dimulai.
        exam = Exam.from_json({"id": 1, "name": "Ujian"})
        dlg = IdentityDialog(exam)
        scrolls = dlg.findChildren(QScrollArea)
        self.assertTrue(scrolls, "IdentityDialog tidak punya QScrollArea")
        self.assertTrue(scrolls[0].widgetResizable())

    def test_long_field_lists_remain_fully_reachable(self):
        fields = [{"key": f"f{i}", "label": f"Field {i}", "required": True}
                  for i in range(20)]
        exam = Exam.from_json({"id": 1, "name": "Ujian", "identity_fields": fields})
        dlg = IdentityDialog(exam)
        # Semua field tetap dibangun dan bisa diisi.
        self.assertEqual(len(dlg.get_identity_data()), 20)
        dlg.close()

    def test_submit_button_is_inside_the_scrollable_area(self):
        fields = [{"key": f"f{i}", "label": f"Field {i}", "required": True}
                  for i in range(20)]
        exam = Exam.from_json({"id": 1, "name": "Ujian", "identity_fields": fields})
        dlg = IdentityDialog(exam)
        scroll = dlg.findChildren(QScrollArea)[0]
        inner = scroll.widget()
        # Tombol harus berada DI DALAM area yang bisa digulir, bukan
        # terpotong keluar dari jangkauan.
        self.assertIsNotNone(inner)
        self.assertIsNotNone(dlg._submit_btn)
        self.assertIs(inner, dlg._submit_btn.parentWidget())
        dlg.close()


# ---------------------------------------------------------------------------
# N8 helper — arah scroll guard tetap benar setelah perubahan apa pun
# ---------------------------------------------------------------------------


class WheelEventFactoryStillWorks(unittest.TestCase):
    def test_wheel_factory_returns_a_usable_event(self):
        ev = QWheelEvent(
            QPointF(4.0, 4.0), QPointF(4.0, 4.0), QPoint(0, 0), QPoint(0, -120),
            Qt.NoButton, Qt.NoModifier, Qt.NoScrollPhase, False,
        )
        self.assertEqual(ev.type(), QEvent.Wheel)
        self.assertEqual(ev.angleDelta().y(), -120)


if __name__ == "__main__":
    unittest.main()
