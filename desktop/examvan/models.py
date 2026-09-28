"""Data models for EXAMVAN API responses."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional

from .security_levels import (
    DEFAULT_LEVEL,
    display_level,
    enforces_no_free_exit,
    is_effective_strict,
    normalize_level,
)


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
    identity_fields: List[IdentityField] = field(default_factory=list)
    panel_color: str = "#6366f1"
    size_mb: float = 0.0
    start_time: Optional[str] = None
    end_time: Optional[str] = None
    questions: List[Dict[str, Any]] = field(default_factory=list)

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "Exam":
        fields_raw = data.get("identity_fields", [])
        fields = [
            IdentityField(
                key=f.get("key", ""),
                label=f.get("label", ""),
                required=bool(f.get("required", False)),
            )
            for f in fields_raw
        ]
        return cls(
            id=int(data.get("id", 0)),
            name=data.get("name", ""),
            status=data.get("status", ""),
            security_level=data.get("security_level", DEFAULT_LEVEL),
            strict_mode=bool(data.get("strict_mode", False)),
            identity_fields=fields,
            panel_color=data.get("panel_color", "#6366f1"),
            size_mb=float(data.get("size_mb", 0)),
            start_time=data.get("start_time"),
            end_time=data.get("end_time"),
            questions=data.get("questions") or [],
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
        exam = None
        if "exam" in data and data["exam"]:
            exam = Exam.from_json(data["exam"])
        elif "data" in data and data["data"]:
            exam_data = data["data"]
            if isinstance(exam_data, dict):
                exam = Exam.from_json(exam_data)
        return cls(
            success=bool(data.get("success")),
            exam=exam,
            error=data.get("error"),
            message=data.get("message"),
        )


@dataclass
class SubmitResponse:
    success: bool
    message: str = ""
    status: Optional[str] = None
    job_id: Optional[str] = None
    score: Optional[float] = None
    congrats_message: Optional[str] = None

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "SubmitResponse":
        score = data.get("score")
        if score is not None:
            score = float(score)
        return cls(
            success=bool(data.get("success")),
            message=data.get("message", ""),
            status=data.get("status"),
            job_id=data.get("job_id"),
            score=score,
            congrats_message=data.get("congrats_message"),
        )

@dataclass
class RequestApprovalResponse:
    success: bool
    status: str = "pending"
    message: str = ""

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "RequestApprovalResponse":
        return cls(
            success=bool(data.get("success", "status" in data)),
            status=data.get("status", "pending"),
            message=data.get("message", data.get("error", ""))
        )

