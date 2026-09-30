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


def resolve_submit_answers(memory_answers: Dict[str, Any], exam_id: int) -> Dict[str, Any]:
    """Jawaban efektif untuk submit: utamakan memori, fallback ke disk.

    Mirror fix Android F1: deadline bisa menembak SEBELUM jawaban dipulihkan
    dari disk (re-entry setelah proses mati; timer fire saat konstruktor,
    restore dijadwalkan 500ms kemudian). Tanpa fallback, submit kosong dalam
    window grace server (end_time + 60 dtk) MENIMPA jawaban asli dan
    clear_answers menghapusnya. Memori tidak kosong → dipakai apa adanya
    (siswa mungkin baru mengubah jawaban setelah auto-save terakhir).
    """
    if memory_answers:
        return memory_answers
    saved = load_answers(exam_id)
    return saved if saved else {}


def save_start_time(exam_id: int, iso: str) -> None:
    """Persist start_time ujian untuk recovery re-entry (mirror Android
    AppPrefs.KEY_EXAM_START_TIME).

    Dipakai layar recovery (ServerConfigDialog) saat mengirim ulang jawaban
    dari disk — server menghitung durasi dari start_time, jadi harus sama
    dengan nilai yang dipakai submit asli.
    """
    set(f"start_time_{exam_id}", iso)


def load_start_time(exam_id: int) -> str:
    """Return the persisted start_time for [exam_id] ("" bila belum ada)."""
    return str(get(f"start_time_{exam_id}", "") or "")


def _submitted_key(exam_id: int, token: str) -> str:
    """Kunci marker "sudah dikumpulkan", di-scope per token.

    Dulu kuncinya hanya `submitted_<exam_id>`: per MESIN, per ujian, selamanya.
    Di lab sekolah satu PC dipakai beberapa siswa, jadi begitu siswa pertama
    selesai, PC itu memblokir ujian yang sama untuk semua siswa berikutnya --
    persis keluhan "tidak bisa mengerjakan ujian yang sama untuk kedua
    kalinya". Dan blokirannya terjadi SEBELUM dialog identitas, sehingga
    aplikasi tidak pernah tahu itu siswa yang berbeda.

    Token adalah satu-satunya identitas percobaan yang sudah tersedia di
    titik gate ini (lihat ServerConfigDialog._on_connect: token dibaca
    sebelum dialog identitas tampil). Menyimpanya ke sini memperbaiki lab
    tanpa mengorbankan overwrite protection yang jadi alasan marker ini ada.

    Token di-hash supaya kunci tetap pendek dan tidak pernah masuk ke
    config.json apa adanya -- file itu tersimpan di disk dan ikut terkirim
    bersama jawaban.
    """
    import hashlib

    digest = hashlib.sha256((token or "").strip().encode()).hexdigest()[:16]
    return f"submitted_{exam_id}_{digest}"


def mark_submitted(exam_id: int, token: str = "") -> None:
    """Persist a sticky "exam already finished" marker for this attempt.

    Mirrors Android's submittedOrExited flag (F2 fix): setelah submit SUKES
    (durable), re-entry dengan token yang SAMA harus menampilkan "sudah
    selesai", BUKAN menjalankan ulang alur ujian. Tanpa marker ini, re-entry
    dalam window grace server (end_time + 60 dtk) → watchdog deadline →
    submit kosong (jawaban disk sudah dihapus oleh clear_answers) →
    MENIMPA jawaban asli yang sudah terkirim. Marker dibiarkan sticky
    (tidak dihapus oleh clear_answers) — selesai = selesai.

    Di-scope per token: token yang sama berarti percobaan yang sama, jadi
    overwrite protection tetap bekerja persis seperti sebelumnya. Token
    berbeda berarti percobaan baru, dan PC yang sama boleh dipakai lagi.

    Marker versi lama (`submitted_<exam_id>`, tanpa token) sengaja
    DIABAIKAN: itu perilaku yang memblokir lab, dan membacanya kembali akan
    membuat PC yang sudah pernah terkirim tetap terkunci selamanya.
    """
    set(_submitted_key(exam_id, token), True)


def is_submitted(exam_id: int, token: str = "") -> bool:
    """True bila percobaan ini (exam + token) sudah mengumpulkan jawaban."""
    return bool(get(_submitted_key(exam_id, token), False))


def clear_identity() -> None:
    """Hapus identitas siswa yang tersimpan.

    WAJIB dipanggil saat jendela ujian ditutup, di samping
    `ServerConfigDialog.input_token.clear()`.

    Kenapa: `identity_data` dibaca lagi di
    `ServerConfigDialog._show_identity_dialog` (`:356`) lalu dipakai untuk
    MENGISI form `IdentityDialog`. Kalau tidak dihapus, siswa berikutnya
    mendapat form yang sudah terisi nama siswa sebelumnya, dan karena
    `last_input.returnPressed` terikat ke submit, dia bisa menekan Enter
    tanpa membaca apa pun — jawabannya lalu tercatat atas nama orang lain.
    Tidak ada dialog, tidak ada warning, tidak ada log.

    Dulu fungsi ini tidak ada sama sekali: yang dibersihkan hanya token,
    padahal yang paling sensitif justru identitas. Lihat
    review_windows_2026-09-30.md Bagian 1.
    """
    set("identity_data", {})

