"""MEDIUM — token kelas kelas ini masuk ke `access_log` nginx lewat `/ws/`.

Bug
---
`ws.py` membuka WS sebagai

    ws://host/ws/7?token=ABCD1234

karena `QWebSocket` tidak bisa memasang header request arbitrer. Di sisi
proxy, `nginx.conf` punya `access_log /dev/stdout;` (format `combined`,
yang memuat `$request` = method + URI + query), dan blok `location /ws/`
TIDAK punya `access_log off` — berbeda dengan `location = /api/health`
(`:126`) dan `location = /healthz` (`:135`) yang sudah sengaja
mematikan lognya.

Jadi setiap connect dan setiap reconnect (backoff eksponensial sampai 30 s,
selama ujian berjalan) menulis `GET /ws/7?token=ABCD1234` ke access log
proxy dalam bentuk apa adanya. Kredensial yang lebih sensitif dari
`identity_data` yang sengaja dibuang dari query `/result` justru yang
masuk ke sini — `api.exam_result` menolak menaruh identitas di URL dengan
alasan "URL tercatat apa adanya oleh setiap proxy di jalur sekolah".

Test di bawah membaca `nginx.conf` sebagai teks (bukan menjalankannya):
yang perlu dikunci adalah KONTRAK konfigurasi, dan `nginx -t` dijalankan
terpisah sebagai verifikasi sintaks.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

NGINX_CONF = (
    Path(__file__).resolve().parents[2] / "webui" / "nginx" / "nginx.conf"
)


def _block_after(text: str, marker: str) -> str:
    """Isi blok `{ ... }` yang dimulai di baris `marker`."""
    start = text.index(marker)
    open_brace = text.index("{", start)
    depth = 0
    for i in range(open_brace, len(text)):
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
            if depth == 0:
                return text[open_brace:i + 1]
    raise AssertionError(f"blok untuk {marker!r} tidak tertutup")


def _directives(block: str) -> str:
    """Blok tanpa komentar.

    Komentar di dalam blok sah mentioning `$request` (justru itu explains
    kenapa logging dimatikan), jadi yang diperiksa hanya direktifnya.
    """
    return re.sub(r"#[^\n]*", "", block)


class WsLocationDoesNotLogTheTokenTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.src = NGINX_CONF.read_text(encoding="utf-8")
        cls.ws_block = _block_after(cls.src, "location /ws/")

    def test_the_ws_location_turns_access_logging_off(self):
        self.assertRegex(
            self.ws_block, r"access_log\s+off\s*;",
            "location /ws/ tidak mematikan access_log — query string "
            "berisi token kelas masuk ke log proxy apa adanya pada setiap "
            "connect dan setiap reconnect",
        )

    def test_no_custom_log_format_re_introduces_the_query(self):
        # `log_format` dengan `$request`/`$request_uri`/`$args` di blok
        # /ws/ akan mengembalikan kebocoran meski access_log off ada.
        directives = _directives(self.ws_block)
        for variable in ("$request", "$request_uri", "$args", "$query_string"):
            self.assertNotIn(
                variable, directives,
                f"{variable} di blok /ws/ menulis query string (token) ke "
                "log, `access_log off` jadi tidak ada artinya",
            )

    def test_the_other_sensitive_locations_are_still_quiet(self):
        # existing behavior yang harus tetap: /api/health dan /healthz.
        for marker in ("location = /api/health", "location = /healthz"):
            block = _block_after(self.src, marker)
            self.assertRegex(block, r"access_log\s+off\s*;", marker)

    def test_the_conf_file_is_the_one_the_deploy_actually_uses(self):
        compose = (Path(__file__).resolve().parents[2] / "webui"
                   / "docker-compose.yml")
        if not compose.exists():
            self.skipTest("docker-compose.yml tidak ada di repo ini")
        text = compose.read_text(encoding="utf-8")
        self.assertRegex(
            text, re.escape(str(NGINX_CONF.name)),
            "compose tidak merujuk nginx.conf yang sama — konfigurasi yang "
            "diperbaiki di sini mungkin tidak yang dipakai",
        )


class WsUrlIsTheOnlyCredentialPathTestCase(unittest.TestCase):
    """Klien tidak boleh "memperbaiki" query token lalu merusak auth."""

    def test_the_server_still_accepts_the_query_token(self):
        # Justru kalau main.go BERHENTI membaca `c.Query("token")`, klien
        # tidak boleh diam-diam tetap mengirimnya thinking-nya tidak berarti
        # apa-apa. Test ini mengunci urutan prioritas header-then-query di
        # server supaya keputusan itu disengaja, bukan tak sengaja.
        main_go = (Path(__file__).resolve().parents[2] / "webui"
                   / "cmd" / "server" / "main.go").read_text(encoding="utf-8")
        # Jangkar ke BARIS KODE, bukan literal yang muncul di komentar
        # mana pun: komentar yang menjelaskan mengapa token ada di URL
        # juga menyebut `c.Query("token")`, dan `str.index` akan menemukan
        # komentar itu lebih dulu sehingga tesnya mengukur dokumentasi,
        # bukan urutan auth.
        header_at = main_go.index('token := c.GetHeader("X-Exam-Token")')
        query_at = main_go.index('token = c.Query("token")')
        self.assertLess(
            header_at, query_at,
            "server tidak lagi memprioritaskan header di atas query — "
            "urutan auth berubah dan klien harus disesuaikan",
        )


if __name__ == "__main__":
    unittest.main()