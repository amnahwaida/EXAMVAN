"""Data models for EXAMVAN API responses."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


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

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> "HealthResponse":
        return cls(
            success=bool(data.get("success")),
            status=data.get("status", ""),
            version=data.get("version", ""),
            certificate_fingerprint=data.get("certificate_fingerprint"),
        )


@dataclass
class Exam:
    id: int
    name: str
    status: str
    security_level: str = "low"
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
            security_level=data.get("security_level", "low"),
            strict_mode=bool(data.get("strict_mode", False)),
            identity_fields=fields,
            panel_color=data.get("panel_color", "#6366f1"),
            size_mb=float(data.get("size_mb", 0)),
            start_time=data.get("start_time"),
            end_time=data.get("end_time"),
            questions=data.get("questions") or [],
        )

    @property
    def is_strict(self) -> bool:
        return self.strict_mode or self.security_level == "strict"


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
        )
