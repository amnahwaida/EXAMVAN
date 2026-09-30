"""PDF tidak boleh membawa token ke origin lain, dan status HTTP harus utuh (#10, #13).

Bug #10 -- X-Exam-Token ikut ke Cloudflare R2 saat redirect 302
--------------------------------------------------------------
`download_pdf()` memakai opener bawaan `urllib.request.urlopen`. Endpoint
PDF menjawab `c.Redirect(http.StatusFound, signedURL)` ke bucket R2.
`HTTPRedirectHandler.redirect_request` di CPython menyalin SEMUA custom
header dari request asal ke request baru:

    newheaders = {k: v for k, v in req.headers.items()
                  if k.lower() not in ("content-length", "content-type")}

Jadi `X-Exam-Token` (kredensial seluruh kelas pada mode static) dan
`X-Device-Id` dikirim ke origin yang berbeda, mendarat di access log
origin itu, DAN digabung dengan signed URL dalam satu request. Yang
memin conçue check-nya: yang memeriksa header hanya `exams.go` di origin
pertama, jadi "sudah lolos gate" terasa aman.

Bug #13 -- status HTTP dibuang, jadi "belum" dan "tidak akan pernah" sama
-----------------------------------------------------------------------
`exam_result()` menangkap `urllib.error.HTTPError` lalu membangun
`SubmitResponse` dari body -- tanpa pernah menyertakan `e.code`.
`poll_queued_result()` lalu memperlakukan apa pun yang bukan `success`
dan bukan `done` sebagai "masih pending" dan MELANJUTKAN polling:

    401 "Token tidak valid"      -> polling terus, selamanya
    403 "Waktu ujian telah berakhir" -> polling terus
    429 "Terlalu banyak request" -> polling terus

Untuk satu siswa itu berarti membaca "Gagal mengumpulkan jawaban" padahal
jawabannya sudah durable di PostgreSQL, dan tombol "Kirim Lagi" yang
mencoba mengirim ulang. Untuk se-RUANGAN, `31 x 2.5s` = 24 request/menit
per perangkat, dan `resultExamRateLimitMax` = 12000/menit: 500 perangkat
yang tersinkron-phase menghabiskan bucket bersama dan started students
ikut jadi ikut 429, yang kembali ke cabang "Koneksi Terganggu" yang sama.

`submit_with_retry` sudah benar melakukan hal sebaliknya lewat
`SubmitResponse.retryable`; kedua jalur ini tidak pernah-playing
together.
"""

from __future__ import annotations

import io
import json
import unittest
import urllib.error
from unittest import mock

from examvan import api
from examvan.models import SubmitResponse


class TokenStaysOnTheExamOriginTestCase(unittest.TestCase):
    """#10 — token hanya boleh dikirim ke origin yang memang memeriksa."""

    def _recording_opener(self, target, seen):
        class _Resp(io.BytesIO):
            def __enter__(self):
                return self

            def __exit__(self, *exc):
                self.close()
                return False

            @property
            def headers(self):
                return type("H", (), {"get": lambda _s, k, d=None: None})()

        real = urllib.request.HTTPRedirectHandler

        class _Handler(real):
            def http_error_302(self, req, fp, code, msg, headers):
                seen.append(("302", dict(req.headers)))
                new = super().http_error_302(req, fp, code, msg, headers)
                if new is not None:
                    seen.append(("redirected", dict(new.headers)))
                return new

        opener = urllib.request.build_opener(_Handler())
        return opener, _Resp(b"%PDF-1.4")

    def test_token_is_not_replayed_to_the_redirect_target(self):
        seen = []
        opener, body = self._recording_opener(None, seen)
        redirect_url = "https://bucket.r2.cloudflarestorage.com/signed/abc.pdf"
        real_open = urllib.request.OpenerDirector.open

        def fake_open(self_opener, req, timeout=None):
            seen.append(("request", req.full_url, dict(req.headers)))
            if req.full_url.startswith("https://exam.example"):
                raise urllib.error.HTTPError(
                    req.full_url, 302, "Found", {},
                    io.BytesIO(redirect_url.encode()),
                )
            return body

        with mock.patch.object(urllib.request.OpenerDirector, "open", fake_open), \
             mock.patch("urllib.request.urlopen", opener.open):
            with self.assertRaises(Exception):
                api.download_pdf(
                    "https://exam.example", 7, "ABCD1234", "DESKTOP:abc", "/tmp/x.pdf"
                )

        redirected = [h for kind, h in
                      ((s[0], s[1]) for s in seen if s[0] == "redirected")]
        for headers in redirected:
            self.assertNotIn(
                "X-Exam-token", {k.lower() for k in headers},
                "X-Exam-Token diteruskan ke target redirect (origin storage), "
                "bukan ke server ujian",
            )
            self.assertNotIn(
                "X-device-id", {k.lower() for k in headers},
                "X-Device-Id diteruskan ke target redirect",
            )

    def test_the_download_opener_strips_our_headers_on_redirect(self):
        # Mekanismenya harus benar-benar ada, bukan hanya "tidak terjadi
        # kebetulan". Kalau ini hilang, header ikut lagi.
        from examvan.api import _ExamOnlyRedirectHandler

        handler = _ExamOnlyRedirectHandler()
        first = urllib.request.Request(
            "https://exam.example/api/exams/7/pdf",
            headers={
                "X-Exam-Token": "ABCD1234",
                "X-Device-Id": "DESKTOP:abc",
                "User-Agent": "EXAMVAN-Windows/2.5.1",
            },
        )
        new = handler.redirect_request(
            first,
            io.BytesIO(), 302, "Found", {},
            "https://bucket.r2.cloudflarestorage.com/signed/abc.pdf",
        )
        names = {k.lower() for k in new.headers}
        self.assertIn("user-agent", names, "header yang tidak sensitif harus ikut")
        self.assertNotIn("x-exam-token", names)
        self.assertNotIn("x-device-id", names)


class HttpStatusSurvivesTheRoundTripTestCase(unittest.TestCase):
    """#13 — kode status harus sampai ke pemanggil."""

    def _exam_result_with_http_status(self, code, body=None):
        payload = json.dumps(body or {"success": False, "message": f"HTTP {code}"}).encode()
        error = urllib.error.HTTPError(
            "https://exam.example/result", code, "msg", {},
            io.BytesIO(payload),
        )
        with mock.patch.object(api, "_make_request", side_effect=error):
            return api.exam_result(
                "https://exam.example", 7, "ABCD1234", "DESKTOP:abc", "job-1"
            )

    def test_401_is_not_reported_as_a_retryable_pending_state(self):
        resp = self._exam_result_with_http_status(401)
        self.assertEqual(
            resp.http_status, 401,
            "kode status dibuang, jadi 401 'Token tidak valid' tidak bisa "
            "dibedakan dari gangguan jaringan",
        )

    def test_403_and_429_also_survive(self):
        for code in (403, 404, 429, 500):
            with self.subTest(code=code):
                resp = self._exam_result_with_http_status(code)
                self.assertEqual(resp.http_status, code)

    def test_a_rate_limited_poll_backs_off_instead_of_hammering(self):
        # 429 itu TRANSIENT, bukan verdikt permanen. Tapi `interval=2.5s`
        # untuk semua perangkat yang starts in phase = 24 req/menit per
        # perangkat, dan `resultExamRateLimitMax` = 12000/menit. 500
        # perangkat yang sinkron-phase menghabiskan bucket bersama, dan
        # siswa lain ikut jadi 429. Backoff+jitter memutus pola itu.
        sleeps = []
        calls = []

        def fake_result(*args, **kwargs):
            calls.append(1)
            return SubmitResponse(
                success=False, status="error",
                message="Terlalu banyak request", http_status=429,
            )

        with mock.patch.object(api, "exam_result", side_effect=fake_result), \
             mock.patch.object(api.time, "sleep", side_effect=sleeps.append):
            resp = api.poll_queued_result(
                "https://exam.example", 7, "ABCD1234", "DESKTOP:abc", "job-1",
                max_attempts=6,
            )

        self.assertGreater(len(calls), 1, "429 dianggap final padahal transient")
        self.assertFalse(resp.success)
        self.assertEqual(resp.http_status, 429)
        self.assertTrue(
            any(s > 2.5 for s in sleeps),
            f"tidak ada backoff saat 429: {sleeps}",
        )
        # Jitter: jeda tidak semuanya identik, jadi dua perangkat yang
        # kena 429 pada detik sama tidak retry pada detik yang sama juga.
        self.assertGreater(
            len(set(sleeps)), 1,
            f"tidak ada jitter: {sleeps} -- semua perangkat akan retry "
            "pada detik yang sama persis",
        )

    def test_401_stops_polling_immediately(self):
        calls = []

        def fake_result(*args, **kwargs):
            calls.append(1)
            return SubmitResponse(
                success=False, status="error", message="Token tidak valid",
                http_status=401,
            )

        with mock.patch.object(api, "exam_result", side_effect=fake_result), \
             mock.patch.object(api.time, "sleep"):
            api.poll_queued_result(
                "https://exam.example", 7, "ABCD1234", "DESKTOP:abc", "job-1"
            )
        self.assertEqual(len(calls), 1, "401 dipoll selamanya")

    def test_403_stops_polling_immediately(self):
        for code in (403, 404, 410):
            calls = []

            def fake_result(*args, _c=code, **kwargs):
                calls.append(1)
                return SubmitResponse(
                    success=False, status="error", message=f"HTTP {_c}",
                    http_status=_c,
                )

            with self.subTest(code=code), \
                 mock.patch.object(api, "exam_result", side_effect=fake_result), \
                 mock.patch.object(api.time, "sleep"):
                api.poll_queued_result(
                    "https://exam.example", 7, "ABCD1234", "DESKTOP:abc",
                    "job-1", max_attempts=5,
                )
            self.assertEqual(len(calls), 1)

    def test_a_genuine_network_blip_still_polls(self):
        # Jangan sampai perbaikannya mematikan polling yang memang perlu:
        # "pending" (success=True, belum done) harus tetap diulang sampai
        # worker selesai.
        calls = []

        def fake_result(*args, **kwargs):
            # Bentuk asli dari server: worker belum selesai -> pending.
            calls.append(1)
            if len(calls) < 3:
                return SubmitResponse(success=True, status="pending")
            return SubmitResponse(success=True, status="done")

        with mock.patch.object(api, "exam_result", side_effect=fake_result), \
             mock.patch.object(api.time, "sleep"):
            resp = api.poll_queued_result(
                "https://exam.example", 7, "ABCD1234", "DESKTOP:abc", "job-1"
            )
        self.assertTrue(resp.success)
        self.assertEqual(len(calls), 3)


if __name__ == "__main__":
    unittest.main()