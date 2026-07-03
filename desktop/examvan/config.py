"""Persistent configuration stored in ~/.config/examvan/config.json.

Answers saved to disk are obfuscated (XOR) to prevent casual tampering.
"""

from __future__ import annotations

import base64
import json
import os
from pathlib import Path
from typing import Any, Dict, Optional


_CONFIG_DIR = Path.home() / ".config" / "examvan"
_CONFIG_FILE = _CONFIG_DIR / "config.json"

_defaults: Dict[str, Any] = {
    "server_url": "",
    "exam_token": "",
    "remember_url": True,
    "identity_data": {},
}

_cache: Optional[Dict[str, Any]] = None

# Simple XOR obfuscation key for answer files — prevents casual reading.
# Not cryptographic security (answers stay on disk only during exam).
_OBFUSCATE_KEY = b"EXAMVAN_OBF_2024!!"


def _xor_obfuscate(data: bytes) -> bytes:
    """XOR obfuscation. Same function for encrypt and decrypt."""
    token = get("exam_token") or ""
    if token:
        mixed_key = bytes(b ^ ord(token[i % len(token)]) for i, b in enumerate(_OBFUSCATE_KEY))
    else:
        mixed_key = _OBFUSCATE_KEY
    return bytes(b ^ mixed_key[i % len(mixed_key)] for i, b in enumerate(data))


def _encode_answers(answers: Dict[str, Any]) -> str:
    """Serialize + obfuscate + base64 encode."""
    raw = json.dumps(answers, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    obfuscated = _xor_obfuscate(raw)
    return base64.urlsafe_b64encode(obfuscated).decode("ascii")


def _decode_answers(data: str) -> Optional[Dict[str, Any]]:
    """Base64 decode + deobfuscate + parse."""
    try:
        obfuscated = base64.urlsafe_b64decode(data.encode("ascii"))
        raw = _xor_obfuscate(obfuscated)
        return json.loads(raw.decode("utf-8"))
    except Exception:
        return None


def _load() -> Dict[str, Any]:
    global _cache
    if _cache is not None:
        return _cache
    if _CONFIG_FILE.exists():
        try:
            with open(_CONFIG_FILE, "r", encoding="utf-8") as f:
                _cache = {**_defaults, **json.load(f)}
        except (json.JSONDecodeError, OSError):
            _cache = dict(_defaults)
    else:
        _cache = dict(_defaults)
    return _cache


def _save() -> None:
    if _cache is None:
        return
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = _CONFIG_FILE.with_suffix(".tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(_cache, f, indent=2, ensure_ascii=False)
    tmp.replace(_CONFIG_FILE)


def get(key: str, default: Any = None) -> Any:
    return _load().get(key, default)


def set(key: str, value: Any) -> None:
    _load()[key] = value
    _save()


def get_all() -> Dict[str, Any]:
    return dict(_load())


def save_answers(exam_id: int, answers: Dict[str, Any]) -> None:
    """Save answers to disk for crash recovery (obfuscated)."""
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    try:
        encoded = _encode_answers(answers)
        with open(tmp, "w", encoding="ascii") as f:
            f.write(encoded)
        tmp.replace(path)
    except Exception:
        # If obfuscation fails, don't write anything readable
        if tmp.exists():
            tmp.unlink()


def load_answers(exam_id: int) -> Optional[Dict[str, Any]]:
    """Load saved answers for crash recovery."""
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    if not path.exists():
        # Try legacy .json path (migration from unencrypted format)
        legacy = path.with_suffix(".json")
        if legacy.exists():
            try:
                with open(legacy, "r", encoding="utf-8") as f:
                    data = json.load(f)
                # Migrate to new format
                save_answers(exam_id, data)
                legacy.unlink()
                return data
            except (json.JSONDecodeError, OSError):
                return None
        return None
    try:
        with open(path, "r", encoding="ascii") as f:
            return _decode_answers(f.read())
    except Exception:
        return None


def clear_answers(exam_id: int) -> None:
    """Remove saved answers after successful submit."""
    # Remove both legacy .json and new .dat
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    if path.exists():
        path.unlink()
    legacy = path.with_suffix(".json")
    if legacy.exists():
        legacy.unlink()
