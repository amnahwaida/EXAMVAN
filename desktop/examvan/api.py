"""HTTP client for EXAMVAN API (urllib.request, no external deps)."""

from __future__ import annotations

import io
import json
import logging
import random
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable, Dict, Optional, Tuple

from . import APP_VERSION
from .models import Exam, HealthResponse, SubmitResponse, TokenExamResponse
from .utils import map_identity_to_standard

log = logging.getLogger(__name__)

# Platform label for User-Agent
_PLATFORM = "Windows" if sys.platform == "win32" else "Linux"

# Server time skew (ms) — selisih jam perangkat vs server, dihitung dari
# `server_time_utc` pada GET /api/health (mirror ApiClient.serverTimeSkewMs
# di Android). Dipakai countdown deadline agar akurat walau jam perangkat
# meleset dari jam server.
_server_skew_ms: int = 0


def get_server_skew_ms() -> int:
    """Return the current server time skew in milliseconds."""
    return _server_skew_ms


def set_server_skew_ms(skew_ms: int) -> None:
    """Set the server time skew (ms). Terutama untuk test."""
    global _server_skew_ms
    _server_skew_ms = skew_ms


def _make_request(
    url: str,
    method: str = "GET",
    headers: Optional[Dict[str, str]] = None,
    body: Optional[Dict[str, Any]] = None,
    timeout: int = 30,
) -> Dict[str, Any]:
    """Execute HTTP request and return parsed JSON.

    Handles both JSON and plain-text responses gracefully.
    """
    hdrs = {
        "User-Agent": f"EXAMVAN-{_PLATFORM}/{APP_VERSION}",
        "Accept": "application/json",
    }
    if headers:
        hdrs.update(headers)

    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        hdrs["Content-Type"] = "application/json; charset=utf-8"

    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8", errors="replace").strip()
        # Try JSON first.
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            # BUKAN sukses. Response non-JSON hampir selalu berarti kita
            # tidak bicara dengan server EXAMVAN sama sekali: halaman blok
            # proxy/captive portal, 403 dari WAF, atau login page.
            #
            # Versi sebelumnya mengembalikan success=True di sini, dan itu
            # merusak dua hal sekaligus:
            #   * check_health melaporkan True saat jaringan diblokir
            #   * submit_exam melaporkan sucesso, exam_viewer menghapus
            #     jawaban dari disk, lalu siswa diberi notifikasi "Terkumpul"
            #     -- padahal server tidak menerima apa pun
            #
            # Jawabannya sekarang failure yang jujur, dan teks aslinya
            # dibawa supaya diagnosis (log, atau pesan ke siswa) bisa
            # menyebutkan apa yang sebenarnya terjadi.
            return {
                "success": False,
                "error_code": "non_json_response",
                "status": "error",
                "message": (
                    "Server tidak mengembalikan JSON yang valid. "
                    "Kemungkinan jaringan diblokir proxy sekolah, captive "
                    "portal, atau WAF. Isi respons: " + raw[:200]
                ),
                "raw": raw[:2000],
            }


def _url_join(base: str, path: str) -> str:
    """Join base URL and path, handling trailing/leading slashes."""
    return base.rstrip("/") + "/" + path.lstrip("/")


# ---------------------------------------------------------------------------
# Health check
# ---------------------------------------------------------------------------


def compute_server_skew_ms(server_time_utc: Optional[str], device_now_ms: int) -> Optional[int]:
    """Return skew (server - device) in ms, or None when unparseable.

    Murni & bisa diuji: dipakai check_health untuk mengisi module-level skew.
    """
    if not server_time_utc:
        return None
    try:
        from datetime import datetime, timezone
        server = datetime.fromisoformat(server_time_utc.replace("Z", "+00:00"))
        device_now = datetime.fromtimestamp(device_now_ms / 1000.0, tz=timezone.utc)
        return int((server - device_now).total_seconds() * 1000)
    except Exception:
        return None


def check_health(base_url: str) -> HealthResponse:
    """GET /api/health → HealthResponse.

    Menghitung server time skew dari `server_time_utc` (mirror Android):
    selisih jam server vs perangkat disimpan dan dipakai countdown deadline
    (ElapsedTimerWidget) agar akurat walau jam lokal meleset.
    """
    try:
        data = _make_request(_url_join(base_url, "/api/health"), timeout=10)
        resp = HealthResponse.from_json(data)
        skew = compute_server_skew_ms(resp.server_time_utc, int(time.time() * 1000))
        if skew is not None:
            set_server_skew_ms(skew)
        return resp
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        return HealthResponse(success=False, status=f"Error: {e}")


# ---------------------------------------------------------------------------
# Exam lookup by token
# ---------------------------------------------------------------------------


def get_exam_by_token(base_url: str, token: str) -> TokenExamResponse:
    """GET /api/exams/token/{token} → TokenExamResponse."""
    try:
        data = _make_request(
            _url_join(base_url, f"/api/exams/token/{token}"),
            headers={"X-App-Version": APP_VERSION},
            timeout=15,
        )
        return TokenExamResponse.from_json(data)
    except urllib.error.HTTPError as e:
        try:
            body = json.loads(e.read().decode("utf-8"))
            return TokenExamResponse.from_json(body)
        except Exception:
            return TokenExamResponse(
                success=False, error=str(e), message=f"HTTP {e.code}"
            )
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        return TokenExamResponse(success=False, error=str(e), message=str(e))


# ---------------------------------------------------------------------------
# PDF download
# ---------------------------------------------------------------------------


# Header yang hanya boleh dikirim ke SERVER UJIAN.
#
# Endpoint PDF menjawab 302 ke signed URL di origin storage (R2).
# `urllib.request.HTTPRedirectHandler.redirect_request` menyalin SEMUA
# custom header ke request berikutnya, jadi tanpa handler di bawah
# `X-Exam-Token` (kredensial seluruh kelas pada mode static) dan
# `X-Device-Id` ikut terkirim ke origin yang berbeda, mendarat di access
# log sana, dan digabung dengan signed URL dalam satu request.
#
# Yang memeriksa header itu hanya exams.go di origin pertama, jadi "sudah
# lolos gate" terasa aman padahal tidak.
_REDIRECT_SENSITIVE_HEADERS = frozenset(
    {"x-exam-token", "x-device-id", "authorization", "cookie"}
)


class _ExamOnlyRedirectHandler(urllib.request.HTTPRedirectHandler):
    """Buang kredensial kami saat redirect, pertahankan yang netral."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        new = super().redirect_request(req, fp, code, msg, headers, newurl)
        if new is None:
            return None
        kept = {
            k: v for k, v in new.headers.items()
            if k.lower() not in _REDIRECT_SENSITIVE_HEADERS
        }
        # Header map Request bisa dimodifikasi lewat .headers, jadi cukup
        # dibersihkan di tempat.
        new.headers.clear()
        new.headers.update(kept)
        return new


_PDF_OPENER = None


def _pdf_opener():
    """Opener yang memakai `_ExamOnlyRedirectHandler`."""
    global _PDF_OPENER
    if _PDF_OPENER is None:
        _PDF_OPENER = urllib.request.build_opener(_ExamOnlyRedirectHandler())
    return _PDF_OPENER


def download_pdf(
    base_url: str,
    exam_id: int,
    token: str,
    dest_path: str,
    progress_cb: Optional[Callable[[int, int], None]] = None,
    device_id: str = "",
) -> str:
    """GET /api/exams/{exam_id}/pdf → save to dest_path. Returns path on success.

    The server's approval gate (14 Agustus 2026) serves the PDF only to
    devices holding an approved exam_approvals row; `device_id` must be the
    SAME value used at request-approval time (the device MAC). Passed in the
    X-Device-Id header.

    Args:
        progress_cb: Called with (bytes_read, total_bytes) during download.
                     total_bytes is -1 if Content-Length unknown.
    """
    headers = {
        "X-Exam-Token": token,
        "User-Agent": f"EXAMVAN-{_PLATFORM}/{APP_VERSION}",
    }
    if device_id:
        headers["X-Device-Id"] = device_id
    url = _url_join(base_url, f"/api/exams/{exam_id}/pdf")
    req = urllib.request.Request(
        url,
        headers=headers,
    )

    tmp_path = dest_path + ".tmp"
    try:
        with _pdf_opener().open(req, timeout=120) as resp:
            total = int(resp.headers.get("Content-Length", -1))
            read_bytes = 0
            chunk_size = 65536
            with open(tmp_path, "wb") as f:
                while True:
                    chunk = resp.read(chunk_size)
                    if not chunk:
                        break
                    f.write(chunk)
                    read_bytes += len(chunk)
                    if progress_cb:
                        progress_cb(read_bytes, total)

        # Atomic rename on success
        import os
        os.replace(tmp_path, dest_path)
        return dest_path

    except Exception:
        # Clean up temp file on failure
        import os
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        raise


# ---------------------------------------------------------------------------
# Submit answers
# ---------------------------------------------------------------------------


def submit_exam(
    base_url: str,
    exam_id: int,
    student_name: str,
    exam_number: str,
    student_class: str,
    answers: Dict[str, Any],
    start_time: str,
    mac_address: str,
    identity_data: Optional[Dict[str, Any]] = None,
    token: str = "",
) -> SubmitResponse:
    """POST /api/exams/{exam_id}/submit → SubmitResponse.

    `token` is the exam token this device joined with, sent as X-Exam-Token.
    It is NOT optional in practice: the server rejects the request with 401
    "Token tidak disertakan" before looking at anything else
    (webui/internal/handlers/api/exams.go:1016-1023). It used to be missing
    here, so every submit from this client failed — on Android the same header
    has always been sent (ApiClient.kt:550).

    An empty token fails locally instead of spending four round trips to be
    told the same thing: the 401 is permanent, and submit_with_retry now
    recognises that.
    """
    if not token or not token.strip():
        return SubmitResponse(
            success=False,
            message=(
                "Token ujian tidak tersedia. Keluar dari aplikasi, masukkan "
                "kode token lagi, lalu ulangi."
            ),
            http_status=401,
        )

    # Send identity_data as-is (custom keys like 'nama', 'nomor_ujian').
    # Go backend reads student_name/exam_number/student_class from
    # top-level body fields as fallback, so no need to inject standard keys.
    if not identity_data:
        # Fallback: build identity_data from top-level fields
        fallback = {}
        if student_name:
            fallback["student_name"] = student_name
        if exam_number:
            fallback["exam_number"] = exam_number
        if student_class:
            fallback["student_class"] = student_class
        identity_data = fallback if fallback else None

    body: Dict[str, Any] = {
        "student_name": student_name,
        "exam_number": exam_number,
        "student_class": student_class,
        "answers": answers,
        "start_time": start_time,
        "mac_address": mac_address,
    }
    if identity_data:
        body["identity_data"] = identity_data

    try:
        data = _make_request(
            _url_join(base_url, f"/api/exams/{exam_id}/submit"),
            method="POST",
            headers={"X-Exam-Token": token, "X-App-Version": APP_VERSION},
            body=body,
            timeout=30,
        )
        return SubmitResponse.from_json(data)
    except urllib.error.HTTPError as e:
        try:
            body_data = json.loads(e.read().decode("utf-8"))
            resp = SubmitResponse.from_json(body_data)
        except Exception:
            resp = SubmitResponse(success=False, message=f"HTTP {e.code}: {e.reason}")
        # Keep the code so submit_with_retry can tell a permanent rejection
        # (401/403/404) from a transient one (408/429/5xx, network).
        resp.http_status = e.code
        return resp
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        return SubmitResponse(success=False, message=str(e))


def submit_with_retry(
    base_url: str,
    exam_id: int,
    student_name: str,
    exam_number: str,
    student_class: str,
    answers: Dict[str, Any],
    start_time: str,
    mac_address: str,
    identity_data: Optional[Dict[str, Any]] = None,
    delays: Optional[list] = None,
    on_retry: Optional[Callable[[int, int], None]] = None,
    token: str = "",
) -> SubmitResponse:
    """Submit with exponential backoff retry (1s, 2s, 4s).

    Stops immediately on a permanent rejection. Retrying a 401 is not "more
    robust", it is the student staring at "Submit gagal, percobaan 4/4..."
    for seven seconds before failing anyway — which is exactly how a missing
    token looked in the field.
    """
    if delays is None:
        delays = [1, 2, 4]

    resp = submit_exam(
        base_url, exam_id, student_name, exam_number, student_class,
        answers, start_time, mac_address, identity_data, token,
    )
    if resp.success or not resp.retryable:
        return resp

    max_attempts = len(delays) + 1
    for i, delay in enumerate(delays):
        if on_retry:
            on_retry(i + 2, max_attempts)
        time.sleep(delay)
        resp = submit_exam(
            base_url, exam_id, student_name, exam_number, student_class,
            answers, start_time, mac_address, identity_data, token,
        )
        if resp.success or not resp.retryable:
            return resp

    return resp

# ---------------------------------------------------------------------------
# Presence (access-log & complete)
# ---------------------------------------------------------------------------


def send_access_log(
    base_url: str,
    exam_id: int,
    token: str,
    mac_address: str,
    event: str,
    student_name: str = "",
    exam_number: str = "",
    student_class: str = "",
    device_info: str = "",
    identity_data: Optional[Dict[str, Any]] = None,
) -> bool:
    """POST /api/exams/{exam_id}/access-log → True on success.

    Melaporkan presence siswa (login / heartbeat / logout) — mirror Android
    WebSocketManager.sendHeartbeat + ApiClient.sendAccessLog. Heartbeat
    menulis presence Redis (TTL 5 menit) sehingga siswa tampil ONLINE di
    dashboard monitoring pengawas; login/logout dicatat di tabel access-log.
    Best-effort: kegagalan tidak menggagalkan alur ujian.
    """
    body: Dict[str, Any] = {
        "event": event,
        "mac_address": mac_address,
    }
    if student_name:
        body["student_name"] = student_name
    if exam_number:
        body["exam_number"] = exam_number
    if student_class:
        body["student_class"] = student_class
    if device_info:
        body["device_info"] = device_info
    if identity_data:
        body["identity_data"] = identity_data
    try:
        data = _make_request(
            _url_join(base_url, f"/api/exams/{exam_id}/access-log"),
            method="POST",
            headers={"X-Exam-Token": token, "X-App-Version": APP_VERSION},
            body=body,
            timeout=10,
        )
        return bool(data.get("success", False))
    except (urllib.error.URLError, OSError, json.JSONDecodeError):
        return False


def complete_exam(
    base_url: str,
    exam_id: int,
    token: str,
    mac_address: str,
) -> bool:
    """POST /api/exams/{exam_id}/complete → True on success.

    Menghapus presence Redis siswa (tampil offline segera di dashboard) saat
    ujian selesai — mirror Android WebSocketManager.notifyCompletedViaHttp.
    Best-effort.
    """
    body: Dict[str, Any] = {
        "mac_address": mac_address,
        "token": token,
    }
    try:
        data = _make_request(
            _url_join(base_url, f"/api/exams/{exam_id}/complete"),
            method="POST",
            headers={"X-Exam-Token": token, "X-App-Version": APP_VERSION},
            body=body,
            timeout=10,
        )
        return bool(data.get("success", False))
    except (urllib.error.URLError, OSError, json.JSONDecodeError):
        return False


# ---------------------------------------------------------------------------
# Exam result (async submission poll)
# ---------------------------------------------------------------------------


def exam_result(
    base_url: str,
    exam_id: int,
    token: str,
    mac_address: str,
    job_id: str,
    identity_data: Optional[Dict[str, Any]] = None,
) -> SubmitResponse:
    """GET /api/exams/{exam_id}/result → SubmitResponse.

    Polls the outcome of an async (202 queued) submission. The response
    mirrors the sync submit shape so both paths can be treated uniformly:

      {"success":true, "status":"done", "score":87.5, "message":"..."}
      {"success":true, "status":"pending"}   // queued but not yet processed
      {"success":false, "message":"..."}     // job failed after retries

    The server gate admits the poller via the exam token (X-Exam-Token) —
    same credential used on join — plus the device's own job_id/mac_address.
    """
    params = [
        f"job_id={urllib.parse.quote(job_id)}" if job_id else None,
        f"mac_address={urllib.parse.quote(mac_address)}" if mac_address else None,
    ]
    if identity_data:
        params.append(
            "identity_data=" + urllib.parse.quote(
                json.dumps(identity_data, ensure_ascii=False)
            )
        )
    qs = "&".join(p for p in params if p)
    url = _url_join(base_url, f"/api/exams/{exam_id}/result")
    if qs:
        url += "?" + qs

    try:
        data = _make_request(
            url,
            headers={"X-Exam-Token": token},
            timeout=15,
        )
        return SubmitResponse.from_json(data)
    except urllib.error.HTTPError as e:
        # KODE STATUS HARUS DIBAWA NAIK.
        #
        # Tanpa ini, `poll_queued_result()` melihat `success=False` tanpa
        # HTTP sama sekali -- identik dengan "jaringan putus" -- lalu
        # melanjutkan polling 31 kali. 401 "Token tidak valid",
        # 403 "Waktu ujian telah berakhir" dan 429 semuanya jadi "masih
        # diproses", padahal tiga-tiganya verdict yang tidak berubah dengan
        # mencoba lagi.
        #
        # Untuk satu siswa itu berarti membaca "Gagal mengumpulkan" untuk
        # jawaban yang sudah durable, lalu menekan "Kirim Lagi". Untuk satu
        # ruangan, 24 request/menit per perangkat adalah `resultExamRateLimitMax`
        # = 12000/menit: 500 perangkat yang mulai polling sefase menghabiskan
        # bucket bersama dan siswa lain ikut kena 429.
        code = e.code
        try:
            body_data = json.loads(e.read().decode("utf-8"))
            resp = SubmitResponse.from_json(body_data)
            if resp.http_status is None:
                resp.http_status = code
            return resp
        except Exception:
            return SubmitResponse(
                success=False, message=f"HTTP {code}: {e.reason}",
                http_status=code,
            )
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        # http_status sengaja None: tidak ada HTTP sama sekali, jadi ini
        # kategori "masih dicoba", bukan "ditolak".
        return SubmitResponse(success=False, message=str(e))


# HTTP status yang verdict-nya tidak berubah dengan mencoba lagi.
_PERMANENT_HTTP_STATUS = frozenset({401, 403, 404, 410})


def _sleep_or_give_up(wait: float, deadline: float, spent: list):
    """Sleep `wait` (dengan jitter), atau berhenti kalau budget habis.

    Mengembalikan jeda berikutnya, atau None kalau tidak ada lagi waktu
    untuk menunggu. Memakai daftar satu-elemen untuk `spent` supaya bisa
    diperbarui dari sini tanpa `nonlocal` pada loop pemanggil.
    """
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        return None
    gap = min(wait + random.uniform(0, wait * 0.3), remaining)
    spent[0] += gap
    time.sleep(gap)
    return min(wait * 1.5, gap * 2)


def poll_queued_result(
    base_url: str,
    exam_id: int,
    token: str,
    mac_address: str,
    job_id: str,
    identity_data: Optional[Dict[str, Any]] = None,
    max_attempts: int = 31,
    interval: float = 2.5,
    initial_congrats: str = "",
    deadline_seconds: Optional[float] = None,
) -> SubmitResponse:
    """Poll /result until the queued submission is durable ("done").

    Returns success ONLY on "done"; "pending" keeps polling; a terminal
    failure stops immediately. Mirrors Android's QueuedResultPolling
    contract: the client must NOT clear its local answer copy until the
    worker confirms durability — a raw 202 means the answers are only
    QUEUED, not yet in the database.

    Defaults: 31 attempts × 2.5s ≈ 77s of polling, matching Android's 75s
    deadline. `deadline_seconds` caps the TOTAL wait (default: the same
    77s budget); jitter only shifts a fraction of each individual gap, so
    it breaks lockstep without extending the deadline. Do not raise the
    per-gap cap without re-deriving this number -- students read the
    "menunggu konfirmasi" message during that whole window.
    """
    # Backoff + jitter. Tanpa ini semua perangkat yang dapat 202 pada
    # detik yang sama akan poll pada interval yang sama selamanya, dan
    # rate-limit bersama menjadi masalah yang dibuat oleh klien sendiri.
    # Backoff DIKUNCI DALAM TOTAL BUDGET.
    #
    # Versi sebelumnya memakai cap `interval * 8` per tick, sehingga total
    # sebenarnya menjadi 2.5 + 3.75 + 5.63 + ... + 20 (sejumlah itu) ~= 9
    # menit -- bukan "77s" seperti dijanjikan docstring dan seperti yang
    # dilakukan Android (75 detik). Siswa yang submit-nya tidak pernah
    # terkonfirmasi akan duduk jauh lebih lama dari yang damesingkan, dan
    # burst /result yang awalnya 77 detik jadi tersebar selama 9 menit.
    #
    # Sekarang total waktu tunggu dibatasi `deadline_seconds` (default
    # mengikuti 31 x interval), dan jitter hanya menggeser SEBAGIAN kecil
    # dari jeda itu -- cukup untuk memecah lockstep tanpa memperpanjang
    # deadline.
    if interval <= 0:
        # Tidak ada jeda sama sekali (dipakai test, dan sah sebagai pemanggilan
        # cepat). Deadline tidak relevan karena tidak ada yang menunggu.
        deadline_seconds = None
    elif deadline_seconds is None:
        deadline_seconds = max_attempts * interval
    deadline = (
        time.monotonic() + deadline_seconds
        if deadline_seconds is not None
        else float("inf")
    )
    spent = [0.0]
    wait = interval
    last: Optional[SubmitResponse] = None
    # Pesan ucapan guru diambil dari respons 202 sebelum polling menimpa
    # respons. Lihat catatan di dalam loop.
    previous_congrats = initial_congrats
    for _ in range(max_attempts):
        resp = exam_result(
            base_url, exam_id, token, mac_address, job_id, identity_data
        )
        if resp.status == "done":
            # `congrats_message` HANYA ada di respons 202; `/result`
            # mengirim `message` generik saja. Tanpa-carry ini, objek
            # respons 202 ditimpa dan pesan guru hilang -- padahal
            # `CongratulationsDialog` menjadikan pesan itu seluruh isi
            # headline-nya, jadi fiturnya mati di jalur normal (antrean).
            if previous_congrats and not resp.congrats_message:
                resp.congrats_message = previous_congrats
            return resp

        if not resp.success:
            # Kegagalan TANPA kode HTTP = tidak ada jawaban dari server
            # sama sekali. Ini kontrak lama: hentikan, jangan ulangi.
            if resp.http_status is None:
                return resp
            # Verdikt permanen: mencoba lagi tidak akan mengubah apa pun.
            # Melanjutkan polling hanya menguras token dan quota.
            if resp.http_status in _PERMANENT_HTTP_STATUS:
                log.warning(
                    "/result refused permanently with HTTP %s: %s",
                    resp.http_status, resp.message,
                )
                return resp
            # 429 / 5xx = sementara. Jawabannya mungkin SUDAH durable di
            # server dan kita hanya belum boleh meminta lagi, jadi
            # dilaporkan gagal akan membuat siswa mengirim ulang jawaban
            # yang sudah tersimpan. Lanjut dengan backoff + jitter.
            last = resp
            wait = _sleep_or_give_up(wait, deadline, spent)
            if wait is None:
                break
            continue

        # success=True tapi belum "done": masih diproses worker server.
        last = resp
        wait = _sleep_or_give_up(wait, deadline, spent)
        if wait is None:
            break

    if last is not None and last.http_status is not None:
        # Transient tapi tidak pernah berhasil: jangan diam-diam
        # menyalahkan siswa.
        return SubmitResponse(
            success=False, status="error",
            http_status=last.http_status,
            message=(
                f"Server belum mengonfirmasi (HTTP {last.http_status}). "
                "Jawaban tetap tersimpan di perangkat ini."
            ),
        )
    return SubmitResponse(
        success=False,
        status="timeout",
        message="Jawaban masih diproses server. Jawaban tetap tersimpan di perangkat ini — silakan coba kumpulkan lagi.",
    )


# ---------------------------------------------------------------------------
# Request Approval
# ---------------------------------------------------------------------------

def request_approval(
    base_url: str,
    exam_id: int,
    student_name: str,
    exam_number: str,
    student_class: str,
    identity_data: Dict[str, Any],
    mac_address: str,
    reset: bool = False,
    token: str = "",
):
    """POST /api/exams/request-approval.

    token is the exam token the device joined with — the server rejects
    request-approval without a valid one (anti-spam).
    """
    from .models import RequestApprovalResponse
    body = {
        "exam_id": exam_id,
        "mac_address": mac_address,
        "student_name": student_name,
        "exam_number": exam_number,
        "student_class": student_class,
        "identity_data": identity_data,
        "reset": reset,
        "token": token,
    }
    try:
        data = _make_request(
            _url_join(base_url, "/api/exams/request-approval"),
            method="POST",
            body=body,
            timeout=10,
        )
        return RequestApprovalResponse.from_json(data)
    except urllib.error.HTTPError as e:
        try:
            body_data = json.loads(e.read().decode("utf-8"))
            return RequestApprovalResponse.from_json(body_data)
        except Exception:
            return RequestApprovalResponse(success=False, message=f"HTTP {e.code}")
    except Exception as e:
        return RequestApprovalResponse(success=False, message=str(e))
