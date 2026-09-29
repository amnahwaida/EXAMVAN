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

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def connect(self, base_url: str, exam_id: int, token: str) -> None:
        """Start the WebSocket session (connect + auto-reconnect)."""
        self._base_url = base_url.rstrip("/")
        self._exam_id = exam_id
        self._token = token
        self._should_reconnect = True
        self._reconnect_attempts = 0
        self._do_connect()

    def disconnect(self) -> None:
        """Stop the session and close the socket.

        `self._ws.disconnected.disconnect(self._on_disconnected)` first, so
        the abort below cannot schedule a reconnect. This used to read
        `self._ws.disconnect(self._ws.connected)` — `connected` is a signal,
        not a method, so that call did nothing at all and the handler stayed
        wired. Harmless while `_should_reconnect` is cleared beforehand, but
        it meant `disconnect()` never actually disconnected.
        """
        self._should_reconnect = False
        self._reconnect_timer.stop()
        if self._ws is not None:
            try:
                self._ws.disconnected.disconnect(self._on_disconnected)
            except (TypeError, RuntimeError):
                pass  # sudah tidak terhubung, atau handler tidak terpasang
            self._ws.disconnect()
            self._ws.abort()
            self._ws.deleteLater()
            self._ws = None

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
        url = QUrl(f"{scheme}://{host}/ws/{self._exam_id}?token={self._token}")
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
        self._reconnect_attempts = 0
        log.info("WS connected to /ws/%s", self._exam_id)

    def _on_disconnected(self) -> None:
        log.info("WS disconnected from /ws/%s", self._exam_id)
        if self._should_reconnect:
            self._schedule_reconnect()

    def _on_error(self, error) -> None:
        # QWebSocket emits error (old signal) — log and let the disconnected
        # path handle reconnection.
        log.warning("WS error: %s", error)

    def _schedule_reconnect(self) -> None:
        delay = min(_RECONNECT_BASE_MS * (2 ** self._reconnect_attempts), _RECONNECT_MAX_MS)
        self._reconnect_attempts += 1
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
