"""Unit tests for examvan.api — queued submit parsing and /result polling.

Covers the desktop↔Android consistency fixes (Agustus 2026):
- a 202 queued response must NOT be treated as final success — the client
  polls /result until the worker confirms durability ("done");
- only a "done" status is a durable success; pending keeps polling, failure
  stops immediately, and a polling timeout returns a non-success so the
  local answer copy is preserved (mirror of Android QueuedResultPolling).
"""

from __future__ import annotations

import json
import unittest
from unittest import mock

from examvan import api
from examvan.models import SubmitResponse


class SubmitExamQueuedTest(unittest.TestCase):
    """202 queued must be parsed as success=True + status='queued' + job_id."""

    def test_queued_response_parsed(self):
        body = {
            "success": True,
            "message": "Jawaban berhasil dikirim",
            "status": "queued",
            "job_id": "abc123def456",
            "score": None,
        }
        with mock.patch.object(api, "_make_request", return_value=body) as mk:
            resp = api.submit_exam(
                base_url="https://exam.example",
                exam_id=7,
                student_name="Budi",
                exam_number="N01",
                student_class="9A",
                answers={"1": "A"},
                start_time="2026-08-16T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
            )
        mk.assert_called_once()
        self.assertTrue(resp.success)
        self.assertEqual(resp.status, "queued")
        self.assertEqual(resp.job_id, "abc123def456")

    def test_sync_response_parsed(self):
        body = {"success": True, "message": "Jawaban berhasil dikirim", "score": 87.5}
        with mock.patch.object(api, "_make_request", return_value=body):
            resp = api.submit_exam(
                base_url="https://exam.example",
                exam_id=7,
                student_name="Budi",
                exam_number="N01",
                student_class="9A",
                answers={"1": "A"},
                start_time="2026-08-16T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
            )
        self.assertTrue(resp.success)
        self.assertEqual(resp.status, None)
        self.assertEqual(resp.score, 87.5)


class ExamResultTest(unittest.TestCase):
    """GET /result response shapes (done / pending / failure)."""

    def _call(self, body):
        with mock.patch.object(api, "_make_request", return_value=body) as mk:
            resp = api.exam_result(
                base_url="https://exam.example",
                exam_id=7,
                token="ABCD1234",
                mac_address="DESKTOP:abcd1234",
                job_id="abc123",
                identity_data={"nama": "Budi"},
            )
        return resp, mk

    def test_done(self):
        resp, mk = self._call({"success": True, "status": "done", "score": 92.0, "message": "ok"})
        self.assertTrue(resp.success)
        self.assertEqual(resp.status, "done")
        self.assertEqual(resp.score, 92.0)
        # URL must carry job_id + mac_address + identity_data for the gate.
        url = mk.call_args.args[0]
        self.assertIn("job_id=abc123", url)
        self.assertIn("mac_address=DESKTOP%3Aabcd1234", url)
        self.assertIn("identity_data=", url)

    def test_pending(self):
        resp, _ = self._call({"success": True, "status": "pending"})
        self.assertTrue(resp.success)
        self.assertEqual(resp.status, "pending")

    def test_failure(self):
        resp, _ = self._call({"success": False, "message": "job failed after retries"})
        self.assertFalse(resp.success)


class PollQueuedResultTest(unittest.TestCase):
    """poll_queued_result: done wins, pending polls, failure stops, timeout fails."""

    def test_polls_until_done(self):
        seq = [
            SubmitResponse(success=True, status="pending"),
            SubmitResponse(success=True, status="pending"),
            SubmitResponse(success=True, status="done", score=88.0, message="ok"),
        ]
        with mock.patch.object(api, "exam_result", side_effect=seq) as er:
            resp = api.poll_queued_result(
                base_url="https://x", exam_id=7, token="T", mac_address="M",
                job_id="j", interval=0,
            )
        self.assertEqual(er.call_count, 3)
        self.assertTrue(resp.success)
        self.assertEqual(resp.status, "done")
        self.assertEqual(resp.score, 88.0)

    def test_terminal_failure_stops_immediately(self):
        with mock.patch.object(
            api, "exam_result",
            return_value=SubmitResponse(success=False, message="failed"),
        ) as er:
            resp = api.poll_queued_result(
                base_url="https://x", exam_id=7, token="T", mac_address="M",
                job_id="j", interval=0,
            )
        er.assert_called_once()
        self.assertFalse(resp.success)

    def test_timeout_returns_failure(self):
        with mock.patch.object(
            api, "exam_result",
            return_value=SubmitResponse(success=True, status="pending"),
        ) as er:
            resp = api.poll_queued_result(
                base_url="https://x", exam_id=7, token="T", mac_address="M",
                job_id="j", max_attempts=3, interval=0,
            )
        self.assertEqual(er.call_count, 3)
        self.assertFalse(resp.success)
        self.assertEqual(resp.status, "timeout")


if __name__ == "__main__":
    unittest.main()
