"""Unit tests for examvan.ws — SocketIO message parsing.

Covers the desktop↔Android feature parity work (Agustus 2026): the desktop
client now listens for real-time exam events over WebSocket (exam_terminated
→ auto-submit), mirroring Android's WebSocketManager.
"""

from __future__ import annotations

import unittest

from examvan.ws import parse_socketio_message


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


if __name__ == "__main__":
    unittest.main()
