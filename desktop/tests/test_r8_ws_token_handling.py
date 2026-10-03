"""MEDIUM — kredensial token kelas di URL WebSocket: apa yang bisa, apa yang tidak.

Bug / temuan
---
`ws.py` membuka `ws://host/ws/<id>?token=<token kelas>`. Alasannya jujur
dan tidak bisa dihapus di sisi klien: `QWebSocket` (Qt5) TIDAK bisa
memasang header request arbitrer — `QNetworkAccessManager.createRequest`
adalah virtual C++, tidak bisa di-override dari Python. Server
(`webui/cmd/server/main.go`) membaca header `X-Exam-Token` lebih dulu,
lalu baru `c.Query("token")`, jadi menghapus query token dari klien tanpa
perubahan server akan mematikan sinkronisasi realtime (dan dengan itu
auto-submit saat ujian dihentikan pengawas).

Jadi perbaikan yang benar ada di sisi yang LAIN:

  * proxy (`webui/nginx/nginx.conf`, jalur test
    `test_r8_nginx_ws_token_log.py`) — `access_log off` untuk `/ws/`;
  * origin (`cmd/server/main.go`, BUKAN berkas milik ronde ini) —
    `gin.Logger()` masih menempelkan query string ke path, dan header
    `X-Exam-Token` pun belum dibaca sebagai alternatif yang tidak logged.

Yang diuji di sini adalah batas yang JELAAS dari sisi klien:

  1. token tidak pernah ditulis ke log klien sendiri (termasuk URL penuh);
  2. URL dibangun di SATU tempat, dan persis seperti kontrak server;
  3. membangun URL tidak menyentuh timer reconnect maupun counter
     backoff — perubahan URL tidak boleh menambah atau me-reset timer
     (gelombang "reconnect storm" yang sudah diverifikasi bebas).

Catatan jujur: test-test di kelas ini sebagian besar sudah hijau sebelum
perubahan apa pun; yang benar-benar berubah di ronde ini ada di
`nginx.conf` dan di dokumen yang menyertai.
"""

from __future__ import annotations

import logging
import unittest
import urllib.parse
from unittest import mock

from PyQt5.QtCore import QCoreApplication, QUrl

from examvan import ws as ws_mod
from examvan.ws import ExamWebSocket

TOKEN = "CLASS7TOKEN"


def _app():
    if QCoreApplication.instance() is None:
        return QCoreApplication([])
    return QCoreApplication.instance()


class ClientNeverLogsTheTokenTestCase(unittest.TestCase):
    def setUp(self):
        _app()
        self.ws = ExamWebSocket()
        self.addCleanup(self.ws.disconnect)
        self.records = []

        class _Capture(logging.Handler):
            def emit(inner, record):  # noqa: N805 — handler, bukan method
                self.records.append(record)

        self.handler = _Capture()
        ws_mod.log.addHandler(self.handler)
        ws_mod.log.setLevel(logging.DEBUG)
        self.addCleanup(ws_mod.log.removeHandler, self.handler)

    def _connect(self):
        with mock.patch.object(ws_mod, "QWebSocket") as sock:
            self.ws.connect("https://exam.example", 42, TOKEN)
        return sock.return_value

    def test_the_class_token_never_reaches_the_client_log(self):
        self._connect()
        rendered = "\n".join(
            r.getMessage() for r in self.records
        )
        self.assertTrue(rendered, "kontrol: harus ada log yang terekam")
        self.assertNotIn(
            TOKEN, rendered,
            "token kelas masuk ke log klien. `--windowed` membuang stderr "
            "dan log ini dibaca teknisi lab, jadi kebocoran di sini sama "
            "seburuknya ada di proxy",
        )

    def test_the_built_url_matches_the_server_contract(self):
        self._connect()
        url = self.ws._ws_url()
        self.assertEqual(url.path(), "/ws/42")
        self.assertEqual(
            urllib.parse.unquote(url.query()), f"token={TOKEN}",
            "server membaca `c.Query(\"token\")` — kalau query-nya berubah, "
            "WS tidak akan pernah terautentikasi",
        )

    def test_a_token_with_url_metacharacters_is_percent_encoded(self):
        # Token dari QLineEdit bisa berisi apa saja di config.json yang
        # boleh ditulis siswa; tanpa quoting, `&` memotong query.
        weird = "A&B=C D"
        with mock.patch.object(ws_mod, "QWebSocket"):
            self.ws.connect("https://exam.example", 42, weird)
        url = self.ws._ws_url()
        # `query()` tanpa FullyEncoded mengembalikan %20 sebagai spasi;
        # yang benar-benar dikirim ke jaringan adalah bentuk `toEncoded()`.
        self.assertEqual(
            url.query(QUrl.FullyEncoded),
            f"token={urllib.parse.quote(weird, safe='')}",
        )
        self.assertIn(b"token=A%26B%3DC%20D", bytes(url.toEncoded()))
        self.assertEqual(
            dict(urllib.parse.parse_qsl(url.query(), keep_blank_values=True)),
            {"token": weird},
        )


class UrlBuildingTouchesNoTimerTestCase(unittest.TestCase):
    """Membangun URL tidak boleh menambah atau me-reset timer reconnect."""

    def setUp(self):
        _app()
        self.ws = ExamWebSocket()
        self.addCleanup(self.ws.disconnect)
        self.ws._base_url = "https://exam.example"
        self.ws._exam_id = 42
        self.ws._token = TOKEN
        self.ws._should_reconnect = True
        self.ws._reconnect_attempts = 7

    def test_building_the_url_leaves_the_backoff_counter_alone(self):
        self.ws._ws_url()
        self.assertEqual(self.ws._reconnect_attempts, 7)

    def test_building_the_url_leaves_the_timer_untouched(self):
        with mock.patch.object(self.ws._reconnect_timer, "start") as start, \
             mock.patch.object(self.ws._reconnect_timer, "stop") as stop:
            for _ in range(5):
                self.ws._ws_url()
        start.assert_not_called()
        stop.assert_not_called()
        self.assertFalse(
            self.ws._reconnect_timer.isActive(),
            "satu timer reconnect, tidak pernah di-starter oleh pembuatan URL",
        )

    def test_two_url_builds_produce_one_timer_and_one_socket(self):
        # `_do_connect` mengulang setiap reconnect; kalau URL ikut
        # memulai timer, tiap disconnect menambah satu timer paralel.
        sockets = []

        def _make_sock():
            sock = mock.MagicMock()
            sockets.append(sock)
            return sock

        with mock.patch.object(ws_mod, "QWebSocket", _make_sock), \
             mock.patch.object(self.ws._reconnect_timer, "start") as start:
            self.ws._do_connect()
            self.ws._do_connect()
        self.assertEqual(len(sockets), 2, "socket lama harus dibuang, bukan ditumpuk")
        self.assertEqual(start.call_count, 0)
        self.assertEqual(self.ws._reconnect_attempts, 7)


class NoClientSideCredentialUpgradeTestCase(unittest.TestCase):
    """Klien tidak boleh 'memperbaiki' kredensial tanpa dukungan server."""

    def test_no_subprotocol_or_cookie_is_offered_to_the_server(self):
        # Kalau suatu saat ada yang menambahkan subprotocol/cookie, server
        # TIDAK membacanya (main.go hanya `GetHeader` lalu `Query`), dan
        # auth akan gagal diam-diam. Test ini memaksa perubahan itu disengaja.
        with mock.patch.object(ws_mod, "QWebSocket") as sock:
            self._ws = ExamWebSocket()
            self.addCleanup(self._ws.disconnect)
            self._ws.connect("https://exam.example", 42, TOKEN)
        for call in sock.return_value.method_calls:
            self.assertNotIn(
                "setSubprotocols", call[0],
                "subprotocol dikirim tapi `cmd/server/main.go` tidak "
                "membacanya — auth WS akan gagal",
            )
        self.assertTrue(
            self._ws._ws_url().query().startswith("token="),
            "satu-satunya jalur kredensial yang didukung server saat ini "
            "adalah query string",
        )


if __name__ == "__main__":
    unittest.main()