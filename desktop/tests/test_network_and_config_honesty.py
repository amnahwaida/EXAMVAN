"""Jaringan yang tidak sesuai harapan harus gagal dengan jujur.

Tiga kelas bug yang sama: sistem yang tidak tahu apa yang sebenarnya
terjadi, lalu melaporkannya sebagai keberhasilan.

1. Response non-JSON dianggap sukses
   `api._make_request` mengembalikan `success: True` untuk apa pun yang
   bukan JSON. Di lab sekolah ituHTML blokir proxy, captive portal, atau
   403 dari WAF. Akibatnya `submit_exam` melaporkan sukses,
   `exam_viewer` menghapus jawaban dari disk, lalu siswa diberi notifikasi
   "Terkumpul" — padahal server tidak menerima apa pun.

2. Config yang gagal ditulis melempar
   `set()` dan `clear_answers()` dipanggil dari slot Qt. Exception dari
   sana jadi `qFatal()` lalu SIGABRT -- aplikasi mati, dan pada kasus
   clear_answers itu terjadi SETELAH jawaban sampai server, jadi siswa
   tidak melihat konfirmasi dan tetap tampil ONLINE di dashboard.

3. `PermissionError` adalah kondisi nyata, bukan teoretis: jawaban ditulis
   ulang tiap ~500ms, Defender memindai file yang baru ditulis dan
   memegang handle beberapa ratus milidetik, dan `unlink()` di Windows
   tidak bisa menghapus file yang sedang dibuka. OneDrive/Dropbox di lab
   juga mengunci %USERPROFILE%\\.config.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import api, config

HTML_BLOCK_PAGE = (
    "<html><head><title>Access Denied</title></head>"
    "<body>Blocked by school proxy</body></html>"
)


class _FakeResponse:
    """Respons urlopen yang mengembalikan body tidak_valid."""

    def __init__(self, body: bytes):
        self._body = body

    def read(self):
        return self._body

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def _respond_with(body: bytes):
    """Patch _pdf_opener supaya _make_request dapat body itu.

    Sekarang _make_request memakai _pdf_opener().open() (bukan
    urllib.request.urlopen) -- lihat audit HIGH H17. Mock _pdf_opener
    untuk mengembalikan objek opener yang .open() kembalikan _FakeResponse.
    """
    opener = mock.MagicMock()
    opener.open.return_value = _FakeResponse(body)
    return mock.patch.object(
        api, "_pdf_opener",
        return_value=opener,
    )


class NonJsonResponseIsNotSuccessTest(unittest.TestCase):
    """Response yang bukan JSON = kita tidak bicara dengan server EXAMVAN."""

    def _get(self, body: bytes):
        with _respond_with(body):
            return api._make_request("https://exam.example/x", method="GET")

    def test_html_block_page_is_a_failure(self):
        out = self._get(HTML_BLOCK_PAGE.encode("utf-8"))
        self.assertFalse(
            out.get("success"),
            "halaman blokir proxy bukan jawaban server -- melaporkannya "
            "sukses membuat jawaban dihapus dan siswa diberi kabar "
            '"Terkumpul" padahal server tidak menerima apa pun',
        )
        self.assertEqual(out.get("error_code"), "non_json_response")

    def test_the_actual_body_is_carried_for_diagnosis(self):
        # Tanpa isi respons, tidak ada yang bisa melihat bahwa yang
        # memblokir adalah proxy sekolah.
        out = self._get(HTML_BLOCK_PAGE.encode("utf-8"))
        self.assertIn("proxy", out.get("message", "").lower() + " "
                      + out.get("raw", "").lower())

    def test_health_check_does_not_report_healthy(self):
        # check_health True saat jaringan diblokir berarti dialog
        # konfigurasi tetap hijau padahal tidak ada server.
        with _respond_with(HTML_BLOCK_PAGE.encode("utf-8")):
            health = api.check_health("https://exam.example")
        self.assertFalse(
            health.success,
            "check_health tidak boleh True kalau responsnya bukan JSON",
        )

    def test_binary_garbage_does_not_raise(self):
        # byte non-UTF-8 pernah membuat .decode("utf-8") melempar
        # UnicodeDecodeError yang keluar dari thread submit --
        # dan itu mematikan thread tanpa pernah menyalakan sinyal hasil,
        # sehingga tombol submit macet selamanya.
        out = self._get(b"\xff\xfe\x00\x00binary")
        self.assertFalse(out.get("success"))
        self.assertEqual(out.get("error_code"), "non_json_response")

    def test_real_json_is_still_parsed(self):
        # Jangan sampai perbaikannya kemakan: JSON valid harus tetap jalan.
        out = self._get(b'{"success": true, "status": "approved"}')
        self.assertTrue(out["success"])
        self.assertEqual(out["status"], "approved")

    def test_submit_reports_failure_on_a_block_page(self):
        # Jalur yang paling merusak: submit yang "berhasil" menghapus
        # jawaban lokal.
        with _respond_with(HTML_BLOCK_PAGE.encode("utf-8")):
            resp = api.submit_exam("https://exam.example", 1, "Andi", "N01", "9A",
                                   {"1": "A"}, "2026-01-01T00:00:00Z",
                                   "DESKTOP:hash", {"nama": "Andi"},
                                   token="ABCD1234")
        self.assertFalse(resp.success)
        self.assertIsNone(resp.score)


class ConfigWriteResilienceTest(unittest.TestCase):
    """Config dan file jawaban: gagal adalah kondisi nyata, bukan error."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="examvan-cfg-")
        self._dir, self._file, self._cache = config._CONFIG_DIR, config._CONFIG_FILE, config._cache
        config._CONFIG_DIR = Path(self.tmp)
        config._CONFIG_FILE = Path(self.tmp) / "config.json"
        config._cache = None

    def tearDown(self):
        os.chmod(self.tmp, 0o755)
        config._CONFIG_DIR, config._CONFIG_FILE, config._cache = self._dir, self._file, self._cache
        shutil.rmtree(self.tmp, ignore_errors=True)

    def test_set_does_not_raise_when_the_file_cannot_be_written(self):
        os.chmod(self.tmp, 0o555)
        try:
            config.set("exam_token", "ABCD1234")   # tidak boleh melempar
        finally:
            os.chmod(self.tmp, 0o755)

    def test_clear_answers_does_not_raise_when_the_file_cannot_be_removed(self):
        # Direktorinya dibuat tidak bisa ditulis, jadi unlink gagal.
        (config._CONFIG_DIR / "answers_9.dat").write_text("{}")
        os.chmod(self.tmp, 0o555)
        try:
            config.clear_answers(9)                  # tidak boleh melempar
        finally:
            os.chmod(self.tmp, 0o755)

    def test_clear_answers_still_removes_the_file_when_it_can(self):
        # Perbaikannya tidak boleh membuat clear_answers menjadi tidak
        # melakukan apa-apa: file yatim berarti recovery lama bisa terbaca
        # dan ikut terkirim.
        target = config._CONFIG_DIR / "answers_9.dat"
        target.write_text("{}")
        config.clear_answers(9)
        self.assertFalse(
            target.exists(),
            "file jawaban harus terhapus saat memang bisa dihapus",
        )

    def test_save_answers_still_writes(self):
        config.set("exam_token", "ABCD1234")
        config.save_answers(9, {"1": "A"})
        self.assertEqual(config.load_answers(9), {"1": "A"})


if __name__ == "__main__":
    unittest.main()
