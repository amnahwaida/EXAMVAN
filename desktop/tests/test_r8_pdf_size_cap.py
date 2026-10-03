"""MEDIUM — `download_pdf` menulis ke `%TEMP%` tanpa plafon ukuran.

Bug
---
`api.download_pdf` melakukan

    while True:
        chunk = resp.read(65536)
        f.write(chunk)

sampai EOF, tanpa plafon. Sementara `_make_request` SENGAJA membatasi body
non-PDF 32 MB, dan komentar di kode itu sendiri mengatakannya: "Jalur PDF
chunked (download_pdf) TIDAK tersentuh batas ini". Jadi ada dua plafon yang
tidak sama, dan yang tidak punya plafon justru yang menulis ke disk.

Dampak: origin yang salah konfigurasi atau sengaja Tossing (atau proxy
yang menjawab berkas lain) mengirim badan tanpa akhir. Siswa melihat
progress bar lalu panel PDF hitam; PC lab kehabisan ruang disk, dan berkas
jawaban yang sudah tersimpan ikut rusak karena tidak ada ruang untuk
menulis.

Perbaikan: plafon yang bisa dipertanggungjawabkan, 4x plafon JSON,
ditegakkan DUA kali —

  1. sebelum menulis apa pun, kalau `Content-Length` sudah di atas plafon
     (respons ditolak tanpa menyentuh disk sama sekali);
  2. saat streaming, karena `Content-Length` bisa BERBOHONG (nilai kecil)
     atau tidak ada sama sekali (chunked). Plafon streaming inilah yang
     tidak bisa dilewati header.

Angka yang diuji sengaja dipatch ke nilai kecil supaya test tidak menulis
128 MB ke disk; konstanta production-nya sendiri dikunci terpisah di
`CapConstantsTestCase`.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from typing import Optional
from unittest import mock

from examvan import api

CHUNK = 65536


class _FakeResponse:
    """Respons unduhan yang boleh berbohong soal `Content-Length`."""

    def __init__(self, total_bytes: int, declared: Optional[int] = None):
        self.headers = {}
        if declared is not None:
            self.headers["Content-Length"] = str(declared)
        self._left = total_bytes
        self.reads = 0

    def read(self, size: int = -1) -> bytes:
        self.reads += 1
        if self._left <= 0:
            return b""
        want = self._left if size is None or size < 0 else min(size, self._left)
        self._left -= want
        return b"%PDF-1.4\n" + b"0" * max(want - 9, 0)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class _DownloadSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-pdfcap-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.dest = str(self.tmp / "examvan_exam_7.pdf")

    def _download(self, response, cap: int = 100_000, **kwargs):
        opener = mock.MagicMock()
        opener.open.return_value = response
        with mock.patch.object(api, "_pdf_opener", return_value=opener), \
             mock.patch.object(api, "_MAX_PDF_BYTES", cap, create=True):
            return api.download_pdf("https://srv", 7, "T", self.dest, **kwargs)

    def _tmp_files(self):
        return [p.name for p in self.tmp.glob("*.tmp.*")]


class DeclaredOversizeIsRejectedBeforeDiskTestCase(_DownloadSandbox):
    def test_a_declared_oversize_body_never_reaches_the_disk(self):
        resp = _FakeResponse(total_bytes=10, declared=900_000)
        with self.assertRaises(ValueError) as caught:
            self._download(resp, cap=100_000)
        self.assertEqual(
            resp.reads, 0,
            "server sudah menyatakan ukurannya di luar plafon, tapi isi "
            "tetap ditulis ke %TEMP%",
        )
        self.assertFalse(os.path.exists(self.dest))
        self.assertEqual(
            self._tmp_files(), [],
            "berkas temp tidak dihapus: unduhan yang dibatalkan karena "
            "terlalu besar tetap meninggalkan salinan naskah di disk",
        )
        message = str(caught.exception)
        self.assertIn(
            "pengawas", message,
            "pesan untuk siswa harus menyebut apa yang harus dilakukan",
        )
        self.assertIn(
            "terlalu besar", message,
            "siswa harus diberi tahu berkasnya ditolak karena ukuran, "
            "bukan karena jaringan putus",
        )


class StreamingIsCappedTestCase(_DownloadSandbox):
    def test_a_lying_content_length_cannot_bypass_the_cap(self):
        # Header bilang 12 byte, body 5 MB — persis kasus yang tidak bisa
        # dicegat hanya dari header.
        resp = _FakeResponse(total_bytes=5_000_000, declared=12)
        seen = []
        with self.assertRaises(ValueError) as caught:
            self._download(
                resp, cap=100_000,
                progress_cb=lambda read, total: seen.append(read),
            )
        self.assertFalse(os.path.exists(self.dest))
        self.assertEqual(self._tmp_files(), [])
        self.assertTrue(seen, "progress harus dilaporkan (kontrol)")
        self.assertLessEqual(
            max(seen), 100_000 + CHUNK,
            "lebih dari satu chunk ditulis melewati plafon: "
            f"{max(seen)} byte untuk plafon 100.000",
        )
        self.assertIn("terlalu besar", str(caught.exception))

    def test_a_chunked_response_without_content_length_is_capped(self):
        resp = _FakeResponse(total_bytes=5_000_000, declared=None)
        with self.assertRaises(ValueError):
            self._download(resp, cap=100_000)
        self.assertFalse(os.path.exists(self.dest))
        self.assertEqual(self._tmp_files(), [])

    def test_the_written_bytes_never_exceed_the_cap(self):
        # Yang dipantau adalah ukuran di disk, bukan hanya exception-nya.
        resp = _FakeResponse(total_bytes=3_000_000, declared=None)
        peak = {"size": 0}
        real_open = open

        def _tracking_open(path, *a, **kw):
            fh = real_open(path, *a, **kw)
            orig_write = fh.write

            def _write(data):
                try:
                    peak["size"] = max(peak["size"], os.path.getsize(path))
                except OSError:
                    pass
                return orig_write(data)

            fh.write = _write  # type: ignore[method-assign]
            return fh

        with self.assertRaises(ValueError):
            with mock.patch("builtins.open", _tracking_open):
                self._download(resp, cap=100_000)
        self.assertLessEqual(
            peak["size"], 100_000 + CHUNK,
            f"{peak['size']} byte ditulis ke %TEMP% untuk plafon 100.000",
        )


class NormalDownloadStillWorksTestCase(_DownloadSandbox):
    def test_a_pdf_under_the_cap_is_installed_normally(self):
        pdf = b"%PDF-1.4\n" + b"0" * 40_000
        chunks = [pdf]

        class _Resp:
            headers = {"Content-Length": str(len(pdf))}

            def read(self, size=-1):
                return chunks.pop(0) if chunks else b""

            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        got = self._download(_Resp(), cap=100_000)
        self.assertEqual(got, self.dest)
        self.assertTrue(os.path.exists(self.dest))
        self.assertEqual(
            Path(self.dest).read_bytes(), pdf,
            "berkas yang sah harus utuh — kontrol: test tidak boleh hijau "
            "karena semua unduhan ditolak",
        )


class JsonCapIsCappedTooTestCase(unittest.TestCase):
    """Plafon JSON juga tidak boleh dilewati `Content-Length` yang bohong."""

    class _Resp:
        def __init__(self, headers=None):
            self.headers = headers or {}
            self._left = 200 * 1024

        def read(self, size=-1):
            if self._left <= 0:
                return b""
            want = min(size, self._left)
            self._left -= want
            return b"x" * want

        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

    def _make_request(self, headers=None):
        opener = mock.MagicMock()
        opener.open.return_value = self._Resp(headers)
        with mock.patch.object(api, "_pdf_opener", return_value=opener):
            return api._make_request("https://srv/x")

    def test_a_lying_content_length_does_not_bypass_the_json_cap(self):
        out = self._make_request({"Content-Length": "10"})
        self.assertFalse(out.get("success"))
        self.assertEqual(out.get("error_code"), "non_json_response")

    def test_a_normal_json_body_still_parses(self):
        body = b'{"success": true}'
        opener = mock.MagicMock()
        resp = self._Resp()
        resp.read = lambda size=-1: body[:size] if size >= 0 else body
        opener.open.return_value = resp
        with mock.patch.object(api, "_pdf_opener", return_value=opener):
            out = api._make_request("https://srv/x")
        self.assertTrue(out.get("success"))


class CapConstantsTestCase(unittest.TestCase):
    """Plafonnya harus punya angka yang bisa dipertanggungjawabkan."""

    def test_the_pdf_cap_is_named_and_bounded(self):
        cap = getattr(api, "_MAX_PDF_BYTES", None)
        self.assertIsInstance(cap, int, "plafon PDF tidak didefinisikan")
        self.assertGreaterEqual(
            cap, 64 * 1024 * 1024,
            "plafon terlalu kecil untuk naskah ujian hasil pindai",
        )
        self.assertLessEqual(
            cap, 256 * 1024 * 1024,
            "plafon terlalu longgar: nilainya jadi tidak membatas apa pun",
        )

    def test_the_json_cap_is_named_too_and_smaller_than_the_pdf_one(self):
        # Dua plafon, masing-masing dengan satu nama, supaya "konsisten"
        # bisa diuji dan bukan sekadar harapan.
        self.assertIsInstance(getattr(api, "_MAX_JSON_BYTES", None), int)
        self.assertLess(api._MAX_JSON_BYTES, api._MAX_PDF_BYTES)
        self.assertEqual(api._MAX_JSON_BYTES, 32 * 1024 * 1024)


if __name__ == "__main__":
    unittest.main()