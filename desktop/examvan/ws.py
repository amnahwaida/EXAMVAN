"""WebSocket client for real-time exam events (exam_terminated, etc.).

Mirror of Android's WebSocketManager: connects to /ws/:examId with the exam
token (query param — QWebSocket cannot set custom headers), receives
SocketIO-style messages and auto-reconnects with exponential backoff.

The socket is RECEIVE-ONLY for token-authenticated clients (the server hub
ignores mutating events from non-privileged sockets), so presence stays on
HTTP access-log (see exam_viewer._send_access_log) — exactly like Android.
"""

from __future__ import annotations

import json
import logging
import random
import time
import urllib.parse
from typing import Any, Callable, Dict, Optional

from PyQt5.QtCore import QObject, QTimer, QUrl, pyqtSignal
from PyQt5.QtWebSockets import QWebSocket

log = logging.getLogger(__name__)

# Reconnect backoff (mirror Android RECONNECT_BASE/MAX_DELAY_MS).
_RECONNECT_BASE_MS = 1000
_RECONNECT_MAX_MS = 30000


class ExamWebSocket(QObject):
    """QObject wrapper around QWebSocket with auto-reconnect + event parsing."""

    # Emitted when the server sends an event: (event_name, payload_dict).
    event_received = pyqtSignal(str, dict)

    def __init__(self, parent=None):
        super().__init__(parent)
        self._ws: Optional[QWebSocket] = None
        self._should_reconnect = False
        self._reconnect_attempts = 0
        self._base_url = ""
        self._exam_id = 0
        self._token = ""
        self._reconnect_timer = QTimer(self)
        self._reconnect_timer.setSingleShot(True)
        self._reconnect_timer.timeout.connect(self._do_connect)
        # Waktu koneksi stabil (monotonic): reset counter hanya bila
        # koneksi bertahan >= 10 detik (lihat _on_disconnected).
        self._connect_time_mono: Optional[float] = None

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def connect(self, base_url: str, exam_id: int, token: str) -> None:
        """Start the WebSocket session (connect + auto-reconnect)."""
        # config.json ada di jangkauan tulis siswa (audit 30 Sep 2026):
        # "server_url": null dulu melempar AttributeError di rstrip, dan
        # "exam_token": null terkirim ke server sebagai kredensial literal
        # "None". Koersi di sini menutup keduanya; URL/id yang tidak sah
        # tidak boleh memulai loop reconnect.
        self._base_url = str(base_url or "").rstrip("/")
        try:
            self._exam_id = int(exam_id)
        except (TypeError, ValueError):
            self._exam_id = 0
        self._token = str(token or "").strip()
        self._reconnect_attempts = 0
        if not self._base_url or self._exam_id <= 0:
            log.warning(
                "WS connect dilewati: base URL / exam id tidak sah "
                "(%r, %r)", base_url, exam_id,
            )
            self._should_reconnect = False
            return
        self._should_reconnect = True
        self._do_connect()

    def disconnect(self) -> None:
        """Stop the session and close the socket.

        Catatan audit (30 Sep 2026): `self._ws.disconnect()` TANPA argumen
        bukan penutup jaringan — itu `QObject.disconnect()`, pelepas
        sambungan sinyal, dan melempar TypeError bila salah satu lepasan
        gagal (repro nyata: "disconnect() of all signals failed"). Saat itu
        terjadi, abort()/deleteLater() di bawahnya melompat dan socket
        tetap terbuka sampai proses mati. Yang benar:

        * sinyal `disconnected` TIDAK dilepas manual — socket dibuang utuh
          lewat deleteLater(), jadi melepasnya hanya menambah jalur gagal;
        * penutupan jaringan = abort(): `_should_reconnect` sudah False
          lebih dulu, jadi sinyal disconnected yang terpicu tidak akan
          menjadwalkan reconnect hantu.
        """
        self._should_reconnect = False
        self._reconnect_timer.stop()
        ws = self._ws
        self._ws = None
        if ws is not None:
            try:
                ws.abort()
            except RuntimeError:
                pass  # objek C++ sudah dihapus (deleteLater ganda, dll.)
            try:
                ws.deleteLater()
            except RuntimeError:
                pass

    def is_connected(self) -> bool:
        return self._ws is not None and self._ws.state() == QWebSocket.ConnectedState

    # ------------------------------------------------------------------
    # Internals
    # ------------------------------------------------------------------

    def _do_connect(self) -> None:
        if not self._should_reconnect:
            return

        scheme = "wss" if self._base_url.startswith("https") else "ws"
        host = self._base_url.replace("https://", "").replace("http://", "")
        url = QUrl(f"{scheme}://{host}/ws/{self._exam_id}?token={urllib.parse.quote(self._token or '', safe='')}")
        log.info("WS connecting to /ws/%s", self._exam_id)

        if self._ws is not None:
            self._ws.deleteLater()
        self._ws = QWebSocket()
        self._ws.connected.connect(self._on_connected)
        self._ws.disconnected.connect(self._on_disconnected)
        self._ws.error.connect(self._on_error)
        self._ws.textMessageReceived.connect(self._on_text_message)
        self._ws.open(url)

    def _on_connected(self) -> None:
        # Catat waktu koneksi; counter HANYA di-reset bila koneksi
        # terbukti stabil (>= 10 dtk) saat disconnect. Reset langsung di
        # sini membuat flapping (putus tiap 2 dtk) tidak pernah menaikkan
        # backoff.
        self._connect_time_mono = time.monotonic()
        log.info("WS connected to /ws/%s", self._exam_id)

    def _on_disconnected(self) -> None:
        log.info("WS disconnected from /ws/%s", self._exam_id)
        now = time.monotonic()
        if (
            self._connect_time_mono is not None
            and (now - self._connect_time_mono) >= 10.0
        ):
            self._reconnect_attempts = 0
        self._connect_time_mono = None
        if self._should_reconnect:
            self._schedule_reconnect()

    def _on_error(self, error) -> None:
        # QWebSocket emits error (old signal) — log and let the disconnected
        # path handle reconnection.
        log.warning("WS error: %s", error)

    def _schedule_reconnect(self) -> None:
        delay = min(_RECONNECT_BASE_MS * (2 ** self._reconnect_attempts), _RECONNECT_MAX_MS)
        # Jitter: campur ±25% agar banyak klien yang drop sama-sama tidak
        # menghasilkan gempa bumi reconnect yang membebani server.
        jitter = random.randint(-delay // 4, delay // 4)
        delay = max(delay + jitter, _RECONNECT_BASE_MS)
        # Cap counter supaya 2**attempts tidak tumbuh tanpa batas pada
        # flapping panjang (delay sendiri sudah di-cap MAX).
        self._reconnect_attempts = min(self._reconnect_attempts + 1, 12)
        log.info("WS reconnecting in %d ms (attempt %d)", delay, self._reconnect_attempts)
        self._reconnect_timer.start(delay)

    def _on_text_message(self, text: str) -> None:
        try:
            parsed = parse_socketio_message(text)
        except Exception as e:
            log.warning("WS unparseable message: %s", e)
            return
        if parsed is not None:
            event, payload = parsed
            log.info("WS event: %s", event)
            self.event_received.emit(event, payload)


def parse_socketio_message(text: str) -> Optional[tuple]:
    """Parse a SocketIO-style message into (event, payload) or None.

    Accepts the wire formats the EXAMVAN hub sends:
      ["event", {...}]                  — raw JSON array
      42["event", {...}]                — legacy SocketIO event frame
    Returns None for non-event frames (ping, ack, etc.).
    """
    raw = text.strip()
    if not raw:
        return None
    if raw.startswith("42"):
        raw = raw[2:]
    if not raw.startswith("["):
        return None
    arr = json.loads(raw)
    # Kontrak wire: ["event_name", {payload_dict}] — event HARUS string,
    # payload HARUS objek JSON. Frame lain (mis. [1,2,3] atau event tanpa
    # payload) bukan event yang dikirim hub dan diabaikan.
    if not isinstance(arr, list) or len(arr) < 2:
        return None
    event = arr[0]
    payload = arr[1]
    if not isinstance(event, str) or not isinstance(payload, dict):
        return None
    return event, payload
