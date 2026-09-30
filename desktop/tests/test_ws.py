"""Unit tests for examvan.ws — SocketIO message parsing + lifecycle.

Covers the desktop↔Android feature parity work (Agustus 2026): the desktop
client now listens for real-time exam events over WebSocket (exam_terminated
→ auto-submit), mirroring Android's WebSocketManager.

Kelas LifecycleRobustnessTestCase berasal dari audit pola exception
(30 Sep 2026): frame/event yang salah bentuk sudah disaring di
parse_socketio_message, tapi lifecycle-nya punya dua jebakan —
`QWebSocket.disconnect()` TANPA argumen adalah QObject.disconnect()
(melepas sambungan sinyal, bukan menutup jaringan; melempar TypeError
bila salah satu lepasan gagal — repro nyata di runner offscreen), dan
`connect(None, ...)` dari config yang bisa ditulis siswa.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtCore import QCoreApplication

from examvan.ws import ExamWebSocket, parse_socketio_message


class ParseSocketIOMessageTest(unittest.TestCase):
    def test_raw_json_array(self):
        parsed = parse_socketio_message('["exam_terminated",{"exam_id":7}]')
        self.assertEqual(parsed, ("exam_terminated", {"exam_id": 7}))

    def test_legacy_42_frame(self):
        parsed = parse_socketio_message('42["notification",{"msg":"halo"}]')
        self.assertEqual(parsed, ("notification", {"msg": "halo"}))

    def test_non_event_frames_return_none(self):
        self.assertIsNone(parse_socketio_message("2"))            # ping
        self.assertIsNone(parse_socketio_message("3"))            # pong
        self.assertIsNone(parse_socketio_message("0"))            # open
        self.assertIsNone(parse_socketio_message("40"))           # namespace connect
        self.assertIsNone(parse_socketio_message(""))             # empty

    def test_malformed_returns_none(self):
        self.assertIsNone(parse_socketio_message("not json"))
        self.assertIsNone(parse_socketio_message("[1,2,3]"))      # not (event, payload)
        self.assertIsNone(parse_socketio_message("[\"only_event\"]"))  # len < 2

    def test_payload_not_dict_returns_none(self):
        # Kontrak hub: payload selalu objek JSON — frame lain diabaikan.
        self.assertIsNone(parse_socketio_message('["exam_terminated","oops"]'))
        self.assertIsNone(parse_socketio_message('["exam_terminated",42]'))

    def test_exam_terminated_is_the_trigger_event(self):
        # Contract: pengawas menghentikan ujian → auto-submit. Parsing harus
        # mengenali persis nama event ini (dipakai ExamViewerWindow._on_ws_event).
        parsed = parse_socketio_message('["exam_terminated",{}]')
        self.assertIsNotNone(parsed)
        self.assertEqual(parsed[0], "exam_terminated")


class LifecycleRobustnessTestCase(unittest.TestCase):
    """disconnect()/connect() tidak boleh bocor atau melempar."""

    def setUp(self):
        if QCoreApplication.instance() is None:
            self._app = QCoreApplication([])
        else:
            self._app = QCoreApplication.instance()
        self.ws = ExamWebSocket()

    def test_disconnect_survives_a_failing_signal_disconnect(self):
        # RED (repro nyata): `self._ws.disconnect()` tanpa argumen adalah
        # QObject.disconnect() — melepas SEMUA sambungan sinyal dan melempar
        # TypeError bila salah satu gagal. abort()/deleteLater() di bawahnya
        # melompat → socket terbuka sampai proses mati.
        stub = mock.Mock()
        stub.disconnect.side_effect = TypeError(
            "disconnect() of all signals failed"
        )
        self.ws._ws = stub
        self.ws._should_reconnect = False

        self.ws.disconnect()  # TIDAK boleh melempar

        stub.abort.assert_called_once()
        stub.deleteLater.assert_called_once()
        self.assertIsNone(self.ws._ws, "socket yang gagal ditutup bocor")

    def test_disconnect_is_idempotent(self):
        self.ws.disconnect()
        self.ws.disconnect()  # kedua kali: _ws sudah None — jangan crash
        self.assertIsNone(self.ws._ws)

    def test_disconnect_does_not_touch_the_signal_machinery(self):
        # `disconnect()` yang benar memutus JARINGAN (abort), bukan sinyal:
        # handler sinyal dibiarkan terpasang — socket dibuang utuh lewat
        # deleteLater, jadi melepasnya manual hanya menambah jalur gagal.
        stub = mock.Mock()
        self.ws._ws = stub
        self.ws.disconnect()
        stub.disconnected.disconnect.assert_not_called()

    def test_connect_with_none_base_url_does_not_raise(self):
        # config.json milik akun siswa: "server_url": null lolos guard
        # `or ""` di beberapa pemanggil lama. connect() harus menolaknya
        # tanpa AttributeError di rstrip — dan TANPA memulai loop reconnect
        # ke URL kosong.
        self.ws.connect(None, 7, "TOK")
        self.assertIsNone(self.ws._ws)
        self.assertFalse(self.ws._reconnect_timer.isActive())

    def test_connect_coerces_non_string_token(self):
        # "exam_token": null di config — f-string tidak pernah crash, tapi
        # mengirim literal "None" sebagai kredensial ke server.
        self.ws.connect("https://srv.example", 7, None)
        self.assertEqual(self.ws._token, "")
        # Bersihkan: jangan biarkan timer reconnect hidup setelah test.
        self.ws.disconnect()


if __name__ == "__main__":
    unittest.main()
