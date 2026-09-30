"""Audit api.py + models.py — respons server adalah input musuh.

Lanjutan pola dari audit config.py: respons server (atau proxy/WAF yang
menghadang) bisa berupa JSON VALID tapi bukan object — `[]`, `123`,
`null`, `"ok"`. Semua konsumen di api.py memanggil `data.get(...)` atau
`from_json(data)` yang mengasumsikan dict, dan except-nya hanya
menangkap URLError/OSError/JSONDecodeError — AttributeError/TypeError
dari bentuk salah LOLOS SEMUA.

Repro nyata (sebelum fix): 20+ jalur melempar, termasuk:

* `check_health` — crash saat cek koneksi (tepat saat app mencari tahu
  apakah jaringan bisa dipakai);
* `get_exam_by_token` — crash di dialog join;
* `send_access_log` / `complete_exam` — best-effort yang kontraknya
  TIDAK boleh melempar (dipanggil dari jalur cleanup/Qt slot → qFatal);
* `exam_result` — thread polling mati, siswa terjebak "menunggu
  konfirmasi" tanpa verdict;
* `SubmitResponse.from_json({"score": "abc"})` → ValueError;
* `Exam.from_json({"identity_fields": "abc"})` → AttributeError.

Plus `download_pdf`: server yang menjawab 200 dengan isi BUKAN PDF
(halaman blok proxy) dulu ditulis utuh jadi `exam.pdf` — crash di thread
viewer dengan traceback tanpa pesan yang bisa dipahami siswa. Sekarang
divalidasi magic `%PDF` sebelum rename atomik.
"""

from __future__ import annotations

import json
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from examvan import api
from examvan.models import (
    Exam,
    HealthResponse,
    SubmitResponse,
    TokenExamResponse,
)

# Respons "server rusak / proxy salah config": JSON valid, bukan object.
_GARBAGE_PAYLOADS = ("[]", "123", "null", '"ok"')


def _patch_make_request(payload: str):
    """Simulasi server yang menjawab `payload` untuk SEMUA endpoint."""

    def fake(url, **kwargs):
        return json.loads(payload)

    return mock.patch.object(api, "_make_request", fake)


class ApiNeverRaisesOnNonObjectResponseTestCase(unittest.TestCase):
    """Setiap fungsi API harus mengembalikanFailure, bukan melempar."""

    def _assert_failure(self, fn, *args, **kwargs):
        for payload in _GARBAGE_PAYLOADS:
            with self.subTest(payload=payload):
                with _patch_make_request(payload):
                    result = fn(*args, **kwargs)
                self.assertIsNotNone(result)
                self.assertFalse(
                    getattr(result, "success", True),
                    f"{fn.__name__} melaporkan sukses untuk payload {payload!r}",
                )

    def test_check_health(self):
        for payload in _GARBAGE_PAYLOADS:
            with self.subTest(payload=payload):
                with _patch_make_request(payload):
                    resp = api.check_health("https://srv")
        self.assertFalse(resp.success)

    def test_get_exam_by_token(self):
        self._assert_failure(api.get_exam_by_token, "https://srv", "ABCD1234")

    def test_submit_exam(self):
        self._assert_failure(
            api.submit_exam,
            "https://srv", 7, "Budi", "123", "X-A",
            {"1": "A"}, "2026-09-30T08:00:00", "AA:BB",
            token="TOKEN01",
        )

    def test_send_access_log_returns_false(self):
        # Best-effort: kontraknya boolean, TIDAK boleh melempar apa pun.
        for payload in _GARBAGE_PAYLOADS:
            with self.subTest(payload=payload):
                with _patch_make_request(payload):
                    self.assertFalse(
                        api.send_access_log(
                            "https://srv", 7, "TOK", "AA:BB", "heartbeat"
                        )
                    )

    def test_complete_exam_returns_false(self):
        for payload in _GARBAGE_PAYLOADS:
            with self.subTest(payload=payload):
                with _patch_make_request(payload):
                    self.assertFalse(
                        api.complete_exam("https://srv", 7, "TOK", "AA:BB")
                    )

    def test_exam_result(self):
        self._assert_failure(
            api.exam_result, "https://srv", 7, "TOK", "AA:BB", "job-1"
        )

    def test_poll_queued_result_stops_instead_of_hanging(self):
        # Respons sampah = tanpa http_status = kontrak "berhenti, jangan
        # ulangi". Polling tidak boleh duduk 77 detik pada server yang
        # jelas-jelas bukan server EXAMVAN.
        start = time.monotonic()
        with _patch_make_request('"ok"'):
            resp = api.poll_queued_result(
                "https://srv", 7, "TOK", "AA:BB", "job-1",
                max_attempts=31, interval=0.05,
            )
        elapsed = time.monotonic() - start
        self.assertFalse(resp.success)
        self.assertLess(
            elapsed, 2.0,
            f"polling makan {elapsed:.1f}s pada respons sampah — "
            "harus berhenti segera (kontrak http_status None)",
        )


class ModelsAcceptAnyJsonShapeTestCase(unittest.TestCase):
    """from_json tidak boleh mengasumsikan bentuk apa pun dari kawat."""

    def test_submit_response_score_garbage_becomes_none(self):
        resp = SubmitResponse.from_json({"score": "abc"})
        self.assertIsNone(resp.score)

    def test_submit_response_score_bool_is_accepted(self):
        # Server boleh mengirim 0/1; float(True) sah — jangan terlalu galak.
        resp = SubmitResponse.from_json({"score": 1})
        self.assertEqual(resp.score, 1.0)

    def test_submit_response_non_dict(self):
        for payload in ([], None, 123, "ok"):
            with self.subTest(payload=payload):
                resp = SubmitResponse.from_json(payload)
                self.assertFalse(resp.success)
                self.assertEqual(resp.message, "")

    def test_token_exam_response_non_dict(self):
        for payload in ([], None, 123):
            with self.subTest(payload=payload):
                resp = TokenExamResponse.from_json(payload)
                self.assertFalse(resp.success)
                self.assertIsNone(resp.exam)

    def test_token_exam_response_exam_list_is_not_an_exam(self):
        resp = TokenExamResponse.from_json({"exam": ["x"], "success": True})
        self.assertIsNone(resp.exam)

    def test_exam_non_dict(self):
        for payload in ([], None, "abc"):
            with self.subTest(payload=payload):
                exam = Exam.from_json(payload)
                self.assertEqual(exam.id, 0)

    def test_exam_id_garbage_becomes_zero(self):
        self.assertEqual(Exam.from_json({"id": "abc"}).id, 0)

    def test_exam_size_mb_garbage_becomes_zero(self):
        self.assertEqual(Exam.from_json({"id": 1, "size_mb": "besar"}).size_mb, 0.0)

    def test_exam_identity_fields_non_list_is_ignored(self):
        for raw in ("abc", {"a": 1}, 5, [None, "budi", {"key": "k", "label": "L"}]):
            with self.subTest(raw=raw):
                exam = Exam.from_json({"id": 1, "identity_fields": raw})
                # Entri non-dict dibuang; dict sah tetap dipetakan.
                self.assertTrue(
                    all(isinstance(f.key, str) for f in exam.identity_fields)
                )
        exam = Exam.from_json(
            {"id": 1, "identity_fields": [None, {"key": "k", "label": "L"}]}
        )
        self.assertEqual(
            [(f.key, f.label) for f in exam.identity_fields], [("k", "L")]
        )

    def test_exam_questions_non_list_is_ignored(self):
        exam = Exam.from_json({"id": 1, "questions": {"bukan": "list"}})
        self.assertEqual(exam.questions, [])

    def test_health_response_non_dict(self):
        resp = HealthResponse.from_json([])
        self.assertFalse(resp.success)


class _FakeResponse:
    """urlopen palsu: header dict + chunk yang sudah disiapkan."""

    def __init__(self, chunks, headers=None):
        self._chunks = list(chunks)
        self.headers = headers or {}

    def read(self, n=-1):
        if not self._chunks:
            return b""
        return self._chunks.pop(0)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class _FakeOpener:
    def __init__(self, resp):
        self._resp = resp

    def open(self, req, timeout=None):
        return self._resp


class DownloadPdfValidatesContentTestCase(unittest.TestCase):
    """200 + isi bukan PDF = error yang bisa dipahami, bukan file sampah."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dest = str(Path(self._tmp.name) / "exam.pdf")

    def _download(self, chunks, headers=None):
        fake = _FakeOpener(_FakeResponse(chunks, headers))
        with mock.patch.object(api, "_pdf_opener", lambda: fake):
            return api.download_pdf(
                "https://srv", 7, "TOK", self.dest, device_id="AA:BB"
            )

    def test_html_error_page_is_rejected_and_tmp_cleaned(self):
        with self.assertRaises(ValueError):
            self._download([b"<html>403 blocked by WAF</html>"])
        self.assertFalse(Path(self.dest + ".tmp").exists())
        self.assertFalse(Path(self.dest).exists())

    def test_empty_response_is_rejected(self):
        with self.assertRaises(ValueError):
            self._download([b""])

    def test_valid_pdf_is_saved(self):
        payload = b"%PDF-1.4\n" + b"\x00" * 64
        result = self._download([payload])
        self.assertEqual(result, self.dest)
        self.assertTrue(Path(self.dest).exists())

    def test_garbage_content_length_does_not_break_the_download(self):
        # int("11 MB!") dulu melempar di tengah try — file dibersihkan,
        # tapi download sah ikut gagal. Sekarang dianggap unknown (-1).
        payload = b"%PDF-1.4\n" + b"\x00" * 32
        result = self._download(
            [payload], headers={"Content-Length": "11 MB!"}
        )
        self.assertEqual(result, self.dest)


if __name__ == "__main__":
    unittest.main()
