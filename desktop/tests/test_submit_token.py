"""Submit wajib membawa X-Exam-Token — dan tidak boleh mengulang 401.

Gejala lapangan (Windows, 30 September 2026): "kapan submit selalu gagal,
katanya token tidak ada".

Server menolak submit TANPA token sebelum apa pun yang lain dicek
(webui/internal/handlers/api/exams.go:1016-1023):

    token := strings.TrimSpace(c.GetHeader("X-Exam-Token"))
    if token == "" { token = strings.TrimSpace(c.Query("token")) }
    if token == "" {
        errorResponse(c, http.StatusUnauthorized, "Token tidak disertakan")
        return
    }

Setiap endpoint lain yang dipakai klien desktop mengirim header itu —
download_pdf (api.py:167), send_access_log (api.py:351), complete_exam
(api.py:380), exam_result (api.py:432) — dan Android juga mengirimnya di
submit (ApiClient.kt:550). Hanya submit_exam yang tidak, di ketiga call site
nya (exam_viewer.py:602, exam_viewer.py:675, server_config.py:303). Akibatnya
seluruh kelas desktop tidak pernah bisa mengumpulkan jawaban.

Dua cacat yang saling menguatkan di jalur itu:
  * token tidak pernah dikirim  -> 401;
  * submit_with_retry mengulang percobaan secara buta (1s+2s+4s) tanpa
    membedakan kegagalan sementara dari penolakan permanen -> siswa menunggu
    7 detik, membaca "Submit gagal, percobaan 4/4...", lalu tetap gagal.
"""

from __future__ import annotations

import io
import json
import types
import unittest
import urllib.error
from unittest import mock

from examvan import api
from examvan.models import Exam, SubmitResponse


def _http_error(status: int, message: str) -> urllib.error.HTTPError:
    body = json.dumps({"success": False, "message": message}).encode("utf-8")
    return urllib.error.HTTPError(
        url="https://exam.example/api/exams/7/submit",
        code=status,
        msg=message,
        hdrs=None,
        fp=io.BytesIO(body),
    )


class SubmitTokenTest(unittest.TestCase):
    """Header X-Exam-Token wajib ada di setiap POST /submit."""

    def _headers_seen(self, token):
        captured = {}

        def _fake_request(url, method="GET", headers=None, body=None, timeout=30):
            captured["headers"] = headers or {}
            captured["url"] = url
            return {"success": True, "status": "done"}

        with mock.patch.object(api, "_make_request", side_effect=_fake_request):
            resp = api.submit_exam(
                base_url="https://exam.example",
                exam_id=7,
                student_name="Budi",
                exam_number="N01",
                student_class="9A",
                answers={"1": "A"},
                start_time="2026-08-16T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
                token=token,
            )
        return captured["headers"], resp

    def test_submit_sends_the_exam_token_header(self):
        headers, resp = self._headers_seen("ABCD1234")
        self.assertEqual(headers.get("X-Exam-Token"), "ABCD1234")
        self.assertTrue(resp.success)

    def test_retry_forwards_the_token_to_every_attempt(self):
        # Kalau hanya percobaan pertama yang membawa token, retry tetap 401.
        seen = []

        def _fake_request(url, method="GET", headers=None, body=None, timeout=30):
            seen.append((headers or {}).get("X-Exam-Token"))
            return {"success": True, "status": "done"}

        with mock.patch.object(api, "_make_request", side_effect=_fake_request):
            api.submit_with_retry(
                base_url="https://exam.example",
                exam_id=7,
                student_name="Budi",
                exam_number="N01",
                student_class="9A",
                answers={"1": "A"},
                start_time="2026-08-16T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
                token="ABCD1234",
            )
        self.assertTrue(seen)
        self.assertEqual(set(seen), {"ABCD1234"})

    def test_missing_token_fails_locally_without_a_request(self):
        # Guardrail: lebih baik pesan jelas di perangkat daripada 401 dari
        # server setelah semua percobaan gagal.
        with mock.patch.object(api, "_make_request") as mk:
            resp = api.submit_exam(
                base_url="https://exam.example",
                exam_id=7,
                student_name="Budi",
                exam_number="N01",
                student_class="9A",
                answers={"1": "A"},
                start_time="2026-08-16T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
                token="",
            )
        mk.assert_not_called()
        self.assertFalse(resp.success)
        self.assertFalse(resp.retryable)
        self.assertIn("token", resp.message.lower())


class SubmitRetryPolicyTest(unittest.TestCase):
    """401/403/404 tidak diulang; timeout/429/5xx dan error jaringan diulang."""

    def test_401_is_not_retried(self):
        calls = []

        def _fake_submit(*args, **kwargs):
            calls.append(1)
            return SubmitResponse(
                success=False,
                message="Token tidak disertakan",
                http_status=401,
            )

        with mock.patch.object(api, "submit_exam", side_effect=_fake_submit), \
             mock.patch.object(api.time, "sleep") as slp:
            resp = api.submit_with_retry(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "2026-08-16T07:00:00Z", "DESKTOP:x",
                token="BAD", delays=[1, 2, 4],
            )
        self.assertEqual(len(calls), 1)
        slp.assert_not_called()
        self.assertFalse(resp.success)

    def test_403_and_404_are_not_retried(self):
        for status in (403, 404, 400, 409):
            with self.subTest(status=status):
                with mock.patch.object(
                    api, "submit_exam",
                    return_value=SubmitResponse(
                        success=False, message="ditolak", http_status=status,
                    ),
                ) as sub, mock.patch.object(api.time, "sleep"):
                    api.submit_with_retry(
                        "https://exam.example", 7, "Budi", "N01", "9A",
                        {"1": "A"}, "t", "m", token="T",
                    )
                self.assertEqual(sub.call_count, 1)

    def test_408_and_429_are_retried(self):
        for status in (408, 429):
            with self.subTest(status=status):
                with mock.patch.object(
                    api, "submit_exam",
                    return_value=SubmitResponse(
                        success=False, message="slow down", http_status=status,
                    ),
                ) as sub, mock.patch.object(api.time, "sleep"):
                    api.submit_with_retry(
                        "https://exam.example", 7, "Budi", "N01", "9A",
                        {"1": "A"}, "t", "m", token="T",
                    )
                self.assertEqual(sub.call_count, 4)  # 1 + len([1,2,4])

    def test_server_error_is_retried(self):
        seq = [
            SubmitResponse(success=False, message="boom", http_status=500),
            SubmitResponse(success=False, message="boom", http_status=503),
            SubmitResponse(success=True, message="ok", status="done"),
        ]
        with mock.patch.object(api, "submit_exam", side_effect=seq) as sub, \
             mock.patch.object(api.time, "sleep"):
            resp = api.submit_with_retry(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "t", "m", token="T",
            )
        self.assertEqual(sub.call_count, 3)
        self.assertTrue(resp.success)

    def test_network_error_without_status_is_retried(self):
        seq = [
            SubmitResponse(success=False, message="timed out"),
            SubmitResponse(success=True, message="ok", status="done"),
        ]
        with mock.patch.object(api, "submit_exam", side_effect=seq) as sub, \
             mock.patch.object(api.time, "sleep"):
            resp = api.submit_with_retry(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "t", "m", token="T",
            )
        self.assertEqual(sub.call_count, 2)
        self.assertTrue(resp.success)

    def test_retry_reports_each_attempt(self):
        with mock.patch.object(
            api, "submit_exam",
            return_value=SubmitResponse(
                success=False, message="boom", http_status=500,
            ),
        ), mock.patch.object(api.time, "sleep"), \
                mock.patch("builtins.print"):
            seen = []
            api.submit_with_retry(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "t", "m", token="T",
                on_retry=lambda attempt, total: seen.append((attempt, total)),
            )
        self.assertEqual(seen, [(2, 4), (3, 4), (4, 4)])


class HttpErrorStatusTest(unittest.TestCase):
    """Kode HTTP harus tersimpan di respons, supaya retry bisa membedakan."""

    def test_401_body_message_is_preserved(self):
        with mock.patch.object(
            api, "_make_request", side_effect=_http_error(401, "Token tidak disertakan")
        ):
            resp = api.submit_exam(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "t", "DESKTOP:x", token="T",
            )
        self.assertFalse(resp.success)
        self.assertEqual(resp.http_status, 401)
        self.assertEqual(resp.message, "Token tidak disertakan")

    def test_401_with_unparseable_body_still_records_the_status(self):
        err = urllib.error.HTTPError(
            url="https://exam.example", code=401, msg="Unauthorized",
            hdrs=None, fp=io.BytesIO(b"<html>401</html>"),
        )
        with mock.patch.object(api, "_make_request", side_effect=err):
            resp = api.submit_exam(
                "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "t", "DESKTOP:x", token="T",
            )
        self.assertFalse(resp.success)
        self.assertEqual(resp.http_status, 401)


class SubmitResponseRetryableTest(unittest.TestCase):
    """Aturan retryability, murni."""

    def test_no_status_is_retryable(self):
        self.assertTrue(SubmitResponse(success=False).retryable)

    def test_completed_2xx_and_3xx_are_not_retried(self):
        # The server answered and the answer was not success — repeating the
        # identical request will not change that, so spending seven seconds on
        # it only delays the error message.
        for status in (200, 202, 301, 302):
            with self.subTest(status=status):
                self.assertFalse(
                    SubmitResponse(success=False, http_status=status).retryable
                )

    def test_4xx_except_408_429_are_permanent(self):
        for status in (400, 401, 403, 404, 409, 410, 422):
            with self.subTest(status=status):
                self.assertFalse(
                    SubmitResponse(success=False, http_status=status).retryable
                )

    def test_408_429_and_5xx_are_retryable(self):
        for status in (408, 429, 500, 502, 503, 504):
            with self.subTest(status=status):
                self.assertTrue(
                    SubmitResponse(success=False, http_status=status).retryable
                )


# ---------------------------------------------------------------------------
# Call site: token harus benar-benar diteruskan, bukan hanya tersedia di api.py
# ---------------------------------------------------------------------------


class _Sig:
    def __init__(self):
        self.calls = []

    def emit(self, *a):
        self.calls.append(a)

    def connect(self, *a, **k):
        pass


class SubmitCallSiteTokenTest(unittest.TestCase):
    """Tiap call site harus meneruskan token.

    Tanpa test ini, `api.submit_with_retry` bisa menerima parameter `token`,
    mengirim header dengan benar, dan tetap tidak pernah dipakai: test yang
    ada hanya memock submit_with_retry, jadi tiga call site asli bisa
    melewatinya tanpa terdeteksi. Dan itulah persis bentuk bug-nya —
    deklarasi benar, pemanggilan tidak.
    """

    TOKEN = "ABCD1234"

    def _viewer_stub(self):
        return types.SimpleNamespace(
            _token=self.TOKEN, _sig_status=_Sig(), _sig_submit_result=_Sig()
        )

    def test_manual_submit_passes_the_token(self):
        from examvan.ui.exam_viewer import ExamViewerWindow

        stub = self._viewer_stub()
        ok = SubmitResponse(success=True, message="ok", status="done")
        with mock.patch.object(api, "submit_with_retry", return_value=ok) as sub:
            ExamViewerWindow._submit_thread(
                stub, "https://exam.example", 7, "Budi", "N01", "9A",
                {"1": "A"}, "2026-08-16T07:00:00Z", "DESKTOP:m", {"nama": "Budi"},
            )
        self.assertEqual(sub.call_count, 1)
        self.assertEqual(sub.call_args.kwargs.get("token"), self.TOKEN)

    def test_auto_submit_background_passes_the_token(self):
        from examvan.ui.exam_viewer import ExamViewerWindow

        ok = SubmitResponse(success=True, message="ok", status="done")
        with mock.patch.object(api, "submit_with_retry", return_value=ok) as sub, \
             mock.patch.object(api, "complete_exam"), \
             mock.patch("examvan.notify.send_notification"):
            ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", 7, self.TOKEN, "Budi", "N01", "9A",
                {"1": "A"}, "2026-08-16T07:00:00Z", "DESKTOP:m", {"nama": "Budi"},
            )
        self.assertEqual(sub.call_count, 1)
        self.assertEqual(sub.call_args.kwargs.get("token"), self.TOKEN)

    def test_recovery_resend_passes_the_token(self):
        from examvan.ui.server_config import ServerConfigDialog

        exam = Exam(id=7, name="Ujian", status="active")
        stub = types.SimpleNamespace(
            input_token=mock.Mock(**{"text.return_value": "  abcd1234 \n"}),
            _server_url="https://exam.example",
            _sig_status=_Sig(),
            _sig_recovery_done=_Sig(),
            _sig_enable_btn=_Sig(),
        )
        ok = SubmitResponse(success=True, message="ok", status="done")
        with mock.patch.object(
            api, "submit_with_retry", return_value=ok
        ) as sub, mock.patch.object(api, "complete_exam"), \
             mock.patch("examvan.config.load_answers", return_value={"1": "A"}), \
             mock.patch("examvan.config.get",
                        side_effect=lambda k, d=None:
                        "ABCD1234" if k == "exam_token" else d), \
             mock.patch("examvan.config.load_start_time", return_value="t"), \
             mock.patch("examvan.config.clear_answers"), \
             mock.patch("examvan.utils.get_device_label", return_value="DESKTOP:m"):
            ServerConfigDialog._recovery_submit_thread(stub, exam, {"nama": "Budi"})
        self.assertEqual(sub.call_count, 1)
        self.assertEqual(sub.call_args.kwargs.get("token"), "ABCD1234")


if __name__ == "__main__":
    unittest.main()
