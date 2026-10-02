"""Data models for EXAMVAN API responses."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional

from .security_levels import (
    DEFAULT_LEVEL,
    display_level,
    enforces_no_free_exit,
    is_effective_strict,
    normalize_level,
)


def _as_dict(data: Any) -> Dict[str, Any]:
    """Kawat bisa membawa apa saja — proxy/WAF salah config, captive
    portal, atau server yang sedang rusak menjawab JSON valid tapi bukan
    object (`[]`, `123`, `null`, `"ok"`). Semua from_json mengasumsikan
    dict; tanpa normalisasi ini AttributeError/TypeError lolos dari setiap
    except di api.py (yang hanya menangkap URLError/OSError/JSONDecodeError)
    dan menjatuhkan dialog persis saat siswa membutuhkannya.

    Salah bentuk = "tidak ada jawaban": None masuk sebagai dict kosong,
    dan from_json memetakan default failure — bukan exception.
    """
    return data if isinstance(data, dict) else {}


@dataclass
class IdentityField:
    key: str
    label: str
    required: bool = False


@dataclass
class HealthResponse:
    success: bool
    status: str = ""
    version: str = ""
    certificate_fingerprint: Optional[str] = None
    server_time_utc: Optional[str] = None

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "HealthResponse":
        data = _as_dict(data)
        return cls(
            success=bool(data.get("success")),
            status=data.get("status", ""),
            version=data.get("version", ""),
            certificate_fingerprint=data.get("certificate_fingerprint"),
            server_time_utc=data.get("server_time_utc"),
        )


@dataclass
class Exam:
    id: int
    name: str
    status: str
    # Raw value exactly as the server sent it ("low" | "medium" | "high").
    # Do NOT branch on this — it is the server's vocabulary, not the
    # client's. Use the canonical `level` / `is_strict` properties below.
    security_level: str = DEFAULT_LEVEL
    strict_mode: bool = False
    # Apakah guru mempublikasikan hasil ke link /hasil/<token> (kolom
    # `public_results` di exams.go, default 1). False → halaman selamat
    # menyembunyikan tombol salin link. Default True = perilaku lama bila
    # kunci tidak ada di respons.
    public_results: bool = True
    identity_fields: List[IdentityField] = field(default_factory=list)
    panel_color: str = "#6366f1"
    size_mb: float = 0.0
    start_time: Optional[str] = None
    end_time: Optional[str] = None
    questions: List[Dict[str, Any]] = field(default_factory=list)

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "Exam":
        data = _as_dict(data)
        # identity_fields bisa datang berupa string/angka/dict dari server
        # yang rusak (atau di-edit di tengah jalan); hanya list yang
        # diiterasi, dan hanya item dict yang dipetakan — sisanya dibuang
        # alih-alih AttributeError 'str' object has no attribute 'get'.
        fields_raw = data.get("identity_fields")
        fields: List[IdentityField] = []
        if isinstance(fields_raw, list):
            for f in fields_raw:
                if not isinstance(f, dict):
                    continue
                fields.append(
                    IdentityField(
                        key=str(f.get("key") or ""),
                        label=str(f.get("label") or f.get("key") or ""),
                        required=bool(f.get("required", False)),
                    )
                )
        questions = data.get("questions")
        if not isinstance(questions, list):
            questions = []
        # id/size_mb: konversi angka yang salah bentuk = 0 (default),
        # bukan ValueError yang menjatuhkan dialog join.
        try:
            exam_id = int(data.get("id", 0))
        except (TypeError, ValueError):
            exam_id = 0
        try:
            size_mb = float(data.get("size_mb", 0))
        except (TypeError, ValueError):
            size_mb = 0.0
        raw_strict = data.get("strict_mode", False)
        # String "false"/"0"/""/"no"/"off" -> False; "1"/"true"/"yes"/"on"
        # -> True (strip + case-insensitive). Non-string ikut bool(v).
        if isinstance(raw_strict, str):
            _s = raw_strict.strip().lower()
            if _s in ("", "0", "false", "no", "off"):
                strict_mode = False
            elif _s in ("1", "true", "yes", "on"):
                strict_mode = True
            else:
                strict_mode = bool(raw_strict)
        else:
            strict_mode = bool(raw_strict)
        raw_color = data.get("panel_color", "#6366f1")
        panel_color = str(raw_color or "")
        if not re.fullmatch(r"#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?", panel_color):
            panel_color = "#6366f1"
        # Kunci PERSIS "public_results" seperti dikirim server (int 0/1).
        # Absen → True (perilaku lama). Bentuk string ("0"/"false"/...)
        # ditoleransi karena proxy/payload tangan bisa membawanya.
        raw_public = data.get("public_results", True)
        if isinstance(raw_public, str):
            public_results = raw_public.strip().lower() not in (
                "", "0", "false", "no", "off")
        else:
            public_results = bool(raw_public)
        return cls(
            id=exam_id,
            name=str(data.get("name") or ""),
            status=data.get("status", ""),
            security_level=data.get("security_level", DEFAULT_LEVEL),
            strict_mode=strict_mode,
            public_results=public_results,
            identity_fields=fields,
            panel_color=panel_color,
            size_mb=size_mb,
            start_time=data.get("start_time"),
            end_time=data.get("end_time"),
            questions=questions,
        )

    # ------------------------------------------------------------------
    # Security level — always read these, never `security_level`
    # ------------------------------------------------------------------

    @property
    def raw_security_level(self) -> str:
        """The level string the server actually sent, untranslated.

        Kept so a support log can show what the server said when a client's
        behaviour looks wrong. It is deliberately NOT what the security
        decisions branch on.
        """
        return self.security_level

    @property
    def level(self) -> str:
        """Canonical client tier: "low" | "medium" | "strict".

        The server's "high" maps to "strict" here. This is the only level
        string the desktop client should compare against.
        """
        return normalize_level(self.security_level)

    @property
    def is_strict(self) -> bool:
        """True when the exam runs with the strictest lockdown."""
        return is_effective_strict(self.security_level, self.strict_mode)

    @property
    def blocks_free_exit(self) -> bool:
        """True when closing the window must auto-submit, not let go.

        Medium and strict close the gate; only low lets a student leave
        without submitting, and even then only after a confirmation.
        """
        return enforces_no_free_exit(self.security_level, self.strict_mode)

    @property
    def display_level(self) -> str:
        """Canonical tier name for the security banner."""
        return display_level(self.security_level, self.strict_mode)


@dataclass
class TokenExamResponse:
    success: bool
    exam: Optional[Exam] = None
    error: Optional[str] = None
    message: Optional[str] = None

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "TokenExamResponse":
        data = _as_dict(data)
        exam = None
        # Nested "exam"/"data" yang bukan dict (list dari server rusak)
        # tidak boleh dipaksa jadi Exam.
        for key in ("exam", "data"):
            candidate = data.get(key)
            if isinstance(candidate, dict):
                exam = Exam.from_json(candidate)
                break
        return cls(
            success=bool(data.get("success")),
            exam=exam,
            error=data.get("error"),
            message=data.get("message"),        )


@dataclass
class SubmitResponse:
    success: bool
    message: str = ""
    status: Optional[str] = None
    job_id: Optional[str] = None
    score: Optional[float] = None
    congrats_message: Optional[str] = None
    # Kode kesalahan klien (bukan server) saat respons tidak bisa dipercaya
    # sebagai hasil submit — mis. "non_json_response" (halaman blok proxy).
    # Dipakai `retryable` untuk memutuskan apakah mengulang masih ada harapan.
    error_code: Optional[str] = None
    # HTTP status when the request actually completed. None means the request
    # never got a response (network down, DNS, timeout), which is retryable.
    http_status: Optional[int] = None

    @property
    def retryable(self) -> bool:
        """True when sending the exact same payload again could still work.

        Retryable: no HTTP response at all (network down, DNS, timeout),
        408 Request Timeout, 429 Too Many Requests, and any 5xx.

        Not retryable: everything the server actually answered. A 4xx is it
        saying "this request is wrong" — a missing or stale token, an unknown
        exam, a duplicate — and repeating it changes nothing. A completed 2xx
        or 3xx that still reports failure is a server-side verdict, not a
        hiccup.

        submit_with_retry used to retry unconditionally, which turned a 401
        into seven seconds of "percobaan 2/4... 3/4... 4/4..." followed by
        the same failure.

        Audit 2 Okt 2026: respons 200 yang ISINYA bukan JSON (halaman blok
        proxy/WAF, captive portal) dulu dianggap "tidak ada HTTP sama
        sekali" → retryable=True → empat percobaan penuh terhadap halaman
        yang sama. Yang menjawab bukan server EXAMVAN; mengulang tidak
        mengubah halamannya, hanya memakan waktu siswa di depan layar
        "percobaan 2/4...". Sekarang error_code="non_json_response" adalah
        verdict permanen juga.
        """
        if self.error_code == "non_json_response":
            return False
        if self.http_status is None:
            return True
        if self.http_status in (408, 429):
            return True
        return self.http_status >= 500

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "SubmitResponse":
        data = _as_dict(data)
        # "score": "abc" dulu melempar ValueError di tengah jalur submit;
        # nilai yang tidak bisa dikonversi = tidak ada skor (None).
        score = data.get("score")
        if score is not None:
            try:
                score = float(score)
            except (TypeError, ValueError):
                score = None
        return cls(
            success=bool(data.get("success")),
            message=data.get("message", ""),
            status=data.get("status"),
            job_id=data.get("job_id"),
            score=score,
            congrats_message=data.get("congrats_message"),
            error_code=data.get("error_code"),
        )

@dataclass
class RequestApprovalResponse:
    success: bool
    status: str = "pending"
    message: str = ""
    # Kode HTTP respons yang benar-benar datang. None = tidak ada HTTP
    # (jaringan/DNS/timeout).
    #
    # Audit 2 Okt 2026 (HIGH): kode ini dulu DIBUANG. HTTPError 401/403/404
    # yang body-nya gagal di-parse jatuh ke default `status="pending"`, dan
    # WaitingApprovalDialog memperlakukan "pending" sebagai "tunggu lagi"
    # — polling tiap 5 detik SELAMANYA untuk penolakan yang tidak akan
    # pernah berubah. Dengan kode ini dibawa naik, dialog bisa membedakan
    # "masih menunggu" dari "ditolak permanen" dan berhenti.
    http_status: Optional[int] = None

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "RequestApprovalResponse":
        data = _as_dict(data)
        return cls(
            success=bool(data.get("success", "status" in data)),
            status=data.get("status", "pending"),
            message=data.get("message", data.get("error", "")),
        )

