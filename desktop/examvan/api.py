"""HTTP client for EXAMVAN API (urllib.request, no external deps)."""

from __future__ import annotations

import io
import json
import sys
import time
import urllib.error
import urllib.request
from typing import Any, Callable, Dict, Optional, Tuple

from . import APP_VERSION
from .models import Exam, HealthResponse, SubmitResponse, TokenExamResponse
from .utils import map_identity_to_standard

# Platform label for User-Agent
_PLATFORM = "Windows" if sys.platform == "win32" else "Linux"


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
        raw = resp.read().decode("utf-8").strip()
        # Try JSON first; fall back to wrapping plain text
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {"success": True, "status": raw, "message": raw}


def _url_join(base: str, path: str) -> str:
    """Join base URL and path, handling trailing/leading slashes."""
    return base.rstrip("/") + "/" + path.lstrip("/")


# ---------------------------------------------------------------------------
# Health check
# ---------------------------------------------------------------------------


def check_health(base_url: str) -> HealthResponse:
    """GET /api/health → HealthResponse."""
    try:
        data = _make_request(_url_join(base_url, "/api/health"), timeout=10)
        return HealthResponse.from_json(data)
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


def download_pdf(
    base_url: str,
    exam_id: int,
    token: str,
    dest_path: str,
    progress_cb: Optional[Callable[[int, int], None]] = None,
) -> str:
    """GET /api/exams/{exam_id}/pdf → save to dest_path. Returns path on success.

    Args:
        progress_cb: Called with (bytes_read, total_bytes) during download.
                     total_bytes is -1 if Content-Length unknown.
    """
    url = _url_join(base_url, f"/api/exams/{exam_id}/pdf")
    req = urllib.request.Request(
        url,
        headers={
            "X-Exam-Token": token,
            "User-Agent": f"EXAMVAN-{_PLATFORM}/{APP_VERSION}",
        },
    )

    tmp_path = dest_path + ".tmp"
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
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
) -> SubmitResponse:
    """POST /api/exams/{exam_id}/submit → SubmitResponse."""
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
            headers={"X-App-Version": APP_VERSION},
            body=body,
            timeout=30,
        )
        return SubmitResponse.from_json(data)
    except urllib.error.HTTPError as e:
        try:
            body_data = json.loads(e.read().decode("utf-8"))
            return SubmitResponse.from_json(body_data)
        except Exception:
            return SubmitResponse(success=False, message=f"HTTP {e.code}: {e.reason}")
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
) -> SubmitResponse:
    """Submit with exponential backoff retry (1s, 2s, 4s)."""
    if delays is None:
        delays = [1, 2, 4]

    resp = submit_exam(
        base_url, exam_id, student_name, exam_number, student_class,
        answers, start_time, mac_address, identity_data,
    )
    if resp.success:
        return resp

    max_attempts = len(delays) + 1
    for i, delay in enumerate(delays):
        if on_retry:
            on_retry(i + 2, max_attempts)
        time.sleep(delay)
        resp = submit_exam(
            base_url, exam_id, student_name, exam_number, student_class,
            answers, start_time, mac_address, identity_data,
        )
        if resp.success:
            return resp

    return resp

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
):
    """POST /api/exams/request-approval."""
    from .models import RequestApprovalResponse
    body = {
        "exam_id": exam_id,
        "mac_address": mac_address,
        "student_name": student_name,
        "exam_number": exam_number,
        "student_class": student_class,
        "identity_data": identity_data,
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
