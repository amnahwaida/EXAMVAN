"""Unit tests for desktop presence + server time skew.

Covers the desktop↔Android feature parity work (Agustus 2026):
- POST /access-log (login/heartbeat/logout) — siswa tampil ONLINE di
  dashboard monitoring pengawas (mirror Android WebSocketManager heartbeat);
- POST /complete — presence Redis dihapus saat ujian selesai (tampil OFFLINE);
- server time skew dari GET /api/health dipakai countdown deadline agar
  akurat walau jam perangkat meleset (mirror ApiClient.serverTimeSkewMs).
"""

from __future__ import annotations

import time
import unittest
from unittest import mock

from examvan import api


class ServerTimeSkewTest(unittest.TestCase):
    def test_skew_computed_from_server_time(self):
        # Server mengklaim 07:00:00Z; perangkat pukul 06:59:00Z → server 60s
        # di depan → skew = +60000 ms.
        device_now_ms = 1786863540000  # 2026-08-16T06:59:00Z
        skew = api.compute_server_skew_ms("2026-08-16T07:00:00Z", device_now_ms)
        self.assertEqual(skew, 60000)

    def test_skew_zero_when_synced(self):
        skew = api.compute_server_skew_ms("2026-08-16T07:00:00Z", 1786863600000)
        self.assertEqual(skew, 0)

    def test_skew_none_when_unparseable(self):
        self.assertIsNone(api.compute_server_skew_ms(None, 0))
        self.assertIsNone(api.compute_server_skew_ms("garbage", 0))

    def test_check_health_stores_skew(self):
        body = {
            "success": True,
            "status": "healthy",
            "server_time_utc": "2026-08-16T07:00:00Z",
        }
        api.set_server_skew_ms(0)
        with mock.patch.object(api, "_make_request", return_value=body):
            with mock.patch("time.time", return_value=1786863540.0):
                resp = api.check_health("https://x")
        self.assertTrue(resp.success)
        self.assertEqual(api.get_server_skew_ms(), 60000)

    def test_health_without_server_time_keeps_skew(self):
        api.set_server_skew_ms(123)
        with mock.patch.object(api, "_make_request", return_value={"success": True}):
            api.check_health("https://x")
        self.assertEqual(api.get_server_skew_ms(), 123)


class AccessLogTest(unittest.TestCase):
    def _call(self, event, **kwargs):
        with mock.patch.object(api, "_make_request", return_value={"success": True}) as mk:
            ok = api.send_access_log(
                base_url="https://x", exam_id=7, token="ABCD1234",
                mac_address="DESKTOP:hash", event=event, **kwargs,
            )
        return ok, mk

    def test_login_sends_event_and_identity(self):
        ok, mk = self._call("login", student_name="Budi", exam_number="N01", student_class="9A")
        self.assertTrue(ok)
        url, kwargs = mk.call_args.args, mk.call_args.kwargs
        self.assertIn("/api/exams/7/access-log", url[0])
        body = kwargs["body"]
        self.assertEqual(body["event"], "login")
        self.assertEqual(body["mac_address"], "DESKTOP:hash")
        self.assertEqual(body["student_name"], "Budi")
        # X-Exam-Token dikirim (server mewajibkan token / approval row).
        self.assertEqual(kwargs["headers"]["X-Exam-Token"], "ABCD1234")

    def test_heartbeat_defaults_event(self):
        ok, mk = self._call("heartbeat")
        self.assertTrue(ok)
        self.assertEqual(mk.call_args.kwargs["body"]["event"], "heartbeat")

    def test_failure_returns_false(self):
        with mock.patch.object(api, "_make_request", side_effect=OSError("net")):
            self.assertFalse(api.send_access_log("https://x", 7, "T", "M", "login"))


class CompleteExamTest(unittest.TestCase):
    def test_complete_sends_mac_and_token(self):
        with mock.patch.object(api, "_make_request", return_value={"success": True}) as mk:
            ok = api.complete_exam("https://x", 7, "ABCD1234", "DESKTOP:hash")
        self.assertTrue(ok)
        body = mk.call_args.kwargs["body"]
        self.assertEqual(body["mac_address"], "DESKTOP:hash")
        self.assertEqual(body["token"], "ABCD1234")
        self.assertIn("/api/exams/7/complete", mk.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
